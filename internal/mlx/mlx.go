// Package mlx provides MLX LLM service management for macOS.
package mlx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manager handles MLX LLM service lifecycle
type Manager struct {
	baseDir   string // Base directory for MLX apps (e.g., /usr/local/deployer/mlx)
	processes map[string]*Process
	mu        sync.RWMutex
}

// Process represents a running MLX server process
type Process struct {
	AppID   string
	Model   string
	Port    int
	PID     int
	Cmd     *exec.Cmd
	LogFile string
	Started time.Time
}

// NewManager creates a new MLX manager
func NewManager(baseDir string) *Manager {
	if baseDir == "" {
		baseDir = "/usr/local/deployer/mlx"
	}
	return &Manager{
		baseDir:   baseDir,
		processes: make(map[string]*Process),
	}
}

// IsSupported checks if MLX is supported on this platform
func IsSupported() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

// SetupApp creates the venv and installs mlx-lm for an app
func (m *Manager) SetupApp(appID, model string) error {
	appDir := filepath.Join(m.baseDir, appID)
	venvPath := filepath.Join(appDir, "venv")

	// Create app directory
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Create Python venv
	cmd := exec.Command("python3", "-m", "venv", venvPath)
	cmd.Dir = appDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create venv: %s: %w", string(output), err)
	}

	// Install mlx-lm
	pipPath := filepath.Join(venvPath, "bin", "pip")
	cmd = exec.Command(pipPath, "install", "mlx-lm")
	cmd.Dir = appDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install mlx-lm: %s: %w", string(output), err)
	}

	// Pre-download model
	pythonPath := filepath.Join(venvPath, "bin", "python")
	downloadScript := fmt.Sprintf(`
from mlx_lm import load
print("Downloading model: %s")
load("%s")
print("Model downloaded successfully")
`, model, model)

	cmd = exec.Command(pythonPath, "-c", downloadScript)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(appDir, ".cache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to download model: %s: %w", string(output), err)
	}

	return nil
}

// Start starts an MLX server for an app
func (m *Manager) Start(appID, model string, port int) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if proc, exists := m.processes[appID]; exists {
		if m.isProcessRunning(proc.PID) {
			return proc, nil
		}
		// Process died, clean up
		delete(m.processes, appID)
	}

	appDir := filepath.Join(m.baseDir, appID)
	venvPath := filepath.Join(appDir, "venv")
	pythonPath := filepath.Join(venvPath, "bin", "python")

	// Check venv exists
	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("venv not found, run setup first")
	}

	// Create log file
	logDir := filepath.Join(appDir, "logs")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, "server.log")

	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	// Start MLX server
	cmd := exec.Command(pythonPath, "-m", "mlx_lm.server",
		"--model", model,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
	)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(appDir, ".cache"),
	)
	cmd.Stdout = logFd
	cmd.Stderr = logFd

	// Start in new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		logFd.Close()
		return nil, fmt.Errorf("failed to start MLX server: %w", err)
	}

	proc := &Process{
		AppID:   appID,
		Model:   model,
		Port:    port,
		PID:     cmd.Process.Pid,
		Cmd:     cmd,
		LogFile: logFile,
		Started: time.Now(),
	}

	m.processes[appID] = proc

	// Wait for process in goroutine (to clean up zombie)
	go func() {
		cmd.Wait()
		logFd.Close()
	}()

	return proc, nil
}

// Stop stops an MLX server
func (m *Manager) Stop(appID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, exists := m.processes[appID]
	if !exists {
		// Try to find by PID file
		return m.stopByPIDFile(appID)
	}

	// Send SIGTERM
	if proc.Cmd != nil && proc.Cmd.Process != nil {
		proc.Cmd.Process.Signal(syscall.SIGTERM)

		// Wait up to 5 seconds for graceful shutdown
		done := make(chan error, 1)
		go func() {
			done <- proc.Cmd.Wait()
		}()

		select {
		case <-done:
			// Process exited
		case <-time.After(5 * time.Second):
			// Force kill
			proc.Cmd.Process.Kill()
		}
	}

	delete(m.processes, appID)
	return nil
}

// stopByPIDFile stops a process using saved PID file
func (m *Manager) stopByPIDFile(appID string) error {
	pidFile := filepath.Join(m.baseDir, appID, "server.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return nil // No PID file, nothing to stop
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	proc.Signal(syscall.SIGTERM)
	os.Remove(pidFile)
	return nil
}

// Status returns the status of an MLX app
func (m *Manager) Status(appID string) (bool, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	proc, exists := m.processes[appID]
	if !exists {
		return false, 0, nil
	}

	if m.isProcessRunning(proc.PID) {
		return true, proc.Port, nil
	}

	return false, 0, nil
}

// GetLogs returns recent logs for an MLX app
func (m *Manager) GetLogs(appID string, lines int) ([]string, error) {
	logFile := filepath.Join(m.baseDir, appID, "logs", "server.log")

	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	// Read all lines and return last N
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

// StreamLogs streams logs for an MLX app
func (m *Manager) StreamLogs(ctx context.Context, appID string, writer io.Writer) error {
	logFile := filepath.Join(m.baseDir, appID, "logs", "server.log")

	cmd := exec.CommandContext(ctx, "tail", "-f", logFile)
	cmd.Stdout = writer
	cmd.Stderr = writer

	return cmd.Run()
}

// Cleanup removes an MLX app directory
func (m *Manager) Cleanup(appID string) error {
	m.Stop(appID)
	appDir := filepath.Join(m.baseDir, appID)
	return os.RemoveAll(appDir)
}

// isProcessRunning checks if a process is running
func (m *Manager) isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, need to check with signal 0
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// ListModels returns popular MLX models for the UI
func ListModels() []ModelInfo {
	return []ModelInfo{
		{ID: "mlx-community/Llama-3.2-3B-Instruct-4bit", Name: "Llama 3.2 3B Instruct", Size: "2GB", Description: "Fast, efficient 3B model"},
		{ID: "mlx-community/Llama-3.2-1B-Instruct-4bit", Name: "Llama 3.2 1B Instruct", Size: "0.7GB", Description: "Ultra-fast 1B model"},
		{ID: "mlx-community/Qwen2.5-7B-Instruct-4bit", Name: "Qwen 2.5 7B Instruct", Size: "4GB", Description: "Powerful 7B model"},
		{ID: "mlx-community/Qwen2.5-3B-Instruct-4bit", Name: "Qwen 2.5 3B Instruct", Size: "2GB", Description: "Balanced 3B model"},
		{ID: "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit", Name: "Qwen 2.5 Coder 7B", Size: "4GB", Description: "Coding-focused model"},
		{ID: "mlx-community/Mistral-7B-Instruct-v0.3-4bit", Name: "Mistral 7B v0.3", Size: "4GB", Description: "Strong general-purpose model"},
		{ID: "mlx-community/gemma-2-9b-it-4bit", Name: "Gemma 2 9B Instruct", Size: "5GB", Description: "Google's Gemma 2"},
		{ID: "mlx-community/Phi-3.5-mini-instruct-4bit", Name: "Phi 3.5 Mini", Size: "2GB", Description: "Microsoft's compact model"},
		{ID: "mlx-community/DeepSeek-Coder-V2-Lite-Instruct-4bit", Name: "DeepSeek Coder V2 Lite", Size: "2GB", Description: "Coding specialist"},
	}
}

// ModelInfo represents an MLX model
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	Description string `json:"description"`
}
