// Package config provides configuration management for deployer.
// All paths are relative to ~/deployer for rootless operation.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds the main configuration for deployer
type Config struct {
	// Server settings
	Server ServerConfig `yaml:"server"`

	// Auth settings
	Auth AuthConfig `yaml:"auth"`

	// Domain settings
	Domain DomainConfig `yaml:"domain"`

	// Podman settings
	Podman PodmanConfig `yaml:"podman"`

	// Database settings
	Database DatabaseConfig `yaml:"database"`
}

type AuthConfig struct {
	PasswordHash string `yaml:"password_hash"` // SHA256 hash of the password
}

type ServerConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	APIPort  int    `yaml:"api_port"`
	LogLevel string `yaml:"log_level"`
}

type DomainConfig struct {
	Root     string `yaml:"root"`      // e.g., deployer.example.com
	Base     string `yaml:"base"`      // e.g., common.al (used for app subdomains: appname.common.al)
	Suffix   string `yaml:"suffix"`    // e.g., .pod for local dev or empty for production
	Wildcard bool   `yaml:"wildcard"`  // Enable *.deployer.example.com
	Email    string `yaml:"email"`     // For Let's Encrypt
}

type PodmanConfig struct {
	SocketPath string `yaml:"socket_path"` // Auto-detected if empty
	Network    string `yaml:"network"`     // Default network name
}

type DatabaseConfig struct {
	Path string `yaml:"path"` // SQLite database path
}

// Paths holds all the directory paths used by deployer
type Paths struct {
	Base   string // ~/deployer
	Bin    string // ~/deployer/bin
	Config string // ~/deployer/config
	Data   string // ~/deployer/data
	Apps   string // ~/deployer/data/apps
	Certs  string // ~/deployer/data/certs
	Logs   string // ~/deployer/logs
	Caddy  string // ~/deployer/caddy
	Tmp    string // ~/deployer/tmp
}

// GetBaseDir returns the base directory for deployer (~/deployer)
func GetBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "deployer"), nil
}

// GetPaths returns all paths used by deployer
func GetPaths() (*Paths, error) {
	base, err := GetBaseDir()
	if err != nil {
		return nil, err
	}

	return &Paths{
		Base:   base,
		Bin:    filepath.Join(base, "bin"),
		Config: filepath.Join(base, "config"),
		Data:   filepath.Join(base, "data"),
		Apps:   filepath.Join(base, "data", "apps"),
		Certs:  filepath.Join(base, "data", "certs"),
		Logs:   filepath.Join(base, "logs"),
		Caddy:  filepath.Join(base, "caddy"),
		Tmp:    filepath.Join(base, "tmp"),
	}, nil
}

// EnsureDirectories creates all required directories
func EnsureDirectories() error {
	paths, err := GetPaths()
	if err != nil {
		return err
	}

	dirs := []string{
		paths.Base,
		paths.Bin,
		paths.Config,
		paths.Data,
		paths.Apps,
		paths.Certs,
		paths.Logs,
		paths.Caddy,
		paths.Tmp,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:     "0.0.0.0",
			Port:     443,
			APIPort:  3000,
			LogLevel: "info",
		},
		Domain: DomainConfig{
			Suffix:   ".pod", // Local development default
			Wildcard: true,
		},
		Podman: PodmanConfig{
			Network: "deployer",
		},
		Database: DatabaseConfig{
			Path: "data/deployer.db",
		},
	}
}

// GetAppDomain generates the domain for an app based on config
// For local dev: appname.pod
// For production: appname.basedomain (e.g., myapp.common.al)
func (c *Config) GetAppDomain(appName string) string {
	if c.Domain.Base != "" {
		// Production mode with base domain
		return appName + "." + c.Domain.Base
	}
	// Local development mode with suffix
	suffix := c.Domain.Suffix
	if suffix == "" {
		suffix = ".pod"
	}
	return appName + suffix
}

// Load loads the configuration from file
func Load() (*Config, error) {
	// Check for explicit config file path via environment variable
	configFile := os.Getenv("DEPLOYER_CONFIG")
	if configFile == "" {
		paths, err := GetPaths()
		if err != nil {
			return nil, err
		}
		configFile = filepath.Join(paths.Config, "deployer.yaml")
	}

	// If config doesn't exist, return defaults
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to file
func (c *Config) Save() error {
	paths, err := GetPaths()
	if err != nil {
		return err
	}

	if err := EnsureDirectories(); err != nil {
		return err
	}

	configFile := filepath.Join(paths.Config, "deployer.yaml")

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetPodmanSocket returns the Podman socket path for the current OS
func GetPodmanSocket() string {
	// Check for explicit environment variable first
	if socket := os.Getenv("PODMAN_SOCKET"); socket != "" {
		return socket
	}

	switch runtime.GOOS {
	case "linux":
		// Linux: Check XDG_RUNTIME_DIR first, then fallback
		if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
			return filepath.Join(xdgRuntime, "podman", "podman.sock")
		}
		// Fallback for Linux
		uid := os.Getuid()
		return fmt.Sprintf("/run/user/%d/podman/podman.sock", uid)

	case "darwin":
		// macOS: Try to get socket path from podman machine inspect
		if socketPath := getPodmanMachineSocket(); socketPath != "" {
			return socketPath
		}
		// Fallback: check common locations
		tmpDir := os.TempDir()
		socketPath := filepath.Join(tmpDir, "podman", "podman-machine-default-api.sock")
		if _, err := os.Stat(socketPath); err == nil {
			return socketPath
		}
		// Fallback to old location
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "containers", "podman", "machine", "podman.sock")

	case "windows":
		// Windows: Named pipe (through WSL2)
		return `\\.\pipe\podman-machine-default`

	default:
		return ""
	}
}

// getPodmanMachineSocket tries to get the socket path from podman machine inspect
func getPodmanMachineSocket() string {
	cmd := exec.Command("podman", "machine", "inspect", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	var machines []struct {
		ConnectionInfo struct {
			PodmanSocket struct {
				Path string `json:"Path"`
			} `json:"PodmanSocket"`
		} `json:"ConnectionInfo"`
	}

	if err := json.Unmarshal(output, &machines); err != nil {
		return ""
	}

	if len(machines) > 0 && machines[0].ConnectionInfo.PodmanSocket.Path != "" {
		socketPath := machines[0].ConnectionInfo.PodmanSocket.Path
		if _, err := os.Stat(socketPath); err == nil {
			return socketPath
		}
	}

	return ""
}
