// Package main is the entry point for the deployer CLI.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/base-go/basepod/internal/app"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var (
	version = "1.0.66"
)

// ServerConfig holds configuration for a single server
type ServerConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token,omitempty"`
}

// CLIConfig holds CLI configuration with multiple servers
type CLIConfig struct {
	CurrentContext string                  `yaml:"current_context"`
	Servers        map[string]ServerConfig `yaml:"servers"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Check for updates in background (skip for version/upgrade commands)
	if cmd != "version" && cmd != "-v" && cmd != "--version" && cmd != "upgrade" {
		go checkForUpdates()
	}

	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("bp version %s\n", version)
		checkForUpdatesSync() // Show update notice after version
	case "help", "-h", "--help":
		printUsage()
	// Connection commands
	case "login":
		cmdLogin(args)
	case "logout":
		cmdLogout(args)
	case "context", "ctx":
		cmdContext(args)
	// Project commands
	case "init":
		cmdInit(args)
	case "run":
		cmdRun(args)
	case "deploy":
		cmdDeploy(args)
	case "push":
		// Deprecated: use deploy instead
		fmt.Println("Note: 'bp push' is deprecated. Use 'bp deploy' instead.")
		cmdDeploy(args)
	// App commands
	case "apps", "app", "list", "ls":
		cmdApps(args)
	case "create":
		cmdCreate(args)
	case "start":
		cmdStart(args)
	case "stop":
		cmdStop(args)
	case "restart":
		cmdRestart(args)
	case "logs":
		cmdLogs(args)
	case "delete", "rm":
		cmdDelete(args)
	// Template commands
	case "templates":
		cmdTemplates(args)
	case "template":
		cmdTemplate(args)
	// Model commands (LLM)
	case "models":
		cmdModels(args)
	case "model":
		cmdModel(args)
	case "chat":
		cmdChat(args)
	// System commands
	case "info":
		cmdInfo(args)
	case "status":
		cmdStatus(args)
	case "prune":
		cmdPrune(args)
	case "upgrade":
		cmdUpgrade(args)
	case "backup":
		cmdBackup(args)
	case "completion":
		cmdCompletion(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`bp - CLI for Basepod PaaS

Usage:
  bp <command> [arguments] [flags]

Connection Commands:
  login <server>          Connect to a Basepod server
  logout [name]           Disconnect from server
  context [name]          List or switch server contexts

Project Commands:
  init                    Initialize basepod.yaml config
  run [path]              Run app locally with Podman
  deploy [path]           Deploy app (local, image, or git)

App Commands:
  apps                    List all apps
  create <name>           Create a new app
  start <name>            Start an app
  stop <name>             Stop an app
  restart <name>          Restart an app
  logs <name>             View app logs
  delete <name>           Delete an app

Template Commands:
  templates               List available templates
  template deploy <name>  Deploy a template
  template export <name>  Export app config as template

Model Commands (LLM):
  models                  List LLM models
  model pull <model>      Download a model
  model run <model>       Start LLM server
  model stop              Stop LLM server
  model rm <model>        Delete a model
  chat                    Chat with running model

System Commands:
  info                    Show server info
  status                  Show detailed status
  prune                   Clean unused resources
  upgrade                 Update Basepod
  backup                  Create or list backups
  backup list             List all backups
  backup create           Create a new backup
  backup download <id>    Download a backup
  backup delete <id>      Delete a backup
  completion <shell>      Generate shell completion (bash, zsh, fish)

Options:
  -h, --help              Show help
  -v, --version           Show version

Examples:
  bp login bp.example.com
  bp init
  bp run                           # Run app locally with Podman
  bp run -d                        # Run in background (detached)
  bp run -p 8080                   # Run on custom port
  bp deploy                        # Deploy from local source
  bp deploy --image nginx:latest   # Deploy Docker image
  bp template deploy postgres
  bp model pull Llama-3.2-3B
  bp chat`)
}

// getConfigPath returns the path to the CLI config file
func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".basepod.yaml")
}

// loadConfig loads the CLI configuration
func loadConfig() (*CLIConfig, error) {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIConfig{Servers: make(map[string]ServerConfig)}, nil
		}
		return nil, err
	}

	var cfg CLIConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Servers == nil {
		cfg.Servers = make(map[string]ServerConfig)
	}

	return &cfg, nil
}

// saveConfig saves the CLI configuration
func saveConfig(cfg *CLIConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(getConfigPath(), data, 0600)
}

// getCurrentServer returns the current server config
func getCurrentServer(cfg *CLIConfig) (*ServerConfig, string, error) {
	if cfg.CurrentContext == "" {
		return nil, "", fmt.Errorf("not logged in. Run: bp login <server>")
	}

	server, ok := cfg.Servers[cfg.CurrentContext]
	if !ok {
		return nil, "", fmt.Errorf("context '%s' not found. Run: bp context", cfg.CurrentContext)
	}

	return &server, cfg.CurrentContext, nil
}

// getClient returns an HTTP client configured for the current server
func getClient() (*http.Client, string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, "", err
	}

	server, _, err := getCurrentServer(cfg)
	if err != nil {
		return nil, "", err
	}

	client := &http.Client{
		Timeout: 5 * time.Minute, // Longer timeout for uploads
	}

	return client, server.URL, nil
}

// apiRequest makes an API request
func apiRequest(method, path string, body interface{}) (*http.Response, error) {
	client, server, err := getClient()
	if err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(server, "/") + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	cfg, _ := loadConfig()
	if server, _, err := getCurrentServer(cfg); err == nil && server.Token != "" {
		req.Header.Set("Authorization", "Bearer "+server.Token)
	}

	return client.Do(req)
}

func cmdLogin(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp login <server>")
		os.Exit(1)
	}

	server := args[0]
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		server = "https://" + server
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Test connection
	resp, err := client.Get(server + "/api/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to server: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Server returned status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	// Check if auth is required
	resp, err = client.Get(server + "/api/auth/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check auth status: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var authStatus struct {
		NeedsSetup    bool `json:"needsSetup"`
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authStatus); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse auth status: %v\n", err)
		os.Exit(1)
	}

	// Load existing config or create new one
	cfg, err := loadConfig()
	if err != nil {
		cfg = &CLIConfig{Servers: make(map[string]ServerConfig)}
	}

	// Extract context name from server URL (hostname without protocol)
	contextName := strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")
	contextName = strings.Split(contextName, "/")[0] // Remove any path

	serverCfg := ServerConfig{URL: server}

	// Auth is required if password is configured (needsSetup=false) and not authenticated
	authRequired := !authStatus.NeedsSetup && !authStatus.Authenticated

	// If auth is required, prompt for password
	if authRequired {
		fmt.Print("Password: ")
		passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // newline after password input
		if err != nil {
			// Fallback for non-terminal input
			reader := bufio.NewReader(os.Stdin)
			password, _ := reader.ReadString('\n')
			passwordBytes = []byte(strings.TrimSpace(password))
		}

		// Authenticate
		loginReq := map[string]string{"password": string(passwordBytes)}
		loginBody, _ := json.Marshal(loginReq)
		resp, err = client.Post(server+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to authenticate: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			fmt.Fprintln(os.Stderr, "Invalid password")
			os.Exit(1)
		}
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Authentication failed: status %d\n", resp.StatusCode)
			os.Exit(1)
		}

		var loginResp struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse login response: %v\n", err)
			os.Exit(1)
		}

		serverCfg.Token = loginResp.Token
	}

	// Add server to config and set as current context
	cfg.Servers[contextName] = serverCfg
	cfg.CurrentContext = contextName

	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Logged in to %s (context: %s)\n", server, contextName)
}

func cmdLogout(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Println("Not logged in")
		return
	}

	// Determine which context to logout from
	contextName := cfg.CurrentContext
	if len(args) > 0 {
		contextName = args[0]
	}

	if contextName == "" {
		fmt.Println("Not logged in")
		return
	}

	server, ok := cfg.Servers[contextName]
	if !ok {
		fmt.Printf("Context '%s' not found\n", contextName)
		return
	}

	// Try to logout on server (invalidate session)
	if server.Token != "" {
		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("POST", server.URL+"/api/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+server.Token)
		client.Do(req) // Ignore errors - just best effort
	}

	// Remove this server from config
	delete(cfg.Servers, contextName)

	// If this was the current context, clear it or set to another
	if cfg.CurrentContext == contextName {
		cfg.CurrentContext = ""
		for name := range cfg.Servers {
			cfg.CurrentContext = name
			break
		}
	}

	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Logged out from %s\n", contextName)
}

func cmdContext(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// If no args, list all contexts
	if len(args) == 0 {
		if len(cfg.Servers) == 0 {
			fmt.Println("No contexts configured. Run: bp login <server>")
			return
		}

		fmt.Println("CONTEXTS:")
		for name, server := range cfg.Servers {
			marker := "  "
			if name == cfg.CurrentContext {
				marker = "* "
			}
			fmt.Printf("%s%s (%s)\n", marker, name, server.URL)
		}
		return
	}

	// Switch to specified context
	contextName := args[0]
	if _, ok := cfg.Servers[contextName]; !ok {
		fmt.Printf("Context '%s' not found\n", contextName)
		fmt.Println("Available contexts:")
		for name := range cfg.Servers {
			fmt.Printf("  %s\n", name)
		}
		os.Exit(1)
	}

	cfg.CurrentContext = contextName
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Switched to context: %s\n", contextName)
}

func cmdApps(args []string) {
	resp, err := apiRequest("GET", "/api/apps", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result app.AppListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Apps) == 0 {
		fmt.Println("No apps found. Create one with: bp create <name>")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tDOMAIN\tIMAGE")
	for _, a := range result.Apps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Name, a.Status, a.Domain, a.Image)
	}
	w.Flush()
}

func cmdCreate(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp create <name> [--domain <domain>] [--port <port>]")
		os.Exit(1)
	}

	name := args[0]
	req := app.CreateAppRequest{
		Name:      name,
		EnableSSL: true,
	}

	// Parse flags
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--domain", "-d":
			if i+1 < len(args) {
				req.Domain = args[i+1]
				i++
			}
		case "--port", "-p":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &req.Port)
				i++
			}
		case "--image", "-i":
			if i+1 < len(args) {
				req.Image = args[i+1]
				i++
			}
		}
	}

	resp, err := apiRequest("POST", "/api/apps", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to create app: %s\n", string(body))
		os.Exit(1)
	}

	var newApp app.App
	json.NewDecoder(resp.Body).Decode(&newApp)

	fmt.Printf("App '%s' created successfully!\n", newApp.Name)
	fmt.Printf("ID: %s\n", newApp.ID)
	if newApp.Domain != "" {
		fmt.Printf("Domain: %s\n", newApp.Domain)
	}
	fmt.Println("\nNext steps:")
	fmt.Printf("  bp push             # Deploy from current directory\n")
	fmt.Printf("  bp deploy %s -i <image>  # Deploy with Docker image\n", name)
}

// AppConfig represents the basepod.yaml configuration
type AppConfig struct {
	Name      string                    `yaml:"name"`
	Type      string                    `yaml:"type,omitempty"`      // "static", "container", or "multi"
	Server    string                    `yaml:"server,omitempty"`    // Server context to deploy to
	Domain    string                    `yaml:"domain,omitempty"`
	Port      int                       `yaml:"port,omitempty"`
	Public    string                    `yaml:"public,omitempty"`    // Public directory for static sites
	Build     BuildConfig               `yaml:"build,omitempty"`
	Env       map[string]string         `yaml:"env,omitempty"`
	Volumes   []string                  `yaml:"volumes,omitempty"`
	Processes []ProcessConfig           `yaml:"processes,omitempty"` // Multiple processes for multi-service apps
	Services  map[string]*ServiceConfig `yaml:"services,omitempty"`  // Multiple services (docker-compose style)
	// Git info (populated at deploy time, not in yaml)
	GitCommit  string `yaml:"-" json:"git_commit,omitempty"`
	GitMessage string `yaml:"-" json:"git_message,omitempty"`
	GitBranch  string `yaml:"-" json:"git_branch,omitempty"`
}

// BuildConfig contains build configuration
type BuildConfig struct {
	Dockerfile string `yaml:"dockerfile,omitempty"`
	Context    string `yaml:"context,omitempty"`
	Command    string `yaml:"command,omitempty"` // Local build command (e.g., "npm run build")
}

// ProcessConfig defines a process in a multi-service app
type ProcessConfig struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	Workdir string `yaml:"workdir,omitempty"`
}

// ServiceConfig defines a service in a multi-service app
type ServiceConfig struct {
	Type       string            `yaml:"type,omitempty"`       // "static", "container", "go", "python"
	Image      string            `yaml:"image,omitempty"`      // Docker image to use
	Build      ServiceBuild      `yaml:"build,omitempty"`      // Build configuration
	Port       int               `yaml:"port,omitempty"`       // Internal port
	Public     string            `yaml:"public,omitempty"`     // Public directory for static
	Command    string            `yaml:"command,omitempty"`    // Command to run
	Env        map[string]string `yaml:"env,omitempty"`        // Environment variables
	Volumes    []string          `yaml:"volumes,omitempty"`    // Volume mounts
	DependsOn  []string          `yaml:"depends_on,omitempty"` // Service dependencies
}

// ServiceBuild defines build config for a service
type ServiceBuild struct {
	Context    string `yaml:"context,omitempty"`    // Build context path
	Dockerfile string `yaml:"dockerfile,omitempty"` // Dockerfile path
	Command    string `yaml:"command,omitempty"`    // Pre-build command
}

// loadAppConfig loads basepod.yaml from the specified directory
func loadAppConfig(dir string) (*AppConfig, error) {
	configPath := filepath.Join(dir, "basepod.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func cmdInit(args []string) {
	dir := "."
	forceStatic := false
	forceContainer := false

	// Parse args
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--static", "-s":
			forceStatic = true
		case "--container", "-c":
			forceContainer = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				dir = args[i]
			}
		}
	}

	configPath := filepath.Join(dir, "basepod.yaml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "basepod.yaml already exists\n")
		os.Exit(1)
	}

	// Get app name from directory
	absDir, _ := filepath.Abs(dir)
	appName := filepath.Base(absDir)

	// Detect project type
	projectType := detectProjectType(dir)
	fmt.Printf("Detected: %s\n", projectType.description)

	// Determine deployment type
	deployType := "container"
	if forceStatic {
		deployType = "static"
	} else if forceContainer {
		deployType = "container"
	} else if projectType.isStatic {
		deployType = "static"
	}

	// Interactive prompts
	reader := bufio.NewReader(os.Stdin)

	// App name
	fmt.Printf("? App name: (%s) ", appName)
	if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
		appName = strings.TrimSpace(input)
	}

	// Deployment type (only ask if not forced)
	if !forceStatic && !forceContainer {
		defaultType := "Container"
		if projectType.isStatic {
			defaultType = "Static"
		}
		fmt.Printf("? Deployment type [Container/Static]: (%s) ", defaultType)
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			input = strings.ToLower(strings.TrimSpace(input))
			if input == "static" || input == "s" {
				deployType = "static"
			}
		}
	}

	// Port (only for containers)
	port := projectType.defaultPort
	if deployType == "container" {
		fmt.Printf("? Port: (%d) ", port)
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			fmt.Sscanf(strings.TrimSpace(input), "%d", &port)
		}
	}

	// Public directory (only for static)
	publicDir := "dist"
	if deployType == "static" {
		if projectType.publicDir != "" {
			publicDir = projectType.publicDir
		}
		fmt.Printf("? Public directory: (%s) ", publicDir)
		if input, _ := reader.ReadString('\n'); strings.TrimSpace(input) != "" {
			publicDir = strings.TrimSpace(input)
		}
	}

	// Get current server context
	cliCfg, _ := loadConfig()

	// Create config
	var configData []byte
	if deployType == "static" {
		cfg := map[string]interface{}{
			"name":   appName,
			"type":   "static",
			"public": publicDir,
		}
		if cliCfg.CurrentContext != "" {
			cfg["server"] = cliCfg.CurrentContext
		}
		configData, _ = yaml.Marshal(cfg)
	} else {
		cfg := AppConfig{
			Name:   appName,
			Server: cliCfg.CurrentContext,
			Port:   port,
			Build: BuildConfig{
				Dockerfile: "Dockerfile",
				Context:    ".",
			},
		}
		// Only add env for Node projects
		if projectType.runtime == "node" {
			cfg.Env = map[string]string{"NODE_ENV": "production"}
		}
		configData, _ = yaml.Marshal(cfg)
	}

	// Write basepod.yaml
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write config: %v\n", err)
		os.Exit(1)
	}

	// Generate Dockerfile if needed (container mode, no existing Dockerfile)
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	createdDockerfile := false
	if deployType == "container" && !projectType.hasDockerfile {
		dockerfile := generateDockerfile(projectType, port)
		if dockerfile != "" {
			if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to create Dockerfile: %v\n", err)
			} else {
				createdDockerfile = true
			}
		}
	}

	// Output summary
	fmt.Println()
	if createdDockerfile {
		fmt.Println("Created: basepod.yaml, Dockerfile")
	} else {
		fmt.Println("Created: basepod.yaml")
	}
	fmt.Println("\nNext steps:")
	fmt.Println("  bp deploy")
}

// ProjectType holds detected project information
type ProjectType struct {
	runtime       string // node, go, python, static
	description   string
	hasDockerfile bool
	isStatic      bool
	defaultPort   int
	publicDir     string
}

// detectProjectType analyzes a directory to determine the project type
func detectProjectType(dir string) ProjectType {
	pt := ProjectType{
		runtime:     "unknown",
		description: "Unknown project",
		defaultPort: 3000,
	}

	// Check for existing Dockerfile
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		pt.hasDockerfile = true
		pt.description = "Dockerfile (existing)"
		pt.runtime = "docker"
		return pt
	}

	// Check for package.json (Node/Bun)
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		pt.runtime = "node"
		pt.description = "package.json (Node/Bun project)"
		pt.defaultPort = 3000
		return pt
	}

	// Check for go.mod (Go)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		pt.runtime = "go"
		pt.description = "go.mod (Go project)"
		pt.defaultPort = 8080
		return pt
	}

	// Check for requirements.txt (Python)
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		pt.runtime = "python"
		pt.description = "requirements.txt (Python project)"
		pt.defaultPort = 8000
		return pt
	}

	// Check for common static build output directories
	staticDirs := []string{"dist", "build", "public", "out", ".output/public", "_site"}
	for _, d := range staticDirs {
		if fi, err := os.Stat(filepath.Join(dir, d)); err == nil && fi.IsDir() {
			pt.runtime = "static"
			pt.description = fmt.Sprintf("%s/ (Static site)", d)
			pt.isStatic = true
			pt.publicDir = d
			return pt
		}
	}

	// Check for HTML files (static site)
	files, _ := filepath.Glob(filepath.Join(dir, "*.html"))
	if len(files) > 0 {
		pt.runtime = "static"
		pt.description = "*.html (Static site)"
		pt.isStatic = true
		pt.publicDir = "."
		return pt
	}

	return pt
}

// generateDockerfile creates a Dockerfile based on project type
func generateDockerfile(pt ProjectType, port int) string {
	switch pt.runtime {
	case "node":
		return fmt.Sprintf(`FROM oven/bun:1-alpine
WORKDIR /app
COPY package*.json ./
RUN bun install --production
COPY . .
EXPOSE %d
CMD ["bun", "run", "start"]
`, port)
	case "go":
		return fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM alpine:latest
COPY --from=builder /app/main /main
EXPOSE %d
CMD ["/main"]
`, port)
	case "python":
		return fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE %d
