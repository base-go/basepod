// Package api provides the REST API for deployer.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/auth"
	"github.com/deployer/deployer/internal/caddy"
	"github.com/deployer/deployer/internal/config"
	"github.com/deployer/deployer/internal/podman"
	"github.com/deployer/deployer/internal/storage"
	"github.com/deployer/deployer/internal/templates"
	"github.com/deployer/deployer/internal/web"
	"github.com/google/uuid"
)

// assignHostPort generates a unique host port based on app ID
func assignHostPort(appID string) int {
	h := fnv.New32a()
	h.Write([]byte(appID))
	// Port range 10000-60000
	return 10000 + int(h.Sum32()%50000)
}

// Server represents the API server
type Server struct {
	storage      *storage.Storage
	podman       podman.Client
	caddy        *caddy.Client
	config       *config.Config
	auth         *auth.Manager
	router       *http.ServeMux
	staticFS     http.Handler
	staticDir    string // Path to static files on disk (preferred over embedded)
	version      string
}

// NewServer creates a new API server
func NewServer(store *storage.Storage, pm podman.Client, caddyClient *caddy.Client) *Server {
	return NewServerWithVersion(store, pm, caddyClient, "0.1.0")
}

// NewServerWithVersion creates a new API server with version
func NewServerWithVersion(store *storage.Storage, pm podman.Client, caddyClient *caddy.Client, version string) *Server {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	s := &Server{
		storage: store,
		podman:  pm,
		caddy:   caddyClient,
		config:  cfg,
		auth:    auth.NewManager(cfg.Auth.PasswordHash),
		router:  http.NewServeMux(),
		version: version,
	}

	// Setup static file serving - prefer disk over embedded
	// Check /opt/deployer/web first, then embedded files
	staticPaths := []string{
		"/opt/deployer/web",
		os.Getenv("DEPLOYER_WEB_DIR"),
	}
	for _, dir := range staticPaths {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			// Check if index.html exists
			if _, err := os.Stat(dir + "/index.html"); err == nil {
				s.staticDir = dir
				s.staticFS = http.FileServer(http.Dir(dir))
				break
			}
		}
	}
	// Fall back to embedded files
	if s.staticFS == nil {
		if staticFS, err := web.GetFileSystem(); err == nil {
			s.staticFS = http.FileServer(http.FS(staticFS))
		}
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Health check (no auth required)
	s.router.HandleFunc("GET /health", s.handleHealth)
	s.router.HandleFunc("GET /api/health", s.handleHealth)

	// Auth routes (no auth required)
	s.router.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.router.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.router.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.router.HandleFunc("POST /api/auth/change-password", s.requireAuth(s.handleChangePassword))

	// Apps (auth required)
	s.router.HandleFunc("GET /api/apps", s.requireAuth(s.handleListApps))
	s.router.HandleFunc("POST /api/apps", s.requireAuth(s.handleCreateApp))
	s.router.HandleFunc("GET /api/apps/{id}", s.requireAuth(s.handleGetApp))
	s.router.HandleFunc("PUT /api/apps/{id}", s.requireAuth(s.handleUpdateApp))
	s.router.HandleFunc("DELETE /api/apps/{id}", s.requireAuth(s.handleDeleteApp))

	// App actions (auth required)
	s.router.HandleFunc("POST /api/apps/{id}/start", s.requireAuth(s.handleStartApp))
	s.router.HandleFunc("POST /api/apps/{id}/stop", s.requireAuth(s.handleStopApp))
	s.router.HandleFunc("POST /api/apps/{id}/restart", s.requireAuth(s.handleRestartApp))
	s.router.HandleFunc("POST /api/apps/{id}/deploy", s.requireAuth(s.handleDeployApp))
	s.router.HandleFunc("GET /api/apps/{id}/logs", s.requireAuth(s.handleGetAppLogs))

	// System (auth required)
	s.router.HandleFunc("GET /api/system/info", s.requireAuth(s.handleSystemInfo))
	s.router.HandleFunc("GET /api/system/config", s.handleGetConfig) // No auth - needed for login page
	s.router.HandleFunc("GET /api/system/version", s.requireAuth(s.handleGetVersion))
	s.router.HandleFunc("POST /api/system/update", s.requireAuth(s.handleSystemUpdate))
	s.router.HandleFunc("POST /api/system/prune", s.requireAuth(s.handleSystemPrune))
	s.router.HandleFunc("GET /api/containers", s.requireAuth(s.handleListContainers))

	// Templates (auth required)
	s.router.HandleFunc("GET /api/templates", s.requireAuth(s.handleListTemplates))
	s.router.HandleFunc("POST /api/templates/{id}/deploy", s.requireAuth(s.handleDeployTemplate))

	// Caddy on-demand TLS check (no auth - called by Caddy)
	s.router.HandleFunc("GET /api/caddy/check", s.handleCaddyCheck)

	// Source deploy endpoint (auth required)
	s.router.HandleFunc("POST /api/deploy", s.requireAuth(s.handleSourceDeploy))
}

