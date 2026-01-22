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
	ModelID    string  `json:"model_id"`
	Status     string  `json:"status"` // pending, downloading, completed, failed
	Progress   float64 `json:"progress"`
	Message    string  `json:"message"`
	BytesDone  int64   `json:"bytes_done"`
	BytesTotal int64   `json:"bytes_total"`
	Speed      int64   `json:"speed"` // bytes per second
	ETA        int     `json:"eta"`   // seconds remaining
	cancel     context.CancelFunc
	mu         sync.RWMutex
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

// findPython3 finds a suitable Python 3.10+ interpreter
// mflux requires Python 3.10+ for modern type hint syntax
func findPython3() (string, error) {
	// Try specific versions first (prefer newer)
	pythonPaths := []string{
		"/opt/homebrew/bin/python3.13",
		"/opt/homebrew/bin/python3.12",
		"/opt/homebrew/bin/python3.11",
		"/opt/homebrew/bin/python3.10",
		"/usr/local/bin/python3.13",
		"/usr/local/bin/python3.12",
		"/usr/local/bin/python3.11",
		"/usr/local/bin/python3.10",
		"python3.13",
		"python3.12",
		"python3.11",
		"python3.10",
	}

	for _, p := range pythonPaths {
		if path, err := exec.LookPath(p); err == nil {
			// Verify it's actually 3.10+
			cmd := exec.Command(path, "--version")
			output, err := cmd.Output()
			if err == nil {
				version := strings.TrimSpace(string(output))
				// Parse version like "Python 3.12.1"
				if strings.HasPrefix(version, "Python 3.1") || strings.HasPrefix(version, "Python 3.2") {
					return path, nil
				}
			}
		}
	}

	// Fall back to python3 and hope for the best
	if path, err := exec.LookPath("python3"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("Python 3.10+ not found. Please install: brew install python@3.12")
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
			Description: "Fast generation, 4 steps (requires HF token + license)",
			Size:        "~15GB",
			Steps:       4,
		},
		{
			ID:          "dev",
			Name:        "FLUX.1 Dev",
			Description: "High quality, 20+ steps (requires HF token + license)",
			Size:        "~32GB",
			Steps:       20,
		},
		{
			ID:          "flux2-klein-4b",
			Name:        "FLUX.2 Klein 4B",
			Description: "Compact model, fast generation, 4 steps",
			Size:        "~8GB",
			Steps:       4,
		},
		{
			ID:          "flux2-klein-9b",
			Name:        "FLUX.2 Klein 9B",
			Description: "Larger model, better quality, 4 steps",
			Size:        "~18GB",
			Steps:       4,
		},
	}
}

// ListModels returns all models with download status
func (s *Service) ListModels() []Model {
	available := GetAvailableModels()

	for i := range available {
		modelID := available[i].ID
		isFlux2 := strings.HasPrefix(modelID, "flux2-")

		if isFlux2 {
			// FLUX.2 models are stored in HuggingFace cache
			// Check ~/.cache/huggingface/hub/models--black-forest-labs--FLUX.2-*
			home, _ := os.UserHomeDir()
			var hfModelName string
			switch modelID {
			case "flux2-klein-4b":
				hfModelName = "FLUX.2-klein-4B"
			case "flux2-klein-9b":
				hfModelName = "FLUX.2-klein-9B"
			}
			hfCachePath := filepath.Join(home, ".cache", "huggingface", "hub", "models--black-forest-labs--"+hfModelName)
			if info, err := os.Stat(hfCachePath); err == nil && info.IsDir() {
				// Check if it has blobs (actual model files)
				blobsPath := filepath.Join(hfCachePath, "blobs")
				if entries, err := os.ReadDir(blobsPath); err == nil && len(entries) > 0 {
					available[i].Downloaded = true
				}
			}
		} else {
			// FLUX.1 models are stored in our models directory
			modelPath := filepath.Join(s.modelsDir, modelID)
			if info, err := os.Stat(modelPath); err == nil && info.IsDir() {
				// Check if model has actual files
				entries, _ := os.ReadDir(modelPath)
				if len(entries) > 0 {
					available[i].Downloaded = true
				}
			}
		}
	}

	return available
}

