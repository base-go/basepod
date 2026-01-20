// Package mlx provides MLX LLM service management for macOS.
// Designed like Ollama - one server, multiple models that can be loaded/switched.
package mlx

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Service manages the MLX LLM server (singleton pattern like Ollama)
type Service struct {
	baseDir     string // Base directory for models and data
	port        int    // Server port (default 11434 like Ollama)
	process     *exec.Cmd
	pid         int
	activeModel string
	mu          sync.RWMutex
	client      *http.Client
}

// Model represents a downloaded model
type Model struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Size        string    `json:"size"`
	Downloaded  bool      `json:"downloaded"`
	DownloadedAt time.Time `json:"downloaded_at,omitempty"`
}

// ModelInfo represents model metadata from the catalog
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	Description string `json:"description"`
}

// Status represents the service status
type Status struct {
	Running     bool   `json:"running"`
	Port        int    `json:"port"`
	PID         int    `json:"pid"`
	ActiveModel string `json:"active_model"`
}

// DownloadProgress tracks the progress of a model download
type DownloadProgress struct {
	ModelID       string    `json:"model_id"`
	Status        string    `json:"status"` // "pending", "downloading", "completed", "failed", "cancelled"
	Progress      float64   `json:"progress"` // 0-100
	BytesTotal    int64     `json:"bytes_total"`
	BytesDone     int64     `json:"bytes_done"`
	Speed         int64     `json:"speed"` // bytes per second
	ETA           int       `json:"eta"`   // seconds remaining
	CurrentFile   string    `json:"current_file"`
	Message       string    `json:"message"`
	StartedAt     time.Time `json:"started_at"`
	cancel        context.CancelFunc
	mu            sync.RWMutex
}

// Global download tracker
var (
	activeDownloads   = make(map[string]*DownloadProgress)
	activeDownloadsMu sync.RWMutex
)

var (
	instance *Service
	once     sync.Once
)

// GetService returns the singleton MLX service instance
func GetService() *Service {
	once.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/tmp"
		}
		baseDir := filepath.Join(home, ".local", "share", "deployer", "mlx")
		os.MkdirAll(baseDir, 0755)

		instance = &Service{
			baseDir: baseDir,
			port:    11434, // Same as Ollama default
			client:  &http.Client{Timeout: 30 * time.Second},
		}

		// Initialize model cache (loads from disk, updates in background)
		InitModelCache(baseDir)
	})
	return instance
}

// IsSupported checks if MLX is supported on this platform
func IsSupported() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

// SystemInfo holds system memory information
type SystemInfo struct {
	TotalRAM     uint64 `json:"total_ram"`      // Total RAM in bytes
	TotalRAMGB   int    `json:"total_ram_gb"`   // Total RAM in GB
	AvailableRAM uint64 `json:"available_ram"`  // Available RAM in bytes (approximate)
	Supported    bool   `json:"supported"`
}

// GetSystemInfo returns system information for MLX compatibility
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Supported: IsSupported(),
	}

	if runtime.GOOS == "darwin" {
		// Get total memory using sysctl
		cmd := exec.Command("sysctl", "-n", "hw.memsize")
		output, err := cmd.Output()
		if err == nil {
			var memBytes uint64
			fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &memBytes)
			info.TotalRAM = memBytes
			info.TotalRAMGB = int(memBytes / (1024 * 1024 * 1024))
			// Estimate available as 70% of total (conservative for MLX)
			info.AvailableRAM = uint64(float64(memBytes) * 0.7)
		}
	}

	return info
}

// CanRunModel checks if a model can run on this system
func CanRunModel(modelID string, totalRAMGB int) (bool, string) {
	requiredGB := EstimateModelRAM(modelID)

	if totalRAMGB == 0 {
		return true, "" // Can't determine, assume ok
	}

	// Need some headroom - model + OS + other apps
	availableForModel := int(float64(totalRAMGB) * 0.7)

	if requiredGB > availableForModel {
		return false, fmt.Sprintf("Model needs ~%dGB RAM, but only ~%dGB available (of %dGB total)",
			requiredGB, availableForModel, totalRAMGB)
	}

	if requiredGB > availableForModel-2 {
		return true, fmt.Sprintf("Model needs ~%dGB RAM - may be slow with only %dGB total",
			requiredGB, totalRAMGB)
	}

	return true, ""
}

