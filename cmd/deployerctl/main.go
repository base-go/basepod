// Package main is the entry point for the deployerctl CLI.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/deployer/deployer/internal/app"
	"gopkg.in/yaml.v3"
)

var (
	version = "0.1.0"
)

// CLIConfig holds CLI configuration
type CLIConfig struct {
	Server string `yaml:"server"`
	Token  string `yaml:"token,omitempty"`
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
		fmt.Printf("deployerctl version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	case "login":
		cmdLogin(args)
	case "apps", "app":
		cmdApps(args)
	case "create":
		cmdCreate(args)
	case "deploy":
		cmdDeploy(args)
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
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`deployerctl - CLI for Deployer

Usage:
  deployerctl <command> [arguments]

Commands:
  login <server>    Login to a Deployer server
  apps              List all apps
  create <name>     Create a new app
  deploy <name>     Deploy an app
  logs <name>       View app logs
  start <name>      Start an app
  stop <name>       Stop an app
  restart <name>    Restart an app
  delete <name>     Delete an app
  info              Show server info

Options:
  -h, --help        Show help
  -v, --version     Show version

Examples:
  deployerctl login https://deployer.example.com
  deployerctl create myapp --domain myapp.example.com
  deployerctl deploy myapp --image nginx:latest
  deployerctl logs myapp --tail 50`)
}

// getConfigPath returns the path to the CLI config file
func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deployerctl.yaml")
}

// loadConfig loads the CLI configuration
func loadConfig() (*CLIConfig, error) {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIConfig{}, nil
		}
		return nil, err
	}

	var cfg CLIConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
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

// getClient returns an HTTP client configured for the server
func getClient() (*http.Client, string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, "", err
	}

	if cfg.Server == "" {
		return nil, "", fmt.Errorf("not logged in. Run: deployerctl login <server>")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return client, cfg.Server, nil
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
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	return client.Do(req)
}

func cmdLogin(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: deployerctl login <server>")
		os.Exit(1)
	}

	server := args[0]
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		server = "https://" + server
	}

	// Test connection
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(server + "/api/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Server returned status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	cfg := &CLIConfig{Server: server}
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Logged in to %s\n", server)
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
		fmt.Println("No apps found. Create one with: deployerctl create <name>")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl create <name> [--domain <domain>] [--port <port>]")
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
	fmt.Printf("  deployerctl deploy %s --image <image>\n", name)
}

func cmdDeploy(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: deployerctl deploy <name> --image <image>")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl logs <name> [--tail <n>]")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl start <name>")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl stop <name>")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl restart <name>")
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
		fmt.Fprintln(os.Stderr, "Usage: deployerctl delete <name>")
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