// requireAuth wraps a handler with authentication check
func (s *Server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.IsAuthRequired() {
			handler(w, r)
			return
		}

		// Check for token in cookie or Authorization header
		token := ""
		if cookie, err := r.Cookie("deployer_token"); err == nil {
			token = cookie.Value
		}
		if token == "" {
			token = r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
		}

		if !s.auth.ValidateSession(token) {
			errorResponse(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		handler(w, r)
	}
}

// handleLogin handles password authentication
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if !s.auth.ValidatePassword(req.Password) {
		errorResponse(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	session, err := s.auth.CreateSession()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "deployer_token",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
	})
}

// handleLogout handles logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("deployer_token"); err == nil {
		s.auth.DeleteSession(cookie.Value)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "deployer_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	jsonResponse(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// handleAuthStatus returns current auth status
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	authRequired := s.auth.IsAuthRequired()
	authenticated := false

	if authRequired {
		token := ""
		if cookie, err := r.Cookie("deployer_token"); err == nil {
			token = cookie.Value
		}
		if token == "" {
			token = r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
		}
		authenticated = s.auth.ValidateSession(token)
	} else {
		authenticated = true
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"authRequired":  authRequired,
		"authenticated": authenticated,
	})
}

// handleChangePassword handles password change
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if !s.auth.ValidatePassword(req.CurrentPassword) {
		errorResponse(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	if len(req.NewPassword) < 8 {
		errorResponse(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	// Update password in memory
	s.auth.UpdatePassword(req.NewPassword)

	// Update config file
	s.config.Auth.PasswordHash = s.auth.GetPasswordHash()
	if err := s.config.Save(); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to save config")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "password changed"})
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check if this is an app domain and proxy to the app
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Check if it's an app domain (not the dashboard)
	dashboardDomain := "d." + s.config.Domain.Base
	if host != dashboardDomain && s.config.Domain.Base != "" && strings.HasSuffix(host, "."+s.config.Domain.Base) {
		// Look up app by domain
		if a, _ := s.storage.GetAppByDomain(host); a != nil && a.Status == app.StatusRunning && a.Ports.HostPort > 0 {
			// Proxy to the app
			s.proxyToApp(w, r, a)
			return
		}
	}

	// Serve API routes
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
		s.router.ServeHTTP(w, r)
		return
	}

	// Serve static files for everything else
	if s.staticFS != nil {
		// Try to serve the exact file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists (disk or embedded)
		fileExists := false
		if s.staticDir != "" {
			// Check disk
			if _, err := os.Stat(s.staticDir + path); err == nil {
				fileExists = true
			}
		} else if webFS, err := web.GetFileSystem(); err == nil {
			// Check embedded
			if _, err := fs.Stat(webFS, strings.TrimPrefix(path, "/")); err == nil {
				fileExists = true
			}
		}

		if fileExists {
			// Set proper MIME type for JS files
			if strings.HasSuffix(path, ".js") {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			} else if strings.HasSuffix(path, ".css") {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			} else if strings.HasSuffix(path, ".json") {
				w.Header().Set("Content-Type", "application/json")
			} else if strings.HasSuffix(path, ".svg") {
				w.Header().Set("Content-Type", "image/svg+xml")
			}
			// Cache hashed assets forever, don't cache HTML
			if strings.Contains(path, "/_nuxt/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if strings.HasSuffix(path, ".html") || path == "/" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}
			s.staticFS.ServeHTTP(w, r)
			return
		}

		// For SPA routing, serve index.html for non-existent paths
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		r.URL.Path = "/"
		s.staticFS.ServeHTTP(w, r)
		return
	}

	// No static files embedded, return API info
	jsonResponse(w, http.StatusOK, map[string]string{
		"name":    "Deployer API",
		"version": "0.1.0",
		"message": "Web UI not available. Use API endpoints at /api/*",
	})
}

// Response helpers
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

// Health check handler
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
	}

	// Check Podman connection
	if err := s.podman.Ping(ctx); err != nil {
		status["podman"] = "disconnected"
		status["podman_error"] = err.Error()
	} else {
		status["podman"] = "connected"
	}

	jsonResponse(w, http.StatusOK, status)
}

// handleListApps lists all apps
func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.storage.ListApps()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	if apps == nil {
		apps = []app.App{}
	}

	jsonResponse(w, http.StatusOK, app.AppListResponse{
		Apps:  apps,
		Total: len(apps),
	})
}

