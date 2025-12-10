// Package main is the entry point for the deployerd server daemon.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/deployer/deployer/internal/api"
	"github.com/deployer/deployer/internal/caddy"
	"github.com/deployer/deployer/internal/config"
	"github.com/deployer/deployer/internal/podman"
	"github.com/deployer/deployer/internal/storage"
)

var (
	version = "0.1.27"

	// Release URL for updates (uses GitHub releases API)
	releaseBaseURL = "https://github.com/base-go/dr/releases/latest/download"
)

func main() {
	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update":
			runUpdate()
			return
		case "restart":
			runRestart()
			return
		case "version":
			fmt.Printf("deployerd version %s\n", version)
			return
		}
	}

	// Parse command line flags
	var (
		showVersion = flag.Bool("version", false, "Show version")
		port        = flag.Int("port", 3000, "API server port")
		host        = flag.String("host", "0.0.0.0", "API server host")
		setup       = flag.Bool("setup", false, "Run initial setup")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("deployerd version %s\n", version)
		os.Exit(0)
	}

	// Ensure directories exist
	if err := config.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	paths, err := config.GetPaths()
	if err != nil {
		log.Fatalf("Failed to get paths: %v", err)
	}

	if *setup {
		runSetup(paths)
		return
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize storage
	store, err := storage.New()
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Initialize Podman client (auto-start if needed)
	log.Printf("Connecting to Podman...")
	if err := ensurePodmanRunning(); err != nil {
		log.Printf("Warning: Failed to ensure Podman is running: %v", err)
	}

	pm, err := podman.NewClient()
	if err != nil {
		log.Printf("Warning: Failed to connect to Podman: %v", err)
		log.Printf("Please start Podman manually: podman machine start")
	} else {
		// Verify connection with ping
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if pingErr := pm.Ping(ctx); pingErr != nil {
			log.Printf("Warning: Podman ping failed: %v", pingErr)
		} else {
			log.Printf("Podman connected successfully")
		}
		cancel()
	}

	// Initialize Caddy client (auto-start if needed)
	caddyURL := os.Getenv("CADDY_ADMIN_URL")
	if caddyURL == "" {
		caddyURL = "http://localhost:2019"
	}
	caddyClient := caddy.NewClient(caddyURL)
	if err := caddyClient.Ping(); err != nil {
		log.Printf("Caddy not running, attempting to start...")
		if startErr := ensureCaddyRunning(); startErr != nil {
			log.Printf("Warning: Failed to start Caddy: %v", startErr)
			caddyClient = nil
		} else {
			// Retry ping
			time.Sleep(1 * time.Second)
			if err := caddyClient.Ping(); err != nil {
				log.Printf("Warning: Still failed to connect to Caddy: %v", err)
				caddyClient = nil
			} else {
				log.Printf("Caddy started successfully")
			}
		}
	} else {
		log.Printf("Caddy connected successfully")
	}

	// Create API server with version
	apiServer := api.NewServerWithVersion(store, pm, caddyClient, version)

	// Override port from flag
	if *port != 0 {
		cfg.Server.APIPort = *port
	}

	addr := fmt.Sprintf("%s:%d", *host, cfg.Server.APIPort)

	// Create HTTP server
	server := &http.Server{
		Addr:         addr,
		Handler:      apiServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Deployer server starting on %s", addr)
		log.Printf("Base directory: %s", paths.Base)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func runSetup(paths *config.Paths) {
	fmt.Println("=== Deployer Setup ===")
	fmt.Printf("Base directory: %s\n", paths.Base)
	fmt.Println()

	// Check Podman
	fmt.Print("Checking Podman... ")
	pm, err := podman.NewClient()
	if err != nil {
		fmt.Printf("NOT FOUND\n")
		fmt.Printf("  Error: %v\n", err)
		fmt.Printf("  Socket: %s\n", config.GetPodmanSocket())
		fmt.Println()
		fmt.Println("To start Podman socket:")
		fmt.Println("  podman system service --time=0 &")
		fmt.Println()
	} else {
		if err := pm.Ping(context.Background()); err != nil {
			fmt.Printf("ERROR\n")
			fmt.Printf("  %v\n", err)
		} else {
			fmt.Printf("OK\n")
		}
	}

	// Create default config
	cfg := config.DefaultConfig()
	if err := cfg.Save(); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Config saved to: %s/config/deployer.yaml\n", paths.Base)

	// Initialize storage
	_, err = storage.New()
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Database initialized: %s/data/deployer.db\n", paths.Base)

	fmt.Println()
	fmt.Println("Setup complete! Start the server with:")
	fmt.Printf("  %s/bin/deployerd\n", paths.Base)
	fmt.Println()
	fmt.Println("Or run directly:")
	fmt.Println("  go run ./cmd/deployerd")
}

// ensurePodmanRunning starts Podman machine if not running (macOS) or service (Linux)
func ensurePodmanRunning() error {
	if runtime.GOOS == "darwin" {
		// Check if machine is running
		cmd := exec.Command("podman", "machine", "list", "--format", "{{.Running}}")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to check Podman machine status: %w", err)
		}

		if !strings.Contains(string(output), "true") {
			log.Printf("Starting Podman machine...")
			startCmd := exec.Command("podman", "machine", "start")
			startCmd.Stdout = os.Stdout
			startCmd.Stderr = os.Stderr
			if err := startCmd.Run(); err != nil {
				return fmt.Errorf("failed to start Podman machine: %w", err)
			}
			// Wait for machine to be ready
			time.Sleep(5 * time.Second)
		}
		return nil
	}

	// Linux: start podman socket service
	cmd := exec.Command("systemctl", "--user", "start", "podman.socket")
	if err := cmd.Run(); err != nil {
		// Try without systemd
		cmd = exec.Command("podman", "system", "service", "--time=0")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start Podman service: %w", err)
		}
	}
	return nil
}

