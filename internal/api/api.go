// Package api provides the REST API for basepod.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/base-go/basepod/internal/app"
	"github.com/base-go/basepod/internal/auth"
	"github.com/base-go/basepod/internal/caddy"
	"github.com/base-go/basepod/internal/config"
	"github.com/base-go/basepod/internal/flux"
	"github.com/base-go/basepod/internal/mlx"
	"github.com/base-go/basepod/internal/podman"
	"github.com/base-go/basepod/internal/storage"
	"github.com/base-go/basepod/internal/templates"
	"github.com/base-go/basepod/internal/web"
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
	// Check various paths for static files
	webDir := os.Getenv("BASEPOD_WEB_DIR")
	if webDir == "" {
		webDir = os.Getenv("DEPLOYER_WEB_DIR") // backwards compatibility
	}
	staticPaths := []string{
		webDir,
		"./dist",                       // Relative to binary
		"/opt/basepod/web/dist",       // Linux production
		"/usr/local/basepod/web/dist", // macOS production
	}
	for _, dir := range staticPaths {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			// Check if _nuxt folder exists (Nuxt SPA build)
			if _, err := os.Stat(dir + "/_nuxt"); err == nil {
				s.staticDir = dir
				s.staticFS = http.FileServer(http.Dir(dir))
				log.Printf("Serving static files from: %s", dir)
				break
			}
		}
	}
	// Fall back to embedded files
	if s.staticFS == nil {
		if staticFS, source, err := web.GetFileSystem(); err == nil {
			log.Printf("Serving static files from %s", source)
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
	s.router.HandleFunc("POST /api/auth/setup", s.handleSetup) // Initial password setup
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
	s.router.HandleFunc("GET /api/system/processes", s.requireAuth(s.handleSystemProcesses))
	s.router.HandleFunc("GET /api/system/config", s.handleGetConfig) // No auth - needed for login page
	s.router.HandleFunc("PUT /api/system/config", s.requireAuth(s.handleUpdateConfig))
	s.router.HandleFunc("GET /api/system/version", s.requireAuth(s.handleGetVersion))
	s.router.HandleFunc("POST /api/system/update", s.requireAuth(s.handleSystemUpdate))
	s.router.HandleFunc("POST /api/system/prune", s.requireAuth(s.handleSystemPrune))
	s.router.HandleFunc("POST /api/system/restart/{service}", s.requireAuth(s.handleServiceRestart))
	s.router.HandleFunc("GET /api/containers", s.requireAuth(s.handleListContainers))

	// Templates (auth required)
	s.router.HandleFunc("GET /api/templates", s.requireAuth(s.handleListTemplates))
	s.router.HandleFunc("POST /api/templates/{id}/deploy", s.requireAuth(s.handleDeployTemplate))

	// MLX LLM service (auth required) - Ollama-like API
	s.router.HandleFunc("GET /api/mlx/status", s.requireAuth(s.handleMLXStatus))
	s.router.HandleFunc("GET /api/mlx/models", s.requireAuth(s.handleListMLXModels))
	s.router.HandleFunc("POST /api/mlx/pull", s.requireAuth(s.handleMLXPull))
	s.router.HandleFunc("GET /api/mlx/pull/progress", s.requireAuth(s.handleMLXPullProgress))
	s.router.HandleFunc("POST /api/mlx/pull/cancel", s.requireAuth(s.handleMLXPullCancel))
	s.router.HandleFunc("POST /api/mlx/run", s.requireAuth(s.handleMLXRun))
	s.router.HandleFunc("POST /api/mlx/stop", s.requireAuth(s.handleMLXStop))
	s.router.HandleFunc("POST /api/mlx/transcribe", s.requireAuth(s.handleMLXTranscribe))
	s.router.HandleFunc("DELETE /api/mlx/models/{id}", s.requireAuth(s.handleMLXDeleteModel))

	// Chat messages (auth required)
	s.router.HandleFunc("GET /api/chat/messages/{modelId}", s.requireAuth(s.handleGetChatMessages))
	s.router.HandleFunc("POST /api/chat/messages/{modelId}", s.requireAuth(s.handleSaveChatMessage))
	s.router.HandleFunc("DELETE /api/chat/messages/{modelId}", s.requireAuth(s.handleClearChatMessages))

	// FLUX image generation (auth required)
	s.router.HandleFunc("GET /api/flux/status", s.requireAuth(s.handleFluxStatus))
	s.router.HandleFunc("GET /api/flux/models", s.requireAuth(s.handleFluxModels))
	s.router.HandleFunc("POST /api/flux/models/{id}", s.requireAuth(s.handleFluxDownloadModel))
	s.router.HandleFunc("DELETE /api/flux/models/{id}", s.requireAuth(s.handleFluxDeleteModel))
	s.router.HandleFunc("GET /api/flux/models/{id}/progress", s.requireAuth(s.handleFluxDownloadProgress))
	s.router.HandleFunc("POST /api/flux/generate", s.requireAuth(s.handleFluxGenerate))
	s.router.HandleFunc("POST /api/flux/edit", s.requireAuth(s.handleFluxEdit))
	s.router.HandleFunc("POST /api/flux/upload", s.requireAuth(s.handleFluxUpload))
	s.router.HandleFunc("GET /api/flux/jobs/{id}", s.requireAuth(s.handleFluxGetJob))
	s.router.HandleFunc("GET /api/flux/generations", s.requireAuth(s.handleFluxListGenerations))
	s.router.HandleFunc("GET /api/flux/image/{id}", s.handleFluxGetImage) // No auth - images use random IDs
	s.router.HandleFunc("DELETE /api/flux/generations/{id}", s.requireAuth(s.handleFluxDeleteGeneration))
	s.router.HandleFunc("GET /api/flux/sessions", s.requireAuth(s.handleFluxListSessions))
	s.router.HandleFunc("GET /api/flux/sessions/{id}", s.requireAuth(s.handleFluxGetSession))
	s.router.HandleFunc("DELETE /api/flux/sessions/{id}", s.requireAuth(s.handleFluxDeleteSession))
	s.router.HandleFunc("GET /api/flux/storage", s.requireAuth(s.handleFluxStorage))
	s.router.HandleFunc("GET /api/flux/storage/{type}", s.requireAuth(s.handleFluxStorageFiles))

	// Image tags (auth required)
	s.router.HandleFunc("GET /api/images/tags", s.requireAuth(s.handleImageTags))

	// Caddy on-demand TLS check (no auth - called by Caddy)
	s.router.HandleFunc("GET /api/caddy/check", s.handleCaddyCheck)

	// Source deploy endpoint (auth required)
	s.router.HandleFunc("POST /api/deploy", s.requireAuth(s.handleSourceDeploy))
}

// requireAuth wraps a handler with authentication check
func (s *Server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if initial setup is needed
		if s.auth.NeedsSetup() {
			errorResponse(w, http.StatusForbidden, "Setup required: please set an admin password")
			return
		}

		// Check for token in cookie or Authorization header
		token := ""
		if cookie, err := r.Cookie("basepod_token"); err == nil {
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

	// Set cookie - check both TLS and X-Forwarded-Proto for HTTPS detection
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "basepod_token",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode, // Lax allows same-site navigation
		Expires:  session.ExpiresAt,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
	})
}