CMD ["python", "app.py"]
`, port)
	default:
		return ""
	}
}

// createTarball creates a gzipped tarball of the directory
func createTarball(dir string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Files/dirs to ignore
	ignorePatterns := []string{
		".git",
		"node_modules",
		".env",
		".env.local",
		"__pycache__",
		"*.pyc",
		".DS_Store",
		"vendor",
		"dist",
		"build",
		".next",
		".nuxt",
		".output",
		"sdk",
		"bin",
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Check ignore patterns
		for _, pattern := range ignorePatterns {
			if matched, _ := filepath.Match(pattern, info.Name()); matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			// Also check if path contains ignored dir
			if strings.Contains(relPath, pattern+string(filepath.Separator)) {
				return nil
			}
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

// createStaticTarball creates a tarball of only the static site public directory
// It includes all files in the public directory but preserves the directory structure
// so the server can find files at the expected path (e.g., build/jaspr/index.html)
func createStaticTarball(publicDir, publicPath string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Files to ignore
	ignorePatterns := []string{
		".DS_Store",
		"*.pyc",
		"__pycache__",
	}

	err := filepath.Walk(publicDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from publicDir
		relPath, err := filepath.Rel(publicDir, path)
		if err != nil {
			return err
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Check ignore patterns
		for _, pattern := range ignorePatterns {
			if matched, _ := filepath.Match(pattern, info.Name()); matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Create tar header with the public path prefix (e.g., build/jaspr/file.html)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.Join(publicPath, relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

// cmdRun runs the app locally using Podman
func cmdRun(args []string) {
	var dir string
	var port int
	var detach bool

	// Parse flags
	positionalArgs := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &port)
				i++
			}
		case "--detach", "-d":
			detach = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				positionalArgs = append(positionalArgs, args[i])
			}
		}
	}

	if len(positionalArgs) > 0 {
		dir = positionalArgs[0]
	} else {
		dir = "."
	}

	// Load app config
	appCfg, err := loadAppConfig(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "No basepod.yaml found. Run 'bp init' first.")
		} else {
			fmt.Fprintf(os.Stderr, "Failed to load basepod.yaml: %v\n", err)
		}
		os.Exit(1)
	}

	if appCfg.Name == "" {
		fmt.Fprintln(os.Stderr, "App name is required in basepod.yaml")
		os.Exit(1)
	}

	// Determine port
	if port == 0 {
		port = appCfg.Port
	}
	if port == 0 {
		port = 3000 // Default port
	}

	// Run local build command if specified
	if appCfg.Build.Command != "" {
		fmt.Printf("Running build command: %s\n", appCfg.Build.Command)
		if err := runBuildCommand(dir, appCfg.Build.Command); err != nil {
			fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Build completed successfully!")
	}

	// Check if podman is available
	if _, err := exec.LookPath("podman"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: podman is not installed or not in PATH")
		fmt.Fprintln(os.Stderr, "Install podman: https://podman.io/getting-started/installation")
		os.Exit(1)
	}

	// Stop and remove existing container with same name
	containerName := appCfg.Name
	fmt.Printf("Stopping existing container (if any)...\n")
	exec.Command("podman", "stop", containerName).Run()
	exec.Command("podman", "rm", containerName).Run()

	// Handle based on app type
	if len(appCfg.Services) > 0 {
		// Multi-service app: run with podman pod
		runServicesApp(dir, appCfg, port, detach)
	} else if len(appCfg.Processes) > 0 {
		// Multi-process app: run with supervisord
		runMultiProcessApp(dir, appCfg, port, detach)
	} else if appCfg.Type == "static" && appCfg.Public != "" {
		// Static site: serve with Caddy
		runStaticSite(dir, appCfg, port, detach)
	} else if appCfg.Build.Dockerfile != "" {
		// Container with Dockerfile
		runContainerApp(dir, appCfg, port, detach)
	} else {
		fmt.Fprintln(os.Stderr, "Error: Cannot determine how to run this app.")
		fmt.Fprintln(os.Stderr, "Set 'type: static' with 'public: <dir>' for static sites,")
		fmt.Fprintln(os.Stderr, "set 'build.dockerfile: <path>' for container apps,")
		fmt.Fprintln(os.Stderr, "or define 'processes' for multi-service apps.")
		os.Exit(1)
	}
}

// runStaticSite runs a static site locally by building a container image
func runStaticSite(dir string, appCfg *AppConfig, port int, detach bool) {
	absDir, _ := filepath.Abs(dir)
	publicPath := filepath.Join(absDir, appCfg.Public)

	// Check if public directory exists
	if _, err := os.Stat(publicPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Public directory '%s' does not exist.\n", appCfg.Public)
		fmt.Fprintln(os.Stderr, "Run the build command first if needed.")
		os.Exit(1)
	}

	fmt.Printf("Running static site: %s\n", appCfg.Name)
	fmt.Printf("Source: %s\n", publicPath)

	imageName := appCfg.Name + ":local"

	// Create a temporary Containerfile
	containerfile := `FROM docker.io/caddy:2-alpine
