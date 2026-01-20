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
	version = "1.0.3"
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

	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("bp version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	case "login":
		cmdLogin(args)
	case "logout":
		cmdLogout(args)
	case "context", "ctx":
		cmdContext(args)
	case "apps", "app", "list", "ls":
		cmdApps(args)
	case "create":
		cmdCreate(args)
	case "deploy":
		cmdDeploy(args)
	case "push":
		cmdPush(args)
	case "logs":
		cmdLogs(args)
	case "start":
		cmdStart(args)
	case "stop":
		cmdStop(args)
	case "restart":
		cmdRestart(args)
	case "delete", "rm":
		cmdDelete(args)
	case "info":
		cmdInfo(args)
	case "status":
		cmdStatus(args)
	case "init":
		cmdInit(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`bp - CLI for Basepod PaaS

Usage:
  bp <command> [arguments]

Commands:
  login <server>    Login to a Basepod server (adds to contexts)
  logout [name]     Logout from server (current or named)
  context [name]    List contexts or switch to named context
  apps              List all apps
  create <name>     Create a new app
  push [path]       Deploy from local source (creates tarball and uploads)
  deploy <name>     Deploy an app with image or git
  logs <name>       View app logs
  start <name>      Start an app
  stop <name>       Stop an app
  restart <name>    Restart an app
  delete <name>     Delete an app
  info              Show server info
  status            Show detailed server and app status
  init              Initialize basepod.yaml in current directory

Options:
  -h, --help        Show help
  -v, --version     Show version

Examples:
  bp login d.example.com
  bp init
  bp push                          # Deploy current directory
  bp push ./myapp                  # Deploy specific path
  bp create myapp -d myapp.example.com
  bp deploy myapp -i nginx:latest
  bp logs myapp -n 50`)
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
		AuthRequired  bool `json:"authRequired"`
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

	// If auth is required, prompt for password
	if authStatus.AuthRequired {
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
	Name    string            `yaml:"name"`
	Server  string            `yaml:"server,omitempty"`  // Server context to deploy to
	Domain  string            `yaml:"domain,omitempty"`
	Port    int               `yaml:"port,omitempty"`
	Build   BuildConfig       `yaml:"build,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Volumes []string          `yaml:"volumes,omitempty"`
}

// BuildConfig contains build configuration
type BuildConfig struct {
	Dockerfile string `yaml:"dockerfile,omitempty"`
	Context    string `yaml:"context,omitempty"`
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
	if len(args) > 0 {
		dir = args[0]
	}

	configPath := filepath.Join(dir, "basepod.yaml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "basepod.yaml already exists\n")
		os.Exit(1)
	}

	// Try to get app name from directory
	absDir, _ := filepath.Abs(dir)
	appName := filepath.Base(absDir)

	// Get current server context if logged in
	cliCfg, _ := loadConfig()
	serverContext := cliCfg.CurrentContext

	cfg := AppConfig{
		Name:   appName,
		Server: serverContext, // Use current context
		Port:   3000,
		Build: BuildConfig{
			Dockerfile: "Dockerfile",
			Context:    ".",
		},
		Env: map[string]string{
			"NODE_ENV": "production",
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate config: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created basepod.yaml for '%s'\n", appName)
	fmt.Println("\nEdit the file to configure your app, then run:")
	fmt.Println("  bp push")
}

func cmdPush(args []string) {
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
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

	// Load CLI config
	cliCfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Determine which server to use:
	// 1. If basepod.yaml has 'server' field, use that context
	// 2. Otherwise use current context
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

	fmt.Printf("Pushing %s to %s...\n", appCfg.Name, contextName)

	// Create tarball of the directory
	tarball, err := createTarball(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create tarball: %v\n", err)
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

func cmdDeploy(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: bp deploy <name> --image <image>")
		os.Exit(1)
	}

	name := args[0]
	req := app.DeployRequest{}

	// Parse flags
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--image", "-i":
			if i+1 < len(args) {
				req.Image = args[i+1]
				i++
			}
		case "--git", "-g":
			if i+1 < len(args) {
				req.GitURL = args[i+1]
				i++
			}
		case "--branch", "-b":
			if i+1 < len(args) {
				req.Branch = args[i+1]
				i++
			}
		}
	}

	if req.Image == "" && req.GitURL == "" {
		fmt.Fprintln(os.Stderr, "Error: --image or --git is required")
		fmt.Fprintln(os.Stderr, "Tip: Use 'bp push' to deploy from local source")
		os.Exit(1)
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
