// Package flux provides FLUX image generation service using mflux on Apple Silicon.
package flux

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/base-go/basepod/internal/config"
	"github.com/google/uuid"
)

// Service manages the FLUX image generation service
type Service struct {
	baseDir    string
	modelsDir  string
	outputDir  string
	db         *sql.DB
	generating bool
	currentJob *GenerationJob
	mu         sync.Mutex
}

// Model represents a FLUX model
type Model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        string `json:"size"`
	Downloaded  bool   `json:"downloaded"`
	Steps       int    `json:"default_steps"` // Default steps for this model
}

// GenerationJob represents an image generation job
type GenerationJob struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Steps     int       `json:"steps"`
	Seed      int64     `json:"seed"`
	Status    string    `json:"status"` // pending, generating, completed, failed
	Progress  int       `json:"progress"`
	ImagePath string    `json:"image_path,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Status represents the service status
type Status struct {
	Supported   bool           `json:"supported"`
	Reason      string         `json:"unsupported_reason,omitempty"`
	Generating  bool           `json:"generating"`
	CurrentJob  *GenerationJob `json:"current_job,omitempty"`
	ModelsCount int            `json:"models_count"`
}

// DownloadProgress tracks model download progress
type DownloadProgress struct {
	ModelID   string  `json:"model_id"`
	Status    string  `json:"status"` // pending, downloading, completed, failed
	Progress  float64 `json:"progress"`
	Message   string  `json:"message"`
	cancel    context.CancelFunc
	mu        sync.RWMutex
}

var (
	instance        *Service
	once            sync.Once
	activeDownloads = make(map[string]*DownloadProgress)
	downloadsMu     sync.RWMutex
)

// GetService returns the singleton FLUX service instance
func GetService(db *sql.DB) *Service {
	once.Do(func() {
		paths, err := config.GetPaths()
		if err != nil {
			paths = &config.Paths{Data: "/tmp"}
		}
		baseDir := filepath.Join(paths.Data, "flux")
		os.MkdirAll(baseDir, 0755)

		modelsDir := filepath.Join(baseDir, "models")
		os.MkdirAll(modelsDir, 0755)

		outputDir := filepath.Join(baseDir, "outputs")
		os.MkdirAll(outputDir, 0755)

		instance = &Service{
			baseDir:   baseDir,
			modelsDir: modelsDir,
			outputDir: outputDir,
			db:        db,
		}
	})
	return instance
}

// IsSupported checks if FLUX/mflux is supported on this platform
func IsSupported() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

// GetUnsupportedReason returns why FLUX is not supported
func GetUnsupportedReason() string {
	if runtime.GOOS != "darwin" {
		return "FLUX/mflux requires macOS. Current OS: " + runtime.GOOS
	}
	if runtime.GOARCH != "arm64" {
		return "FLUX/mflux requires Apple Silicon (M series). Current architecture: " + runtime.GOARCH
	}
	return ""
}

// GetStatus returns the current service status
func (s *Service) GetStatus() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	models := s.ListModels()
	downloadedCount := 0
	for _, m := range models {
		if m.Downloaded {
			downloadedCount++
		}
	}

	return Status{
		Supported:   IsSupported(),
		Reason:      GetUnsupportedReason(),
		Generating:  s.generating,
		CurrentJob:  s.currentJob,
		ModelsCount: downloadedCount,
	}
}

// GetAvailableModels returns the list of available FLUX models
func GetAvailableModels() []Model {
	return []Model{
		{
			ID:          "schnell",
			Name:        "FLUX.1 Schnell",
			Description: "Fast generation, 4 steps, good quality",
			Size:        "~15GB",
			Steps:       4,
		},
		{
			ID:          "dev",
			Name:        "FLUX.1 Dev",
			Description: "High quality, 20+ steps, best results",
			Size:        "~32GB",
			Steps:       20,
		},
	}
}

// ListModels returns all models with download status
func (s *Service) ListModels() []Model {
	available := GetAvailableModels()

	for i := range available {
		modelPath := filepath.Join(s.modelsDir, available[i].ID)
		if info, err := os.Stat(modelPath); err == nil && info.IsDir() {
			// Check if model has actual files
			entries, _ := os.ReadDir(modelPath)
			if len(entries) > 0 {
				available[i].Downloaded = true
			}
		}
	}

	return available
}

// DownloadProgressData is a safe copy of download progress data
type DownloadProgressData struct {
	ModelID  string  `json:"model_id"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
}

