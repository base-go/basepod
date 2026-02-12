// Package main is the entry point for the basepod server daemon.
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

	"github.com/base-go/basepod/internal/api"
	"github.com/base-go/basepod/internal/caddy"
	"github.com/base-go/basepod/internal/config"
	"github.com/base-go/basepod/internal/dns"
	"github.com/base-go/basepod/internal/imagesync"
	"github.com/base-go/basepod/internal/podman"
	"github.com/base-go/basepod/internal/storage"
	"github.com/base-go/basepod/internal/web"
)

var (
	version = "2.0.19"

	// Release URL for updates (uses GitHub releases API)
	releaseBaseURL = "https://github.com/base-go/basepod/releases/latest/download"
)

func main() {
	// Custom usage output
	flag.Usage = printUsage

	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "start":
			runStart()
			return
		case "stop":
			runStop()
			return
		case "status":
			runStatus()
			return
		case "restart":
			runRestart()
			return
		case "update":
			runUpdate()
			return
		case "version":
			fmt.Printf("basepod version %s\n", version)
			return
		case "help", "--help", "-h":
			printUsage()
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
		fmt.Printf("basepod version %s\n", version)
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

	// Configure WebUI path if set in config
	if cfg.WebUI.Path != "" {
		web.SetWebUIPath(cfg.WebUI.Path)
		log.Printf("WebUI path set to: %s", cfg.WebUI.Path)
	}

	// Initialize storage
	store, err := storage.New()
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	// Start image tag syncer (syncs Docker Hub tags for templates)
	tagSyncer := imagesync.NewSyncer(store)
	tagSyncer.Start()
	defer tagSyncer.Stop()

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

		// Ensure basepod network exists for inter-container communication
		networkCtx, networkCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := pm.CreateNetwork(networkCtx, "basepod"); err != nil {
			// Ignore "already exists" error
			if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "network already exists") {
				log.Printf("Warning: Failed to create basepod network: %v", err)
			}
		} else {
			log.Printf("Basepod network created")
		}
		networkCancel()
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

	// Initialize Caddy HTTP server and sync routes for running apps
	if caddyClient != nil {
		if err := initializeCaddyRoutes(caddyClient, store); err != nil {
			log.Printf("Warning: Failed to initialize Caddy routes: %v", err)
		}
		// Enable Caddy access logging (logs go to stderr which launchd captures to caddy.err)
		if err := caddyClient.EnableAccessLog(); err != nil {
			log.Printf("Warning: Failed to enable Caddy access logging: %v", err)
		} else {
			log.Printf("Caddy access logging enabled (via stderr)")
		}
	}

	// Start built-in DNS server if enabled or if using local domain suffix
	var dnsServer *dns.Server
	// Determine DNS domain: use Base if set, otherwise use Suffix (strip leading dot)
	dnsDomain := cfg.Domain.Base
	if dnsDomain == "" && cfg.Domain.Suffix != "" {
		dnsDomain = strings.TrimPrefix(cfg.Domain.Suffix, ".")
	}
	// Auto-enable DNS for local development domains (non-standard TLDs)
	isLocalDomain := dnsDomain != "" && !strings.Contains(dnsDomain, ".com") && !strings.Contains(dnsDomain, ".net") && !strings.Contains(dnsDomain, ".org") && !strings.Contains(dnsDomain, ".io")
	if cfg.DNS.Enabled || isLocalDomain {
		dnsPort := cfg.DNS.Port
		if dnsPort == 0 {
			dnsPort = 5353 // Use non-privileged port by default
		}
		dnsServer, err = dns.NewServer(dns.Config{
			Domain:   dnsDomain,
			ServerIP: "127.0.0.2", // Local development (separate from 127.0.0.1 to avoid conflicts)
			Port:     dnsPort,
			Upstream: cfg.DNS.Upstream,
		})
		if err != nil {
			log.Printf("Warning: Failed to create DNS server: %v", err)
		} else {
			if err := dnsServer.Start(); err != nil {
				log.Printf("Warning: Failed to start DNS server: %v", err)
			} else {
				log.Printf("DNS server started - configure clients to use this server's IP as DNS on port %d", dnsPort)
			}
		}
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
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Basepod server starting on %s", addr)
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

	// Stop DNS server if running
	if dnsServer != nil {
		dnsServer.Stop()
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}

func runSetup(paths *config.Paths) {
	fmt.Println("=== Basepod Setup ===")
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
	fmt.Printf("Config saved to: %s/config/basepod.yaml\n", paths.Base)

	// Initialize storage
	_, err = storage.New()
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Database initialized: %s/data/basepod.db\n", paths.Base)

	fmt.Println()
	fmt.Println("Setup complete! Start the server with:")
	fmt.Printf("  %s/bin/basepod\n", paths.Base)
	fmt.Println()
	fmt.Println("Or run directly:")
	fmt.Println("  go run ./cmd/basepod")
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
		}

		// Wait for the API socket to appear (gvproxy can be slow)
		socketPath := config.GetPodmanSocket()
		if socketPath != "" {
			log.Printf("Waiting for Podman API socket: %s", socketPath)
			for i := 0; i < 30; i++ {
				if _, err := os.Stat(socketPath); err == nil {
					log.Printf("Podman API socket ready")
					return nil
				}
				time.Sleep(1 * time.Second)
			}
			log.Printf("Warning: Podman API socket not found after 30s, continuing anyway")
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

// initializeCaddyRoutes sets up the Caddy HTTP server and syncs routes for all running apps
func initializeCaddyRoutes(caddyClient *caddy.Client, store *storage.Storage) error {
	// Get all apps
	apps, err := store.ListApps()
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	paths, _ := config.GetPaths()

	// Collect routes for running apps with domains
	var routes []caddy.Route
	var staticCount, aliasCount int
	for _, a := range apps {
		if a.Status != "running" || a.Domain == "" {
			continue
		}

		// Handle static sites
		if a.Type == "static" {
			staticDir := fmt.Sprintf("%s/data/apps/%s", paths.Base, a.Name)
			if err := caddyClient.AddStaticRoute(a.Domain, staticDir); err != nil {
				log.Printf("Warning: Failed to add static route for %s: %v", a.Name, err)
			} else {
				staticCount++
			}
			// Add static routes for aliases
			for _, alias := range a.Aliases {
				if err := caddyClient.AddStaticRoute(alias, staticDir); err != nil {
					log.Printf("Warning: Failed to add static alias route for %s: %v", alias, err)
				} else {
					aliasCount++
				}
			}
			continue
		}

		// Handle container apps
		if a.Ports.HostPort > 0 {
			routes = append(routes, caddy.Route{
				ID:        "basepod-" + a.Name,
				Domain:    a.Domain,
				Upstream:  fmt.Sprintf("127.0.0.1:%d", a.Ports.HostPort),
				EnableSSL: a.SSL.Enabled,
			})
			// Add routes for aliases
			for _, alias := range a.Aliases {
				routes = append(routes, caddy.Route{
					ID:        fmt.Sprintf("alias-%s-%s", a.ID[:8], alias),
					Domain:    alias,
					Upstream:  fmt.Sprintf("127.0.0.1:%d", a.Ports.HostPort),
					EnableSSL: a.SSL.Enabled,
				})
				aliasCount++
			}
		}
	}

	// Initialize container routes
	if len(routes) > 0 {
		if err := caddyClient.InitializeServer(routes); err != nil {
			return fmt.Errorf("failed to initialize Caddy server: %w", err)
		}
	}

	log.Printf("Configured Caddy with %d app routes, %d static sites, and %d aliases", len(routes), staticCount, aliasCount)
	return nil
}

// printUsage displays the custom help output with subcommands and flags
func printUsage() {
	fmt.Fprintf(os.Stderr, `basepod - Container PaaS platform

Usage:
  basepod [command]
  basepod [flags]

Commands:
  start       Start the basepod service
  stop        Stop the basepod service
  restart     Restart the basepod service
  status      Show service status
  update      Update to latest version
  version     Show version
  help        Show this help

Flags:
`)
	flag.PrintDefaults()
}

// runStart starts the basepod service using the system service manager
func runStart() {
	fmt.Println("Starting basepod...")

	if runtime.GOOS == "darwin" {
		plistPath := "/Library/LaunchDaemons/com.basepod.plist"
		cmd := exec.Command("launchctl", "load", "-w", plistPath)
		if err := cmd.Run(); err != nil {
			// Try bootstrap (newer macOS)
			cmd = exec.Command("launchctl", "bootstrap", "system", plistPath)
			if err := cmd.Run(); err != nil {
				fmt.Println("No launchd service found.")
				fmt.Println("If running manually, start with: basepod")
				fmt.Println("If using Homebrew services: brew services start basepod")
				os.Exit(1)
			}
		}
		fmt.Println("Basepod started successfully.")
		return
	}

	// Linux: try system-level systemctl first
	cmd := exec.Command("systemctl", "start", "basepod")
	if err := cmd.Run(); err != nil {
		// Try user-level systemd
		cmd = exec.Command("systemctl", "--user", "start", "basepod")
		if err := cmd.Run(); err != nil {
			fmt.Printf("Error: failed to start basepod service: %v\n", err)
			fmt.Println("You may need to run with sudo: sudo systemctl start basepod")
			os.Exit(1)
		}
	}
	fmt.Println("Basepod started successfully.")
}

// runStop stops the basepod service using the system service manager
func runStop() {
	fmt.Println("Stopping basepod...")

	if runtime.GOOS == "darwin" {
		plistPath := "/Library/LaunchDaemons/com.basepod.plist"
		cmd := exec.Command("launchctl", "unload", plistPath)
		if err := cmd.Run(); err != nil {
			// Try bootout (newer macOS)
			cmd = exec.Command("launchctl", "bootout", "system/com.basepod")
			if err := cmd.Run(); err != nil {
				fmt.Println("No launchd service found.")
				fmt.Println("If running manually, stop the process with Ctrl+C or kill.")
				fmt.Println("If using Homebrew services: brew services stop basepod")
				os.Exit(1)
			}
		}
		fmt.Println("Basepod stopped successfully.")
		return
	}

	// Linux: try system-level systemctl first
	cmd := exec.Command("systemctl", "stop", "basepod")
	if err := cmd.Run(); err != nil {
		// Try user-level systemd
		cmd = exec.Command("systemctl", "--user", "stop", "basepod")
		if err := cmd.Run(); err != nil {
			fmt.Printf("Error: failed to stop basepod service: %v\n", err)
			fmt.Println("You may need to run with sudo: sudo systemctl stop basepod")
			os.Exit(1)
		}
	}
	fmt.Println("Basepod stopped successfully.")
}

// runStatus shows the current status of the basepod service
func runStatus() {
	fmt.Printf("Basepod v%s\n\n", version)

	serviceRunning := false

	if runtime.GOOS == "darwin" {
		cmd := exec.Command("launchctl", "list", "com.basepod")
		output, err := cmd.CombinedOutput()
		if err == nil {
			fmt.Println("Service: running (launchd)")
			// Parse PID from launchctl output
			for _, line := range strings.Split(string(output), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "\"PID\"") || strings.Contains(line, "PID") {
					fmt.Printf("  %s\n", line)
				}
			}
			serviceRunning = true
		} else {
			fmt.Println("Service: not registered with launchd")
		}
	} else {
		cmd := exec.Command("systemctl", "is-active", "basepod")
		output, err := cmd.Output()
		status := strings.TrimSpace(string(output))
		if err == nil && status == "active" {
			fmt.Println("Service: running (systemd)")
			serviceRunning = true
			// Show more details
			cmd = exec.Command("systemctl", "show", "basepod", "--property=MainPID,ActiveEnterTimestamp")
			if details, err := cmd.Output(); err == nil {
				for _, line := range strings.Split(string(details), "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						fmt.Printf("  %s\n", line)
					}
				}
			}
		} else {
			// Try user-level
			cmd = exec.Command("systemctl", "--user", "is-active", "basepod")
			output, err = cmd.Output()
			status = strings.TrimSpace(string(output))
			if err == nil && status == "active" {
				fmt.Println("Service: running (systemd user)")
				serviceRunning = true
			} else {
				fmt.Println("Service: not running (systemd)")
			}
		}
	}

	// Health check via HTTP
	fmt.Println()
	port := 3000
	// Try to read port from config
	if cfg, err := config.Load(); err == nil && cfg.Server.APIPort > 0 {
		port = cfg.Server.APIPort
	}

	healthURL := fmt.Sprintf("http://localhost:%d/api/health", port)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthURL)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			fmt.Printf("API: responding on port %d\n", port)
			serviceRunning = true
		} else {
			fmt.Printf("API: unhealthy (status %d) on port %d\n", resp.StatusCode, port)
		}
	} else {
		fmt.Printf("API: not responding on port %d\n", port)
	}

	if !serviceRunning {
		fmt.Println("\nBasepod is not running. Start with: basepod start")
	}
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
	apiURL := "https://api.github.com/repos/base-go/basepod/releases/latest"
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

	latestVersion := normalizeVersion(strings.TrimPrefix(release.TagName, "v"))
	currentVersion := normalizeVersion(strings.TrimPrefix(version, "v"))

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Printf("Latest version:  %s\n", latestVersion)

	// Compare versions
	if latestVersion == currentVersion {
		fmt.Println("You are already running the latest version.")
		return
	}

	// Validate version format
	if !isValidVersion(latestVersion) {
		fmt.Printf("Error: invalid version format: %s\n", latestVersion)
		os.Exit(1)
	}

	fmt.Println("Downloading update...")

	// Determine binary name based on OS and arch
	binaryName := fmt.Sprintf("basepod-%s-%s", runtime.GOOS, runtime.GOARCH)
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
	tmpFile, err := os.CreateTemp("", "basepod-update-*")
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