// handleLogout handles logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("basepod_token"); err == nil {
		s.auth.DeleteSession(cookie.Value)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "basepod_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	jsonResponse(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// handleAuthStatus returns current auth status
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup := s.auth.NeedsSetup()
	authenticated := false

	if !needsSetup {
		token := ""
		if cookie, err := r.Cookie("basepod_token"); err == nil {
			token = cookie.Value
		}
		if token == "" {
			token = r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
		}
		authenticated = s.auth.ValidateSession(token)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"needsSetup":    needsSetup,
		"authenticated": authenticated,
	})
}

// handleSetup handles initial password setup (only works when no password is set)
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.auth.NeedsSetup() {
		errorResponse(w, http.StatusForbidden, "Setup already completed")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if len(req.Password) < 8 {
		errorResponse(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	if !s.auth.SetPassword(req.Password) {
		errorResponse(w, http.StatusInternalServerError, "Failed to set password")
		return
	}

	// Save password hash to config file
	if err := s.savePasswordToConfig(); err != nil {
		log.Printf("Warning: failed to persist password to config: %v", err)
	}

	// Create session for the user
	session, err := s.auth.CreateSession()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "basepod_token",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Setup completed successfully",
		"token":   session.Token,
	})
}

// savePasswordToConfig persists the current password hash to the config file
func (s *Server) savePasswordToConfig() error {
	s.config.Auth.PasswordHash = s.auth.GetPasswordHash()
	return s.config.Save()
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
	dashboardDomain := "d." + s.config.Domain.Root
	if host != dashboardDomain && s.config.Domain.Root != "" && strings.HasSuffix(host, "."+s.config.Domain.Root) {
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
		} else if webFS, _, err := web.GetFileSystem(); err == nil {
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
		"name":    "Basepod API",
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

	// Determine app type
	appType := req.Type
	if appType == "" {
		appType = app.AppTypeContainer
	}

	// Validate MLX apps
	if appType == app.AppTypeMLX {
		if !mlx.IsSupported() {
			errorResponse(w, http.StatusBadRequest, "MLX apps require macOS with Apple Silicon")
			return
		}
		if req.Model == "" {
			errorResponse(w, http.StatusBadRequest, "Model is required for MLX apps")
			return
		}
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
		Type:      appType,
		Domain:    domain,
		Image:     req.Image,
		Status:    app.StatusPending,
		Env:       req.Env,
		Volumes:   req.Volumes,
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

	// Setup MLX config if MLX app
	if appType == app.AppTypeMLX {
		newApp.MLX = &app.MLXConfig{
			Model:       req.Model,
			MaxTokens:   4096,
			ContextSize: 8192,
			Temperature: 0.7,
		}
		newApp.Image = "mlx:" + req.Model // Display in UI
	}

	if newApp.Env == nil {
		newApp.Env = make(map[string]string)
	}

	if err := s.storage.CreateApp(newApp); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Auto-deploy based on type
	if appType == app.AppTypeMLX {
		go s.deployMLXApp(newApp)
	} else {
		go s.deployPlaceholder(newApp)
	}

	jsonResponse(w, http.StatusCreated, newApp)
}

// handleGetApp retrieves an app by ID
// AppResponse extends App with computed connection info
type AppResponse struct {
	*app.App
	InternalHost string `json:"internal_host"` // e.g., "basepod-mysql"
	ExternalHost string `json:"external_host"` // e.g., "d.common.al:31234"
}

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

	// Build response with computed fields
	response := AppResponse{
		App:          a,
		InternalHost: "basepod-" + a.Name,
	}

	// Compute external host from domain config
	if s.config != nil && a.Ports.HostPort > 0 {
		if s.config.Domain.Root != "" {
			response.ExternalHost = fmt.Sprintf("%s:%d", s.config.Domain.Root, a.Ports.HostPort)
		} else if s.config.Domain.Base != "" {
			response.ExternalHost = fmt.Sprintf("%s:%d", s.config.Domain.Base, a.Ports.HostPort)
		} else {
			response.ExternalHost = fmt.Sprintf("localhost:%d", a.Ports.HostPort)
		}
	}

	jsonResponse(w, http.StatusOK, response)
}

// handleUpdateApp updates an app
func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
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

	var req app.UpdateAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Apply updates
	if req.Name != nil && *req.Name != a.Name {
		// Check if new name is already taken
		existing, _ := s.storage.GetAppByName(*req.Name)
		if existing != nil {
			errorResponse(w, http.StatusConflict, "App with this name already exists")
			return
		}
		a.Name = *req.Name
	}
	if req.Domain != nil {
		a.Domain = *req.Domain
	}
	if req.Image != nil {
		a.Image = *req.Image
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
	if req.ExposeExternal != nil {
		a.Ports.ExposeExternal = *req.ExposeExternal
	}

	// Handle aliases update
	aliasesChanged := false
	oldAliases := a.Aliases
	if req.Aliases != nil {
		a.Aliases = *req.Aliases
		aliasesChanged = true
	}

	if err := s.storage.UpdateApp(a); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update Caddy routes for aliases if changed
	if aliasesChanged && s.caddy != nil && a.Status == app.StatusRunning {
		// Remove old alias routes
		for _, alias := range oldAliases {
			routeID := fmt.Sprintf("alias-%s-%s", a.ID[:8], alias)
			s.caddy.RemoveRoute(routeID)
		}
		// Add new alias routes
		for _, alias := range a.Aliases {
			routeID := fmt.Sprintf("alias-%s-%s", a.ID[:8], alias)
			// Get the upstream from the main app route
			upstream := fmt.Sprintf("localhost:%d", a.Ports.HostPort)
			if a.Ports.HostPort == 0 {
				upstream = fmt.Sprintf("localhost:%d", assignHostPort(a.ID))
			}
			route := caddy.Route{
				ID:       routeID,
				Domain:   alias,
				Upstream: upstream,
			}
			if err := s.caddy.AddRoute(route); err != nil {
				log.Printf("Warning: failed to add alias route for %s: %v", alias, err)
			}
		}
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

	// Handle MLX apps differently
	if a.Type == app.AppTypeMLX {
		if err := s.deleteMLXApp(a); err != nil {
			log.Printf("Warning: failed to cleanup MLX app: %v", err)
		}
	} else {
		// Stop and remove container if exists
		if a.ContainerID != "" {
			_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
			_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
		}
	}

	// Remove Caddy route
	if s.caddy != nil {
		_ = s.caddy.RemoveRoute("basepod-" + a.Name)
		// Remove alias routes
		for _, alias := range a.Aliases {
			_ = s.caddy.RemoveRoute(fmt.Sprintf("alias-%s-%s", a.ID[:8], alias))
		}
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

	// Handle MLX apps differently
	if a.Type == app.AppTypeMLX {
		if err := s.startMLXApp(a); err != nil {
			errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, a)
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

	// Handle MLX apps differently
	if a.Type == app.AppTypeMLX {
		if err := s.stopMLXApp(a); err != nil {
			errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, a)
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

// handleRestartApp restarts an app by recreating the container
func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	// Handle MLX apps differently
	if a.Type == app.AppTypeMLX {
		if err := s.stopMLXApp(a); err != nil {
			log.Printf("Failed to stop MLX app: %v", err)
		}
		if err := s.startMLXApp(a); err != nil {
			errorResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, a)
		return
	}

	if a.ContainerID == "" && a.Image == "" {
		errorResponse(w, http.StatusBadRequest, "App has not been deployed yet")
		return
	}

	// Stop and remove old container
	containerName := "basepod-" + a.Name
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	// Also try by name in case container ID is stale
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	// Build volume mounts from app record
	volumeMounts := []string{}
	for _, v := range a.Volumes {
		if v.HostPath != "" && v.ContainerPath != "" {
			volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath))
		}
	}

	// Create new container with current settings
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     containerName,
		Image:    a.Image,
		Env:      a.Env,
		Networks: []string{"basepod"},
		Volumes:  volumeMounts,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"basepod.app":    a.Name,
			"basepod.app.id": a.ID,
		},
	})
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create container: "+err.Error())
		return
	}

	// Start the new container
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to start container: "+err.Error())
		return
	}

	a.ContainerID = containerID
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

	// Handle MLX apps differently - they don't need container deployment
	if a.Type == app.AppTypeMLX {
		go s.deployMLXApp(a)
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "deploying",
			"message": "MLX app deployment started",
		})
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
	containerName := "basepod-" + a.Name
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

	// Create new container with port mapping and network
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     "basepod-" + a.Name,
		Image:    image,
		Env:      a.Env,
		Networks: []string{"basepod"},
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"basepod.app":    a.Name,
			"basepod.app.id": a.ID,
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
			ID:        "basepod-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		}

		if err := s.caddy.AddRoute(route); err != nil {
			// Log but don't fail deployment
			fmt.Printf("Warning: Failed to configure Caddy route: %v\n", err)
		}

		// Add routes for domain aliases
		for _, alias := range a.Aliases {
			aliasRoute := caddy.Route{
				ID:        fmt.Sprintf("alias-%s-%s", a.ID[:8], alias),
				Domain:    alias,
				Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
				EnableSSL: a.SSL.Enabled,
			}
			if err := s.caddy.AddRoute(aliasRoute); err != nil {
				fmt.Printf("Warning: Failed to configure alias route for %s: %v\n", alias, err)
			}
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
		"version": s.version,
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

// ProcessInfo represents a running process
type ProcessInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // mlx, flux, container, system
	Status   string `json:"status"`
	PID      int    `json:"pid,omitempty"`
	Port     int    `json:"port,omitempty"`
	Model    string `json:"model,omitempty"`
	Image    string `json:"image,omitempty"`
	CPU      string `json:"cpu,omitempty"`
	Memory   string `json:"memory,omitempty"`
	Uptime   string `json:"uptime,omitempty"`
	AppID    string `json:"app_id,omitempty"`
	AppName  string `json:"app_name,omitempty"`
	Progress int    `json:"progress,omitempty"`
}