// handleCreateApp creates a new app
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req app.CreateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		errorResponse(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Check if app already exists by name
	existing, _ := s.storage.GetAppByName(req.Name)
	if existing != nil {
		errorResponse(w, http.StatusConflict, "App with this name already exists")
		return
	}

	// Set defaults
	port := req.Port
	if port == 0 {
		port = 8080
	}

	// Auto-assign domain from config if not specified
	domain := req.Domain
	if domain == "" {
		domain = s.config.GetAppDomain(req.Name)
	}

	// Check if domain is already taken
	existingByDomain, _ := s.storage.GetAppByDomain(domain)
	if existingByDomain != nil {
		errorResponse(w, http.StatusConflict, "Domain already in use by another app")
		return
	}

	newApp := &app.App{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Domain:    domain,
		Image:     req.Image,
		Status:    app.StatusPending,
		Env:       req.Env,
		Ports: app.PortConfig{
			ContainerPort: port,
			Protocol:      "http",
		},
		Resources: app.ResourceConfig{
			Memory:   req.Memory,
			CPUs:     req.CPUs,
			Replicas: 1,
		},
		SSL: app.SSLConfig{
			Enabled:   req.EnableSSL,
			AutoRenew: true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if newApp.Env == nil {
		newApp.Env = make(map[string]string)
	}

	if err := s.storage.CreateApp(newApp); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Auto-deploy with placeholder image
	go s.deployPlaceholder(newApp)

	jsonResponse(w, http.StatusCreated, newApp)
}

// handleGetApp retrieves an app by ID
func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Try by name if not found by ID
	if a == nil {
		a, err = s.storage.GetAppByName(id)
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	jsonResponse(w, http.StatusOK, a)
}

// handleUpdateApp updates an app
func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	var req app.UpdateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Apply updates
	if req.Domain != nil {
		a.Domain = *req.Domain
	}
	if req.Env != nil {
		a.Env = *req.Env
	}
	if req.Port != nil {
		a.Ports.ContainerPort = *req.Port
	}
	if req.Memory != nil {
		a.Resources.Memory = *req.Memory
	}
	if req.CPUs != nil {
		a.Resources.CPUs = *req.CPUs
	}
	if req.EnableSSL != nil {
		a.SSL.Enabled = *req.EnableSSL
	}

	if err := s.storage.UpdateApp(a); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, a)
}

// handleDeleteApp deletes an app
func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	// Stop and remove container if exists
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}

	// Remove Caddy route
	if s.caddy != nil {
		_ = s.caddy.RemoveRoute("deployer-" + a.ID)
	}

	if err := s.storage.DeleteApp(id); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleStartApp starts an app
func (s *Server) handleStartApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	if a.ContainerID == "" {
		errorResponse(w, http.StatusBadRequest, "App has not been deployed yet")
		return
	}

	if err := s.podman.StartContainer(ctx, a.ContainerID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Status = app.StatusRunning
	s.storage.UpdateApp(a)

	jsonResponse(w, http.StatusOK, a)
}