WORKDIR /srv
COPY . .
CMD ["caddy", "file-server", "--root", "/srv", "--listen", ":80"]
`

	// Write Containerfile to public directory temporarily
	containerfilePath := filepath.Join(publicPath, "Containerfile.tmp")
	if err := os.WriteFile(containerfilePath, []byte(containerfile), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Containerfile: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(containerfilePath)

	// Build the image
	fmt.Printf("Building container image: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", containerfilePath, publicPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build image: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Image built successfully!")

	// Build environment variables
	envArgs := []string{}
	for key, val := range appCfg.Env {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Run container
	runArgs := []string{"run"}
	if detach {
		runArgs = append(runArgs, "-d")
	}
	runArgs = append(runArgs,
		"--name", appCfg.Name,
		"-p", fmt.Sprintf("%d:80", port),
	)
	runArgs = append(runArgs, envArgs...)
	runArgs = append(runArgs, imageName)

	fmt.Printf("\nContainer: %s\n", appCfg.Name)
	fmt.Printf("URL: http://localhost:%d\n\n", port)

	if detach {
		cmd := exec.Command("podman", runArgs...)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start container: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Container started in background.\n")
		fmt.Printf("View logs: podman logs -f %s\n", appCfg.Name)
		fmt.Printf("Stop: podman stop %s\n", appCfg.Name)
	} else {
		fmt.Println("Press Ctrl+C to stop...")
		cmd := exec.Command("podman", runArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			// Ignore interrupt errors
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
		}
	}
}

// runContainerApp builds and runs a container app locally
func runContainerApp(dir string, appCfg *AppConfig, port int, detach bool) {
	absDir, _ := filepath.Abs(dir)

	// Determine Dockerfile and context
	dockerfile := appCfg.Build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	context := appCfg.Build.Context
	if context == "" {
		context = "."
	}

	dockerfilePath := filepath.Join(absDir, dockerfile)
	contextPath := filepath.Join(absDir, context)

	// Check if Dockerfile exists
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		// Try Containerfile as fallback
		containerfilePath := filepath.Join(absDir, "Containerfile")
		if _, err := os.Stat(containerfilePath); err == nil {
			dockerfilePath = containerfilePath
		} else {
			fmt.Fprintf(os.Stderr, "Error: Dockerfile '%s' not found.\n", dockerfile)
			os.Exit(1)
		}
	}

	imageName := appCfg.Name + ":local"

	fmt.Printf("Building container: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfilePath, contextPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Build completed!")

	// Build environment variables
	envArgs := []string{}
	for key, val := range appCfg.Env {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Build volume mounts
	volumeArgs := []string{}
	for _, vol := range appCfg.Volumes {
		volumeArgs = append(volumeArgs, "-v", vol)
	}

	// Determine container port
	containerPort := appCfg.Port
	if containerPort == 0 {
		containerPort = 3000
	}

	// Run container
	runArgs := []string{"run"}
	if detach {
		runArgs = append(runArgs, "-d")
	}
	runArgs = append(runArgs,
		"--name", appCfg.Name,
		"-p", fmt.Sprintf("%d:%d", port, containerPort),
	)
	runArgs = append(runArgs, envArgs...)
	runArgs = append(runArgs, volumeArgs...)
	runArgs = append(runArgs, imageName)

	fmt.Printf("\nContainer: %s\n", appCfg.Name)
	fmt.Printf("URL: http://localhost:%d\n\n", port)

	if detach {
		cmd := exec.Command("podman", runArgs...)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start container: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Container started in background.\n")
		fmt.Printf("View logs: podman logs -f %s\n", appCfg.Name)
		fmt.Printf("Stop: podman stop %s\n", appCfg.Name)
	} else {
		fmt.Println("Press Ctrl+C to stop...")
		cmd := exec.Command("podman", runArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			// Ignore interrupt errors
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
		}
	}
}

// runServicesApp runs multiple services using podman pod
func runServicesApp(dir string, appCfg *AppConfig, port int, detach bool) {
	absDir, _ := filepath.Abs(dir)
	podName := appCfg.Name + "-pod"

	fmt.Printf("Running multi-service app: %s\n", appCfg.Name)
	fmt.Printf("Services:\n")
	for name, svc := range appCfg.Services {
		fmt.Printf("  - %s (port %d)\n", name, svc.Port)
	}

	// Stop and remove existing pod
	fmt.Printf("\nStopping existing pod (if any)...\n")
	exec.Command("podman", "pod", "stop", podName).Run()
	exec.Command("podman", "pod", "rm", podName).Run()

	// Collect all ports to expose
	portArgs := []string{}
	portArgs = append(portArgs, "-p", fmt.Sprintf("%d:%d", port, port))

	// Create the pod with all ports
	fmt.Printf("Creating pod: %s\n", podName)
	createPodArgs := []string{"pod", "create", "--name", podName}
	createPodArgs = append(createPodArgs, portArgs...)

	if err := exec.Command("podman", createPodArgs...).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create pod: %v\n", err)
		os.Exit(1)
	}

	// Build and run each service
	for name, svc := range appCfg.Services {
		fmt.Printf("\n--- Service: %s ---\n", name)

		var imageName string

		if svc.Image != "" {
			// Use existing image
			imageName = svc.Image
			fmt.Printf("Using image: %s\n", imageName)
		} else {
			// Build the service
			imageName = fmt.Sprintf("%s-%s:local", appCfg.Name, name)

			// Run pre-build command if specified
			if svc.Build.Command != "" {
				buildDir := absDir
				if svc.Build.Context != "" {
					buildDir = filepath.Join(absDir, svc.Build.Context)
				}
				fmt.Printf("Running build command: %s\n", svc.Build.Command)
				if err := runBuildCommand(buildDir, svc.Build.Command); err != nil {
					fmt.Fprintf(os.Stderr, "Build command failed for %s: %v\n", name, err)
					os.Exit(1)
				}
			}

			// Build container if Dockerfile or type specified
			if svc.Type == "static" && svc.Public != "" {
				// Build static site container
				buildStaticServiceImage(absDir, name, svc, imageName)
			} else if svc.Build.Dockerfile != "" || svc.Build.Context != "" {
				// Build from Dockerfile
				buildServiceImage(absDir, name, svc, imageName)
			} else if svc.Type == "go" {
				// Build Go service
				buildGoServiceImage(absDir, name, svc, imageName)
			} else if svc.Type == "python" {
				// Build Python service
				buildPythonServiceImage(absDir, name, svc, imageName)
			} else {
				fmt.Fprintf(os.Stderr, "Cannot determine how to build service '%s'\n", name)
				os.Exit(1)
			}
		}

		// Run the service in the pod
		runArgs := []string{"run", "-d", "--pod", podName, "--name", fmt.Sprintf("%s-%s", appCfg.Name, name)}

		// Add environment variables
		for key, val := range svc.Env {
			runArgs = append(runArgs, "-e", fmt.Sprintf("%s=%s", key, val))
		}

		// Add volumes
		for _, vol := range svc.Volumes {
			runArgs = append(runArgs, "-v", vol)
		}

		runArgs = append(runArgs, imageName)

		// Add command if specified
		if svc.Command != "" {
			runArgs = append(runArgs, "sh", "-c", svc.Command)
		}

		fmt.Printf("Starting %s...\n", name)
		cmd := exec.Command("podman", runArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start service %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	fmt.Printf("\n✓ All services started!\n")
	fmt.Printf("Pod: %s\n", podName)
	fmt.Printf("URL: http://localhost:%d\n\n", port)
	fmt.Printf("View logs: podman pod logs -f %s\n", podName)
	fmt.Printf("Stop: podman pod stop %s\n", podName)

	if !detach {
		fmt.Println("\nPress Ctrl+C to stop...")
		// Follow logs
		cmd := exec.Command("podman", "pod", "logs", "-f", podName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}

// buildStaticServiceImage builds a static site service image
func buildStaticServiceImage(baseDir, name string, svc *ServiceConfig, imageName string) {
	publicPath := filepath.Join(baseDir, svc.Public)

	if _, err := os.Stat(publicPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Public directory '%s' not found for service '%s'\n", svc.Public, name)
		os.Exit(1)
	}

	// Create Containerfile
	port := svc.Port
	if port == 0 {
		port = 3000
	}

	containerfile := fmt.Sprintf(`FROM docker.io/caddy:2-alpine
