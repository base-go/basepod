// Package proxy provides reverse proxy management using Caddy.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/config"
)

// CaddyManager manages the Caddy reverse proxy
type CaddyManager struct {
	adminAPI   string // Caddy admin API endpoint
	configPath string // Path to Caddyfile
	client     *http.Client
}

// NewCaddyManager creates a new Caddy manager
func NewCaddyManager() (*CaddyManager, error) {
	paths, err := config.GetPaths()
	if err != nil {
		return nil, err
	}

	return &CaddyManager{
		adminAPI:   "http://localhost:2019",
		configPath: filepath.Join(paths.Caddy, "Caddyfile"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// CaddyConfig represents the JSON configuration for Caddy
type CaddyConfig struct {
	Apps CaddyApps `json:"apps"`
}

type CaddyApps struct {
	HTTP CaddyHTTP `json:"http"`
}

type CaddyHTTP struct {
	Servers map[string]*CaddyServer `json:"servers"`
}

type CaddyServer struct {
	Listen []string      `json:"listen"`
	Routes []CaddyRoute  `json:"routes"`
}

type CaddyRoute struct {
	Match   []CaddyMatch   `json:"match,omitempty"`
	Handle  []CaddyHandler `json:"handle"`
	Terminal bool          `json:"terminal,omitempty"`
}

type CaddyMatch struct {
	Host []string `json:"host,omitempty"`
	Path []string `json:"path,omitempty"`
}

type CaddyHandler struct {
	Handler   string            `json:"handler"`
	Upstreams []CaddyUpstream   `json:"upstreams,omitempty"`
	Routes    []CaddyRoute      `json:"routes,omitempty"`
	Root      string            `json:"root,omitempty"`
	Body      string            `json:"body,omitempty"`
	Headers   *CaddyHeaders     `json:"headers,omitempty"`
}

type CaddyUpstream struct {
	Dial string `json:"dial"`
}

type CaddyHeaders struct {
	Response map[string][]string `json:"response,omitempty"`
}

// AppRoute represents routing config for an app
type AppRoute struct {
	App      *app.App
	Upstream string // e.g., "localhost:8080"
}

// GenerateCaddyfile generates a Caddyfile from app routes
func (m *CaddyManager) GenerateCaddyfile(routes []AppRoute, rootDomain string, email string) error {
	tmpl := `# Deployer Caddy Configuration
# Generated at {{ .GeneratedAt }}
# Do not edit manually - changes will be overwritten

{
	# Global options
	{{- if .Email }}
	email {{ .Email }}
	{{- end }}
	admin localhost:2019

	# Use Let's Encrypt staging for testing
	# acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
}

# Deployer Web UI and API
{{ .RootDomain }} {
	# API endpoints
	handle /api/* {
		reverse_proxy localhost:3000
	}

	# WebSocket support for logs
	handle /ws/* {
		reverse_proxy localhost:3000
	}

	# Web UI (Nuxt)
	handle {
		reverse_proxy localhost:4000
	}
}

{{- range .Routes }}

# App: {{ .App.Name }}
{{ .App.Domain }} {
	reverse_proxy {{ .Upstream }} {
		header_up Host {host}
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
	}

	{{- if .App.SSL.Enabled }}
	# SSL enabled - automatic via Let's Encrypt
	{{- else }}
	# SSL disabled - using HTTP only
	{{- end }}
}
{{- end }}
`

	t, err := template.New("caddyfile").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	data := struct {
		GeneratedAt string
		RootDomain  string
		Email       string
		Routes      []AppRoute
	}{
		GeneratedAt: time.Now().Format(time.RFC3339),
		RootDomain:  rootDomain,
		Email:       email,
		Routes:      routes,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(m.configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	return nil
}

// GenerateJSONConfig generates JSON config for Caddy API
func (m *CaddyManager) GenerateJSONConfig(routes []AppRoute, rootDomain string) *CaddyConfig {
	caddyRoutes := make([]CaddyRoute, 0)

	// Add root domain route (API + UI)
	if rootDomain != "" {
		caddyRoutes = append(caddyRoutes, CaddyRoute{
			Match: []CaddyMatch{{Host: []string{rootDomain}}},
			Handle: []CaddyHandler{
				{
					Handler: "subroute",
					Routes: []CaddyRoute{
						{
							Match: []CaddyMatch{{Path: []string{"/api/*", "/ws/*"}}},
							Handle: []CaddyHandler{
								{
									Handler:   "reverse_proxy",
									Upstreams: []CaddyUpstream{{Dial: "localhost:3000"}},
								},
							},
						},
						{
							Handle: []CaddyHandler{
								{
									Handler:   "reverse_proxy",
									Upstreams: []CaddyUpstream{{Dial: "localhost:4000"}},
								},
							},
						},
					},
				},
			},
			Terminal: true,
		})
	}

	// Add app routes
	for _, route := range routes {
		if route.App.Domain == "" {
			continue
		}
		caddyRoutes = append(caddyRoutes, CaddyRoute{
			Match: []CaddyMatch{{Host: []string{route.App.Domain}}},
			Handle: []CaddyHandler{
				{
					Handler:   "reverse_proxy",
					Upstreams: []CaddyUpstream{{Dial: route.Upstream}},
				},
			},
			Terminal: true,
		})
	}

	return &CaddyConfig{
		Apps: CaddyApps{
			HTTP: CaddyHTTP{
				Servers: map[string]*CaddyServer{
					"srv0": {
						Listen: []string{":443", ":80"},
						Routes: caddyRoutes,
					},
				},
			},
		},
	}
}

// Reload tells Caddy to reload its configuration
func (m *CaddyManager) Reload(ctx context.Context) error {
	// Try API reload first
	if err := m.reloadViaAPI(ctx); err == nil {
		return nil
	}

	// Fall back to file-based reload
	return m.reloadViaFile(ctx)
}

// reloadViaAPI reloads Caddy using its admin API
func (m *CaddyManager) reloadViaAPI(ctx context.Context) error {
	configData, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.adminAPI+"/load", bytes.NewReader(configData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy reload failed: %s", string(body))
	}

	return nil
}

// reloadViaFile reloads Caddy by sending SIGUSR1 (Unix only)
func (m *CaddyManager) reloadViaFile(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("file-based reload not supported on Windows")
	}

	// Try to find Caddy process
	cmd := exec.CommandContext(ctx, "pkill", "-USR1", "caddy")
	return cmd.Run()
}

// ApplyJSONConfig applies JSON configuration via Caddy API
func (m *CaddyManager) ApplyJSONConfig(ctx context.Context, cfg *CaddyConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.adminAPI+"/load", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy config apply failed: %s", string(body))
	}

	return nil
}

// AddRoute adds a route for an app
func (m *CaddyManager) AddRoute(ctx context.Context, a *app.App, upstream string) error {
	route := map[string]interface{}{
		"match": []map[string]interface{}{
			{"host": []string{a.Domain}},
		},
		"handle": []map[string]interface{}{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]interface{}{
					{"dial": upstream},
				},
			},
		},
		"terminal": true,
	}

	data, err := json.Marshal(route)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.adminAPI+"/config/apps/http/servers/srv0/routes", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add route: %s", string(body))
	}

	return nil
}