// handleStopApp stops an app
func (s *Server) handleStopApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	if a.ContainerID == "" {
		errorResponse(w, http.StatusBadRequest, "App has not been deployed yet")
		return
	}

	if err := s.podman.StopContainer(ctx, a.ContainerID, 30); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Status = app.StatusStopped
	s.storage.UpdateApp(a)

	jsonResponse(w, http.StatusOK, a)
}

// handleRestartApp restarts an app
func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	if a.ContainerID == "" {
		errorResponse(w, http.StatusBadRequest, "App has not been deployed yet")
		return
	}

	// Stop then start
	_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
	if err := s.podman.StartContainer(ctx, a.ContainerID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Status = app.StatusRunning
	s.storage.UpdateApp(a)

	jsonResponse(w, http.StatusOK, a)
}

// handleDeployApp deploys an app
func (s *Server) handleDeployApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	var req app.DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update status
	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	// Determine image to use
	image := req.Image
	if image == "" {
		image = a.Image
	}
	if image == "" {
		errorResponse(w, http.StatusBadRequest, "No image specified")
		return
	}

	// Pull image
	if err := s.podman.PullImage(ctx, image); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		errorResponse(w, http.StatusInternalServerError, "Failed to pull image: "+err.Error())
		return
	}

	// Remove old container if exists (by ID and by name)
	containerName := "deployer-" + a.Name
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	// Also try to remove by name in case container exists but ID is stale
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	// Assign a host port if not set (start from 10000)
	if a.Ports.HostPort == 0 {
		a.Ports.HostPort = assignHostPort(a.ID)
	}

	// Create new container with port mapping
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:  "deployer-" + a.Name,
		Image: image,
		Env:   a.Env,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"deployer.app":    a.Name,
			"deployer.app.id": a.ID,
		},
	})
	if err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		errorResponse(w, http.StatusInternalServerError, "Failed to create container: "+err.Error())
		return
	}

	// Start container
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		errorResponse(w, http.StatusInternalServerError, "Failed to start container: "+err.Error())
		return
	}

	// Get container IP for reverse proxy
	containerIP := ""
	if inspect, err := s.podman.InspectContainer(ctx, containerID); err == nil {
		containerIP = inspect.NetworkSettings.IPAddress
		// Try to get IP from networks if direct IP is empty
		if containerIP == "" {
			for _, net := range inspect.NetworkSettings.Networks {
				if net.IPAddress != "" {
					containerIP = net.IPAddress
					break
				}
			}
		}
	}

	// Configure Caddy reverse proxy if domain is set
	// Always use localhost with host port (container IP doesn't work on macOS with Podman VM)
	if a.Domain != "" && s.caddy != nil {
		route := caddy.Route{
			ID:        "deployer-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		}

		if err := s.caddy.AddRoute(route); err != nil {
			// Log but don't fail deployment
			fmt.Printf("Warning: Failed to configure Caddy route: %v\n", err)
		}
	}

	// Update app record
	a.ContainerID = containerID
	a.Image = image
	a.Status = app.StatusRunning
	s.storage.UpdateApp(a)

	jsonResponse(w, http.StatusOK, a)
}

// handleGetAppLogs retrieves app logs
func (s *Server) handleGetAppLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	if a.ContainerID == "" {
		errorResponse(w, http.StatusBadRequest, "App has not been deployed yet")
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	logs, err := s.podman.ContainerLogs(ctx, a.ContainerID, podman.LogOpts{
		Stdout: true,
		Stderr: true,
		Tail:   tail,
	})
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer logs.Close()

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 4096)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}

// handleSystemInfo returns system information
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	info := map[string]interface{}{
		"version": "0.1.0",
		"status":  "running",
	}

	// Get container count
	containers, err := s.podman.ListContainers(ctx, true)
	if err == nil {
		info["containers"] = len(containers)
	} else {
		info["containers"] = 0
		info["containers_error"] = err.Error()
	}

	// Get image count
	images, err := s.podman.ListImages(ctx)
	if err == nil {
		info["images"] = len(images)
	} else {
		info["images"] = 0
		info["images_error"] = err.Error()
	}

	jsonResponse(w, http.StatusOK, info)
}