// handleSystemProcesses returns all running processes
func (s *Server) handleSystemProcesses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var processes []ProcessInfo

	// 1. MLX Server process
	mlxService := mlx.GetService()
	mlxStatus := mlxService.GetStatus()
	if mlxStatus.Running {
		processes = append(processes, ProcessInfo{
			ID:     "mlx-server",
			Name:   "MLX LLM Server",
			Type:   "mlx",
			Status: "running",
			PID:    mlxStatus.PID,
			Port:   mlxStatus.Port,
			Model:  mlxStatus.ActiveModel,
		})
	}

	// 2. MLX Downloads in progress
	downloads := mlx.GetAllDownloads()
	for _, dl := range downloads {
		if dl.Status == "downloading" || dl.Status == "pending" {
			processes = append(processes, ProcessInfo{
				ID:       "mlx-download-" + dl.ModelID,
				Name:     "Downloading " + dl.ModelID,
				Type:     "mlx-download",
				Status:   dl.Status,
				Model:    dl.ModelID,
				Progress: int(dl.Progress),
			})
		}
	}

	// 3. FLUX generation in progress
	fluxService := flux.GetService(s.storage.DB())
	fluxStatus := fluxService.GetStatus()
	if fluxStatus.Generating && fluxStatus.CurrentJob != nil {
		processes = append(processes, ProcessInfo{
			ID:       fluxStatus.CurrentJob.ID,
			Name:     "Image Generation",
			Type:     "flux",
			Status:   "generating",
			Model:    fluxStatus.CurrentJob.Model,
			Progress: fluxStatus.CurrentJob.Progress,
		})
	}

	// 4. FLUX Downloads in progress
	fluxDownloads := []string{"schnell", "dev"}
	for _, modelID := range fluxDownloads {
		dp := flux.GetDownloadProgress(modelID)
		if dp != nil && (dp.Status == "downloading" || dp.Status == "pending") {
			processes = append(processes, ProcessInfo{
				ID:       "flux-download-" + modelID,
				Name:     "Downloading FLUX " + modelID,
				Type:     "flux-download",
				Status:   dp.Status,
				Model:    modelID,
				Progress: int(dp.Progress),
			})
		}
	}

	// 5. Running containers
	containers, err := s.podman.ListContainers(ctx, false) // Only running
	if err == nil {
		// Get apps to match container IDs
		apps, _ := s.storage.ListApps()
		appMap := make(map[string]*struct{ ID, Name string })
		for _, a := range apps {
			if a.ContainerID != "" {
				appMap[a.ContainerID] = &struct{ ID, Name string }{a.ID, a.Name}
			}
		}

		for _, c := range containers {
			proc := ProcessInfo{
				ID:     c.ID[:12],
				Name:   c.Names[0],
				Type:   "container",
				Status: c.State,
				Image:  c.Image,
			}

			// Match to app if possible
			if app, ok := appMap[c.ID]; ok {
				proc.AppID = app.ID
				proc.AppName = app.Name
			}

			processes = append(processes, proc)
		}
	}

	// Return empty array instead of null
	if processes == nil {
		processes = []ProcessInfo{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"processes": processes,
		"count":     len(processes),
	})
}