// RemoveRoute removes a route for an app
func (m *CaddyManager) RemoveRoute(ctx context.Context, domain string) error {
	// Get current config
	resp, err := m.client.Get(m.adminAPI + "/config/apps/http/servers/srv0/routes")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var routes []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return err
	}

	// Find and remove the route
	for i, routeData := range routes {
		var route CaddyRoute
		if err := json.Unmarshal(routeData, &route); err != nil {
			continue
		}

		for _, match := range route.Match {
			for _, host := range match.Host {
				if host == domain {
					// Delete this route
					req, err := http.NewRequestWithContext(ctx, "DELETE",
						fmt.Sprintf("%s/config/apps/http/servers/srv0/routes/%d", m.adminAPI, i), nil)
					if err != nil {
						return err
					}

					delResp, err := m.client.Do(req)
					if err != nil {
						return err
					}
					delResp.Body.Close()

					return nil
				}
			}
		}
	}

	return nil
}

// IsRunning checks if Caddy is running
func (m *CaddyManager) IsRunning(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", m.adminAPI+"/config/", nil)
	if err != nil {
		return false
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetCaddyBinaryPath returns the path where Caddy should be installed
func GetCaddyBinaryPath() (string, error) {
	paths, err := config.GetPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.Bin, "caddy"), nil
}

// IsCaddyInstalled checks if Caddy binary exists
func IsCaddyInstalled() bool {
	path, err := GetCaddyBinaryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