WORKDIR /srv
COPY . .
CMD ["caddy", "file-server", "--root", "/srv", "--listen", ":%d"]
`, port)

	containerfilePath := filepath.Join(publicPath, "Containerfile.tmp")
	os.WriteFile(containerfilePath, []byte(containerfile), 0644)
	defer os.Remove(containerfilePath)

	fmt.Printf("Building static service: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", containerfilePath, publicPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build service %s: %v\n", name, err)
		os.Exit(1)
	}
}

// buildServiceImage builds a service from Dockerfile
func buildServiceImage(baseDir, name string, svc *ServiceConfig, imageName string) {
	context := svc.Build.Context
	if context == "" {
		context = "."
	}
	contextPath := filepath.Join(baseDir, context)

	dockerfile := svc.Build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	dockerfilePath := filepath.Join(contextPath, dockerfile)

	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		// Try Containerfile
		containerfilePath := filepath.Join(contextPath, "Containerfile")
		if _, err := os.Stat(containerfilePath); err == nil {
			dockerfilePath = containerfilePath
		} else {
			fmt.Fprintf(os.Stderr, "Dockerfile not found for service '%s'\n", name)
			os.Exit(1)
		}
	}

	fmt.Printf("Building service: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfilePath, contextPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build service %s: %v\n", name, err)
		os.Exit(1)
	}
}

// buildGoServiceImage builds a Go service image
func buildGoServiceImage(baseDir, name string, svc *ServiceConfig, imageName string) {
	context := svc.Build.Context
	if context == "" {
		context = "."
	}
	contextPath := filepath.Join(baseDir, context)

	port := svc.Port
	if port == 0 {
		port = 8080
	}

	// Generate Dockerfile for Go
	dockerfile := fmt.Sprintf(`FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE %d
CMD ["./server"]
`, port)

	dockerfilePath := filepath.Join(contextPath, "Dockerfile.bp")
	os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	defer os.Remove(dockerfilePath)

	fmt.Printf("Building Go service: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfilePath, contextPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build service %s: %v\n", name, err)
		os.Exit(1)
	}
}

// buildPythonServiceImage builds a Python service image
func buildPythonServiceImage(baseDir, name string, svc *ServiceConfig, imageName string) {
	context := svc.Build.Context
	if context == "" {
		context = "."
	}
	contextPath := filepath.Join(baseDir, context)

	port := svc.Port
	if port == 0 {
		port = 8000
	}

	// Generate Dockerfile for Python
	dockerfile := fmt.Sprintf(`FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt* ./
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true
COPY . .
EXPOSE %d
CMD ["python", "-m", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "%d"]
`, port, port)

	dockerfilePath := filepath.Join(contextPath, "Dockerfile.bp")
	os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	defer os.Remove(dockerfilePath)

	fmt.Printf("Building Python service: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfilePath, contextPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build service %s: %v\n", name, err)
		os.Exit(1)
	}
}

// runMultiProcessApp runs a multi-process app using supervisord
func runMultiProcessApp(dir string, appCfg *AppConfig, port int, detach bool) {
	absDir, _ := filepath.Abs(dir)

	// Check if Dockerfile exists for multi-process
	dockerfile := appCfg.Build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	dockerfilePath := filepath.Join(absDir, dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		// Try Containerfile
		containerfilePath := filepath.Join(absDir, "Containerfile")
		if _, err := os.Stat(containerfilePath); err == nil {
			dockerfilePath = containerfilePath
		} else {
			fmt.Fprintf(os.Stderr, "Error: Dockerfile '%s' not found for multi-process app.\n", dockerfile)
			fmt.Fprintln(os.Stderr, "Create a Dockerfile that includes all your services.")
			os.Exit(1)
		}
	}

	context := appCfg.Build.Context
	if context == "" {
		context = "."
	}
	contextPath := filepath.Join(absDir, context)

	// Generate supervisord.conf
	supervisordConf := "[supervisord]\nnodaemon=true\nuser=root\n\n"
	for _, proc := range appCfg.Processes {
		workdir := proc.Workdir
		if workdir == "" {
			workdir = "/app"
		}
		supervisordConf += fmt.Sprintf(`[program:%s]
command=%s
directory=%s
autostart=true
autorestart=true
stdout_logfile=/dev/fd/1
stdout_logfile_maxbytes=0
stderr_logfile=/dev/fd/2
stderr_logfile_maxbytes=0