// handleGetConfig returns domain configuration for frontend
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Mask the HF token for display (show first 6 chars if set)
	maskedToken := ""
	if s.config.AI.HuggingFaceToken != "" {
		token := s.config.AI.HuggingFaceToken
		if len(token) > 10 {
			maskedToken = token[:6] + "****" + token[len(token)-4:]
		} else {
			maskedToken = "****"
		}
	}

	cfg := map[string]interface{}{
		"domain": map[string]interface{}{
			"root":     s.config.Domain.Root,
			"suffix":   s.config.Domain.Suffix,
			"wildcard": s.config.Domain.Wildcard,
		},
		"ai": map[string]interface{}{
			"huggingface_token": maskedToken,
		},
	}
	jsonResponse(w, http.StatusOK, cfg)
}

// handleUpdateConfig updates domain and AI configuration
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	// Use map to detect which fields were actually provided
	var rawReq map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update domain config only if domain field was provided
	if domainRaw, ok := rawReq["domain"]; ok {
		var domainReq struct {
			Root     string `json:"root"`
			Wildcard bool   `json:"wildcard"`
		}
		if err := json.Unmarshal(domainRaw, &domainReq); err == nil {
			s.config.Domain.Root = domainReq.Root
			s.config.Domain.Wildcard = domainReq.Wildcard
		}
	}

	// Update AI config only if ai field was provided
	if aiRaw, ok := rawReq["ai"]; ok {
		var aiReq struct {
			HuggingFaceToken string `json:"huggingface_token"`
		}
		if err := json.Unmarshal(aiRaw, &aiReq); err == nil {
			if aiReq.HuggingFaceToken != "" {
				s.config.AI.HuggingFaceToken = aiReq.HuggingFaceToken
			}
		}
	}

	// Save to file
	if err := s.config.Save(); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to save config: "+err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status": "updated",
		"domain": map[string]interface{}{
			"root":     s.config.Domain.Root,
			"wildcard": s.config.Domain.Wildcard,
		},
	})
}

// handleGetVersion returns current and latest version
func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	current := s.version

	// Fetch latest version from GitHub releases
	latest := current
	updateAvailable := false

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/base-go/basepod/releases/latest")
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

	// Use runtime OS and architecture
	goos := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "" {
		arch = "amd64"
	}

	// Download URL
	downloadURL := fmt.Sprintf("https://github.com/base-go/basepod/releases/latest/download/basepod-%s-%s", goos, arch)

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
	tmpFile, err := os.CreateTemp("", "basepod-update-*")
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
		// Try copy if rename fails (cross-device or permission issue)
		srcFile, err := os.Open(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			errorResponse(w, http.StatusInternalServerError, "Failed to open temp file: "+err.Error())
			return
		}
		defer srcFile.Close()

		dstFile, err := os.Create(execPath)
		if err != nil {
			os.Remove(tmpPath)
			errorResponse(w, http.StatusInternalServerError, "Failed to replace binary (permission denied?): "+err.Error())
			return
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			os.Remove(tmpPath)
			errorResponse(w, http.StatusInternalServerError, "Failed to write binary: "+err.Error())
			return
		}
		os.Remove(tmpPath)
	}

	// Send response first, then trigger restart in background
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":  "updated",
		"message": "Update complete. Restarting service...",
	})

	// Restart properly via service manager
	go func() {
		time.Sleep(1 * time.Second) // Give time for response to be sent

		if runtime.GOOS == "darwin" {
			// macOS: try system daemon first (server install), then user daemon
			if err := exec.Command("launchctl", "kickstart", "-k", "system/com.basepod").Run(); err != nil {
				// Fallback to user daemon
				exec.Command("launchctl", "kickstart", "-k", "gui/"+fmt.Sprint(os.Getuid())+"/com.basepod").Run()
			}
		} else {
			// Linux: try system service first, then user service
			if err := exec.Command("systemctl", "restart", "basepod").Run(); err != nil {
				exec.Command("systemctl", "--user", "restart", "basepod").Run()
			}
		}

		// Fallback: exit and let service manager restart us
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
}

// handleSystemPrune removes unused containers, images, and volumes
func (s *Server) handleSystemPrune(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Find podman path
	podmanPath := "podman"
	if _, err := exec.LookPath("podman"); err != nil {
		for _, p := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman"} {
			if _, err := os.Stat(p); err == nil {
				podmanPath = p
				break
			}
		}
	}

	// Run podman system prune
	cmd := exec.CommandContext(ctx, podmanPath, "system", "prune", "-af", "--volumes")
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