// EstimateModelRAM estimates RAM needed for a model in GB
func EstimateModelRAM(modelID string) int {
	idLower := strings.ToLower(modelID)

	// Extract parameter count from model name
	if strings.Contains(idLower, "0.5b") || strings.Contains(idLower, "0.6b") {
		return 1
	}
	if strings.Contains(idLower, "1b") && !strings.Contains(idLower, "1.5b") && !strings.Contains(idLower, "10b") && !strings.Contains(idLower, "13b") && !strings.Contains(idLower, "14b") {
		return 1
	}
	if strings.Contains(idLower, "1.5b") {
		return 2
	}
	if strings.Contains(idLower, "2b") && !strings.Contains(idLower, "72b") {
		return 2
	}
	if strings.Contains(idLower, "3b") && !strings.Contains(idLower, "13b") {
		return 3
	}
	if strings.Contains(idLower, "4b") && !strings.Contains(idLower, "14b") {
		return 3
	}
	if strings.Contains(idLower, "7b") {
		return 5
	}
	if strings.Contains(idLower, "8b") {
		return 6
	}
	if strings.Contains(idLower, "9b") {
		return 6
	}
	if strings.Contains(idLower, "10b") {
		return 7
	}
	if strings.Contains(idLower, "13b") || strings.Contains(idLower, "14b") {
		return 9
	}
	if strings.Contains(idLower, "20b") || strings.Contains(idLower, "24b") {
		return 14
	}
	if strings.Contains(idLower, "32b") || strings.Contains(idLower, "34b") {
		return 20
	}
	if strings.Contains(idLower, "70b") || strings.Contains(idLower, "72b") {
		return 42
	}

	return 4 // Default estimate
}

// GetStatus returns the current service status
func (s *Service) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	running := s.isRunning()
	return Status{
		Running:     running,
		Port:        s.port,
		PID:         s.pid,
		ActiveModel: s.activeModel,
	}
}