`, proc.Name, proc.Command, workdir)
	}

	// Write supervisord.conf to build context
	supervisordPath := filepath.Join(contextPath, "supervisord.conf")
	if err := os.WriteFile(supervisordPath, []byte(supervisordConf), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write supervisord.conf: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(supervisordPath)

	fmt.Printf("Running multi-process app: %s\n", appCfg.Name)
	fmt.Printf("Processes:\n")
	for _, proc := range appCfg.Processes {
		fmt.Printf("  - %s: %s\n", proc.Name, proc.Command)
	}

	imageName := appCfg.Name + ":local"

	// Build the image
	fmt.Printf("\nBuilding container: %s\n", imageName)
	buildCmd := exec.Command("podman", "build", "-t", imageName, "-f", dockerfilePath, contextPath)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Build completed!")

	// Build environment variables
	envArgs := []string{}
	for key, val := range appCfg.Env {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", key, val))
	}

	// Build volume mounts
	volumeArgs := []string{}
	for _, vol := range appCfg.Volumes {
		volumeArgs = append(volumeArgs, "-v", vol)
	}

	// Run container
	runArgs := []string{"run"}
	if detach {
		runArgs = append(runArgs, "-d")
	}
	runArgs = append(runArgs, "--name", appCfg.Name)

	// Expose main port (default 3000)
	containerPort := appCfg.Port
	if containerPort == 0 {
		containerPort = 3000
	}
	runArgs = append(runArgs, "-p", fmt.Sprintf("%d:%d", port, containerPort))

	runArgs = append(runArgs, envArgs...)
	runArgs = append(runArgs, volumeArgs...)
	runArgs = append(runArgs, imageName)

	fmt.Printf("\nContainer: %s\n", appCfg.Name)
	fmt.Printf("URL: http://localhost:%d\n\n", port)

	if detach {
		cmd := exec.Command("podman", runArgs...)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start container: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Container started in background.\n")
		fmt.Printf("View logs: podman logs -f %s\n", appCfg.Name)
		fmt.Printf("Stop: podman stop %s\n", appCfg.Name)
	} else {
		fmt.Println("Press Ctrl+C to stop...")
		cmd := exec.Command("podman", runArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
		}
	}
}

func cmdDeploy(args []string) {
	var image, gitURL, branch, dir string
	var force bool

	// Parse flags first
	positionalArgs := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--image", "-i":
			if i+1 < len(args) {
				image = args[i+1]
				i++
			}
		case "--git", "-g":
			if i+1 < len(args) {
				gitURL = args[i+1]
				i++
			}
		case "--branch", "-b":
			if i+1 < len(args) {
				branch = args[i+1]
				i++
			}
		case "--force", "-f":
			force = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				positionalArgs = append(positionalArgs, args[i])
			}
		}
	}

	// Determine deployment mode
	if image != "" || gitURL != "" {
		// Image or Git deployment mode - requires app name
		if len(positionalArgs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: bp deploy <name> --image <image>")
			fmt.Fprintln(os.Stderr, "       bp deploy <name> --git <url> [--branch <branch>]")
			os.Exit(1)
		}
		name := positionalArgs[0]
		deployImageOrGit(name, image, gitURL, branch)
	} else {
		// Local source deployment mode (default)
		if len(positionalArgs) > 0 {
			dir = positionalArgs[0]
		} else {
			dir = "."
		}
		deployLocalSource(dir, force)
	}
}

// deployLocalSource deploys from local source code (like old bp push)
func deployLocalSource(dir string, force bool) {
	// Load app config
	appCfg, err := loadAppConfig(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "No basepod.yaml found. Run 'bp init' first.")
		} else {
			fmt.Fprintf(os.Stderr, "Failed to load basepod.yaml: %v\n", err)
		}
		os.Exit(1)
	}

	if appCfg.Name == "" {
		fmt.Fprintln(os.Stderr, "App name is required in basepod.yaml")
		os.Exit(1)
	}

	// Check git status unless --force is used
	// Do this BEFORE running build command to catch uncommitted source changes
	if !force {
		if hasUncommittedChanges(dir) {
			fmt.Fprintln(os.Stderr, "Error: You have uncommitted changes.")
			fmt.Fprintln(os.Stderr, "Commit your changes or use --force to deploy anyway.")
			os.Exit(1)
		}
	}

	// Run local build command if specified (for static sites)
	if appCfg.Build.Command != "" {
		fmt.Printf("Running build command: %s\n", appCfg.Build.Command)
		if err := runBuildCommand(dir, appCfg.Build.Command); err != nil {
			fmt.Fprintf(os.Stderr, "Build failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Build completed successfully!")
	}

	// Load CLI config
	cliCfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Determine which server to use
	var serverCfg *ServerConfig
	var contextName string

	if appCfg.Server != "" {
		srv, ok := cliCfg.Servers[appCfg.Server]
		if !ok {
			fmt.Fprintf(os.Stderr, "Server context '%s' from basepod.yaml not found.\n", appCfg.Server)
			fmt.Fprintln(os.Stderr, "Run: bp login <server>")
			os.Exit(1)
		}
		serverCfg = &srv
		contextName = appCfg.Server
	} else {
		srv, name, err := getCurrentServer(cliCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		serverCfg = srv
		contextName = name
	}

	// Get git info for deployment tracking
	appCfg.GitCommit, appCfg.GitMessage, appCfg.GitBranch = getGitInfo(dir)
	if appCfg.GitCommit != "" {
		fmt.Printf("Deploying %s@%s to %s...\n", appCfg.Name, appCfg.GitCommit, contextName)
	} else {
		fmt.Printf("Deploying %s to %s...\n", appCfg.Name, contextName)
	}

	// Create tarball - for static sites, only include the public directory
	var tarball *bytes.Buffer
	var tarErr error
	if appCfg.Type == "static" && appCfg.Public != "" {
		publicDir := filepath.Join(dir, appCfg.Public)
		if _, err := os.Stat(publicDir); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Public directory not found: %s\n", appCfg.Public)
			fmt.Fprintln(os.Stderr, "Make sure your build completed successfully")
			os.Exit(1)
		}
		tarball, tarErr = createStaticTarball(publicDir, appCfg.Public)
	} else {
		tarball, tarErr = createTarball(dir)
	}
	if tarErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to create tarball: %v\n", tarErr)
		os.Exit(1)
	}

	fmt.Printf("Created tarball: %d bytes\n", tarball.Len())

	// Upload to server
	client := &http.Client{Timeout: 5 * time.Minute}
	server := serverCfg.URL

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add config as JSON
	configJSON, _ := json.Marshal(appCfg)
	_ = writer.WriteField("config", string(configJSON))

	// Add tarball
	part, err := writer.CreateFormFile("source", "source.tar.gz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create form: %v\n", err)
		os.Exit(1)
	}
	if _, err := io.Copy(part, tarball); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write tarball: %v\n", err)
		os.Exit(1)
	}
	writer.Close()

	url := strings.TrimSuffix(server, "/") + "/api/deploy"
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if serverCfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+serverCfg.Token)
	}

	fmt.Println("Uploading...")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to upload: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Stream response (build logs)
	fmt.Println("\n--- Build Output ---")
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			fmt.Print(string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "\nDeploy failed with status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	fmt.Println("\nDeployed successfully!")
	if appCfg.Domain != "" {
		fmt.Printf("URL: https://%s\n", appCfg.Domain)
	}
}

// deployImageOrGit deploys from a Docker image or Git repository
func deployImageOrGit(name, image, gitURL, branch string) {
	req := app.DeployRequest{
		Image:  image,
		GitURL: gitURL,
		Branch: branch,
	}

	fmt.Printf("Deploying %s...\n", name)

	resp, err := apiRequest("POST", "/api/apps/"+name+"/deploy", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Deploy failed: %s\n", string(body))
		os.Exit(1)
	}

	var deployedApp app.App
	json.NewDecoder(resp.Body).Decode(&deployedApp)

	fmt.Printf("Deployed successfully!\n")
	fmt.Printf("Status: %s\n", deployedApp.Status)
	if deployedApp.Domain != "" {
		fmt.Printf("URL: https://%s\n", deployedApp.Domain)
	}
}

// hasUncommittedChanges checks if the directory has uncommitted git changes
func hasUncommittedChanges(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		// Not a git repo or git not available - allow deploy
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// getGitInfo retrieves git commit hash, message, and branch from a directory
func getGitInfo(dir string) (commit, message, branch string) {
	// Get short commit hash
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD")
	if out, err := cmd.Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	// Get commit message (first line)
	cmd = exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s")
	if out, err := cmd.Output(); err == nil {
		message = strings.TrimSpace(string(out))
		// Truncate to 100 chars
		if len(message) > 100 {
			message = message[:97] + "..."
		}
	}

	// Get current branch
	cmd = exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := cmd.Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}

	return
}

// runBuildCommand executes a local build command in the specified directory
func runBuildCommand(dir string, command string) error {
	// Use shell to run the command (supports pipes, &&, etc.)
	var cmd *exec.Cmd
	if _, err := exec.LookPath("bash"); err == nil {
		cmd = exec.Command("bash", "-c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Inherit environment
	cmd.Env = os.Environ()

	return cmd.Run()
}

func cmdLogs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp logs <name> [--tail <n>]")
		os.Exit(1)
	}

	name := args[0]
	tail := "100"

	// Parse flags
	for i := 1; i < len(args); i++ {
		if args[i] == "--tail" || args[i] == "-n" {
			if i+1 < len(args) {
				tail = args[i+1]
				i++
			}
		}
	}

	resp, err := apiRequest("GET", fmt.Sprintf("/api/apps/%s/logs?tail=%s", name, tail), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to get logs: %s\n", string(body))
		os.Exit(1)
	}

	io.Copy(os.Stdout, resp.Body)
}

func cmdStart(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp start <name>")
		os.Exit(1)
	}

	name := args[0]
	resp, err := apiRequest("POST", "/api/apps/"+name+"/start", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to start app: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("App '%s' started\n", name)
}

func cmdStop(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp stop <name>")
		os.Exit(1)
	}

	name := args[0]
	resp, err := apiRequest("POST", "/api/apps/"+name+"/stop", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to stop app: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("App '%s' stopped\n", name)
}

func cmdRestart(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp restart <name>")
		os.Exit(1)
	}

	name := args[0]
	resp, err := apiRequest("POST", "/api/apps/"+name+"/restart", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to restart app: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("App '%s' restarted\n", name)
}

func cmdDelete(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp delete <name>")
		os.Exit(1)
	}

	name := args[0]

	// Confirm deletion
	fmt.Printf("Are you sure you want to delete '%s'? (y/N): ", name)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled")
		return
	}

	resp, err := apiRequest("DELETE", "/api/apps/"+name, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to delete app: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("App '%s' deleted\n", name)
}

func cmdInfo(args []string) {
	resp, err := apiRequest("GET", "/api/system/info", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Server Info:")
	for k, v := range info {
		fmt.Printf("  %s: %v\n", k, v)
	}
}

func cmdStatus(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	server, contextName, err := getCurrentServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Context: %s\n", contextName)
	fmt.Printf("Server: %s\n", server.URL)
	fmt.Println()

	// Get system info
	resp, err := apiRequest("GET", "/api/system/info", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("System:")
	fmt.Printf("  Version: %v\n", info["version"])
	fmt.Printf("  Platform: %v/%v\n", info["os"], info["arch"])
	if podmanStatus, ok := info["podman_status"].(string); ok {
		fmt.Printf("  Podman: %s\n", podmanStatus)
	}
	if caddyStatus, ok := info["caddy_status"].(string); ok {
		fmt.Printf("  Caddy: %s\n", caddyStatus)
	}
	fmt.Println()

	// Get apps
	appsResp, err := apiRequest("GET", "/api/apps", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting apps: %v\n", err)
		os.Exit(1)
	}
	defer appsResp.Body.Close()

	var result app.AppListResponse
	if err := json.NewDecoder(appsResp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse apps response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Apps:")
	if len(result.Apps) == 0 {
		fmt.Println("  No apps deployed")
	} else {
		running := 0
		stopped := 0
		for _, a := range result.Apps {
			if a.Status == "running" {
				running++
			} else {
				stopped++
			}
		}
		fmt.Printf("  Total: %d (running: %d, stopped: %d)\n", len(result.Apps), running, stopped)
	}
}

// ==================== Template Commands ====================

func cmdTemplates(args []string) {
	category := ""
	for i := 0; i < len(args); i++ {
		if (args[i] == "--category" || args[i] == "-c") && i+1 < len(args) {
			category = args[i+1]
			i++
		}
	}

	path := "/api/templates"
	if category != "" {
		path += "?category=" + category
	}

	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var templates []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Image       string `json:"image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&templates); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if len(templates) == 0 {
		fmt.Println("No templates available")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCATEGORY\tDESCRIPTION")
	for _, t := range templates {
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Category, t.Description)
	}
	w.Flush()
}