// handleServiceRestart restarts a system service
func (s *Server) handleServiceRestart(w http.ResponseWriter, r *http.Request) {
	service := r.PathValue("service")

	switch service {
	case "podman":
		// For podman, restart depends on OS
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			// macOS: restart podman machine
			cmd = exec.Command("podman", "machine", "stop")
			cmd.Run() // Ignore error if already stopped
			cmd = exec.Command("podman", "machine", "start")
		} else {
			// Linux: restart podman socket service
			cmd = exec.Command("systemctl", "--user", "restart", "podman.socket")
		}
		if err := cmd.Run(); err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to restart Podman: "+err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "restarted",
			"service": "podman",
		})

	case "caddy":
		// Restart caddy service
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			// macOS: use launchctl
			exec.Command("launchctl", "unload", "/Library/LaunchDaemons/com.caddy.plist").Run()
			cmd = exec.Command("launchctl", "load", "/Library/LaunchDaemons/com.caddy.plist")
		} else {
			// Linux: use systemctl
			cmd = exec.Command("systemctl", "restart", "caddy")
		}
		if err := cmd.Run(); err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to restart Caddy: "+err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "restarted",
			"service": "caddy",
		})

	case "basepod":
		// Send response first, then exit to trigger service manager restart
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "restarting",
			"service": "basepod",
		})
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()

	default:
		errorResponse(w, http.StatusBadRequest, "Unknown service: "+service)
	}
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

	// Create container with port mapping and network
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     "basepod-" + a.Name,
		Image:    placeholderImage,
		Env:      a.Env,
		Networks: []string{"basepod"},
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"basepod.app":    a.Name,
			"basepod.app.id": a.ID,
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
			ID:        "basepod-" + a.Name,
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
		Name           string            `json:"name"`
		Domain         string            `json:"domain"`
		Env            map[string]string `json:"env"`
		EnableSSL      bool              `json:"enableSSL"`
		ExposeExternal bool              `json:"exposeExternal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get the appropriate image for current architecture
	image := tmpl.GetImage()

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

	// For non-database templates, assign domain; for databases, skip domain
	domain := req.Domain
	if tmpl.Category != "database" {
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
	} else {
		// No domain for database apps
		domain = ""
	}

	// Convert template volumes to app volumes
	var volumes []app.VolumeMount
	for _, v := range tmpl.Volumes {
		volumes = append(volumes, app.VolumeMount{
			Name:          v.Name,
			ContainerPath: v.ContainerPath,
		})
	}

	newApp := &app.App{
		ID:      uuid.New().String(),
		Name:    name,
		Domain:  domain,
		Image:   image,
		Status:  app.StatusPending,
		Env:     env,
		Volumes: volumes,
		Ports: app.PortConfig{
			ContainerPort:  tmpl.Port,
			Protocol:       "http",
			ExposeExternal: req.ExposeExternal,
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
	image := a.Image // Use image from app record (already selected based on alpine preference)

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

	// Build volume mounts from app config
	volumeMounts := []string{}
	for _, v := range a.Volumes {
		// Use named volume format: volumeName:containerPath
		volumeName := fmt.Sprintf("basepod-%s-%s", a.Name, v.Name)
		volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", volumeName, v.ContainerPath))
	}

	// Create container with port mapping and network
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     "basepod-" + a.Name,
		Image:    image,
		Env:      a.Env,
		Command:  tmpl.Command,
		Networks: []string{"basepod"},
		Volumes:  volumeMounts,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"basepod.app":      a.Name,
			"basepod.app.id":   a.ID,
			"basepod.template": tmpl.ID,
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
			ID:        "basepod-" + a.Name,
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
	baseDomain := s.config.Domain.Root
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
	Name       string            `json:"name"`
	Type       string            `json:"type,omitempty"`   // "static" or "container" (default)
	Domain     string            `json:"domain,omitempty"`
	Port       int               `json:"port,omitempty"`
	Public     string            `json:"public,omitempty"` // Public directory for static sites
	Build      BuildConfig       `json:"build,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Volumes    []string          `json:"volumes,omitempty"`
	GitCommit  string            `json:"git_commit,omitempty"`
	GitMessage string            `json:"git_message,omitempty"`
	GitBranch  string            `json:"git_branch,omitempty"`
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

		// Parse volumes from string format "name:container_path" to VolumeMount
		var volumes []app.VolumeMount
		for _, vol := range deployConfig.Volumes {
			parts := strings.SplitN(vol, ":", 2)
			if len(parts) == 2 {
				volumes = append(volumes, app.VolumeMount{
					Name:          parts[0],
					ContainerPath: parts[1],
				})
			}
		}

		// Determine app type
		appType := app.AppTypeContainer
		if deployConfig.Type == "static" {
			appType = app.AppTypeStatic
		}

		a = &app.App{
			ID:      uuid.New().String(),
			Name:    deployConfig.Name,
			Type:    appType,
			Domain:  domain,
			Status:  app.StatusPending,
			Env:     deployConfig.Env,
			Volumes: volumes,
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
		// Update volumes if provided
		if len(deployConfig.Volumes) > 0 {
			var volumes []app.VolumeMount
			for _, vol := range deployConfig.Volumes {
				parts := strings.SplitN(vol, ":", 2)
				if len(parts) == 2 {
					volumes = append(volumes, app.VolumeMount{
						Name:          parts[0],
						ContainerPath: parts[1],
					})
				}
			}
			a.Volumes = volumes
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

	// Handle static site deployment
	if deployConfig.Type == "static" || a.Type == app.AppTypeStatic {
		writeLine("Deploying static site...")

		// Determine public directory
		publicDir := deployConfig.Public
		if publicDir == "" {
			publicDir = "dist" // Default
		}

		publicPath := sourceDir + "/" + publicDir
		if _, err := os.Stat(publicPath); os.IsNotExist(err) {
			writeLine("ERROR: Public directory not found: " + publicDir)
			writeLine("Make sure your build output is in the correct directory")
			return
		}

		// Copy public directory to app data directory
		appDataDir := fmt.Sprintf("%s/data/apps/%s", paths.Base, a.Name)
		writeLine("Copying static files to: " + appDataDir)

		// Remove old files
		os.RemoveAll(appDataDir)
		if err := os.MkdirAll(appDataDir, 0755); err != nil {
			writeLine("ERROR: Failed to create app data directory: " + err.Error())
			return
		}

		// Copy files using cp -r
		copyCmd := fmt.Sprintf("cp -r %s/* %s/", publicPath, appDataDir)
		if output, err := execCommand(ctx, "sh", "-c", copyCmd); err != nil {
			writeLine("ERROR: Failed to copy static files: " + err.Error())
			writeLine(output)
			return
		}

		// Update app status and type
		a.Type = app.AppTypeStatic
		a.Status = app.StatusRunning
		a.UpdatedAt = time.Now()

		// Add deployment record
		deployRecord := app.DeploymentRecord{
			ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
			CommitHash: deployConfig.GitCommit,
			CommitMsg:  deployConfig.GitMessage,
			Branch:     deployConfig.GitBranch,
			Status:     "success",
			DeployedAt: time.Now(),
		}
		a.Deployments = append([]app.DeploymentRecord{deployRecord}, a.Deployments...)
		// Keep only last 10 deployments
		if len(a.Deployments) > 10 {
			a.Deployments = a.Deployments[:10]
		}

		if err := s.storage.UpdateApp(a); err != nil {
			writeLine("ERROR: Failed to update app: " + err.Error())
			return
		}

		// Update Caddy configuration for static site
		if err := s.caddy.AddStaticRoute(a.Domain, appDataDir); err != nil {
			writeLine("WARNING: Failed to update Caddy: " + err.Error())
			// Continue anyway, can manually configure
		}

		writeLine("Static site deployed successfully!")
		writeLine(fmt.Sprintf("URL: https://%s", a.Domain))
		return
	}

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
	imageName := fmt.Sprintf("basepod/%s:latest", a.Name)
	writeLine("Building image: " + imageName)

	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	// Build using podman build (use full path for launchd compatibility)
	podmanPath := "podman"
	if _, err := exec.LookPath("podman"); err != nil {
		// Try common paths
		for _, p := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman"} {
			if _, err := os.Stat(p); err == nil {
				podmanPath = p
				break
			}
		}
	}
	buildCmd := fmt.Sprintf("cd %s && %s build -t %s -f %s .", sourceDir, podmanPath, imageName, dockerfile)
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
	containerName := "basepod-" + a.Name
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

	// Build volume mounts from app config
	volumeMounts := []string{}
	for _, v := range a.Volumes {
		// Use named volume format: volumeName:containerPath
		volumeName := fmt.Sprintf("basepod-%s-%s", a.Name, v.Name)
		volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", volumeName, v.ContainerPath))
		writeLine(fmt.Sprintf("Volume: %s -> %s", volumeName, v.ContainerPath))
	}

	// Create new container with network
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     containerName,
		Image:    imageName,
		Env:      a.Env,
		Networks: []string{"basepod"},
		Volumes:  volumeMounts,
		Ports: map[string]string{
			fmt.Sprintf("%d", a.Ports.ContainerPort): fmt.Sprintf("%d", a.Ports.HostPort),
		},
		Labels: map[string]string{
			"basepod.app":    a.Name,
			"basepod.app.id": a.ID,
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

	// Add deployment record
	deployRecord := app.DeploymentRecord{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		CommitHash: deployConfig.GitCommit,
		CommitMsg:  deployConfig.GitMessage,
		Branch:     deployConfig.GitBranch,
		Status:     "success",
		DeployedAt: time.Now(),
	}
	a.Deployments = append([]app.DeploymentRecord{deployRecord}, a.Deployments...)
	// Keep only last 10 deployments
	if len(a.Deployments) > 10 {
		a.Deployments = a.Deployments[:10]
	}

	s.storage.UpdateApp(a)

	// Configure Caddy if domain is set
	if a.Domain != "" && s.caddy != nil {
		writeLine("Configuring routing for: " + a.Domain)
		_ = s.caddy.AddRoute(caddy.Route{
			ID:        "basepod-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		})

		// Add routes for domain aliases
		for _, alias := range a.Aliases {
			writeLine("Configuring alias: " + alias)
			_ = s.caddy.AddRoute(caddy.Route{
				ID:        fmt.Sprintf("alias-%s-%s", a.ID[:8], alias),
				Domain:    alias,
				Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
				EnableSSL: a.SSL.Enabled,
			})
		}
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

// handleImageTags returns tags for an image
func (s *Server) handleImageTags(w http.ResponseWriter, r *http.Request) {
	image := r.URL.Query().Get("image")
	if image == "" {
		errorResponse(w, http.StatusBadRequest, "image parameter required")
		return
	}

	// Return empty tags - image tag selection feature not yet implemented
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"image": image,
		"tags":  []string{},
	})
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

	// Make the request - disable redirect following to properly proxy 302 responses with cookies
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects, return the response as-is
		},
	}
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

// ============================================
// MLX LLM Handlers
// ============================================

// handleListMLXModels returns available MLX models with download status
func (s *Server) handleListMLXModels(w http.ResponseWriter, r *http.Request) {
	svc := mlx.GetService()
	models := svc.ListModels()
	status := svc.GetStatus()
	sysInfo := mlx.GetSystemInfo()

	// Add RAM requirements to each model
	type ModelWithRAM struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Size         string `json:"size"`
		Category     string `json:"category"`
		Description  string `json:"description,omitempty"`
		Downloaded   bool   `json:"downloaded"`
		DownloadedAt string `json:"downloaded_at,omitempty"`
		RequiredRAM  int    `json:"required_ram_gb"`
		CanRun       bool   `json:"can_run"`
		Warning      string `json:"warning,omitempty"`
	}

	// Get catalog for descriptions
	catalog := mlx.GetModelCatalog()
	descMap := make(map[string]string)
	for _, c := range catalog {
		descMap[c.ID] = c.Description
	}

	var modelsWithRAM []ModelWithRAM
	for _, m := range models {
		canRun, warning := mlx.CanRunModel(m.ID, sysInfo.TotalRAMGB)
		mwr := ModelWithRAM{
			ID:          m.ID,
			Name:        m.Name,
			Size:        m.Size,
			Category:    m.Category,
			Description: descMap[m.ID],
			Downloaded:  m.Downloaded,
			RequiredRAM: mlx.EstimateModelRAM(m.ID),
			CanRun:      canRun,
			Warning:     warning,
		}
		if !m.DownloadedAt.IsZero() {
			mwr.DownloadedAt = m.DownloadedAt.Format("2006-01-02T15:04:05Z")
		}
		modelsWithRAM = append(modelsWithRAM, mwr)
	}

	// Build endpoint URL using same domain pattern as apps
	var endpoint string
	if s.config != nil {
		llmDomain := s.config.GetAppDomain("llm")
		endpoint = fmt.Sprintf("https://%s/v1/chat/completions", llmDomain)
	} else {
		endpoint = fmt.Sprintf("http://localhost:%d/v1/chat/completions", status.Port)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"models":             modelsWithRAM,
		"supported":          mlx.IsSupported(),
		"platform":           runtime.GOOS + "/" + runtime.GOARCH,
		"unsupported_reason": mlx.GetUnsupportedReason(),
		"active_model":       status.ActiveModel,
		"running":            status.Running,
		"port":               status.Port,
		"endpoint":           endpoint,
		"system": map[string]interface{}{
			"total_ram_gb":     sysInfo.TotalRAMGB,
			"available_ram_gb": int(sysInfo.AvailableRAM / (1024 * 1024 * 1024)),
		},
	})
}