// DownloadProgressData is a safe copy of download progress data
type DownloadProgressData struct {
	ModelID    string  `json:"model_id"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	Message    string  `json:"message"`
	BytesDone  int64   `json:"bytes_done"`
	BytesTotal int64   `json:"bytes_total"`
	Speed      int64   `json:"speed"`
	ETA        int     `json:"eta"`
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
		ModelID:    dp.ModelID,
		Status:     dp.Status,
		Progress:   dp.Progress,
		Message:    dp.Message,
		BytesDone:  dp.BytesDone,
		BytesTotal: dp.BytesTotal,
		Speed:      dp.Speed,
		ETA:        dp.ETA,
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

	// Find Python 3.10+ (required for mflux)
	pythonPath, err := findPython3()
	if err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = err.Error()
		dp.mu.Unlock()
		return
	}

	// Ensure venv exists with mflux
	venvPath := filepath.Join(s.baseDir, "venv")
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		dp.mu.Lock()
		dp.Message = fmt.Sprintf("Creating Python environment with %s...", filepath.Base(pythonPath))
		dp.mu.Unlock()

		cmd := exec.CommandContext(ctx, pythonPath, "-m", "venv", venvPath)
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
	pipCmd := exec.CommandContext(ctx, pipPath, "install", "--upgrade", "mflux")
	if output, err := pipCmd.CombinedOutput(); err != nil {
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

	// Download model
	dp.mu.Lock()
	dp.Message = fmt.Sprintf("Downloading %s model (this may take a while)...", dp.ModelID)
	dp.Progress = 10
	dp.mu.Unlock()

	// Load config to get HuggingFace token
	cfg, _ := config.Load()

	// FLUX.2 models use huggingface-cli download (they're stored in HF cache)
	// FLUX.1 models use mflux-save (stored in our models directory)
	isFlux2 := strings.HasPrefix(dp.ModelID, "flux2-")

	var cmd *exec.Cmd
	if isFlux2 {
		// Use huggingface-cli to download FLUX.2 models
		hfCliPath := filepath.Join(venvPath, "bin", "huggingface-cli")
		var hfModelName string
		switch dp.ModelID {
		case "flux2-klein-4b":
			hfModelName = "black-forest-labs/FLUX.2-klein-4B"
		case "flux2-klein-9b":
			hfModelName = "black-forest-labs/FLUX.2-klein-9B"
		}
		cmd = exec.CommandContext(ctx, hfCliPath, "download", hfModelName)
	} else {
		// Use mflux-save for FLUX.1 models
		modelPath := filepath.Join(s.modelsDir, dp.ModelID)
		mfluxSavePath := filepath.Join(venvPath, "bin", "mflux-save")
		cmd = exec.CommandContext(ctx, mfluxSavePath, "--model", dp.ModelID, "--path", modelPath)
	}
	cmd.Env = os.Environ()

	// Add HuggingFace token if configured
	if cfg != nil && cfg.AI.HuggingFaceToken != "" {
		cmd.Env = append(cmd.Env, "HF_TOKEN="+cfg.AI.HuggingFaceToken)
	}

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
	// HuggingFace download output looks like:
	// Downloading model.safetensors: 45%|████      | 1.5G/3.2G [01:23<01:40, 16.5MB/s]
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		dp.mu.Lock()

		// Parse HuggingFace tqdm progress bars
		if strings.Contains(line, "%|") || strings.Contains(line, "% |") {
			// Extract percentage
			if idx := strings.Index(line, "%"); idx > 0 {
				// Find the start of the number
				start := idx - 1
				for start > 0 && (line[start] >= '0' && line[start] <= '9' || line[start] == '.') {
					start--
				}
				if pct, err := strconv.ParseFloat(strings.TrimSpace(line[start+1:idx]), 64); err == nil {
					// Map percentage to 30-90 range (10% for setup, 90-100% for finalization)
					dp.Progress = 30 + (pct * 0.6)
				}
			}

			// Extract bytes: "1.5G/3.2G" or "1500M/3200M"
			if idx := strings.Index(line, "/"); idx > 0 {
				// Look for size pattern before and after /
				beforeSlash := line[:idx]
				afterSlash := line[idx+1:]

				// Find bytes done (look backwards from /)
				doneStart := idx - 1
				for doneStart > 0 && (line[doneStart] != ' ' && line[doneStart] != '|') {
					doneStart--
				}
				doneStr := strings.TrimSpace(beforeSlash[doneStart:])

				// Find bytes total (look forward from /)
				totalEnd := 0
				for totalEnd < len(afterSlash) && afterSlash[totalEnd] != ' ' && afterSlash[totalEnd] != '[' {
					totalEnd++
				}
				totalStr := afterSlash[:totalEnd]

				dp.BytesDone = parseSize(doneStr)
				dp.BytesTotal = parseSize(totalStr)
			}

			// Extract speed: "16.5MB/s"
			if idx := strings.Index(line, "/s"); idx > 0 {
				speedStart := idx - 1
				for speedStart > 0 && line[speedStart] != ' ' && line[speedStart] != ',' {
					speedStart--
				}
				speedStr := strings.TrimSpace(line[speedStart+1 : idx+2])
				speedStr = strings.TrimSuffix(speedStr, "/s")
				dp.Speed = parseSize(speedStr)
			}

			// Extract ETA: "<01:40" or "eta 01:40"
			if idx := strings.Index(line, "<"); idx > 0 {
				etaEnd := idx + 1
				for etaEnd < len(line) && line[etaEnd] != ',' && line[etaEnd] != ']' {
					etaEnd++
				}
				etaStr := line[idx+1 : etaEnd]
				dp.ETA = parseETA(etaStr)
			}

			dp.Message = "Downloading model files..."
		} else if strings.Contains(line, "Downloading") {
			dp.Progress = 30
			dp.Message = "Downloading model files..."
		} else if strings.Contains(line, "Loading") {
			dp.Progress = 90
			dp.Message = "Loading model..."
		} else if strings.Contains(line, "Saved") || strings.Contains(line, "saved") {
			dp.Progress = 95
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

// parseSize parses a size string like "1.5G", "500M", "100K" to bytes
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	multiplier := int64(1)
	s = strings.ToUpper(s)

	if strings.HasSuffix(s, "GB") || strings.HasSuffix(s, "G") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GB"), "G")
	} else if strings.HasSuffix(s, "MB") || strings.HasSuffix(s, "M") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MB"), "M")
	} else if strings.HasSuffix(s, "KB") || strings.HasSuffix(s, "K") {
		multiplier = 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KB"), "K")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	val, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}

// parseETA parses an ETA string like "01:40" or "1:40:00" to seconds
func parseETA(s string) int {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) == 0 {
		return 0
	}

	total := 0
	for i, part := range parts {
		val, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		// Work backwards: last part is seconds, then minutes, then hours
		switch len(parts) - i {
		case 1: // seconds
			total += val
		case 2: // minutes
			total += val * 60
		case 3: // hours
			total += val * 3600
		}
	}
	return total
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

	// Check if model is available
	// FLUX.2 models download to HuggingFace cache, not our models directory
	isFlux2 := strings.HasPrefix(modelID, "flux2-")
	if !isFlux2 {
		// For FLUX.1 models, check local models directory
		modelPath := filepath.Join(s.modelsDir, modelID)
		if _, err := os.Stat(modelPath); os.IsNotExist(err) {
			s.mu.Unlock()
			return nil, fmt.Errorf("model not downloaded: %s", modelID)
		}
	}
	// FLUX.2 models will download automatically on first use

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
	job.Progress = 5
	s.mu.Unlock()
	s.updateJobInDB(job)

	venvPath := filepath.Join(s.baseDir, "venv")
	outputPath := filepath.Join(s.outputDir, job.ID+".png")

	// Ensure venv and mflux are installed
	if err := s.ensureMfluxInstalled(job); err != nil {
		s.mu.Lock()
		job.Status = "failed"
		job.Error = fmt.Sprintf("Failed to setup mflux: %v", err)
		s.mu.Unlock()
		s.updateJobInDB(job)
		return
	}

	s.mu.Lock()
	job.Progress = 10
	s.mu.Unlock()
	s.updateJobInDB(job)

	// Determine the correct mflux command based on model type
	// FLUX.2 models use mflux-generate-flux2, FLUX.1 models use mflux-generate
	var mfluxGenPath string
	var args []string

	isFlux2 := strings.HasPrefix(job.Model, "flux2-")
	if isFlux2 {
		// FLUX.2 models use mflux-generate-flux2 with --base-model <model-name>
		mfluxGenPath = filepath.Join(venvPath, "bin", "mflux-generate-flux2")
		args = []string{
			"--base-model", job.Model,
			"--prompt", job.Prompt,
			"--width", strconv.Itoa(job.Width),
			"--height", strconv.Itoa(job.Height),
			"--steps", strconv.Itoa(job.Steps),
			"--output", outputPath,
		}
	} else {
		// FLUX.1 models (schnell, dev) use mflux-generate with --model <path>
		mfluxGenPath = filepath.Join(venvPath, "bin", "mflux-generate")
		modelPath := filepath.Join(s.modelsDir, job.Model)
		args = []string{
			"--model", modelPath,
			"--prompt", job.Prompt,
			"--width", strconv.Itoa(job.Width),
			"--height", strconv.Itoa(job.Height),
			"--steps", strconv.Itoa(job.Steps),
			"--output", outputPath,
		}
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

// ensureMfluxInstalled creates venv and installs mflux if not already present
func (s *Service) ensureMfluxInstalled(job *GenerationJob) error {
	venvPath := filepath.Join(s.baseDir, "venv")
	mfluxGenPath := filepath.Join(venvPath, "bin", "mflux-generate")

	// Check if mflux is already installed
	if _, err := os.Stat(mfluxGenPath); err == nil {
		return nil
	}

	// Find Python
	pythonPath := "python3"
	for _, p := range []string{"/opt/homebrew/bin/python3", "/usr/local/bin/python3", "/usr/bin/python3"} {
		if _, err := os.Stat(p); err == nil {
			pythonPath = p
			break
		}
	}

	// Create venv if needed
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		s.mu.Lock()
		job.Status = "generating"
		job.Error = "Setting up Python environment..."
		s.mu.Unlock()
		s.updateJobInDB(job)

		cmd := exec.Command(pythonPath, "-m", "venv", venvPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create venv: %s", string(output))
		}
	}

	// Install mflux
	s.mu.Lock()
	job.Status = "generating"
	job.Error = "Installing mflux (this may take a few minutes)..."
	s.mu.Unlock()
	s.updateJobInDB(job)

	pipPath := filepath.Join(venvPath, "bin", "pip")
	cmd := exec.Command(pipPath, "install", "--upgrade", "mflux")
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install mflux: %s", string(output))
	}

	// Clear the error message
	s.mu.Lock()
	job.Error = ""
	s.mu.Unlock()

	return nil
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