// handleGetConfig returns domain configuration for frontend
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]interface{}{
		"domain": map[string]interface{}{
			"base":     s.config.Domain.Base,
			"suffix":   s.config.Domain.Suffix,
			"wildcard": s.config.Domain.Wildcard,
		},
	}
	jsonResponse(w, http.StatusOK, cfg)
}

// handleGetVersion returns current and latest version
func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	current := s.version

	// Fetch latest version from GitHub releases
	latest := current
	updateAvailable := false

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/base-go/dr/releases/latest")
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var release struct {
			TagName string `json:"tag_name"`
		}
		if json.NewDecoder(resp.Body).Decode(&release) == nil && release.TagName != "" {
			latest = strings.TrimPrefix(release.TagName, "v")
			// Compare versions semantically
			if compareVersions(latest, current) > 0 {
				updateAvailable = true
			}
		}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"current":         current,
		"latest":          latest,
		"updateAvailable": updateAvailable,
	})
}

// compareVersions compares two semver strings, returns 1 if a > b, -1 if a < b, 0 if equal
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum > bNum {
			return 1
		}
		if aNum < bNum {
			return -1
		}
	}
	return 0
}

// handleSystemUpdate triggers a self-update
func (s *Server) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	// Determine binary path and architecture
	execPath, err := os.Executable()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Cannot determine executable path")
		return
	}

	// Use runtime architecture
	arch := runtime.GOARCH
	if arch == "" {
		arch = "amd64"
	}

	// Download URL
	downloadURL := fmt.Sprintf("https://github.com/base-go/dr/releases/latest/download/deployerd-linux-%s", arch)

	// Download new binary to temp file
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to download update: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Download failed with status: %d", resp.StatusCode))
		return
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "deployerd-update-*")
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create temp file: "+err.Error())
		return
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		errorResponse(w, http.StatusInternalServerError, "Failed to write update: "+err.Error())
		return
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		errorResponse(w, http.StatusInternalServerError, "Failed to set permissions: "+err.Error())
		return
	}

	// Replace current binary (atomic move)
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Try copy if rename fails (cross-device)
		srcFile, _ := os.Open(tmpPath)
		dstFile, _ := os.Create(execPath)
		io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		os.Remove(tmpPath)
	}

	// Send response first, then trigger restart in background
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "updated",
		"message": "Update complete. Restarting service...",
	})

	// Restart in background after response is sent
	go func() {
		time.Sleep(1 * time.Second) // Give time for response to be sent
		cmd := exec.Command("systemctl", "restart", "deployer")
		cmd.Run()
	}()
}

// handleSystemPrune removes unused containers, images, and volumes
func (s *Server) handleSystemPrune(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Run podman system prune
	cmd := exec.CommandContext(ctx, "podman", "system", "prune", "-af", "--volumes")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Prune failed: "+err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status": "pruned",
		"output": string(output),
	})
}

// handleListContainers lists all containers
func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	all := r.URL.Query().Get("all") == "true"

	containers, err := s.podman.ListContainers(ctx, all)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, containers)
}

// deployPlaceholder deploys a placeholder nginx container for a new app
func (s *Server) deployPlaceholder(a *app.App) {
	ctx := context.Background()
	placeholderImage := "nginx:alpine"

	// Update status
	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	// Pull image
	if err := s.podman.PullImage(ctx, placeholderImage); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Assign a host port if not set
	if a.Ports.HostPort == 0 {
		a.Ports.HostPort = assignHostPort(a.ID)
	}

	// Create container with port mapping
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:  "deployer-" + a.Name,
		Image: placeholderImage,
		Env:   a.Env,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"deployer.app":    a.Name,
			"deployer.app.id": a.ID,
		},
	})
	if err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Start container
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Update app with container info
	a.ContainerID = containerID
	a.Image = placeholderImage
	a.Status = app.StatusRunning
	a.UpdatedAt = time.Now()
	s.storage.UpdateApp(a)

	// Configure Caddy if domain is set
	if a.Domain != "" && s.caddy != nil {
		_ = s.caddy.AddRoute(caddy.Route{
			ID:        "deployer-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		})
	}
}

// handleListTemplates returns available app templates
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"templates": templates.GetTemplatesForArch(),
		"system":    templates.GetSystemInfo(),
	}
	jsonResponse(w, http.StatusOK, response)
}