// GetDownloadProgress returns the current download progress as a safe copy
func GetDownloadProgress(modelID string) *DownloadProgressData {
	downloadsMu.RLock()
	dp := activeDownloads[modelID]
	downloadsMu.RUnlock()

	if dp == nil {
		return nil
	}

	dp.mu.RLock()
	defer dp.mu.RUnlock()

	return &DownloadProgressData{
		ModelID:  dp.ModelID,
		Status:   dp.Status,
		Progress: dp.Progress,
		Message:  dp.Message,
	}
}

// DownloadModel starts downloading a model
func (s *Service) DownloadModel(modelID string) (*DownloadProgress, error) {
	// Check if model exists
	valid := false
	for _, m := range GetAvailableModels() {
		if m.ID == modelID {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("unknown model: %s", modelID)
	}

	// Check if already downloading
	downloadsMu.Lock()
	if existing, ok := activeDownloads[modelID]; ok {
		existing.mu.RLock()
		status := existing.Status
		existing.mu.RUnlock()
		if status == "downloading" || status == "pending" {
			downloadsMu.Unlock()
			return existing, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	dp := &DownloadProgress{
		ModelID: modelID,
		Status:  "pending",
		Message: "Starting download...",
		cancel:  cancel,
	}
	activeDownloads[modelID] = dp
	downloadsMu.Unlock()

	go s.runDownload(ctx, dp)

	return dp, nil
}

// runDownload performs the actual model download
func (s *Service) runDownload(ctx context.Context, dp *DownloadProgress) {
	dp.mu.Lock()
	dp.Status = "downloading"
	dp.Message = "Setting up Python environment..."
	dp.mu.Unlock()

	// Ensure venv exists with mflux
	venvPath := filepath.Join(s.baseDir, "venv")
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		dp.mu.Lock()
		dp.Message = "Creating Python environment..."
		dp.mu.Unlock()

		cmd := exec.CommandContext(ctx, "python3", "-m", "venv", venvPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			dp.mu.Lock()
			dp.Status = "failed"
			dp.Message = fmt.Sprintf("Failed to create venv: %s", string(output))
			dp.mu.Unlock()
			return
		}
	}

	// Install mflux
	dp.mu.Lock()
	dp.Message = "Installing mflux..."
	dp.mu.Unlock()

	pipPath := filepath.Join(venvPath, "bin", "pip")
	cmd := exec.CommandContext(ctx, pipPath, "install", "--upgrade", "mflux")
	if output, err := cmd.CombinedOutput(); err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Failed to install mflux: %s", string(output))
		dp.mu.Unlock()
		return
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		dp.mu.Lock()
		dp.Status = "cancelled"
		dp.Message = "Download cancelled"
		dp.mu.Unlock()
		return
	default:
	}

	// Download model using mflux-save
	dp.mu.Lock()
	dp.Message = fmt.Sprintf("Downloading %s model (this may take a while)...", dp.ModelID)
	dp.Progress = 10
	dp.mu.Unlock()

	modelPath := filepath.Join(s.modelsDir, dp.ModelID)
	mfluxSavePath := filepath.Join(venvPath, "bin", "mflux-save")

	cmd = exec.CommandContext(ctx, mfluxSavePath, "--model", dp.ModelID, "--path", modelPath)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Failed to start download: %v", err)
		dp.mu.Unlock()
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Failed to start mflux-save: %v", err)
		dp.mu.Unlock()
		return
	}

	// Parse output for progress
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		dp.mu.Lock()
		// Update progress based on output
		if strings.Contains(line, "Downloading") {
			dp.Progress = 30
			dp.Message = "Downloading model files..."
		} else if strings.Contains(line, "Loading") {
			dp.Progress = 70
			dp.Message = "Loading model..."
		} else if strings.Contains(line, "Saved") || strings.Contains(line, "saved") {
			dp.Progress = 90
			dp.Message = "Finalizing..."
		}
		dp.mu.Unlock()
	}

	if err := cmd.Wait(); err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Download failed: %v", err)
		dp.mu.Unlock()
		return
	}

	dp.mu.Lock()
	dp.Status = "completed"
	dp.Progress = 100
	dp.Message = "Model downloaded successfully!"
	dp.mu.Unlock()

	// Clean up after delay
	go func() {
		time.Sleep(5 * time.Minute)
		downloadsMu.Lock()
		delete(activeDownloads, dp.ModelID)
		downloadsMu.Unlock()
	}()
}