func cmdTemplate(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp template <deploy|export> <name>")
		os.Exit(1)
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "deploy":
		cmdTemplateDeployCmd(subargs)
	case "export":
		cmdTemplateExport(subargs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown template command: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: bp template <deploy|export> <name>")
		os.Exit(1)
	}
}

func cmdTemplateDeployCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp template deploy <template> [--name <name>] [--env KEY=value]")
		os.Exit(1)
	}

	template := args[0]
	name := ""
	version := ""
	env := make(map[string]string)

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--name", "-n":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "--version", "-v":
			if i+1 < len(args) {
				version = args[i+1]
				i++
			}
		case "--env", "-e":
			if i+1 < len(args) {
				parts := strings.SplitN(args[i+1], "=", 2)
				if len(parts) == 2 {
					env[parts[0]] = parts[1]
				}
				i++
			}
		}
	}

	// Check if template is a local file or URL
	if strings.HasSuffix(template, ".yaml") || strings.HasSuffix(template, ".yml") || strings.HasPrefix(template, "http") {
		deployCustomTemplate(template, name, env)
		return
	}

	// Deploy predefined template
	req := map[string]interface{}{
		"template": template,
		"name":     name,
		"version":  version,
		"env":      env,
	}

	fmt.Printf("Deploying template: %s...\n", template)

	resp, err := apiRequest("POST", "/api/templates/deploy", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Deploy failed: %s\n", string(body))
		os.Exit(1)
	}

	var result app.App
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Deployed successfully!\n")
	fmt.Printf("Name: %s\n", result.Name)
	if result.Domain != "" {
		fmt.Printf("URL: https://%s\n", result.Domain)
	}
}

func deployCustomTemplate(templatePath, name string, env map[string]string) {
	var templateData []byte
	var err error

	if strings.HasPrefix(templatePath, "http") {
		// Fetch from URL
		resp, err := http.Get(templatePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch template: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		templateData, err = io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read template: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Read local file
		templateData, err = os.ReadFile(templatePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read template file: %v\n", err)
			os.Exit(1)
		}
	}

	// Parse template
	var template struct {
		Name     string `yaml:"name"`
		Version  string `yaml:"version"`
		Services []struct {
			Name      string            `yaml:"name"`
			Image     string            `yaml:"image"`
			Template  string            `yaml:"template"`
			Build     string            `yaml:"build"`
			Port      int               `yaml:"port"`
			Env       map[string]string `yaml:"env"`
			Volumes   []string          `yaml:"volumes"`
			DependsOn []string          `yaml:"depends_on"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(templateData, &template); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse template: %v\n", err)
		os.Exit(1)
	}

	stackName := template.Name
	if name != "" {
		stackName = name
	}

	fmt.Printf("Deploying stack: %s (%d services)...\n", stackName, len(template.Services))

	// Deploy each service
	for _, svc := range template.Services {
		svcName := stackName + "-" + svc.Name
		fmt.Printf("  Deploying %s...\n", svcName)

		// Merge environment variables
		svcEnv := svc.Env
		if svcEnv == nil {
			svcEnv = make(map[string]string)
		}
		for k, v := range env {
			svcEnv[k] = v
		}

		req := map[string]interface{}{
			"name":     svcName,
			"image":    svc.Image,
			"template": svc.Template,
			"port":     svc.Port,
			"env":      svcEnv,
			"volumes":  svc.Volumes,
		}

		resp, err := apiRequest("POST", "/api/templates/deploy", req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    Failed: %v\n", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			fmt.Printf("    Done\n")
		} else {
			fmt.Printf("    Failed (status %d)\n", resp.StatusCode)
		}
	}

	fmt.Println("\nStack deployed!")
}

func cmdTemplateExport(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp template export <name>")
		os.Exit(1)
	}

	name := args[0]

	resp, err := apiRequest("GET", "/api/apps/"+name, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to get app: %s\n", string(body))
		os.Exit(1)
	}

	var appData app.App
	json.NewDecoder(resp.Body).Decode(&appData)

	// Convert to template format
	template := map[string]interface{}{
		"name":    appData.Name,
		"version": "1.0",
		"services": []map[string]interface{}{
			{
				"name":    appData.Name,
				"image":   appData.Image,
				"port":    appData.Ports,
				"env":     appData.Env,
				"volumes": appData.Volumes,
			},
		},
	}

	output, err := yaml.Marshal(template)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate template: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(output))
}

// ==================== Model Commands (LLM) ====================

func cmdModels(args []string) {
	downloaded := false
	category := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--downloaded":
			downloaded = true
		case "--category":
			if i+1 < len(args) {
				category = args[i+1]
				i++
			}
		}
	}

	path := "/api/models"
	params := []string{}
	if downloaded {
		params = append(params, "downloaded=true")
	}
	if category != "" {
		params = append(params, "category="+category)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	resp, err := apiRequest("GET", path, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var models struct {
		Downloaded []struct {
			Name string `json:"name"`
			Size string `json:"size"`
		} `json:"downloaded"`
		Available []struct {
			Name     string `json:"name"`
			Size     string `json:"size"`
			Category string `json:"category"`
		} `json:"available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse response: %v\n", err)
		os.Exit(1)
	}

	if len(models.Downloaded) > 0 {
		fmt.Println("DOWNLOADED:")
		for _, m := range models.Downloaded {
			fmt.Printf("  %s\t%s\n", m.Name, m.Size)
		}
		fmt.Println()
	}

	if !downloaded && len(models.Available) > 0 {
		fmt.Println("AVAILABLE:")
		for _, m := range models.Available {
			fmt.Printf("  %s\t%s\n", m.Name, m.Size)
		}
	}

	if len(models.Downloaded) == 0 && len(models.Available) == 0 {
		fmt.Println("No models available. This feature requires Apple Silicon.")
	}
}

func cmdModel(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp model <pull|run|stop|rm> [model]")
		os.Exit(1)
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "pull":
		cmdModelPull(subargs)
	case "run":
		cmdModelRun(subargs)
	case "stop":
		cmdModelStop(subargs)
	case "rm", "remove", "delete":
		cmdModelRm(subargs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown model command: %s\n", subcmd)
		fmt.Fprintln(os.Stderr, "Usage: bp model <pull|run|stop|rm> [model]")
		os.Exit(1)
	}
}

func cmdModelPull(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp model pull <model>")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  bp model pull Llama-3.2-3B")
		fmt.Fprintln(os.Stderr, "  bp model pull mlx-community/Llama-3.2-3B-Instruct-4bit")
		os.Exit(1)
	}

	model := args[0]
	fmt.Printf("Pulling %s...\n", model)

	resp, err := apiRequest("POST", "/api/models/pull", map[string]string{"model": model})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Stream progress
	buf := make([]byte, 256)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			fmt.Print(string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "\nPull failed\n")
		os.Exit(1)
	}

	fmt.Println("\nModel downloaded successfully!")
}