// handleDeployTemplate creates and deploys an app from a template
func (s *Server) handleDeployTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := r.PathValue("id")

	tmpl := templates.GetTemplate(templateID)
	if tmpl == nil {
		errorResponse(w, http.StatusNotFound, "Template not found")
		return
	}

	if !tmpl.IsArchSupported() {
		errorResponse(w, http.StatusBadRequest, "Template not supported on this architecture")
		return
	}

	var req struct {
		Name      string            `json:"name"`
		Domain    string            `json:"domain"`
		Env       map[string]string `json:"env"`
		EnableSSL bool              `json:"enableSSL"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Generate name if not provided
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("%s-%s", tmpl.ID, uuid.New().String()[:8])
	}

	// Check if app already exists by name
	existing, _ := s.storage.GetAppByName(name)
	if existing != nil {
		errorResponse(w, http.StatusConflict, "App with this name already exists")
		return
	}

	// Merge template env with user-provided env
	env := make(map[string]string)
	for k, v := range tmpl.Env {
		env[k] = v
	}
	for k, v := range req.Env {
		env[k] = v
	}

	// Auto-assign domain from config if not specified
	domain := req.Domain
	if domain == "" {
		domain = s.config.GetAppDomain(name)
	}

	// Check if domain is already taken
	existingByDomain, _ := s.storage.GetAppByDomain(domain)
	if existingByDomain != nil {
		errorResponse(w, http.StatusConflict, "Domain already in use by another app")
		return
	}

	// Override url env var for apps that need it (e.g., Ghost)
	if _, hasURL := env["url"]; hasURL {
		env["url"] = "http://" + domain
	}

	newApp := &app.App{
		ID:     uuid.New().String(),
		Name:   name,
		Domain: domain,
		Image:  tmpl.GetImage(),
		Status: app.StatusPending,
		Env:    env,
		Ports: app.PortConfig{
			ContainerPort: tmpl.Port,
			Protocol:      "http",
		},
		Resources: app.ResourceConfig{
			Replicas: 1,
		},
		SSL: app.SSLConfig{
			Enabled:   req.EnableSSL,
			AutoRenew: true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.storage.CreateApp(newApp); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Deploy with template image
	go s.deployFromTemplate(newApp, tmpl)

	jsonResponse(w, http.StatusCreated, newApp)
}

// deployFromTemplate deploys an app using a template's image
func (s *Server) deployFromTemplate(a *app.App, tmpl *templates.Template) {
	ctx := context.Background()
	image := tmpl.GetImage()

	// Update status
	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	// Pull image
	if err := s.podman.PullImage(ctx, image); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Assign a host port if not set
	if a.Ports.HostPort == 0 {
		a.Ports.HostPort = assignHostPort(a.ID)
	}

	// Create container with port mapping
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:  "deployer-" + a.Name,
		Image: image,
		Env:   a.Env,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"deployer.app":      a.Name,
			"deployer.app.id":   a.ID,
			"deployer.template": tmpl.ID,
		},
	})
	if err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Start container
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Update app with container info
	a.ContainerID = containerID
	a.Status = app.StatusRunning
	a.UpdatedAt = time.Now()
	s.storage.UpdateApp(a)

	// Configure Caddy if domain is set
	if a.Domain != "" && s.caddy != nil {
		_ = s.caddy.AddRoute(caddy.Route{
			ID:        "deployer-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		})
	}
}

// handleCaddyCheck handles Caddy on-demand TLS certificate checks
// Returns 200 if domain is allowed, 404 otherwise
func (s *Server) handleCaddyCheck(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get base domain from config
	baseDomain := s.config.Domain.Base
	if baseDomain == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Allow dashboard subdomain
	if domain == "d."+baseDomain {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Check if it's a valid subdomain of our base domain
	if !strings.HasSuffix(domain, "."+baseDomain) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Check if an app exists with this domain
	apps, _ := s.storage.ListApps()
	for _, a := range apps {
		if a.Domain == domain {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Also allow any subdomain of our base domain (for future apps)
	w.WriteHeader(http.StatusOK)
}

// SourceDeployConfig represents the config sent by the CLI
type SourceDeployConfig struct {
	Name    string            `json:"name"`
	Domain  string            `json:"domain,omitempty"`
	Port    int               `json:"port,omitempty"`
	Build   BuildConfig       `json:"build,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Volumes []string          `json:"volumes,omitempty"`
}