// handleMLXStatus returns MLX service status
func (s *Server) handleMLXStatus(w http.ResponseWriter, r *http.Request) {
	svc := mlx.GetService()
	status := svc.GetStatus()

	// Build endpoint URL using same domain pattern as apps
	var endpoint string
	if s.config != nil {
		llmDomain := s.config.GetAppDomain("llm")
		endpoint = fmt.Sprintf("https://%s/v1/chat/completions", llmDomain)
	} else {
		endpoint = fmt.Sprintf("http://localhost:%d/v1/chat/completions", status.Port)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"supported":          mlx.IsSupported(),
		"platform":           runtime.GOOS + "/" + runtime.GOARCH,
		"unsupported_reason": mlx.GetUnsupportedReason(),
		"running":            status.Running,
		"port":               status.Port,
		"pid":                status.PID,
		"active_model":       status.ActiveModel,
		"endpoint":           endpoint,
	})
}

// handleMLXPull downloads a model
func (s *Server) handleMLXPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if req.Model == "" {
		errorResponse(w, http.StatusBadRequest, "Model is required")
		return
	}

	svc := mlx.GetService()

	// Run pull in background
	go func() {
		log.Printf("Pulling model: %s", req.Model)
		if err := svc.PullModel(req.Model, func(msg string) {
			log.Printf("Pull progress: %s", msg)
		}); err != nil {
			log.Printf("Failed to pull model %s: %v", req.Model, err)
		} else {
			log.Printf("Model %s pulled successfully", req.Model)
		}
	}()

	jsonResponse(w, http.StatusAccepted, map[string]string{
		"status":  "pulling",
		"message": "Model download started",
	})
}

