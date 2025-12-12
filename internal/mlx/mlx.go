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
	})
	return instance
}

// IsSupported checks if MLX is supported on this platform
func IsSupported() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
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

// PullModel downloads a model from HuggingFace
func (s *Service) PullModel(modelID string, progress func(string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure venv exists
	venvPath := filepath.Join(s.baseDir, "venv")
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		if progress != nil {
			progress("Creating Python environment...")
		}
		cmd := exec.Command("python3", "-m", "venv", venvPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create venv: %s: %w", string(output), err)
		}

		// Install mlx-lm
		if progress != nil {
			progress("Installing mlx-lm...")
		}
		pipPath := filepath.Join(venvPath, "bin", "pip")
		cmd = exec.Command(pipPath, "install", "mlx-lm")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install mlx-lm: %s: %w", string(output), err)
		}
	}

	// Download model
	if progress != nil {
		progress(fmt.Sprintf("Downloading %s...", modelID))
	}

	pythonPath := filepath.Join(venvPath, "bin", "python")
	downloadScript := fmt.Sprintf(`
from mlx_lm import load
print("Downloading model: %s")
load("%s")
print("Download complete")
`, modelID, modelID)

	cmd := exec.Command(pythonPath, "-c", downloadScript)
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(s.baseDir, "cache"),
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to download model: %s: %w", string(output), err)
	}

	// Save to downloaded models
	downloaded := s.getDownloadedModels()
	downloaded[modelID] = time.Now()
	s.saveDownloadedModels(downloaded)

	if progress != nil {
		progress("Model ready!")
	}

	return nil
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

// GetModelCatalog returns the list of recommended models
func GetModelCatalog() []ModelInfo {
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