// DeleteModel removes a downloaded model
func (s *Service) DeleteModel(modelID string) error {
	modelPath := filepath.Join(s.modelsDir, modelID)
	if err := os.RemoveAll(modelPath); err != nil {
		return fmt.Errorf("failed to delete model: %w", err)
	}
	return nil
}

// Generate starts an image generation job
func (s *Service) Generate(prompt, modelID string, width, height, steps int, seed int64) (*GenerationJob, error) {
	s.mu.Lock()
	if s.generating {
		s.mu.Unlock()
		return nil, fmt.Errorf("generation already in progress")
	}

	// Check if model is downloaded
	modelPath := filepath.Join(s.modelsDir, modelID)
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		s.mu.Unlock()
		return nil, fmt.Errorf("model not downloaded: %s", modelID)
	}

	// Create job
	job := &GenerationJob{
		ID:        "gen_" + uuid.New().String()[:8],
		Prompt:    prompt,
		Model:     modelID,
		Width:     width,
		Height:    height,
		Steps:     steps,
		Seed:      seed,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
	}

	// Save to database
	if s.db != nil {
		_, err := s.db.Exec(`
			INSERT INTO flux_generations (id, prompt, model, width, height, steps, seed, status, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, job.ID, job.Prompt, job.Model, job.Width, job.Height, job.Steps, job.Seed, job.Status, job.CreatedAt)
		if err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("failed to save job: %w", err)
		}
	}

	s.generating = true
	s.currentJob = job
	s.mu.Unlock()

	// Start generation in background
	go s.runGeneration(job)

	return job, nil
}

// runGeneration performs the actual image generation
func (s *Service) runGeneration(job *GenerationJob) {
	defer func() {
		s.mu.Lock()
		s.generating = false
		s.currentJob = nil
		s.mu.Unlock()
	}()

	s.mu.Lock()
	job.Status = "generating"
	job.Progress = 10
	s.mu.Unlock()
	s.updateJobInDB(job)

	venvPath := filepath.Join(s.baseDir, "venv")
	mfluxGenPath := filepath.Join(venvPath, "bin", "mflux-generate")
	modelPath := filepath.Join(s.modelsDir, job.Model)
	outputPath := filepath.Join(s.outputDir, job.ID+".png")

	// Build command args
	args := []string{
		"--model", job.Model,
		"--path", modelPath,
		"--prompt", job.Prompt,
		"--width", strconv.Itoa(job.Width),
		"--height", strconv.Itoa(job.Height),
		"--steps", strconv.Itoa(job.Steps),
		"--output", outputPath,
	}

	if job.Seed >= 0 {
		args = append(args, "--seed", strconv.FormatInt(job.Seed, 10))
	}

	cmd := exec.Command(mfluxGenPath, args...)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Error = fmt.Sprintf("Failed to start generation: %v", err)
		s.mu.Unlock()
		s.updateJobInDB(job)
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Error = fmt.Sprintf("Failed to start mflux-generate: %v", err)
		s.mu.Unlock()
		s.updateJobInDB(job)
		return
	}

	// Parse output for step progress
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// Try to parse step progress (e.g., "Step 2/4")
		if strings.Contains(line, "Step") || strings.Contains(line, "step") {
			// Extract step numbers
			var current, total int
			if _, err := fmt.Sscanf(line, "Step %d/%d", &current, &total); err == nil {
				s.mu.Lock()
				job.Progress = 10 + int(float64(current)/float64(total)*80)
				s.mu.Unlock()
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Error = fmt.Sprintf("Generation failed: %v", err)
		s.mu.Unlock()
		s.updateJobInDB(job)
		return
	}

	// Verify output exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		s.mu.Lock()
		job.Status = "failed"
		job.Error = "Generation completed but output file not found"
		s.mu.Unlock()
		s.updateJobInDB(job)
		return
	}

	s.mu.Lock()
	job.Status = "completed"
	job.Progress = 100
	job.ImagePath = outputPath
	s.mu.Unlock()
	s.updateJobInDB(job)
}

// updateJobInDB updates job status in database
func (s *Service) updateJobInDB(job *GenerationJob) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec(`
		UPDATE flux_generations SET status = ?, progress = ?, image_path = ?, error = ?
		WHERE id = ?
	`, job.Status, job.Progress, job.ImagePath, job.Error, job.ID)
}

// GetJob returns a generation job by ID
func (s *Service) GetJob(jobID string) (*GenerationJob, error) {
	// Check current job first
	s.mu.Lock()
	if s.currentJob != nil && s.currentJob.ID == jobID {
		job := *s.currentJob // Copy
		s.mu.Unlock()
		return &job, nil
	}
	s.mu.Unlock()

	// Query database
	if s.db == nil {
		return nil, fmt.Errorf("job not found")
	}

	var job GenerationJob
	var imagePath, errMsg sql.NullString
	err := s.db.QueryRow(`
		SELECT id, prompt, model, width, height, steps, seed, status, image_path, error, created_at
		FROM flux_generations WHERE id = ?
	`, jobID).Scan(&job.ID, &job.Prompt, &job.Model, &job.Width, &job.Height,
		&job.Steps, &job.Seed, &job.Status, &imagePath, &errMsg, &job.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	job.ImagePath = imagePath.String
	job.Error = errMsg.String

	return &job, nil
}

// ListGenerations returns all generation jobs
func (s *Service) ListGenerations() ([]GenerationJob, error) {
	if s.db == nil {
		return []GenerationJob{}, nil
	}

	rows, err := s.db.Query(`
		SELECT id, prompt, model, width, height, steps, seed, status, image_path, error, created_at
		FROM flux_generations ORDER BY created_at DESC LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list generations: %w", err)
	}
	defer rows.Close()

	var jobs []GenerationJob
	for rows.Next() {
		var job GenerationJob
		var imagePath, errMsg sql.NullString
		err := rows.Scan(&job.ID, &job.Prompt, &job.Model, &job.Width, &job.Height,
			&job.Steps, &job.Seed, &job.Status, &imagePath, &errMsg, &job.CreatedAt)
		if err != nil {
			continue
		}
		job.ImagePath = imagePath.String
		job.Error = errMsg.String
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetImagePath returns the path to a generated image
func (s *Service) GetImagePath(jobID string) (string, error) {
	job, err := s.GetJob(jobID)
	if err != nil {
		return "", err
	}
	if job.Status != "completed" || job.ImagePath == "" {
		return "", fmt.Errorf("image not available")
	}
	return job.ImagePath, nil
}

// DeleteGeneration removes a generation and its image
func (s *Service) DeleteGeneration(jobID string) error {
	job, err := s.GetJob(jobID)
	if err != nil {
		return err
	}

	// Delete image file if exists
	if job.ImagePath != "" {
		os.Remove(job.ImagePath)
	}

	// Delete from database
	if s.db != nil {
		_, err = s.db.Exec("DELETE FROM flux_generations WHERE id = ?", jobID)
		if err != nil {
			return fmt.Errorf("failed to delete generation: %w", err)
		}
	}

	return nil
}

// Generation represents a saved generation (for JSON responses)
type Generation struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Steps     int       `json:"steps"`
	Seed      int64     `json:"seed"`
	Status    string    `json:"status"`
	ImageURL  string    `json:"image_url,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ToGeneration converts a job to a Generation response
func (j *GenerationJob) ToGeneration() Generation {
	g := Generation{
		ID:        j.ID,
		Prompt:    j.Prompt,
		Model:     j.Model,
		Width:     j.Width,
		Height:    j.Height,
		Steps:     j.Steps,
		Seed:      j.Seed,
		Status:    j.Status,
		Error:     j.Error,
		CreatedAt: j.CreatedAt,
	}
	if j.Status == "completed" && j.ImagePath != "" {
		g.ImageURL = "/api/flux/image/" + j.ID
	}
	return g
}

// MarshalJSON implements custom JSON marshaling
func (j *GenerationJob) MarshalJSON() ([]byte, error) {
	return json.Marshal(j.ToGeneration())
}