func cmdModelRun(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp model run <model>")
		os.Exit(1)
	}

	model := args[0]
	fmt.Printf("Starting LLM server with %s...\n", model)

	resp, err := apiRequest("POST", "/api/models/run", map[string]string{"model": model})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to start: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		URL string `json:"url"`
		API string `json:"api"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Server running!\n")
	if result.URL != "" {
		fmt.Printf("URL: %s\n", result.URL)
	}
	if result.API != "" {
		fmt.Printf("API: %s\n", result.API)
	}
}

func cmdModelStop(args []string) {
	fmt.Println("Stopping LLM server...")

	resp, err := apiRequest("POST", "/api/models/stop", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to stop: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Println("LLM server stopped")
}

func cmdModelRm(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp model rm <model>")
		os.Exit(1)
	}

	model := args[0]

	fmt.Printf("Are you sure you want to delete '%s'? (y/N): ", model)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled")
		return
	}

	resp, err := apiRequest("DELETE", "/api/models/"+model, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Failed to delete: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("Model '%s' deleted\n", model)
}

func cmdChat(args []string) {
	fmt.Println("Connecting to LLM server...")

	// Check if model is running
	resp, err := apiRequest("GET", "/api/models/status", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status struct {
		Running bool   `json:"running"`
		Model   string `json:"model"`
		URL     string `json:"url"`
	}
	json.NewDecoder(resp.Body).Decode(&status)

	if !status.Running {
		fmt.Fprintln(os.Stderr, "No model is running. Start one with: bp model run <model>")
		os.Exit(1)
	}

	fmt.Printf("Connected to %s\n\n", status.Model)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			break
		}

		// Send message to LLM
		chatReq := map[string]interface{}{
			"messages": []map[string]string{
				{"role": "user", "content": input},
			},
			"stream": true,
		}

		resp, err := apiRequest("POST", "/api/chat/completions", chatReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		fmt.Print("AI: ")
		buf := make([]byte, 256)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				fmt.Print(string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		resp.Body.Close()
		fmt.Println("\n")
	}
}

// ==================== System Commands ====================

func cmdPrune(args []string) {
	all := false
	dryRun := false

	for _, arg := range args {
		switch arg {
		case "--all":
			all = true
		case "--dry-run":
			dryRun = true
		}
	}

	req := map[string]bool{
		"all":    all,
		"dryRun": dryRun,
	}

	if dryRun {
		fmt.Println("Dry run - showing what would be removed:")
	} else {
		fmt.Println("Cleaning unused resources...")
	}

	resp, err := apiRequest("POST", "/api/system/prune", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		ContainersRemoved int    `json:"containersRemoved"`
		ImagesRemoved     int    `json:"imagesRemoved"`
		VolumesRemoved    int    `json:"volumesRemoved"`
		SpaceReclaimed    string `json:"spaceReclaimed"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Containers removed: %d\n", result.ContainersRemoved)
	fmt.Printf("Images removed: %d\n", result.ImagesRemoved)
	fmt.Printf("Volumes removed: %d\n", result.VolumesRemoved)
	if result.SpaceReclaimed != "" {
		fmt.Printf("Space reclaimed: %s\n", result.SpaceReclaimed)
	}
}

// cmdBackup handles backup commands
func cmdBackup(args []string) {
	if len(args) == 0 {
		// Default: list backups
		listBackups()
		return
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "list", "ls":
		listBackups()
	case "create", "new":
		createBackup(subargs)
	case "download", "get":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: bp backup download <backup-id>")
			os.Exit(1)
		}
		downloadBackup(subargs[0])
	case "delete", "rm":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: bp backup delete <backup-id>")
			os.Exit(1)
		}
		deleteBackup(subargs[0])
	case "restore":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: bp backup restore <backup-id>")
			os.Exit(1)
		}
		restoreBackup(subargs[0], subargs[1:])
	case "help", "-h", "--help":
		printBackupHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown backup command: %s\n", subcmd)
		printBackupHelp()
		os.Exit(1)
	}
}

func printBackupHelp() {
	fmt.Println(`Backup Commands:
  bp backup                   List all backups
  bp backup list              List all backups
  bp backup create            Create a new backup
  bp backup restore <id>      Restore from a backup
  bp backup download <id>     Download a backup file
  bp backup delete <id>       Delete a backup

Create Options:
  --volumes      Include container volumes (default: true)
  --no-volumes   Exclude container volumes
  --builds       Include build sources

Restore Options:
  --no-database  Don't restore database
  --no-config    Don't restore config files
  --no-apps      Don't restore static sites
  --no-volumes   Don't restore container volumes

Examples:
  bp backup create                    # Full backup
  bp backup create --no-volumes       # Backup without volumes
  bp backup restore 20260130-151200   # Full restore
  bp backup restore 20260130-151200 --no-config  # Restore without config`)
}