// BuildConfig contains build configuration
type BuildConfig struct {
	Dockerfile string `json:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty"`
}

// handleSourceDeploy handles source code deployments from the CLI
func (s *Server) handleSourceDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse multipart form
	if err := r.ParseMultipartForm(500 << 20); err != nil { // 500MB max
		errorResponse(w, http.StatusBadRequest, "Failed to parse form: "+err.Error())
		return
	}

	// Get config JSON
	configStr := r.FormValue("config")
	if configStr == "" {
		errorResponse(w, http.StatusBadRequest, "Missing config")
		return
	}

	var deployConfig SourceDeployConfig
	if err := json.Unmarshal([]byte(configStr), &deployConfig); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid config JSON: "+err.Error())
		return
	}

	if deployConfig.Name == "" {
		errorResponse(w, http.StatusBadRequest, "App name is required")
		return
	}

	// Get source tarball
	file, _, err := r.FormFile("source")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Missing source tarball: "+err.Error())
		return
	}
	defer file.Close()

	// Set response headers for streaming output
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	flusher, ok := w.(http.Flusher)
	if !ok {
		errorResponse(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	writeLine := func(msg string) {
		fmt.Fprintf(w, "%s\n", msg)
		flusher.Flush()
	}

	writeLine("Received source deploy request for: " + deployConfig.Name)

	// Check if app exists, create if not
	a, _ := s.storage.GetAppByName(deployConfig.Name)
	if a == nil {
		writeLine("Creating new app: " + deployConfig.Name)

		// Auto-assign domain from config if not specified
		domain := deployConfig.Domain
		if domain == "" {
			domain = s.config.GetAppDomain(deployConfig.Name)
		}

		port := deployConfig.Port
		if port == 0 {
			port = 8080
		}

		a = &app.App{
			ID:     uuid.New().String(),
			Name:   deployConfig.Name,
			Domain: domain,
			Status: app.StatusPending,
			Env:    deployConfig.Env,
			Ports: app.PortConfig{
				ContainerPort: port,
				Protocol:      "http",
			},
			Resources: app.ResourceConfig{
				Replicas: 1,
			},
			SSL: app.SSLConfig{
				Enabled:   true,
				AutoRenew: true,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if a.Env == nil {
			a.Env = make(map[string]string)
		}

		if err := s.storage.CreateApp(a); err != nil {
			writeLine("ERROR: Failed to create app: " + err.Error())
			return
		}
		writeLine("App created with ID: " + a.ID)
	} else {
		writeLine("Updating existing app: " + a.Name)
		// Update config if provided
		if deployConfig.Port > 0 {
			a.Ports.ContainerPort = deployConfig.Port
		}
		if deployConfig.Domain != "" {
			a.Domain = deployConfig.Domain
		}
		if deployConfig.Env != nil {
			for k, v := range deployConfig.Env {
				a.Env[k] = v
			}
		}
	}

	// Save source tarball to temp file
	paths, _ := config.GetPaths()
	buildDir := fmt.Sprintf("%s/builds/%s", paths.Base, a.ID)
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		writeLine("ERROR: Failed to create build directory: " + err.Error())
		return
	}

	tarballPath := buildDir + "/source.tar.gz"
	tarFile, err := os.Create(tarballPath)
	if err != nil {
		writeLine("ERROR: Failed to create tarball file: " + err.Error())
		return
	}
	if _, err := io.Copy(tarFile, file); err != nil {
		tarFile.Close()
		writeLine("ERROR: Failed to save tarball: " + err.Error())
		return
	}
	tarFile.Close()
	writeLine("Source tarball saved")

	// Extract tarball
	writeLine("Extracting source...")
	sourceDir := buildDir + "/source"
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		writeLine("ERROR: Failed to create source directory: " + err.Error())
		return
	}

	// Use tar command to extract (simpler than Go tar library)
	extractCmd := fmt.Sprintf("tar -xzf %s -C %s", tarballPath, sourceDir)
	if output, err := execCommand(ctx, "sh", "-c", extractCmd); err != nil {
		writeLine("ERROR: Failed to extract tarball: " + err.Error())
		writeLine(output)
		return
	}
	writeLine("Source extracted")

	// Determine Dockerfile path
	dockerfile := "Dockerfile"
	if deployConfig.Build.Dockerfile != "" {
		dockerfile = deployConfig.Build.Dockerfile
	}
	dockerfilePath := sourceDir + "/" + dockerfile

	// Check if Dockerfile exists
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		writeLine("ERROR: Dockerfile not found at " + dockerfilePath)
		writeLine("Please create a Dockerfile in your project")
		return
	}

	// Build image using Podman
	imageName := fmt.Sprintf("deployer/%s:latest", a.Name)
	writeLine("Building image: " + imageName)

	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	// Build using podman build
	buildCmd := fmt.Sprintf("cd %s && podman build -t %s -f %s .", sourceDir, imageName, dockerfile)
	output, err := execCommandStream(ctx, "sh", []string{"-c", buildCmd}, writeLine)
	if err != nil {
		writeLine("ERROR: Build failed: " + err.Error())
		writeLine(output)
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}
	writeLine("Image built successfully")

	// Remove old container if exists
	containerName := "deployer-" + a.Name
	if a.ContainerID != "" {
		writeLine("Stopping old container...")
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	// Assign a host port if not set
	if a.Ports.HostPort == 0 {
		a.Ports.HostPort = assignHostPort(a.ID)
	}

	writeLine(fmt.Sprintf("Creating container with port mapping %d -> %d...", a.Ports.ContainerPort, a.Ports.HostPort))

	// Create new container
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:  containerName,
		Image: imageName,
		Env:   a.Env,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"deployer.app":    a.Name,
			"deployer.app.id": a.ID,
		},
	})
	if err != nil {
		writeLine("ERROR: Failed to create container: " + err.Error())
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Start container
	writeLine("Starting container...")
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		writeLine("ERROR: Failed to start container: " + err.Error())
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Update app record
	a.ContainerID = containerID
	a.Image = imageName
	a.Status = app.StatusRunning
	a.UpdatedAt = time.Now()
	s.storage.UpdateApp(a)

	// Configure Caddy if domain is set
	if a.Domain != "" && s.caddy != nil {
		writeLine("Configuring routing for: " + a.Domain)
		_ = s.caddy.AddRoute(caddy.Route{
			ID:        "deployer-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		})
	}

	writeLine("")
	writeLine("Deploy complete!")
	writeLine("App: " + a.Name)
	if a.Domain != "" {
		writeLine("URL: https://" + a.Domain)
	}
}

// execCommand executes a command and returns output
func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// execCommandStream executes a command and streams output
func execCommandStream(ctx context.Context, name string, args []string, writeLine func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Read output line by line
	var output strings.Builder
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				line := string(buf[:n])
				output.WriteString(line)
				writeLine(strings.TrimRight(line, "\n"))
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				line := string(buf[:n])
				output.WriteString(line)
				writeLine(strings.TrimRight(line, "\n"))
			}
			if err != nil {
				break
			}
		}
	}()

	err = cmd.Wait()
	return output.String(), err
}

// proxyToApp proxies the request to the app's container
func (s *Server) proxyToApp(w http.ResponseWriter, r *http.Request, a *app.App) {
	// Build the upstream URL
	upstream := fmt.Sprintf("http://localhost:%d", a.Ports.HostPort)
	target, err := url.Parse(upstream)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Create the proxy request
	proxyReq, err := http.NewRequest(r.Method, target.String()+r.URL.Path, r.Body)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Set forwarding headers
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	proxyReq.URL.RawQuery = r.URL.RawQuery

	// Make the request
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Bad Gateway: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code and body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