// ensureCaddyRunning starts Caddy in the background
func ensureCaddyRunning() error {
	// Try to find caddy in PATH or common locations
	caddyPath, err := exec.LookPath("caddy")
	if err != nil {
		// Try common paths on macOS
		commonPaths := []string{
			"/opt/homebrew/bin/caddy",
			"/usr/local/bin/caddy",
			"/usr/bin/caddy",
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				caddyPath = p
				break
			}
		}
		if caddyPath == "" {
			return fmt.Errorf("caddy not found. Install with: brew install caddy")
		}
	}

	// Start caddy with default config (admin API only)
	cmd := exec.Command(caddyPath, "start")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start Caddy: %w", err)
	}

	return nil
}

// runUpdate checks for and installs the latest version
func runUpdate() {
	fmt.Println("Checking for updates...")

	// Get current binary path
	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}

	// Fetch latest release info from GitHub API
	apiURL := "https://api.github.com/repos/base-go/dr/releases/latest"
	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Printf("Error: cannot check for updates: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: cannot fetch release info (status %d)\n", resp.StatusCode)
		os.Exit(1)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Printf("Error: cannot parse release info: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Printf("Latest version:  %s\n", latestVersion)

	// Compare versions
	if latestVersion == currentVersion {
		fmt.Println("You are already running the latest version.")
		return
	}

	fmt.Println("Downloading update...")

	// Determine binary name based on OS and arch
	binaryName := fmt.Sprintf("deployerd-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := releaseBaseURL + "/" + binaryName

	// Download new binary
	resp, err = http.Get(downloadURL)
	if err != nil {
		fmt.Printf("Error: cannot download update: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: binary not available for %s/%s (status %d)\n", runtime.GOOS, runtime.GOARCH, resp.StatusCode)
		os.Exit(1)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "deployer-update-*")
	if err != nil {
		fmt.Printf("Error: cannot create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()

	// Download to temp file
	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		fmt.Printf("Error: cannot write update: %v\n", err)
		os.Exit(1)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		fmt.Printf("Error: cannot set permissions: %v\n", err)
		os.Exit(1)
	}

	// Backup old binary
	backupPath := execPath + ".bak"
	if err := os.Rename(execPath, backupPath); err != nil {
		os.Remove(tmpPath)
		fmt.Printf("Error: cannot backup current binary: %v\n", err)
		fmt.Println("You may need to run with sudo")
		os.Exit(1)
	}

	// Move new binary into place
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Restore backup
		os.Rename(backupPath, execPath)
		os.Remove(tmpPath)
		fmt.Printf("Error: cannot install update: %v\n", err)
		fmt.Println("You may need to run with sudo")
		os.Exit(1)
	}

	// Remove backup
	os.Remove(backupPath)

	fmt.Printf("Successfully updated to %s\n", latestVersion)
	fmt.Println("Restarting service...")

	// Auto-restart the service
	runRestart()
}

// runRestart restarts the deployer service based on OS
func runRestart() {
	fmt.Println("Restarting deployer...")

	if runtime.GOOS == "darwin" {
		// macOS: Try launchctl first, then suggest manual restart
		cmd := exec.Command("launchctl", "kickstart", "-k", "system/com.deployer.deployer")
		if err := cmd.Run(); err != nil {
			// Try user-level service
			cmd = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/com.deployer.deployer", os.Getuid()))
			if err := cmd.Run(); err != nil {
				fmt.Println("No launchd service found.")
				fmt.Println("If running manually, restart the process.")
				fmt.Println("If using Homebrew services: brew services restart deployer")
				os.Exit(0)
			}
		}
		fmt.Println("Deployer restarted successfully.")
		return
	}

	// Linux: Use systemctl - try 'deployer' first, then 'deployerd'
	cmd := exec.Command("systemctl", "restart", "deployer")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Try deployerd service name
		cmd = exec.Command("systemctl", "restart", "deployerd")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Try user-level systemd
			cmd = exec.Command("systemctl", "--user", "restart", "deployer")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Error: failed to restart deployer service: %v\n", err)
				fmt.Println("You may need to run with sudo: sudo systemctl restart deployer")
				os.Exit(1)
			}
		}
	}
	fmt.Println("Deployer restarted successfully.")
}