func listBackups() {
	resp, err := apiRequest("GET", "/api/backups", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	var backups []struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		Size      int64     `json:"size"`
		SizeHuman string    `json:"size_human"`
		Contents  struct {
			Database    bool     `json:"database"`
			Config      bool     `json:"config"`
			StaticSites []string `json:"static_sites"`
			Volumes     []string `json:"volumes"`
		} `json:"contents"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&backups); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if len(backups) == 0 {
		fmt.Println("No backups found.")
		fmt.Println("\nCreate a backup with: bp backup create")
		return
	}

	fmt.Printf("%-20s %-20s %-10s %s\n", "ID", "CREATED", "SIZE", "CONTENTS")
	fmt.Println(strings.Repeat("-", 70))

	for _, b := range backups {
		contents := []string{}
		if b.Contents.Database {
			contents = append(contents, "db")
		}
		if b.Contents.Config {
			contents = append(contents, "config")
		}
		if len(b.Contents.StaticSites) > 0 {
			contents = append(contents, fmt.Sprintf("%d sites", len(b.Contents.StaticSites)))
		}
		if len(b.Contents.Volumes) > 0 {
			contents = append(contents, fmt.Sprintf("%d volumes", len(b.Contents.Volumes)))
		}

		fmt.Printf("%-20s %-20s %-10s %s\n",
			b.ID,
			b.CreatedAt.Format("2006-01-02 15:04:05"),
			b.SizeHuman,
			strings.Join(contents, ", "),
		)
	}
}

func createBackup(args []string) {
	includeVolumes := true
	includeBuilds := false

	for _, arg := range args {
		switch arg {
		case "--volumes":
			includeVolumes = true
		case "--no-volumes":
			includeVolumes = false
		case "--builds":
			includeBuilds = true
		}
	}

	fmt.Println("Creating backup...")

	req := map[string]bool{
		"include_volumes": includeVolumes,
		"include_builds":  includeBuilds,
	}

	resp, err := apiRequest("POST", "/api/backups", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		ID        string `json:"id"`
		SizeHuman string `json:"size_human"`
		Path      string `json:"path"`
		Contents  struct {
			Database    bool     `json:"database"`
			Config      bool     `json:"config"`
			StaticSites []string `json:"static_sites"`
			Volumes     []string `json:"volumes"`
		} `json:"contents"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Backup created successfully!")
	fmt.Printf("  ID:       %s\n", result.ID)
	fmt.Printf("  Size:     %s\n", result.SizeHuman)
	fmt.Printf("  Path:     %s\n", result.Path)
	fmt.Println("  Contents:")
	if result.Contents.Database {
		fmt.Println("    - Database")
	}
	if result.Contents.Config {
		fmt.Println("    - Configuration")
	}
	if len(result.Contents.StaticSites) > 0 {
		fmt.Printf("    - Static sites: %s\n", strings.Join(result.Contents.StaticSites, ", "))
	}
	if len(result.Contents.Volumes) > 0 {
		fmt.Printf("    - Volumes: %s\n", strings.Join(result.Contents.Volumes, ", "))
	}
}

func downloadBackup(id string) {
	// First get backup info to get filename
	resp, err := apiRequest("GET", "/api/backups/"+id, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}
	resp.Body.Close()

	// Download the backup
	filename := fmt.Sprintf("basepod-backup-%s.tar.gz", id)
	fmt.Printf("Downloading backup to %s...\n", filename)

	resp, err = apiRequest("GET", "/api/backups/"+id+"/download", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	// Create local file
	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Copy response to file
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Downloaded %s (%d bytes)\n", filename, written)
}

func deleteBackup(id string) {
	fmt.Printf("Deleting backup %s...\n", id)

	resp, err := apiRequest("DELETE", "/api/backups/"+id, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Println("Backup deleted.")
}

func restoreBackup(id string, args []string) {
	// Parse options
	restoreDatabase := true
	restoreConfig := true
	restoreApps := true
	restoreVolumes := true

	for _, arg := range args {
		switch arg {
		case "--no-database":
			restoreDatabase = false
		case "--no-config":
			restoreConfig = false
		case "--no-apps":
			restoreApps = false
		case "--no-volumes":
			restoreVolumes = false
		}
	}

	// Confirm restore
	fmt.Printf("Restoring from backup %s...\n", id)
	fmt.Println("This will overwrite existing data. Current files will be backed up with .bak extension.")
	fmt.Print("Continue? [y/N]: ")

	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" && confirm != "yes" {
		fmt.Println("Restore cancelled.")
		return
	}

	fmt.Println("\nRestoring...")

	req := map[string]bool{
		"restore_database": restoreDatabase,
		"restore_config":   restoreConfig,
		"restore_apps":     restoreApps,
		"restore_volumes":  restoreVolumes,
	}

	resp, err := apiRequest("POST", "/api/backups/"+id+"/restore", req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	var result struct {
		Success     bool     `json:"success"`
		Database    bool     `json:"database"`
		ConfigFiles []string `json:"config_files"`
		StaticSites []string `json:"static_sites"`
		Volumes     []string `json:"volumes"`
		Warnings    []string `json:"warnings"`
		Message     string   `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nRestore completed!")
	fmt.Println("Restored:")
	if result.Database {
		fmt.Println("  - Database")
	}
	if len(result.ConfigFiles) > 0 {
		fmt.Printf("  - Config files: %s\n", strings.Join(result.ConfigFiles, ", "))
	}
	if len(result.StaticSites) > 0 {
		fmt.Printf("  - Static sites: %s\n", strings.Join(result.StaticSites, ", "))
	}
	if len(result.Volumes) > 0 {
		fmt.Printf("  - Volumes: %s\n", strings.Join(result.Volumes, ", "))
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	fmt.Println("\n" + result.Message)
}

// getLatestVersion fetches the latest version from GitHub
func getLatestVersion() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/base-go/basepod/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

// checkForUpdates checks for updates in the background and prints a notice
func checkForUpdates() {
	latest, err := getLatestVersion()
	if err != nil {
		return // Silently fail
	}

	if latest != version && latest != "" {
		fmt.Fprintf(os.Stderr, "\n📦 Update available: %s → %s\n", version, latest)
		fmt.Fprintf(os.Stderr, "   Run: curl -fsSL https://pod.base.al/cli | bash\n\n")
	}
}

// checkForUpdatesSync checks for updates synchronously (used after bp version)
func checkForUpdatesSync() {
	latest, err := getLatestVersion()
	if err != nil {
		return
	}

	if latest != version && latest != "" {
		fmt.Printf("\n📦 Update available: %s → %s\n", version, latest)
		fmt.Printf("   Run: curl -fsSL https://pod.base.al/cli | bash\n")
	}
}

func cmdUpgrade(args []string) {
	fmt.Println("Checking for updates...")

	latest, err := getLatestVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Current version: %s\n", version)
	fmt.Printf("Latest version:  %s\n", latest)

	if version == latest {
		fmt.Println("You are running the latest version!")
		return
	}

	fmt.Println("\nUpdate available!")
	fmt.Println("\nTo upgrade, run:")
	fmt.Println("  curl -fsSL https://pod.base.al/cli | bash")
}

func cmdCompletion(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: bp completion <bash|zsh|fish>")
		fmt.Println("\nGenerate shell completion script")
		fmt.Println("\nExamples:")
		fmt.Println("  # Bash (add to ~/.bashrc)")
		fmt.Println("  eval \"$(bp completion bash)\"")
		fmt.Println("")
		fmt.Println("  # Zsh (add to ~/.zshrc)")
		fmt.Println("  eval \"$(bp completion zsh)\"")
		fmt.Println("")
		fmt.Println("  # Fish (add to ~/.config/fish/config.fish)")
		fmt.Println("  bp completion fish | source")
		os.Exit(1)
	}

	shell := args[0]
	switch shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		fmt.Fprintf(os.Stderr, "Unsupported shell: %s\n", shell)
		fmt.Println("Supported shells: bash, zsh, fish")
		os.Exit(1)
	}
}

const bashCompletion = `# bp bash completion
_bp_completions() {
    local cur prev commands
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    commands="login logout context init deploy apps create start stop restart logs delete templates template models model chat info status prune upgrade completion version help"

    case "${prev}" in
        bp)
            COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
            return 0
            ;;
        template)
            COMPREPLY=( $(compgen -W "deploy export" -- ${cur}) )
            return 0
            ;;
        model)
            COMPREPLY=( $(compgen -W "pull run stop rm" -- ${cur}) )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- ${cur}) )
            return 0
            ;;
        start|stop|restart|logs|delete|rm)
            # Complete with app names
            local apps=$(bp apps 2>/dev/null | tail -n +2 | awk '{print $1}')
            COMPREPLY=( $(compgen -W "${apps}" -- ${cur}) )
            return 0
            ;;
    esac

    COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
}
complete -F _bp_completions bp
`

const zshCompletion = `#compdef bp

_bp() {
    local -a commands
    commands=(
        'login:Connect to a Basepod server'
        'logout:Disconnect from server'
        'context:List or switch server contexts'
        'init:Initialize basepod.yaml config'
        'deploy:Deploy app (local, image, or git)'
        'apps:List all apps'
        'create:Create a new app'
        'start:Start an app'
        'stop:Stop an app'
        'restart:Restart an app'
        'logs:View app logs'
        'delete:Delete an app'
        'templates:List available templates'
        'template:Template commands (deploy, export)'
        'models:List LLM models'
        'model:Model commands (pull, run, stop, rm)'
        'chat:Interactive chat with LLM'
        'info:Show server info'
        'status:Show detailed status'
        'prune:Clean up unused resources'
        'upgrade:Upgrade Basepod'
        'completion:Generate shell completion'
        'version:Show version'
        'help:Show help'
    )

    local -a template_cmds model_cmds completion_shells
    template_cmds=('deploy:Deploy a template' 'export:Export app as template')
    model_cmds=('pull:Download a model' 'run:Start LLM server' 'stop:Stop LLM server' 'rm:Delete a model')
    completion_shells=('bash:Bash completion' 'zsh:Zsh completion' 'fish:Fish completion')

    _arguments -C \
        '1: :->command' \
        '*: :->args'

    case $state in
        command)
            _describe -t commands 'bp command' commands
            ;;
        args)
            case $words[2] in
                template)
                    _describe -t template_cmds 'template command' template_cmds
                    ;;
                model)
                    _describe -t model_cmds 'model command' model_cmds
                    ;;
                completion)
                    _describe -t completion_shells 'shell' completion_shells
                    ;;
                start|stop|restart|logs|delete|rm)
                    local apps=(${(f)"$(bp apps 2>/dev/null | tail -n +2 | awk '{print $1}')"})
                    _describe -t apps 'app' apps
                    ;;
            esac
            ;;
    esac
}

compdef _bp bp
`

const fishCompletion = `# bp fish completion
complete -c bp -e
complete -c bp -n "__fish_use_subcommand" -a "login" -d "Connect to a Basepod server"
complete -c bp -n "__fish_use_subcommand" -a "logout" -d "Disconnect from server"
complete -c bp -n "__fish_use_subcommand" -a "context" -d "List or switch server contexts"
complete -c bp -n "__fish_use_subcommand" -a "init" -d "Initialize basepod.yaml config"
complete -c bp -n "__fish_use_subcommand" -a "deploy" -d "Deploy app"
complete -c bp -n "__fish_use_subcommand" -a "apps" -d "List all apps"
complete -c bp -n "__fish_use_subcommand" -a "create" -d "Create a new app"
complete -c bp -n "__fish_use_subcommand" -a "start" -d "Start an app"
complete -c bp -n "__fish_use_subcommand" -a "stop" -d "Stop an app"
complete -c bp -n "__fish_use_subcommand" -a "restart" -d "Restart an app"
complete -c bp -n "__fish_use_subcommand" -a "logs" -d "View app logs"
complete -c bp -n "__fish_use_subcommand" -a "delete" -d "Delete an app"
complete -c bp -n "__fish_use_subcommand" -a "templates" -d "List templates"
complete -c bp -n "__fish_use_subcommand" -a "template" -d "Template commands"
complete -c bp -n "__fish_use_subcommand" -a "models" -d "List LLM models"
complete -c bp -n "__fish_use_subcommand" -a "model" -d "Model commands"
complete -c bp -n "__fish_use_subcommand" -a "chat" -d "Interactive chat"
complete -c bp -n "__fish_use_subcommand" -a "info" -d "Show server info"
complete -c bp -n "__fish_use_subcommand" -a "status" -d "Show detailed status"
complete -c bp -n "__fish_use_subcommand" -a "prune" -d "Clean up resources"
complete -c bp -n "__fish_use_subcommand" -a "upgrade" -d "Upgrade Basepod"
complete -c bp -n "__fish_use_subcommand" -a "completion" -d "Generate completion"
complete -c bp -n "__fish_use_subcommand" -a "version" -d "Show version"
complete -c bp -n "__fish_use_subcommand" -a "help" -d "Show help"

complete -c bp -n "__fish_seen_subcommand_from template" -a "deploy" -d "Deploy a template"
complete -c bp -n "__fish_seen_subcommand_from template" -a "export" -d "Export app as template"
complete -c bp -n "__fish_seen_subcommand_from model" -a "pull" -d "Download a model"
complete -c bp -n "__fish_seen_subcommand_from model" -a "run" -d "Start LLM server"
complete -c bp -n "__fish_seen_subcommand_from model" -a "stop" -d "Stop LLM server"
complete -c bp -n "__fish_seen_subcommand_from model" -a "rm" -d "Delete a model"
complete -c bp -n "__fish_seen_subcommand_from completion" -a "bash zsh fish"
`
