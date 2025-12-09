// Package main is the entry point for the deployer server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deployer/deployer/internal/api"
	"github.com/deployer/deployer/internal/config"
	"github.com/deployer/deployer/internal/podman"
	"github.com/deployer/deployer/internal/storage"
)

var (
	version = "0.1.0"
)

func main() {
	// Parse command line flags
	var (
		showVersion = flag.Bool("version", false, "Show version")
		port        = flag.Int("port", 3000, "API server port")
		host        = flag.String("host", "0.0.0.0", "API server host")
		setup       = flag.Bool("setup", false, "Run initial setup")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("deployer version %s\n", version)
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

	// Initialize Podman client
	pm, err := podman.NewClient()
	if err != nil {
		log.Printf("Warning: Failed to connect to Podman: %v", err)
		log.Printf("Podman socket expected at: %s", config.GetPodmanSocket())
		log.Printf("Make sure Podman is running (try: podman system service --time=0 &)")
	}

	// Create API server
	apiServer := api.NewServer(store, pm)

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
	fmt.Printf("  %s/bin/deployer\n", paths.Base)
	fmt.Println()
	fmt.Println("Or run directly:")
	fmt.Println("  go run ./cmd/deployer")
}