// handleMLXPullProgress returns the current download progress
func (s *Server) handleMLXPullProgress(w http.ResponseWriter, r *http.Request) {
	modelID := r.URL.Query().Get("model")

	if modelID != "" {
		// Get specific model progress
		dp := mlx.GetDownloadProgress(modelID)
		if dp == nil {
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"model_id": modelID,
				"status":   "not_found",
			})
			return
		}

		jsonResponse(w, http.StatusOK, dp)
		return
	}

	// Get all active downloads
	downloads := mlx.GetAllDownloads()
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"downloads": downloads,
	})
}

// handleMLXPullCancel cancels an active download
func (s *Server) handleMLXPullCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if req.Model == "" {
		errorResponse(w, http.StatusBadRequest, "Model is required")
		return
	}

	if mlx.CancelDownload(req.Model) {
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "cancelled",
			"message": "Download cancelled",
		})
	} else {
		errorResponse(w, http.StatusNotFound, "No active download found for this model")
	}
}

// handleMLXRun starts the MLX server with a model
func (s *Server) handleMLXRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if req.Model == "" {
		errorResponse(w, http.StatusBadRequest, "Model is required")
		return
	}

	svc := mlx.GetService()
	if err := svc.Run(req.Model); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := svc.GetStatus()

	// Add Caddy route for the LLM endpoint using same domain pattern as apps
	if s.config != nil && s.caddy != nil {
		llmDomain := s.config.GetAppDomain("llm")
		route := caddy.Route{
			ID:       "mlx-llm",
			Domain:   llmDomain,
			Upstream: fmt.Sprintf("localhost:%d", status.Port),
		}
		if err := s.caddy.AddRoute(route); err != nil {
			log.Printf("Warning: failed to add Caddy route for MLX: %v", err)
		} else {
			log.Printf("Added Caddy route for MLX: %s -> localhost:%d", llmDomain, status.Port)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":       "running",
		"model":        req.Model,
		"port":         status.Port,
		"pid":          status.PID,
	})
}

// handleMLXStop stops the MLX server
func (s *Server) handleMLXStop(w http.ResponseWriter, r *http.Request) {
	svc := mlx.GetService()
	if err := svc.Stop(); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Remove Caddy route for the LLM endpoint
	if s.caddy != nil {
		if err := s.caddy.RemoveRoute("mlx-llm"); err != nil {
			log.Printf("Warning: failed to remove Caddy route for MLX: %v", err)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"status": "stopped",
	})
}

// handleMLXTranscribe transcribes audio using Whisper model
func (s *Server) handleMLXTranscribe(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 25MB audio)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		errorResponse(w, http.StatusBadRequest, "Failed to parse form: "+err.Error())
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Missing audio file")
		return
	}
	defer file.Close()

	svc := mlx.GetService()

	// Find a downloaded Whisper model
	models := svc.ListModels()
	var whisperModel string
	for _, m := range models {
		if m.Category == "speech" && !m.DownloadedAt.IsZero() {
			whisperModel = m.ID
			break
		}
	}

	if whisperModel == "" {
		errorResponse(w, http.StatusBadRequest, "No Whisper model downloaded. Please download a Whisper model first.")
		return
	}

	// Save audio to temp file
	tempFile, err := os.CreateTemp("", "audio-*.webm")
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create temp file")
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to save audio")
		return
	}
	tempFile.Close()

	// Call Whisper transcription
	text, err := svc.Transcribe(tempFile.Name(), whisperModel)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Transcription failed: "+err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"text": text,
	})
}

// handleMLXDeleteModel removes a downloaded model
func (s *Server) handleMLXDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("id")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "Model ID is required")
		return
	}

	svc := mlx.GetService()
	if err := svc.DeleteModel(modelID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"status": "deleted",
	})
}

// deployMLXApp - DEPRECATED: MLX now uses singleton service, not apps
// Kept for backwards compatibility with existing MLX apps
func (s *Server) deployMLXApp(a *app.App) {
	// Mark as failed - use /llms page instead
	a.Status = app.StatusFailed
	s.storage.UpdateApp(a)
	log.Printf("MLX apps deprecated - use LLMs page instead")
}

// startMLXApp - DEPRECATED
func (s *Server) startMLXApp(a *app.App) error {
	return fmt.Errorf("MLX apps deprecated - use LLMs page instead")
}

// stopMLXApp - DEPRECATED
func (s *Server) stopMLXApp(a *app.App) error {
	a.Status = app.StatusStopped
	s.storage.UpdateApp(a)
	return nil
}

// deleteMLXApp - DEPRECATED
func (s *Server) deleteMLXApp(a *app.App) error {
	return nil
}

// handleGetChatMessages returns chat messages for a model
func (s *Server) handleGetChatMessages(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("modelId")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	// URL decode the model ID
	modelID, _ = url.PathUnescape(modelID)

	messages, err := s.storage.GetChatMessages(modelID, 100)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	if messages == nil {
		messages = []storage.ChatMessage{}
	}
	jsonResponse(w, http.StatusOK, messages)
}

// handleSaveChatMessage saves a chat message
func (s *Server) handleSaveChatMessage(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("modelId")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	// URL decode the model ID
	modelID, _ = url.PathUnescape(modelID)

	var req struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role == "" || req.Content == "" {
		errorResponse(w, http.StatusBadRequest, "role and content required")
		return
	}

	if err := s.storage.SaveChatMessage(modelID, req.Role, req.Content); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"success": true})
}

// handleClearChatMessages clears chat messages for a model
func (s *Server) handleClearChatMessages(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("modelId")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	// URL decode the model ID
	modelID, _ = url.PathUnescape(modelID)

	if err := s.storage.ClearChatMessages(modelID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"success": true})
}

// ============================================================================
// FLUX Image Generation Handlers
// ============================================================================

// handleFluxStatus returns FLUX service status
func (s *Server) handleFluxStatus(w http.ResponseWriter, r *http.Request) {
	svc := flux.GetService(s.storage.DB())
	status := svc.GetStatus()
	jsonResponse(w, http.StatusOK, status)
}

// handleFluxModels returns available FLUX models
func (s *Server) handleFluxModels(w http.ResponseWriter, r *http.Request) {
	svc := flux.GetService(s.storage.DB())
	models := svc.ListModels()
	jsonResponse(w, http.StatusOK, models)
}