// isRunning checks if the MLX server is running
func (s *Service) isRunning() bool {
	if s.pid == 0 {
		return false
	}
	proc, err := os.FindProcess(s.pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ListModels returns all available models (catalog + downloaded status)
func (s *Service) ListModels() []Model {
	catalog := GetModelCatalog()
	downloaded := s.getDownloadedModels()

	var models []Model
	for _, info := range catalog {
		model := Model{
			ID:         info.ID,
			Name:       info.Name,
			Size:       info.Size,
			Downloaded: false,
		}
		if dlTime, ok := downloaded[info.ID]; ok {
			model.Downloaded = true
			model.DownloadedAt = dlTime
		}
		models = append(models, model)
	}
	return models
}

// getDownloadedModels returns map of model ID -> download time
func (s *Service) getDownloadedModels() map[string]time.Time {
	result := make(map[string]time.Time)
	metaFile := filepath.Join(s.baseDir, "models.json")

	data, err := os.ReadFile(metaFile)
	if err != nil {
		return result
	}

	json.Unmarshal(data, &result)
	return result
}

// saveDownloadedModels saves the downloaded models metadata
func (s *Service) saveDownloadedModels(models map[string]time.Time) error {
	metaFile := filepath.Join(s.baseDir, "models.json")
	data, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return os.WriteFile(metaFile, data, 0644)
}

// DownloadProgressData is a safe copy of download progress data
type DownloadProgressData struct {
	ModelID    string  `json:"model_id"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	BytesTotal int64   `json:"bytes_total"`
	BytesDone  int64   `json:"bytes_done"`
	Speed      int64   `json:"speed"`
	ETA        int     `json:"eta"`
	Message    string  `json:"message"`
}

// GetDownloadProgress returns the current download progress for a model
func GetDownloadProgress(modelID string) *DownloadProgressData {
	activeDownloadsMu.RLock()
	dp := activeDownloads[modelID]
	activeDownloadsMu.RUnlock()

	if dp == nil {
		return nil
	}

	dp.mu.RLock()
	defer dp.mu.RUnlock()

	return &DownloadProgressData{
		ModelID:    dp.ModelID,
		Status:     dp.Status,
		Progress:   dp.Progress,
		BytesTotal: dp.BytesTotal,
		BytesDone:  dp.BytesDone,
		Speed:      dp.Speed,
		ETA:        dp.ETA,
		Message:    dp.Message,
	}
}

// GetAllDownloads returns all active downloads as safe data copies
func GetAllDownloads() []*DownloadProgressData {
	activeDownloadsMu.RLock()
	defer activeDownloadsMu.RUnlock()

	result := make([]*DownloadProgressData, 0, len(activeDownloads))
	for _, dp := range activeDownloads {
		dp.mu.RLock()
		result = append(result, &DownloadProgressData{
			ModelID:    dp.ModelID,
			Status:     dp.Status,
			Progress:   dp.Progress,
			BytesTotal: dp.BytesTotal,
			BytesDone:  dp.BytesDone,
			Speed:      dp.Speed,
			ETA:        dp.ETA,
			Message:    dp.Message,
		})
		dp.mu.RUnlock()
	}
	return result
}

// CancelDownload cancels an active download
func CancelDownload(modelID string) bool {
	activeDownloadsMu.Lock()
	defer activeDownloadsMu.Unlock()
	if dp, ok := activeDownloads[modelID]; ok {
		if dp.cancel != nil {
			dp.cancel()
		}
		dp.mu.Lock()
		dp.Status = "cancelled"
		dp.Message = "Download cancelled"
		dp.mu.Unlock()
		return true
	}
	return false
}

// PullModel downloads a model from HuggingFace (legacy sync version)
func (s *Service) PullModel(modelID string, progress func(string)) error {
	// Start async download and wait for completion
	dp := s.StartPullModel(modelID)

	// Poll until complete
	for {
		dp.mu.RLock()
		status := dp.Status
		msg := dp.Message
		dp.mu.RUnlock()

		if progress != nil {
			progress(msg)
		}

		if status == "completed" {
			return nil
		}
		if status == "failed" || status == "cancelled" {
			return fmt.Errorf(msg)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// StartPullModel starts an async model download with progress tracking
func (s *Service) StartPullModel(modelID string) *DownloadProgress {
	// Check if already downloading
	activeDownloadsMu.Lock()
	if existing, ok := activeDownloads[modelID]; ok {
		existing.mu.RLock()
		status := existing.Status
		existing.mu.RUnlock()
		if status == "downloading" || status == "pending" {
			activeDownloadsMu.Unlock()
			return existing
		}
	}

	// Create new download progress
	ctx, cancel := context.WithCancel(context.Background())
	dp := &DownloadProgress{
		ModelID:   modelID,
		Status:    "pending",
		Message:   "Starting download...",
		StartedAt: time.Now(),
		cancel:    cancel,
	}
	activeDownloads[modelID] = dp
	activeDownloadsMu.Unlock()

	// Start download in background
	go s.runDownload(ctx, dp)

	return dp
}

// runDownload performs the actual download with progress tracking
func (s *Service) runDownload(ctx context.Context, dp *DownloadProgress) {
	dp.mu.Lock()
	dp.Status = "downloading"
	dp.Message = "Preparing environment..."
	dp.mu.Unlock()

	// Ensure venv exists
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

		dp.mu.Lock()
		dp.Message = "Installing mlx-lm and huggingface-hub..."
		dp.mu.Unlock()

		pipPath := filepath.Join(venvPath, "bin", "pip")
		cmd = exec.CommandContext(ctx, pipPath, "install", "mlx-lm", "huggingface-hub")
		if output, err := cmd.CombinedOutput(); err != nil {
			dp.mu.Lock()
			dp.Status = "failed"
			dp.Message = fmt.Sprintf("Failed to install dependencies: %s", string(output))
			dp.mu.Unlock()
			return
		}
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

	dp.mu.Lock()
	dp.Message = fmt.Sprintf("Downloading %s...", dp.ModelID)
	dp.mu.Unlock()

	// Use huggingface-hub with progress tracking
	pythonPath := filepath.Join(venvPath, "bin", "python")
	downloadScript := fmt.Sprintf(`
import sys
import os
from huggingface_hub import HfApi, hf_hub_download, list_repo_files

model_id = "%s"
cache_dir = "%s"

api = HfApi()

# Get file list and sizes
print("FETCHING_FILES", flush=True)
try:
    files = list(api.list_repo_tree(model_id, recursive=True))
    # Filter to actual files (not directories)
    file_list = [(f.path, f.size) for f in files if hasattr(f, 'size') and f.size and f.size > 0]
    total_size = sum(size for _, size in file_list)
    print(f"TOTAL_SIZE:{total_size}", flush=True)
    print(f"FILE_COUNT:{len(file_list)}", flush=True)
except Exception as e:
    print(f"SIZE_ERROR:{e}", flush=True)
    file_list = []
    total_size = 0

# Download files one by one with progress
print("DOWNLOADING", flush=True)
downloaded_bytes = 0
try:
    for i, (filename, file_size) in enumerate(file_list):
        # Report starting this file
        pct = (downloaded_bytes / total_size * 100) if total_size > 0 else 0
        print(f"FILE_START:{filename}:{file_size}:{pct:.1f}", flush=True)

        # Download file
        hf_hub_download(
            repo_id=model_id,
            filename=filename,
            cache_dir=cache_dir,
            resume_download=True,
        )

        # Update progress
        downloaded_bytes += file_size
        pct = (downloaded_bytes / total_size * 100) if total_size > 0 else 0
        print(f"PROGRESS:{downloaded_bytes}:{total_size}:{pct:.1f}:{filename}", flush=True)

    print("DOWNLOAD_COMPLETE", flush=True)
except KeyboardInterrupt:
    print("DOWNLOAD_CANCELLED", flush=True)
    sys.exit(1)
except Exception as e:
    print(f"DOWNLOAD_ERROR:{e}", flush=True)
    sys.exit(1)

# Now load to verify
print("LOADING_MODEL", flush=True)
from mlx_lm import load
load(model_id)
print("MODEL_READY", flush=True)
`, dp.ModelID, filepath.Join(s.baseDir, "cache"))

	cmd := exec.CommandContext(ctx, pythonPath, "-c", downloadScript)
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(s.baseDir, "cache"),
		"PYTHONUNBUFFERED=1",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Failed to start download: %v", err)
		dp.mu.Unlock()
		return
	}

	if err := cmd.Start(); err != nil {
		dp.mu.Lock()
		dp.Status = "failed"
		dp.Message = fmt.Sprintf("Failed to start download: %v", err)
		dp.mu.Unlock()
		return
	}

	// Parse progress output
	startTime := time.Now()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		dp.mu.Lock()
		if strings.HasPrefix(line, "TOTAL_SIZE:") {
			fmt.Sscanf(line, "TOTAL_SIZE:%d", &dp.BytesTotal)
		} else if strings.HasPrefix(line, "PROGRESS:") {
			// Format: PROGRESS:current:total:pct:filename
			parts := strings.SplitN(line, ":", 5)
			if len(parts) >= 4 {
				fmt.Sscanf(parts[1], "%d", &dp.BytesDone)
				fmt.Sscanf(parts[2], "%d", &dp.BytesTotal)
				fmt.Sscanf(parts[3], "%f", &dp.Progress)

				// Calculate speed and ETA
				elapsed := time.Since(startTime).Seconds()
				if elapsed > 0 && dp.BytesDone > 0 {
					dp.Speed = int64(float64(dp.BytesDone) / elapsed)
					if dp.Speed > 0 && dp.BytesTotal > dp.BytesDone {
						dp.ETA = int(float64(dp.BytesTotal-dp.BytesDone) / float64(dp.Speed))
					}
				}

				filename := ""
				if len(parts) >= 5 {
					filename = parts[4]
				}
				if filename != "" {
					dp.Message = fmt.Sprintf("Downloading %s... %.0f%%", filename, dp.Progress)
					dp.CurrentFile = filename
				} else {
					dp.Message = fmt.Sprintf("Downloading... %.0f%%", dp.Progress)
				}
			}
		} else if line == "FETCHING_FILES" {
			dp.Message = "Fetching file list..."
		} else if strings.HasPrefix(line, "FILE_COUNT:") {
			var count int
			fmt.Sscanf(line, "FILE_COUNT:%d", &count)
			dp.Message = fmt.Sprintf("Found %d files to download", count)
		} else if strings.HasPrefix(line, "FILE_START:") {
			// Format: FILE_START:filename:size:pct
			parts := strings.SplitN(line, ":", 4)
			if len(parts) >= 3 {
				filename := parts[1]
				dp.CurrentFile = filename
				dp.Message = fmt.Sprintf("Downloading %s...", filename)
			}
		} else if line == "DOWNLOADING" {
			dp.Message = "Starting download..."
		} else if line == "LOADING_MODEL" {
			dp.Progress = 95
			dp.Message = "Loading model..."
		} else if line == "MODEL_READY" {
			dp.Progress = 100
			dp.Status = "completed"
			dp.Message = "Model ready!"
		} else if line == "DOWNLOAD_COMPLETE" {
			dp.Progress = 90
			dp.Message = "Download complete, loading..."
		} else if strings.HasPrefix(line, "DOWNLOAD_ERROR:") {
			dp.Status = "failed"
			dp.Message = strings.TrimPrefix(line, "DOWNLOAD_ERROR:")
		} else if line == "DOWNLOAD_CANCELLED" {
			dp.Status = "cancelled"
			dp.Message = "Download cancelled"
		}
		dp.mu.Unlock()
	}

	cmd.Wait()

	// Final status check
	dp.mu.Lock()
	if dp.Status == "downloading" {
		// If we're still downloading, check exit code
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 0 {
			dp.Status = "completed"
			dp.Progress = 100
			dp.Message = "Model ready!"

			// Save to downloaded models
			s.mu.Lock()
			downloaded := s.getDownloadedModels()
			downloaded[dp.ModelID] = time.Now()
			s.saveDownloadedModels(downloaded)
			s.mu.Unlock()
		} else {
			dp.Status = "failed"
			dp.Message = "Download failed"
		}
	} else if dp.Status == "completed" {
		// Save to downloaded models
		s.mu.Lock()
		downloaded := s.getDownloadedModels()
		downloaded[dp.ModelID] = time.Now()
		s.saveDownloadedModels(downloaded)
		s.mu.Unlock()
	}
	dp.mu.Unlock()

	// Clean up after a delay
	go func() {
		time.Sleep(5 * time.Minute)
		activeDownloadsMu.Lock()
		delete(activeDownloads, dp.ModelID)
		activeDownloadsMu.Unlock()
	}()
}

// formatBytes formats bytes to human readable string
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// DeleteModel removes a downloaded model
func (s *Service) DeleteModel(modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop if this model is running
	if s.activeModel == modelID && s.isRunning() {
		s.stopServer()
	}

	// Remove from metadata
	downloaded := s.getDownloadedModels()
	delete(downloaded, modelID)
	s.saveDownloadedModels(downloaded)

	// Note: Actual model files are in HuggingFace cache, we just track metadata
	return nil
}

// Run starts the MLX server with the specified model
func (s *Service) Run(modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if model is downloaded
	downloaded := s.getDownloadedModels()
	if _, ok := downloaded[modelID]; !ok {
		return fmt.Errorf("model not downloaded: %s", modelID)
	}

	// Stop current server if running different model
	if s.isRunning() {
		if s.activeModel == modelID {
			return nil // Already running this model
		}
		s.stopServer()
	}

	// Start server with new model
	venvPath := filepath.Join(s.baseDir, "venv")
	pythonPath := filepath.Join(venvPath, "bin", "python")

	cmd := exec.Command(pythonPath, "-m", "mlx_lm.server",
		"--model", modelID,
		"--port", fmt.Sprintf("%d", s.port),
		"--host", "0.0.0.0",
	)
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(s.baseDir, "cache"),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Log output
	logFile := filepath.Join(s.baseDir, "server.log")
	logFd, _ := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	cmd.Stdout = logFd
	cmd.Stderr = logFd

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MLX server: %w", err)
	}

	s.process = cmd
	s.pid = cmd.Process.Pid
	s.activeModel = modelID

	// Wait for server in background
	go func() {
		cmd.Wait()
		logFd.Close()
	}()

	return nil
}

// Stop stops the MLX server
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopServer()
}

func (s *Service) stopServer() error {
	if s.process != nil && s.process.Process != nil {
		s.process.Process.Signal(syscall.SIGTERM)

		done := make(chan error, 1)
		go func() { done <- s.process.Wait() }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			s.process.Process.Kill()
		}
	}

	s.process = nil
	s.pid = 0
	s.activeModel = ""
	return nil
}

// GetLogs returns recent server logs
func (s *Service) GetLogs(lines int) ([]string, error) {
	logFile := filepath.Join(s.baseDir, "server.log")
	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	if len(allLines) <= lines {
		return allLines, nil
	}
	return allLines[len(allLines)-lines:], nil
}

// StreamLogs streams server logs
func (s *Service) StreamLogs(ctx context.Context, writer io.Writer) error {
	logFile := filepath.Join(s.baseDir, "server.log")
	cmd := exec.CommandContext(ctx, "tail", "-f", logFile)
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

// HF API cache with disk persistence
var (
	hfModelsCache     []ModelInfo
	hfModelsCacheTime time.Time
	hfCacheDuration   = 24 * time.Hour // Update daily
	hfCacheMu         sync.RWMutex
	hfCacheLoaded     bool
)

// ModelCache represents the disk cache structure
type ModelCache struct {
	Models    []ModelInfo `json:"models"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// InitModelCache loads cache from disk and starts background updater
func InitModelCache(baseDir string) {
	cacheFile := filepath.Join(baseDir, "models_cache.json")

	// Load from disk
	if data, err := os.ReadFile(cacheFile); err == nil {
		var cache ModelCache
		if json.Unmarshal(data, &cache) == nil && len(cache.Models) > 0 {
			hfCacheMu.Lock()
			hfModelsCache = cache.Models
			hfModelsCacheTime = cache.UpdatedAt
			hfCacheLoaded = true
			hfCacheMu.Unlock()
		}
	}

	// Start background updater
	go func() {
		// Initial update if cache is old or empty
		hfCacheMu.RLock()
		needsUpdate := !hfCacheLoaded || time.Since(hfModelsCacheTime) > hfCacheDuration
		hfCacheMu.RUnlock()

		if needsUpdate {
			updateModelCache(baseDir)
		}

		// Update daily
		ticker := time.NewTicker(hfCacheDuration)
		for range ticker.C {
			updateModelCache(baseDir)
		}
	}()
}

// updateModelCache fetches fresh data and saves to disk
func updateModelCache(baseDir string) {
	models, err := fetchModelsFromHF()
	if err != nil {
		return // Keep existing cache on error
	}

	hfCacheMu.Lock()
	hfModelsCache = models
	hfModelsCacheTime = time.Now()
	hfCacheLoaded = true
	hfCacheMu.Unlock()

	// Save to disk
	cache := ModelCache{
		Models:    models,
		UpdatedAt: time.Now(),
	}
	if data, err := json.Marshal(cache); err == nil {
		cacheFile := filepath.Join(baseDir, "models_cache.json")
		os.WriteFile(cacheFile, data, 0644)
	}
}

// HFModel represents a model from Hugging Face API
type HFModel struct {
	ID          string   `json:"id"`
	ModelID     string   `json:"modelId"`
	Downloads   int      `json:"downloads"`
	Likes       int      `json:"likes"`
	Tags        []string `json:"tags"`
	LibraryName string   `json:"library_name"`
	Siblings    []struct {
		Filename string `json:"rfilename"`
		Size     int64  `json:"size"`
	} `json:"siblings"`
}

// FetchHuggingFaceModels returns cached models (fast, from memory/disk)
func FetchHuggingFaceModels() ([]ModelInfo, error) {
	hfCacheMu.RLock()
	defer hfCacheMu.RUnlock()

	if len(hfModelsCache) > 0 {
		return hfModelsCache, nil
	}

	// No cache yet, fetch synchronously (only happens on first request)
	hfCacheMu.RUnlock()
	models, err := fetchModelsFromHF()
	hfCacheMu.RLock()
	return models, err
}

// fetchModelsFromHF fetches MLX models from Hugging Face API
func fetchModelsFromHF() ([]ModelInfo, error) {
	// Fetch from HF API - get all MLX models sorted by downloads
	url := "https://huggingface.co/api/models?author=mlx-community&sort=downloads&direction=-1&limit=200"

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from HuggingFace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var hfModels []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&hfModels); err != nil {
		return nil, fmt.Errorf("failed to decode HuggingFace response: %w", err)
	}

	// Filter and convert to our format
	var models []ModelInfo
	for _, hf := range hfModels {
		id := hf.ModelID
		if id == "" {
			id = hf.ID
		}

		// Must be an MLX model
		if hf.LibraryName != "mlx" {
			continue
		}

		idLower := strings.ToLower(id)

		// Extract model name from ID (keep full name for clarity)
		name := strings.TrimPrefix(id, "mlx-community/")

		// Estimate size from downloads/popularity
		size := estimateModelSize(id)

		// Format downloads nicely
		var downloads string
		if hf.Downloads >= 1000000 {
			downloads = fmt.Sprintf("%.1fM downloads", float64(hf.Downloads)/1000000)
		} else if hf.Downloads >= 1000 {
			downloads = fmt.Sprintf("%.1fK downloads", float64(hf.Downloads)/1000)
		} else {
			downloads = fmt.Sprintf("%d downloads", hf.Downloads)
		}

		// Extract quantization type for description
		quantType := ""
		if strings.Contains(idLower, "4bit") || strings.Contains(idLower, "4-bit") {
			quantType = "4-bit"
		} else if strings.Contains(idLower, "8bit") || strings.Contains(idLower, "8-bit") {
			quantType = "8-bit"
		} else if strings.Contains(idLower, "6bit") || strings.Contains(idLower, "6-bit") {
			quantType = "6-bit"
		} else if strings.Contains(idLower, "bf16") {
			quantType = "bf16"
		} else if strings.Contains(idLower, "fp4") || strings.Contains(idLower, "mxfp4") {
			quantType = "fp4"
		}

		desc := downloads
		if quantType != "" {
			desc = quantType + " · " + downloads
		}

		models = append(models, ModelInfo{
			ID:          id,
			Name:        name,
			Size:        size,
			Description: desc,
		})

		// Limit to 100 models
		if len(models) >= 100 {
			break
		}
	}

	return models, nil
}

// estimateModelSize estimates model size based on name
func estimateModelSize(modelID string) string {
	idLower := strings.ToLower(modelID)
	if strings.Contains(idLower, "0.5b") || strings.Contains(idLower, "0.6b") {
		return "~0.4GB"
	}
	if strings.Contains(idLower, "1b") || strings.Contains(idLower, "1.5b") {
		return "~0.7GB"
	}
	if strings.Contains(idLower, "2b") {
		return "~1.2GB"
	}
	if strings.Contains(idLower, "3b") {
		return "~2GB"
	}
	if strings.Contains(idLower, "7b") || strings.Contains(idLower, "8b") {
		return "~4GB"
	}
	if strings.Contains(idLower, "9b") || strings.Contains(idLower, "10b") {
		return "~5GB"
	}
	if strings.Contains(idLower, "13b") || strings.Contains(idLower, "14b") {
		return "~7GB"
	}
	if strings.Contains(idLower, "70b") {
		return "~40GB"
	}
	return "~2GB"
}

// GetModelCatalog returns the list of recommended models (fetches from HF with fallback)
func GetModelCatalog() []ModelInfo {
	// Try to fetch from HuggingFace
	models, err := FetchHuggingFaceModels()
	if err == nil && len(models) > 0 {
		return models
	}

	// Fallback to hardcoded list
	return []ModelInfo{
		{ID: "mlx-community/Llama-3.2-1B-Instruct-4bit", Name: "Llama 3.2 1B", Size: "0.7GB", Description: "Ultra-fast, great for quick tasks"},
		{ID: "mlx-community/Llama-3.2-3B-Instruct-4bit", Name: "Llama 3.2 3B", Size: "2GB", Description: "Fast and capable"},
		{ID: "mlx-community/Qwen2.5-3B-Instruct-4bit", Name: "Qwen 2.5 3B", Size: "2GB", Description: "Strong multilingual support"},
		{ID: "mlx-community/Qwen2.5-7B-Instruct-4bit", Name: "Qwen 2.5 7B", Size: "4GB", Description: "Powerful general purpose"},
		{ID: "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit", Name: "Qwen 2.5 Coder 7B", Size: "4GB", Description: "Optimized for code"},
		{ID: "mlx-community/Mistral-7B-Instruct-v0.3-4bit", Name: "Mistral 7B v0.3", Size: "4GB", Description: "Strong reasoning"},
		{ID: "mlx-community/gemma-2-9b-it-4bit", Name: "Gemma 2 9B", Size: "5GB", Description: "Google's latest"},
		{ID: "mlx-community/Phi-3.5-mini-instruct-4bit", Name: "Phi 3.5 Mini", Size: "2GB", Description: "Microsoft's efficient model"},
		{ID: "mlx-community/DeepSeek-Coder-V2-Lite-Instruct-4bit", Name: "DeepSeek Coder V2", Size: "2GB", Description: "Coding specialist"},
	}
}

// Legacy compatibility - returns same catalog
func ListModels() []ModelInfo {
	return GetModelCatalog()
}
