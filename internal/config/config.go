// Package config provides configuration management for basepod.
// All paths are relative to ~/.basepod for rootless operation.
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

// Config holds the main configuration for basepod
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

	// WebUI settings
	WebUI WebUIConfig `yaml:"webui"`

	// DNS settings
	DNS DNSConfig `yaml:"dns"`
}

// DNSConfig holds DNS server configuration
type DNSConfig struct {
	Enabled  bool     `yaml:"enabled"`   // Enable built-in DNS server
	Port     int      `yaml:"port"`      // DNS port (default 53, use 5353 for non-root)
	Upstream []string `yaml:"upstream"`  // Upstream DNS servers
}

type WebUIConfig struct {
	// Path to serve static files from disk (empty = use embedded)
	Path string `yaml:"path"`
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
	Root     string `yaml:"root"`     // Production: root domain (e.g., app.basecode.al) - apps become {name}.app.basecode.al
	Base     string `yaml:"base"`     // Base domain for subdomains (e.g., base.code) - apps become {name}.base.code
	Suffix   string `yaml:"suffix"`   // Local dev: domain suffix (e.g., .pod) - apps become {name}.pod
	Wildcard bool   `yaml:"wildcard"` // Enable wildcard subdomains
	Email    string `yaml:"email"`    // For Let's Encrypt SSL certificates
}

type PodmanConfig struct {
	SocketPath string `yaml:"socket_path"` // Auto-detected if empty
	Network    string `yaml:"network"`     // Default network name
}

type DatabaseConfig struct {
	Path string `yaml:"path"` // SQLite database path
}

// Paths holds all the directory paths used by basepod
type Paths struct {
	Base   string // ~/.basepod
	Bin    string // ~/.basepod/bin
	Config string // ~/.basepod/config
	Data   string // ~/.basepod/data
	Apps   string // ~/.basepod/data/apps
	Certs  string // ~/.basepod/data/certs
	Logs   string // ~/.basepod/logs
	Caddy  string // ~/.basepod/caddy
	Tmp    string // ~/.basepod/tmp
}

// GetBaseDir returns the base directory for basepod (~/.basepod)
func GetBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "basepod"), nil
}

// GetPaths returns all paths used by basepod
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
			Root:     "",          // Production: set to your domain (e.g., app.basecode.al)
			Suffix:   ".base.code", // Local dev fallback
			Wildcard: true,
		},
		Podman: PodmanConfig{
			Network: "basepod",
		},
		Database: DatabaseConfig{
			Path: "data/basepod.db",
		},
	}
}

// GetAppDomain generates the domain for an app
// Production: {appname}.{root} (e.g., myapp.app.basecode.al)
// Local dev:  {appname}{suffix} (e.g., myapp.base.code)
func (c *Config) GetAppDomain(appName string) string {
	if c.Domain.Root != "" {
		// Production mode with root domain
		return appName + "." + c.Domain.Root
	}
	// Local development mode with suffix
	suffix := c.Domain.Suffix
	if suffix == "" {
		suffix = ".base.code"
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
		configFile = filepath.Join(paths.Config, "basepod.yaml")
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

	configFile := filepath.Join(paths.Config, "basepod.yaml")

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
		// Check for rootful podman socket first (running as root)
		if os.Getuid() == 0 {
			rootfulSocket := "/run/podman/podman.sock"
			if _, err := os.Stat(rootfulSocket); err == nil {
				return rootfulSocket
			}
		}
		// Linux: Check XDG_RUNTIME_DIR first, then fallback
		if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
			return filepath.Join(xdgRuntime, "podman", "podman.sock")
		}
		// Fallback for Linux (rootless)
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
