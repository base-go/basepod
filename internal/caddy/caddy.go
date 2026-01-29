// Package caddy provides integration with Caddy server for reverse proxy and SSL.
package caddy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client manages Caddy configuration via its admin API
type Client struct {
	adminURL   string
	httpClient *http.Client
}

// Route represents a reverse proxy route
type Route struct {
	ID          string
	Domain      string
	Upstream    string // e.g., "localhost:8080" or container IP
	EnableSSL   bool
	ForceHTTPS  bool
}

// NewClient creates a new Caddy client
func NewClient(adminURL string) *Client {
	if adminURL == "" {
		adminURL = "http://localhost:2019"
	}
	return &Client{
		adminURL: adminURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Ping checks if Caddy admin API is accessible
func (c *Client) Ping() error {
	resp, err := c.httpClient.Get(c.adminURL + "/config/")
	if err != nil {
		return fmt.Errorf("failed to connect to Caddy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Caddy returned status %d", resp.StatusCode)
	}
	return nil
}

// AddRoute adds a reverse proxy route for an app (removes existing route with same ID first)
func (c *Client) AddRoute(route Route) error {
	// Remove existing route with same ID first (ignore errors - route may not exist)
	c.RemoveRoute(route.ID)

	// Build the route configuration with proper headers for reverse proxy
	routeConfig := map[string]interface{}{
		"@id": route.ID,
		"match": []map[string]interface{}{
			{"host": []string{route.Domain}},
		},
		"handle": []map[string]interface{}{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]string{
					{"dial": route.Upstream},
				},
				"headers": map[string]interface{}{
					"request": map[string]interface{}{
						"set": map[string][]string{
							"Host":             {"{http.request.host}"},
							"X-Forwarded-Host": {"{http.request.host}"},
							"X-Forwarded-Proto": {"{http.request.scheme}"},
							"X-Real-IP":        {"{http.request.remote.host}"},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(routeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal route config: %w", err)
	}

	// Add to main server (srv0) routes - prepend to take priority over wildcard
	url := c.adminURL + "/config/apps/http/servers/srv0/routes/0"
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to add route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to add route (status %d)", resp.StatusCode)
	}

	return nil
}

// InitializeServer adds routes for running apps to the existing Caddy server
// Note: The main server (srv0) should already be configured via Caddyfile
// This function adds dynamic routes for container apps without disturbing existing config
func (c *Client) InitializeServer(routes []Route) error {
	// Check if srv0 already exists (configured by Caddyfile)
	resp, err := c.httpClient.Get(c.adminURL + "/config/apps/http/servers/srv0")
	if err != nil {
		return fmt.Errorf("failed to check server config: %w", err)
	}
	resp.Body.Close()

	// If server already exists (from Caddyfile), just add routes individually
	if resp.StatusCode == http.StatusOK {
		for _, route := range routes {
			if err := c.AddRoute(route); err != nil {
				// Log but don't fail - route might already exist
				continue
			}
		}
		return nil
	}

	// No server exists - this shouldn't happen if Caddyfile is loaded
	// Return nil to avoid breaking startup, routes will be added as apps deploy
	return nil
}

// UpdateRoute updates an existing route
func (c *Client) UpdateRoute(route Route) error {
	routeConfig := map[string]interface{}{
		"@id": route.ID,
		"match": []map[string]interface{}{
			{"host": []string{route.Domain}},
		},
		"handle": []map[string]interface{}{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]string{
					{"dial": route.Upstream},
				},
			},
		},
	}

	body, err := json.Marshal(routeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal route config: %w", err)
	}

	url := c.adminURL + "/id/" + route.ID
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update route (status %d)", resp.StatusCode)
	}

	return nil
}

// RemoveRoute removes a route by ID
func (c *Client) RemoveRoute(routeID string) error {
	url := c.adminURL + "/id/" + routeID
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to remove route: %w", err)
	}
	defer resp.Body.Close()

	// 404 is ok - route already doesn't exist
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("failed to remove route (status %d)", resp.StatusCode)
	}

	return nil
}

// GetRoutes returns all configured routes
func (c *Client) GetRoutes() ([]Route, error) {
	resp, err := c.httpClient.Get(c.adminURL + "/config/apps/http/servers/srv0/routes")
	if err != nil {
		return nil, fmt.Errorf("failed to get routes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []Route{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get routes (status %d)", resp.StatusCode)
	}

	var rawRoutes []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawRoutes); err != nil {
		return nil, fmt.Errorf("failed to decode routes: %w", err)
	}

	routes := make([]Route, 0, len(rawRoutes))
	for _, raw := range rawRoutes {
		route := Route{}
		if id, ok := raw["@id"].(string); ok {
			route.ID = id
		}
		if matches, ok := raw["match"].([]interface{}); ok && len(matches) > 0 {
			if match, ok := matches[0].(map[string]interface{}); ok {
				if hosts, ok := match["host"].([]interface{}); ok && len(hosts) > 0 {
					if host, ok := hosts[0].(string); ok {
						route.Domain = host
					}
				}
			}
		}
		if handles, ok := raw["handle"].([]interface{}); ok && len(handles) > 0 {
			if handle, ok := handles[0].(map[string]interface{}); ok {
				if upstreams, ok := handle["upstreams"].([]interface{}); ok && len(upstreams) > 0 {
					if upstream, ok := upstreams[0].(map[string]interface{}); ok {
						if dial, ok := upstream["dial"].(string); ok {
							route.Upstream = dial
						}
					}
				}
			}
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// AddStaticRoute adds a static file serving route for a domain
func (c *Client) AddStaticRoute(domain, rootDir string) error {
	routeID := "static-" + domain

	// Remove existing route with same ID first
	c.RemoveRoute(routeID)

	// Build static file server route with SPA support
	routeConfig := map[string]interface{}{
		"@id": routeID,
		"match": []map[string]interface{}{
			{"host": []string{domain}},
		},
		"terminal": true,
		"handle": []map[string]interface{}{
			{
				"handler": "subroute",
				"routes": []map[string]interface{}{
					{
						"handle": []map[string]interface{}{
							{
								"handler": "vars",
								"root":    rootDir,
							},
						},
					},
					{
						"match": []map[string]interface{}{
							{
								"file": map[string]interface{}{
									"try_files": []string{"{http.request.uri.path}", "{http.request.uri.path}/index.html", "/index.html"},
								},
							},
						},
						"handle": []map[string]interface{}{
							{
								"handler": "rewrite",
								"uri":     "{http.matchers.file.relative}",
							},
						},
					},
					{
						"handle": []map[string]interface{}{
							{
								"handler": "encode",
								"encodings": map[string]interface{}{
									"gzip": map[string]interface{}{},
								},
								"prefer": []string{"gzip"},
							},
							{
								"handler": "file_server",
								"hide":    []string{"./Caddyfile"},
							},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(routeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal route config: %w", err)
	}

	// Add to main server routes - prepend to take priority
	url := c.adminURL + "/config/apps/http/servers/srv0/routes/0"
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to add static route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to add static route (status %d)", resp.StatusCode)
	}

	return nil
}