// runRestart restarts the basepod service based on OS
func runRestart() {
	fmt.Println("Restarting basepod...")

	if runtime.GOOS == "darwin" {
		// macOS: Try launchctl first, then suggest manual restart
		cmd := exec.Command("launchctl", "kickstart", "-k", "system/com.basepod.basepod")
		if err := cmd.Run(); err != nil {
			// Try user-level service
			cmd = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/com.basepod.basepod", os.Getuid()))
			if err := cmd.Run(); err != nil {
				fmt.Println("No launchd service found.")
				fmt.Println("If running manually, restart the process.")
				fmt.Println("If using Homebrew services: brew services restart basepod")
				os.Exit(0)
			}
		}
		fmt.Println("Basepod restarted successfully.")
		return
	}

	// Linux: Use systemctl - try 'basepod' first, then 'basepod'
	cmd := exec.Command("systemctl", "restart", "basepod")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Try basepod service name
		cmd = exec.Command("systemctl", "restart", "basepod")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Try user-level systemd
			cmd = exec.Command("systemctl", "--user", "restart", "basepod")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Error: failed to restart basepod service: %v\n", err)
				fmt.Println("You may need to run with sudo: sudo systemctl restart basepod")
				os.Exit(1)
			}
		}
	}
	fmt.Println("Basepod restarted successfully.")
}

// normalizeVersion converts version to x.x.x format
// "1" -> "1.0.0", "1.2" -> "1.2.0", "1.2.3" -> "1.2.3"
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")

	// Filter out non-numeric parts
	var numericParts []string
	for _, p := range parts {
		if isNumeric(p) {
			numericParts = append(numericParts, p)
		}
	}

	if len(numericParts) == 0 {
		return "0.0.0"
	}

	// Pad to 3 parts
	for len(numericParts) < 3 {
		numericParts = append(numericParts, "0")
	}

	return strings.Join(numericParts[:3], ".")
}

// isValidVersion checks if version matches x.x.x format
func isValidVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if !isNumeric(p) {
			return false
		}
	}
	return true
}

// isNumeric checks if string contains only digits
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