// handleFluxDownloadModel starts downloading a model
func (s *Server) handleFluxDownloadModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("id")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	progress, err := svc.DownloadModel(modelID)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusAccepted, map[string]interface{}{
		"status":  progress.Status,
		"message": progress.Message,
	})
}

// handleFluxDeleteModel deletes a downloaded model
func (s *Server) handleFluxDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("id")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	if err := svc.DeleteModel(modelID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"success": true})
}

// handleFluxDownloadProgress returns download progress for a model
func (s *Server) handleFluxDownloadProgress(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("id")
	if modelID == "" {
		errorResponse(w, http.StatusBadRequest, "model ID required")
		return
	}

	progress := flux.GetDownloadProgress(modelID)
	if progress == nil {
		jsonResponse(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}

	jsonResponse(w, http.StatusOK, progress)
}

// handleFluxGenerate starts an image generation job
func (s *Server) handleFluxGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Steps     int    `json:"steps"`
		Seed      int64  `json:"seed"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Prompt == "" {
		errorResponse(w, http.StatusBadRequest, "prompt required")
		return
	}
	if req.Model == "" {
		req.Model = "z-image-turbo" // Default model
	}
	if req.Width == 0 {
		req.Width = 1024
	}
	if req.Height == 0 {
		req.Height = 1024
	}
	if req.Steps == 0 {
		// Get default steps from model config
		for _, m := range flux.GetAvailableModels() {
			if m.ID == req.Model {
				req.Steps = m.Steps
				break
			}
		}
		if req.Steps == 0 {
			req.Steps = 4
		}
	}
	if req.Seed == 0 {
		req.Seed = -1 // Random
	}

	svc := flux.GetService(s.storage.DB())
	job, err := svc.Generate(req.Prompt, req.Model, req.Width, req.Height, req.Steps, req.Seed, req.SessionID)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusAccepted, job)
}

// handleFluxEdit starts an image editing job with reference images
func (s *Server) handleFluxEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt     string   `json:"prompt"`
		Model      string   `json:"model"`
		Width      int      `json:"width"`
		Height     int      `json:"height"`
		Steps      int      `json:"steps"`
		Seed       int64    `json:"seed"`
		ImagePaths []string `json:"image_paths"` // Paths to uploaded reference images
		SessionID  string   `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Prompt == "" {
		errorResponse(w, http.StatusBadRequest, "prompt required")
		return
	}
	if len(req.ImagePaths) == 0 {
		errorResponse(w, http.StatusBadRequest, "at least one reference image required")
		return
	}
	if req.Model == "" {
		req.Model = "flux2-klein-9b" // Default edit model
	}
	if req.Width == 0 {
		req.Width = 1024
	}
	if req.Height == 0 {
		req.Height = 1024
	}
	if req.Steps == 0 {
		req.Steps = 4
	}
	if req.Seed == 0 {
		req.Seed = -1 // Random
	}

	svc := flux.GetService(s.storage.DB())
	job, err := svc.GenerateEdit(req.Prompt, req.Model, req.Width, req.Height, req.Steps, req.Seed, req.ImagePaths, req.SessionID)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	jsonResponse(w, http.StatusAccepted, job)
}

// handleFluxUpload handles image upload for editing
func (s *Server) handleFluxUpload(w http.ResponseWriter, r *http.Request) {
	// Max 50MB
	r.ParseMultipartForm(50 << 20)

	file, header, err := r.FormFile("image")
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}
	defer file.Close()

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		errorResponse(w, http.StatusBadRequest, "only JPEG, PNG, and WebP images are supported")
		return
	}

	// Generate unique filename
	svc := flux.GetService(s.storage.DB())
	ext := ".jpg"
	if contentType == "image/png" {
		ext = ".png"
	} else if contentType == "image/webp" {
		ext = ".webp"
	}

	filename := fmt.Sprintf("upload_%d%s", time.Now().UnixNano(), ext)
	filepath := filepath.Join(svc.GetUploadsDir(), filename)

	// Save file
	dst, err := os.Create(filepath)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to save file")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"path":     filepath,
		"filename": filename,
	})
}

// handleFluxGetJob returns a generation job status
func (s *Server) handleFluxGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		errorResponse(w, http.StatusBadRequest, "job ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	job, err := svc.GetJob(jobID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, job)
}

// handleFluxListGenerations returns all generations
func (s *Server) handleFluxListGenerations(w http.ResponseWriter, r *http.Request) {
	svc := flux.GetService(s.storage.DB())
	generations, err := svc.ListGenerations()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Convert to response format
	var result []flux.Generation
	for _, g := range generations {
		result = append(result, g.ToGeneration())
	}
	if result == nil {
		result = []flux.Generation{}
	}

	jsonResponse(w, http.StatusOK, result)
}

// handleFluxGetImage serves a generated image
func (s *Server) handleFluxGetImage(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		errorResponse(w, http.StatusBadRequest, "job ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	imagePath, err := svc.GetImagePath(jobID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	http.ServeFile(w, r, imagePath)
}

// handleFluxDeleteGeneration deletes a generation
func (s *Server) handleFluxDeleteGeneration(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		errorResponse(w, http.StatusBadRequest, "job ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	if err := svc.DeleteGeneration(jobID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"success": true})
}

// handleFluxListSessions returns all image generation sessions
func (s *Server) handleFluxListSessions(w http.ResponseWriter, r *http.Request) {
	svc := flux.GetService(s.storage.DB())
	sessions, err := svc.ListSessions()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, http.StatusOK, sessions)
}

// handleFluxGetSession returns a session with all its jobs
func (s *Server) handleFluxGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		errorResponse(w, http.StatusBadRequest, "session ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	session, err := svc.GetSessionWithJobs(sessionID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, session)
}

// handleFluxDeleteSession deletes a session and all its jobs
func (s *Server) handleFluxDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		errorResponse(w, http.StatusBadRequest, "session ID required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	if err := svc.DeleteSession(sessionID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"success": true})
}

// handleFluxStorage returns storage usage information
func (s *Server) handleFluxStorage(w http.ResponseWriter, r *http.Request) {
	svc := flux.GetService(s.storage.DB())
	info := svc.GetStorageInfo()
	jsonResponse(w, http.StatusOK, info)
}

// handleFluxStorageFiles returns detailed file list for a storage type
func (s *Server) handleFluxStorageFiles(w http.ResponseWriter, r *http.Request) {
	storageType := r.PathValue("type")
	if storageType == "" {
		errorResponse(w, http.StatusBadRequest, "storage type required")
		return
	}

	svc := flux.GetService(s.storage.DB())
	files := svc.GetStorageFiles(storageType)
	jsonResponse(w, http.StatusOK, files)
}
