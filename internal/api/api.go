// Package api provides the REST API for basepod.
package api

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/base-go/basepod/internal/app"
	"github.com/base-go/basepod/internal/auth"
	"github.com/base-go/basepod/internal/backup"
	"github.com/base-go/basepod/internal/caddy"
	"github.com/base-go/basepod/internal/config"
	"github.com/base-go/basepod/internal/diskutil"
	"github.com/base-go/basepod/internal/mlx"
	"github.com/base-go/basepod/internal/podman"
	"github.com/base-go/basepod/internal/storage"
	"github.com/base-go/basepod/internal/templates"
	"github.com/base-go/basepod/internal/web"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

// assignHostPort generates a unique host port based on app ID
func assignHostPort(appID string) int {
	h := fnv.New32a()
	h.Write([]byte(appID))
	// Port range 10000-60000
	return 10000 + int(h.Sum32()%50000)
}

// generateRandomString generates a random alphanumeric string of the given length
func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// Server represents the API server
type Server struct {
	storage      *storage.Storage
	podman       podman.Client
	caddy        *caddy.Client
	config       *config.Config
	auth         *auth.Manager
	backup       *backup.Service
	router         *http.ServeMux
	staticFS       http.Handler
	staticDir      string // Path to static files on disk (preferred over embedded)
	version        string
	healthStates   map[string]*app.HealthStatus
	healthStatesMu sync.RWMutex
	healthStop     chan struct{}
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

	// Get paths for backup service
	paths, _ := config.GetPaths()

	s := &Server{
		storage: store,
		podman:  pm,
		caddy:   caddyClient,
		config:  cfg,
		auth:    auth.NewManager(cfg.Auth.PasswordHash),
		backup:  backup.NewService(paths, pm),
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

	s.healthStates = make(map[string]*app.HealthStatus)
	s.healthStop = make(chan struct{})

	s.setupRoutes()

	go s.runHealthChecker()
	go s.runMetricsCollector()

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

	// User management (admin only)
	s.router.HandleFunc("GET /api/users", s.requireAdmin(s.handleListUsers))
	s.router.HandleFunc("POST /api/users/invite", s.requireAdmin(s.handleInviteUser))
	s.router.HandleFunc("PUT /api/users/{id}/role", s.requireAdmin(s.handleUpdateUserRole))
	s.router.HandleFunc("DELETE /api/users/{id}", s.requireAdmin(s.handleDeleteUser))
	s.router.HandleFunc("POST /api/auth/accept-invite", s.handleAcceptInvite)

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
	s.router.HandleFunc("GET /api/apps/{id}/terminal", s.requireAuth(s.handleTerminal))

	// App health checks (auth required)
	s.router.HandleFunc("GET /api/apps/{id}/health", s.requireAuth(s.handleGetAppHealth))
	s.router.HandleFunc("POST /api/apps/{id}/health/check", s.requireAuth(s.handleTriggerHealthCheck))

	// System (auth required)
	s.router.HandleFunc("GET /api/system/info", s.requireAuth(s.handleSystemInfo))
	s.router.HandleFunc("GET /api/system/processes", s.requireAuth(s.handleSystemProcesses))
	s.router.HandleFunc("GET /api/system/config", s.handleGetConfig) // No auth - needed for login page
	s.router.HandleFunc("PUT /api/system/config", s.requireAuth(s.handleUpdateConfig))
	s.router.HandleFunc("GET /api/system/version", s.requireAuth(s.handleGetVersion))
	s.router.HandleFunc("POST /api/system/update", s.requireAuth(s.handleSystemUpdate))
	s.router.HandleFunc("POST /api/system/prune", s.requireAuth(s.handleSystemPrune))
	s.router.HandleFunc("GET /api/system/storage", s.requireAuth(s.handleSystemStorage))
	s.router.HandleFunc("GET /api/system/volumes", s.requireAuth(s.handleListVolumes))
	s.router.HandleFunc("DELETE /api/system/storage/{id}", s.requireAuth(s.handleDeleteStorageCategory))
	s.router.HandleFunc("POST /api/system/restart/{service}", s.requireAuth(s.handleServiceRestart))
	s.router.HandleFunc("GET /api/containers", s.requireAuth(s.handleListContainers))
	s.router.HandleFunc("POST /api/containers/{id}/import", s.requireAuth(s.handleImportContainer))

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

	// Image tags (auth required)
	s.router.HandleFunc("GET /api/images/tags", s.requireAuth(s.handleImageTags))

	// Container images management (auth required)
	s.router.HandleFunc("GET /api/container-images", s.requireAuth(s.handleListContainerImages))
	s.router.HandleFunc("DELETE /api/container-images/{id}", s.requireAuth(s.handleDeleteContainerImage))

	// Access logs (auth required)
	s.router.HandleFunc("GET /api/apps/{id}/access-logs", s.requireAuth(s.handleAppAccessLogs))

	// Caddy on-demand TLS check (no auth - called by Caddy)
	s.router.HandleFunc("GET /api/caddy/check", s.handleCaddyCheck)

	// Webhook endpoint - NO auth (GitHub calls this, validated via HMAC)
	s.router.HandleFunc("POST /api/apps/{id}/webhook", s.handleWebhook)

	// Webhook management (auth required)
	s.router.HandleFunc("POST /api/apps/{id}/webhook/setup", s.requireAuth(s.handleWebhookSetup))
	s.router.HandleFunc("GET /api/apps/{id}/webhook/deliveries", s.requireAuth(s.handleWebhookDeliveries))

	// Rollback and deployment logs (auth required)
	s.router.HandleFunc("POST /api/apps/{id}/rollback", s.requireAuth(s.handleRollback))
	s.router.HandleFunc("GET /api/apps/{id}/deployments/{deployId}/logs", s.requireAuth(s.handleDeploymentLogs))

	// Cron jobs (auth required)
	s.router.HandleFunc("GET /api/apps/{id}/cron", s.requireAuth(s.handleListCronJobs))
	s.router.HandleFunc("POST /api/apps/{id}/cron", s.requireAuth(s.handleCreateCronJob))
	s.router.HandleFunc("PUT /api/apps/{id}/cron/{jobId}", s.requireAuth(s.handleUpdateCronJob))
	s.router.HandleFunc("DELETE /api/apps/{id}/cron/{jobId}", s.requireAuth(s.handleDeleteCronJob))
	s.router.HandleFunc("POST /api/apps/{id}/cron/{jobId}/run", s.requireAuth(s.handleRunCronJob))
	s.router.HandleFunc("GET /api/apps/{id}/cron/{jobId}/executions", s.requireAuth(s.handleListCronExecutions))

	// Activity log (auth required)
	s.router.HandleFunc("GET /api/activity", s.requireAuth(s.handleListActivity))
	s.router.HandleFunc("GET /api/apps/{id}/activity", s.requireAuth(s.handleListAppActivity))

	// Notification hooks (auth required)
	s.router.HandleFunc("GET /api/notifications", s.requireAuth(s.handleListNotifications))
	s.router.HandleFunc("POST /api/notifications", s.requireAuth(s.handleCreateNotification))
	s.router.HandleFunc("PUT /api/notifications/{id}", s.requireAuth(s.handleUpdateNotification))
	s.router.HandleFunc("DELETE /api/notifications/{id}", s.requireAuth(s.handleDeleteNotification))
	s.router.HandleFunc("POST /api/notifications/{id}/test", s.requireAuth(s.handleTestNotification))

	// Deploy tokens (auth required)
	s.router.HandleFunc("GET /api/deploy-tokens", s.requireAuth(s.handleListDeployTokens))
	s.router.HandleFunc("POST /api/deploy-tokens", s.requireAuth(s.handleCreateDeployToken))
	s.router.HandleFunc("DELETE /api/deploy-tokens/{id}", s.requireAuth(s.handleDeleteDeployToken))

	// App metrics (auth required)
	s.router.HandleFunc("GET /api/apps/{id}/metrics", s.requireAuth(s.handleAppMetrics))

	// Database provisioning (auth required)
	s.router.HandleFunc("POST /api/apps/{id}/link/{dbId}", s.requireAuth(s.handleLinkDatabase))
	s.router.HandleFunc("GET /api/apps/{id}/connection-info", s.requireAuth(s.handleConnectionInfo))

	// AI Deploy Assistant (auth required)
	s.router.HandleFunc("POST /api/ai/analyze", s.requireAuth(s.handleAIAnalyze))

	// Status badge (no auth)
	s.router.HandleFunc("GET /api/badge/{id}", s.handleStatusBadge)

	// Source deploy endpoint (auth required)
	s.router.HandleFunc("POST /api/deploy", s.requireAuth(s.handleSourceDeploy))

	// Backup endpoints (auth required)
	s.router.HandleFunc("GET /api/backups", s.requireAuth(s.handleListBackups))
	s.router.HandleFunc("POST /api/backups", s.requireAuth(s.handleCreateBackup))
	s.router.HandleFunc("GET /api/backups/{id}", s.requireAuth(s.handleGetBackup))
	s.router.HandleFunc("GET /api/backups/{id}/download", s.requireAuth(s.handleDownloadBackup))
	s.router.HandleFunc("POST /api/backups/{id}/restore", s.requireAuth(s.handleRestoreBackup))
	s.router.HandleFunc("DELETE /api/backups/{id}", s.requireAuth(s.handleDeleteBackup))
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

// getSessionToken extracts the auth token from request
func (s *Server) getSessionToken(r *http.Request) string {
	token := ""
	if cookie, err := r.Cookie("basepod_token"); err == nil {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
	}
	return token
}

// requireAdmin wraps a handler, requiring the user to be an admin
func (s *Server) requireAdmin(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth.NeedsSetup() {
			errorResponse(w, http.StatusForbidden, "Setup required")
			return
		}

		token := s.getSessionToken(r)
		session := s.auth.GetSession(token)
		if session == nil {
			errorResponse(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		if session.UserRole != "admin" {
			errorResponse(w, http.StatusForbidden, "Admin access required")
			return
		}

		handler(w, r)
	}
}

// handleLogin handles password authentication (supports legacy admin + multi-user)
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Email    string `json:"email,omitempty"` // optional: for multi-user login
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	var session *auth.Session
	var err error

	if req.Email != "" {
		// Multi-user login: authenticate against users table
		user, userErr := s.storage.GetUserByEmail(req.Email)
		if userErr != nil || user == nil {
			errorResponse(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}
		if auth.HashPassword(req.Password) != user.PasswordHash {
			errorResponse(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}
		session, err = s.auth.CreateUserSession(user.ID, user.Email, user.Role)
		if err == nil {
			s.storage.UpdateUserLogin(user.ID)
		}
	} else {
		// Legacy admin login: password only
		if !s.auth.ValidatePassword(req.Password) {
			errorResponse(w, http.StatusUnauthorized, "Invalid password")
			return
		}
		session, err = s.auth.CreateSession()
	}

	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	// Set cookie
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "basepod_token",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
		"user": map[string]string{
			"id":    session.UserID,
			"email": session.UserEmail,
			"role":  session.UserRole,
		},
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

	// Inject runtime health status
	s.healthStatesMu.RLock()
	for i := range apps {
		if hs, ok := s.healthStates[apps[i].ID]; ok {
			apps[i].Health = hs
		}
	}
	s.healthStatesMu.RUnlock()

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

	// Inject runtime health status
	s.healthStatesMu.RLock()
	if hs, ok := s.healthStates[a.ID]; ok {
		a.Health = hs
	}
	s.healthStatesMu.RUnlock()

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
	if req.Volumes != nil {
		a.Volumes = *req.Volumes
	}
	if req.HealthCheck != nil {
		a.HealthCheck = req.HealthCheck
	}
	if req.Deployment != nil {
		a.Deployment = *req.Deployment
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

	// Remove Caddy routes
	if s.caddy != nil {
		// Container app route
		_ = s.caddy.RemoveRoute("basepod-" + a.Name)
		// Static site routes
		if a.Domain != "" {
			_ = s.caddy.RemoveRoute("static-" + a.Domain)
			_ = s.caddy.RemoveRoute("static-" + a.Name + "." + s.config.Domain.Root)
		}
		// Alias routes (both container and static patterns)
		for _, alias := range a.Aliases {
			_ = s.caddy.RemoveRoute(fmt.Sprintf("alias-%s-%s", a.ID[:8], alias))
			_ = s.caddy.RemoveRoute("static-" + alias)
		}
	}

	// Remove static site files from disk
	if a.Type == app.AppTypeStatic {
		paths, err := config.GetPaths()
		if err == nil {
			staticDir := filepath.Join(paths.Apps, a.Name)
			if _, statErr := os.Stat(staticDir); statErr == nil {
				os.RemoveAll(staticDir)
			}
			// Also try domain-named directory
			if a.Domain != "" && a.Domain != a.Name {
				domainDir := filepath.Join(paths.Apps, a.Domain)
				if _, statErr := os.Stat(domainDir); statErr == nil {
					os.RemoveAll(domainDir)
				}
			}
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

	s.logActivity("user", "start", "app", a.ID, a.Name, "success", "")

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

	s.logActivity("user", "stop", "app", a.ID, a.Name, "success", "")

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
		Memory: a.Resources.Memory * 1024 * 1024,
		CPUs:   a.Resources.CPUs,
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

	s.logActivity("user", "restart", "app", a.ID, a.Name, "success", "")

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
		Memory: a.Resources.Memory * 1024 * 1024,
		CPUs:   a.Resources.CPUs,
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

	// Podman multiplexed stream: each frame has an 8-byte header
	// [stream_type(1), padding(3), size(4 big-endian)]
	// Strip headers and output only the payload
	reader := bufio.NewReader(logs)
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(reader, header)
		if err != nil {
			break
		}
		// Frame size from bytes 4-7 (big-endian uint32)
		frameSize := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if frameSize <= 0 || frameSize > 1<<20 {
			// Invalid frame - likely not multiplexed, dump remaining as-is
			w.Write(header[:])
			buf := make([]byte, 4096)
			for {
				n, err := reader.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
			break
		}
		// Read and write the payload
		payload := make([]byte, frameSize)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			break
		}
		w.Write(payload)
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

// handleSystemStorage returns full disk usage overview with basepod categories
func (s *Server) handleSystemStorage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	paths, err := config.GetPaths()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to get paths: "+err.Error())
		return
	}

	// Get filesystem disk usage
	du, err := diskutil.GetDiskUsage(paths.Base)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to get disk usage: "+err.Error())
		return
	}

	type StorageCategory struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Formatted string `json:"formatted"`
		Count     int    `json:"count"`
		Icon      string `json:"icon"`
		Color     string `json:"color"`
	}

	var categories []StorageCategory
	var basepodTotal int64

	// Container Images
	var imagesSize int64
	var imagesCount int
	images, err := s.podman.ListImages(ctx)
	if err == nil {
		for _, img := range images {
			imagesSize += img.Size
		}
		imagesCount = len(images)
	}
	categories = append(categories, StorageCategory{
		ID: "images", Name: "Container Images",
		Size: imagesSize, Formatted: diskutil.FormatBytes(imagesSize),
		Count: imagesCount, Icon: "i-heroicons-square-3-stack-3d", Color: "blue",
	})
	basepodTotal += imagesSize

	// Container Volumes
	var volumesSize int64
	var volumesCount int
	volumes, err := s.podman.ListVolumes(ctx)
	if err == nil {
		volumesCount = len(volumes)
		for _, vol := range volumes {
			if vol.Mountpoint != "" {
				volumesSize += diskutil.DirSize(vol.Mountpoint)
			}
		}
	}
	categories = append(categories, StorageCategory{
		ID: "volumes", Name: "Container Volumes",
		Size: volumesSize, Formatted: diskutil.FormatBytes(volumesSize),
		Count: volumesCount, Icon: "i-heroicons-circle-stack", Color: "cyan",
	})
	basepodTotal += volumesSize

	// Apps & Static Sites
	appsSize := diskutil.DirSize(paths.Apps)
	var appsCount int
	if entries, err := os.ReadDir(paths.Apps); err == nil {
		appsCount = len(entries)
	}
	categories = append(categories, StorageCategory{
		ID: "apps", Name: "Apps & Static Sites",
		Size: appsSize, Formatted: diskutil.FormatBytes(appsSize),
		Count: appsCount, Icon: "i-heroicons-globe-alt", Color: "green",
	})
	basepodTotal += appsSize

	// Database
	dbPath := filepath.Join(paths.Data, "basepod.db")
	dbSize := diskutil.FileSize(dbPath)
	categories = append(categories, StorageCategory{
		ID: "database", Name: "Database",
		Size: dbSize, Formatted: diskutil.FormatBytes(dbSize),
		Count: 1, Icon: "i-heroicons-server", Color: "amber",
	})
	basepodTotal += dbSize

	// Backups
	backupsDir := filepath.Join(paths.Base, "backups")
	backupsSize := diskutil.DirSize(backupsDir)
	var backupsCount int
	if entries, err := os.ReadDir(backupsDir); err == nil {
		backupsCount = len(entries)
	}
	categories = append(categories, StorageCategory{
		ID: "backups", Name: "Backups",
		Size: backupsSize, Formatted: diskutil.FormatBytes(backupsSize),
		Count: backupsCount, Icon: "i-heroicons-archive-box", Color: "orange",
	})
	basepodTotal += backupsSize

	// AI/LLM Models
	home, _ := os.UserHomeDir()
	mlxDir := filepath.Join(home, ".local", "share", "basepod", "mlx")
	mlxSize := diskutil.DirSize(mlxDir)
	var mlxCount int
	if entries, err := os.ReadDir(mlxDir); err == nil {
		mlxCount = len(entries)
	}
	categories = append(categories, StorageCategory{
		ID: "llm", Name: "AI / LLM Models",
		Size: mlxSize, Formatted: diskutil.FormatBytes(mlxSize),
		Count: mlxCount, Icon: "i-heroicons-cpu-chip", Color: "pink",
	})
	basepodTotal += mlxSize

	// Logs
	logsSize := diskutil.DirSize(paths.Logs)
	var logsCount int
	if entries, err := os.ReadDir(paths.Logs); err == nil {
		logsCount = len(entries)
	}
	categories = append(categories, StorageCategory{
		ID: "logs", Name: "Logs",
		Size: logsSize, Formatted: diskutil.FormatBytes(logsSize),
		Count: logsCount, Icon: "i-heroicons-document-text", Color: "gray",
	})
	basepodTotal += logsSize

	// HuggingFace Cache
	hfDir := filepath.Join(home, ".cache", "huggingface")
	hfSize := diskutil.DirSize(hfDir)
	var hfCount int
	if entries, err := os.ReadDir(filepath.Join(hfDir, "hub")); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "models--") {
				hfCount++
			}
		}
	}
	categories = append(categories, StorageCategory{
		ID: "huggingface", Name: "HuggingFace Cache",
		Size: hfSize, Formatted: diskutil.FormatBytes(hfSize),
		Count: hfCount, Icon: "i-heroicons-cloud-arrow-down", Color: "yellow",
	})
	basepodTotal += hfSize

	// Podman Storage
	podmanDir := filepath.Join(home, ".local", "share", "containers")
	podmanStorageSize := diskutil.DirSize(podmanDir)
	categories = append(categories, StorageCategory{
		ID: "podman", Name: "Podman Storage",
		Size: podmanStorageSize, Formatted: diskutil.FormatBytes(podmanStorageSize),
		Count: 0, Icon: "i-heroicons-cube", Color: "indigo",
	})
	basepodTotal += podmanStorageSize

	// Other/System usage = disk used - basepod total
	otherSize := int64(du.Used) - basepodTotal
	if otherSize < 0 {
		otherSize = 0
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"disk":           du,
		"categories":     categories,
		"basepod_total":  basepodTotal,
		"basepod_formatted": diskutil.FormatBytes(basepodTotal),
		"other_size":     otherSize,
		"other_formatted": diskutil.FormatBytes(otherSize),
	})
}

// handleDeleteStorageCategory deletes a clearable storage category
func (s *Server) handleDeleteStorageCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	home, _ := os.UserHomeDir()

	var targetDir string
	var label string

	switch id {
	case "huggingface":
		targetDir = filepath.Join(home, ".cache", "huggingface")
		label = "HuggingFace Cache"
	case "logs":
		paths, err := config.GetPaths()
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "failed to get paths")
			return
		}
		targetDir = paths.Logs
		label = "Logs"
	default:
		errorResponse(w, http.StatusBadRequest, "category not clearable: "+id)
		return
	}

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"message": label + " already empty",
			"cleared": int64(0),
		})
		return
	}

	// Calculate size before clearing
	size := diskutil.DirSize(targetDir)

	// Remove contents but keep the directory
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to read directory: "+err.Error())
		return
	}
	for _, entry := range entries {
		os.RemoveAll(filepath.Join(targetDir, entry.Name()))
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": label + " cleared",
		"cleared": size,
		"cleared_formatted": diskutil.FormatBytes(size),
	})
}

// ProcessInfo represents a running process
type ProcessInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // mlx, container, system
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

	// 3. Running containers
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

	// Restart by exiting - launchd/systemd KeepAlive will restart with new binary
	go func() {
		time.Sleep(500 * time.Millisecond) // Give time for response to be sent
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

// handleImportContainer imports an existing container into basepod
func (s *Server) handleImportContainer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	containerID := r.PathValue("id")
	if containerID == "" {
		errorResponse(w, http.StatusBadRequest, "Container ID is required")
		return
	}

	// Parse request body for domain
	var req struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Find the container from list (to handle short IDs)
	containers, err := s.podman.ListContainers(ctx, true)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to list containers: "+err.Error())
		return
	}

	var foundContainer *podman.Container
	for i, c := range containers {
		// Match by full ID, short ID, or name
		if c.ID == containerID || strings.HasPrefix(c.ID, containerID) || (len(c.Names) > 0 && c.Names[0] == containerID) {
			foundContainer = &containers[i]
			break
		}
	}

	if foundContainer == nil {
		errorResponse(w, http.StatusNotFound, "Container not found")
		return
	}

	// Check if container is already managed by basepod
	apps, _ := s.storage.ListApps()
	for _, a := range apps {
		if a.ContainerID == foundContainer.ID {
			errorResponse(w, http.StatusConflict, "Container is already managed by basepod as app: "+a.Name)
			return
		}
	}

	// Inspect container for details
	inspect, err := s.podman.InspectContainer(ctx, foundContainer.ID)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to inspect container: "+err.Error())
		return
	}

	// Determine app name - use provided name, or container name without basepod- prefix
	appName := req.Name
	if appName == "" {
		if len(foundContainer.Names) > 0 {
			appName = foundContainer.Names[0]
			// Remove common prefixes
			appName = strings.TrimPrefix(appName, "/")
			appName = strings.TrimPrefix(appName, "basepod-")
		} else {
			appName = containerID[:12]
		}
	}

	// Check if app name already exists
	existing, _ := s.storage.GetAppByName(appName)
	if existing != nil {
		errorResponse(w, http.StatusConflict, "App with this name already exists")
		return
	}

	// Determine domain
	domain := req.Domain
	if domain == "" {
		domain = s.config.GetAppDomain(appName)
	}

	// Get container port and host port from port mappings
	var containerPort, hostPort int
	if len(foundContainer.Ports) > 0 {
		containerPort = foundContainer.Ports[0].ContainerPort
		hostPort = foundContainer.Ports[0].HostPort
	}
	if containerPort == 0 {
		containerPort = 8080 // default
	}

	// Parse environment from inspect
	env := make(map[string]string)
	for _, e := range inspect.Config.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			// Skip common system env vars
			if parts[0] == "PATH" || parts[0] == "HOME" || strings.HasPrefix(parts[0], "HOSTNAME") {
				continue
			}
			env[parts[0]] = parts[1]
		}
	}

	// Determine status from container state
	status := app.StatusStopped
	if inspect.State.Running {
		status = app.StatusRunning
	}

	// Create the app
	newApp := &app.App{
		ID:          uuid.New().String(),
		Name:        appName,
		Type:        app.AppTypeContainer,
		Domain:      domain,
		ContainerID: foundContainer.ID,
		Image:       foundContainer.Image,
		Status:      status,
		Env:         env,
		Ports: app.PortConfig{
			ContainerPort: containerPort,
			HostPort:      hostPort,
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

	// Assign host port if not set
	if newApp.Ports.HostPort == 0 {
		newApp.Ports.HostPort = assignHostPort(newApp.ID)
	}

	// Save to database
	if err := s.storage.CreateApp(newApp); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create app: "+err.Error())
		return
	}

	// Add Caddy route
	if s.caddy != nil && newApp.Status == app.StatusRunning {
		internalHost := fmt.Sprintf("localhost:%d", newApp.Ports.HostPort)
		if err := s.caddy.AddRoute(caddy.Route{
			ID:        "basepod-" + newApp.ID,
			Domain:    domain,
			Upstream:  internalHost,
			EnableSSL: newApp.SSL.Enabled,
		}); err != nil {
			log.Printf("Warning: Failed to add Caddy route for %s: %v", domain, err)
		}
	}

	jsonResponse(w, http.StatusCreated, newApp)
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
		Memory: a.Resources.Memory,
		CPUs:   a.Resources.CPUs,
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

		// Auto-generate secure credentials for database templates
		autoPassword := generateRandomString(24)
		autoUser := "basepod"
		autoDB := name

		switch tmpl.ID {
		case "postgres", "postgresql":
			if env["POSTGRES_PASSWORD"] == "" || env["POSTGRES_PASSWORD"] == "changeme" {
				env["POSTGRES_PASSWORD"] = autoPassword
			}
			if env["POSTGRES_USER"] == "" {
				env["POSTGRES_USER"] = autoUser
			}
			if env["POSTGRES_DB"] == "" {
				env["POSTGRES_DB"] = autoDB
			}
		case "mysql", "mariadb":
			if env["MYSQL_ROOT_PASSWORD"] == "" || env["MYSQL_ROOT_PASSWORD"] == "changeme" {
				env["MYSQL_ROOT_PASSWORD"] = autoPassword
			}
			if env["MYSQL_USER"] == "" {
				env["MYSQL_USER"] = autoUser
			}
			if env["MYSQL_PASSWORD"] == "" {
				env["MYSQL_PASSWORD"] = autoPassword
			}
			if env["MYSQL_DATABASE"] == "" || env["MYSQL_DATABASE"] == "app" {
				env["MYSQL_DATABASE"] = autoDB
			}
		case "redis":
			if env["REDIS_PASSWORD"] == "" {
				env["REDIS_PASSWORD"] = autoPassword
			}
		}
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
		Memory: a.Resources.Memory,
		CPUs:   a.Resources.CPUs,
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

	// Log received git info for debugging
	if deployConfig.GitCommit != "" {
		log.Printf("Deploy %s: git commit=%s branch=%s msg=%s",
			deployConfig.Name, deployConfig.GitCommit, deployConfig.GitBranch, deployConfig.GitMessage)
	} else {
		log.Printf("Deploy %s: no git info received", deployConfig.Name)
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

	var buildLog strings.Builder
	writeLine := func(msg string) {
		fmt.Fprintf(w, "%s\n", msg)
		flusher.Flush()
		buildLog.WriteString(msg + "\n")
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

	// Read .basepod config file if it exists
	basepodConfigPath := sourceDir + "/basepod.yaml"
	if _, err := os.Stat(basepodConfigPath); err == nil {
		writeLine("Found basepod.yaml config file")
		configData, err := os.ReadFile(basepodConfigPath)
		if err == nil {
			var repoConfig struct {
				Name       string            `yaml:"name" json:"name"`
				Type       string            `yaml:"type" json:"type"`
				Port       int               `yaml:"port" json:"port"`
				Dockerfile string            `yaml:"dockerfile" json:"dockerfile"`
				Context    string            `yaml:"context" json:"context"`
				Public     string            `yaml:"public" json:"public"`
				Env        map[string]string  `yaml:"env" json:"env"`
				BuildArgs  map[string]string  `yaml:"build_args" json:"build_args"`
			}
			// Try YAML first, then JSON
			if err := yaml.Unmarshal(configData, &repoConfig); err != nil {
				_ = json.Unmarshal(configData, &repoConfig)
			}
			if repoConfig.Port > 0 && deployConfig.Port == 0 {
				deployConfig.Port = repoConfig.Port
				writeLine(fmt.Sprintf("  port: %d", repoConfig.Port))
			}
			if repoConfig.Dockerfile != "" && deployConfig.Build.Dockerfile == "" {
				deployConfig.Build.Dockerfile = repoConfig.Dockerfile
				writeLine(fmt.Sprintf("  dockerfile: %s", repoConfig.Dockerfile))
			}
			if repoConfig.Context != "" && deployConfig.Build.Context == "" {
				deployConfig.Build.Context = repoConfig.Context
				writeLine(fmt.Sprintf("  context: %s", repoConfig.Context))
			}
			if repoConfig.Public != "" && deployConfig.Public == "" {
				deployConfig.Public = repoConfig.Public
				writeLine(fmt.Sprintf("  public: %s", repoConfig.Public))
			}
			if repoConfig.Type != "" && deployConfig.Type == "" {
				deployConfig.Type = repoConfig.Type
				writeLine(fmt.Sprintf("  type: %s", repoConfig.Type))
			}
			// Merge env vars (repo config as defaults, CLI overrides)
			if len(repoConfig.Env) > 0 {
				if deployConfig.Env == nil {
					deployConfig.Env = make(map[string]string)
				}
				for k, v := range repoConfig.Env {
					if _, exists := deployConfig.Env[k]; !exists {
						deployConfig.Env[k] = v
					}
				}
			}
		}
	}

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
			BuildLog:   buildLog.String(),
			DeployedAt: time.Now(),
		}
		a.Deployments = append([]app.DeploymentRecord{deployRecord}, a.Deployments...)
		// Keep only last 10 deployments
		if len(a.Deployments) > 10 {
			a.Deployments = a.Deployments[:10]
		}

		if deployRecord.CommitHash != "" {
			writeLine(fmt.Sprintf("Recording deployment: %s@%s (%s)", a.Name, deployRecord.CommitHash, deployRecord.CommitMsg))
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

	// Check if Dockerfile exists, auto-generate if not
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		writeLine("No Dockerfile found, auto-detecting stack...")
		generated := generateDockerfile(sourceDir, deployConfig.Port)
		if generated == "" {
			writeLine("ERROR: Could not detect project type. Please create a Dockerfile.")
			return
		}
		if err := os.WriteFile(dockerfilePath, []byte(generated), 0644); err != nil {
			writeLine("ERROR: Failed to write generated Dockerfile: " + err.Error())
			return
		}
		writeLine("Auto-generated Dockerfile for detected stack")
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
		Memory: a.Resources.Memory * 1024 * 1024, // MB to bytes
		CPUs:   a.Resources.CPUs,
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
		Image:      imageName,
		CommitHash: deployConfig.GitCommit,
		CommitMsg:  deployConfig.GitMessage,
		Branch:     deployConfig.GitBranch,
		Status:     "success",
		BuildLog:   buildLog.String(),
		DeployedAt: time.Now(),
	}
	a.Deployments = append([]app.DeploymentRecord{deployRecord}, a.Deployments...)
	// Keep only last 10 deployments
	if len(a.Deployments) > 10 {
		a.Deployments = a.Deployments[:10]
	}

	if deployRecord.CommitHash != "" {
		writeLine(fmt.Sprintf("Recording deployment: %s@%s (%s)", a.Name, deployRecord.CommitHash, deployRecord.CommitMsg))
	}

	s.storage.UpdateApp(a)

	// Log activity
	s.logActivity("system", "deploy", "app", a.ID, a.Name, "success", "")

	// Send notifications
	s.sendNotifications("deploy_success", a.ID, a.Name, map[string]string{
		"commit": deployConfig.GitCommit,
		"branch": deployConfig.GitBranch,
	})

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

// generateDockerfile auto-generates a Dockerfile based on detected project stack
func generateDockerfile(sourceDir string, port int) string {
	if port == 0 {
		port = 8080
	}

	// Node.js (package.json)
	if _, err := os.Stat(sourceDir + "/package.json"); err == nil {
		// Check for package-lock.json vs yarn.lock
		installCmd := "npm install"
		lockCopy := "COPY package*.json ./"
		if _, err := os.Stat(sourceDir + "/yarn.lock"); err == nil {
			installCmd = "yarn install --frozen-lockfile"
			lockCopy = "COPY package.json yarn.lock ./"
		} else if _, err := os.Stat(sourceDir + "/pnpm-lock.yaml"); err == nil {
			installCmd = "corepack enable && pnpm install --frozen-lockfile"
			lockCopy = "COPY package.json pnpm-lock.yaml ./"
		}
		return fmt.Sprintf(`FROM node:20-alpine
WORKDIR /app
%s
RUN %s
COPY . .
RUN npm run build 2>/dev/null || true
EXPOSE %d
CMD ["npm", "start"]
`, lockCopy, installCmd, port)
	}

	// Go (go.mod)
	if _, err := os.Stat(sourceDir + "/go.mod"); err == nil {
		return fmt.Sprintf(`FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE %d
CMD ["./server"]
`, port)
	}

	// Python (requirements.txt or pyproject.toml)
	if _, err := os.Stat(sourceDir + "/requirements.txt"); err == nil {
		return fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE %d
CMD ["python", "app.py"]
`, port)
	}
	if _, err := os.Stat(sourceDir + "/pyproject.toml"); err == nil {
		return fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY pyproject.toml .
RUN pip install --no-cache-dir .
COPY . .
EXPOSE %d
CMD ["python", "-m", "app"]
`, port)
	}

	// Ruby (Gemfile)
	if _, err := os.Stat(sourceDir + "/Gemfile"); err == nil {
		return fmt.Sprintf(`FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile Gemfile.lock ./
RUN bundle install
COPY . .
EXPOSE %d
CMD ["ruby", "app.rb"]
`, port)
	}

	// Rust (Cargo.toml)
	if _, err := os.Stat(sourceDir + "/Cargo.toml"); err == nil {
		return fmt.Sprintf(`FROM rust:1.77-slim AS builder
WORKDIR /app
COPY . .
RUN cargo build --release

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=builder /app/target/release/* /app/
EXPOSE %d
CMD ["./app"]
`, port)
	}

	return ""
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

// handleAppAccessLogs returns Caddy access logs filtered by app domain
func (s *Server) handleAppAccessLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.storage.GetApp(id)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	paths, _ := config.GetPaths()
	// Read from caddy.err which contains access logs (Caddy writes all logs to stderr)
	logFile := fmt.Sprintf("%s/logs/caddy.err", paths.Base)

	// Read log file
	file, err := os.Open(logFile)
	if err != nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"logs":    []interface{}{},
			"message": "No access logs yet",
		})
		return
	}
	defer file.Close()

	// Get file size and seek to last 5MB if large (caddy.err has all logs, need more to find access entries)
	stat, _ := file.Stat()
	if stat.Size() > 5*1024*1024 {
		file.Seek(-5*1024*1024, 2) // Last 5MB
	}

	// Collect domains to filter by (primary + aliases)
	domains := map[string]bool{a.Domain: true}
	for _, alias := range a.Aliases {
		domains[alias] = true
	}

	// Parse and filter log lines
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	var logs []map[string]interface{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Only process access log entries (skip Caddy operational logs)
		logger, _ := entry["logger"].(string)
		if !strings.Contains(logger, "http.log.access") {
			continue
		}

		// Filter by domain - check request.host
		reqMap, ok := entry["request"].(map[string]interface{})
		if !ok {
			continue
		}
		host, _ := reqMap["host"].(string)
		if !domains[host] {
			continue
		}

		logs = append(logs, entry)
	}

	// Return last N entries
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"total": len(logs),
	})
}

// handleGetAppHealth returns health status for an app
func (s *Server) handleGetAppHealth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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

	s.healthStatesMu.RLock()
	hs := s.healthStates[a.ID]
	s.healthStatesMu.RUnlock()

	if hs == nil {
		hs = &app.HealthStatus{Status: "unknown"}
	}

	jsonResponse(w, http.StatusOK, hs)
}

// handleTriggerHealthCheck triggers an immediate health check for an app
func (s *Server) handleTriggerHealthCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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

	if a.HealthCheck == nil {
		errorResponse(w, http.StatusBadRequest, "Health checks not configured for this app")
		return
	}

	if a.Status != app.StatusRunning {
		errorResponse(w, http.StatusBadRequest, "App is not running")
		return
	}

	hs := s.checkAppHealth(a)

	jsonResponse(w, http.StatusOK, hs)
}

// checkAppHealth performs a single health check for an app and updates state
func (s *Server) checkAppHealth(a *app.App) *app.HealthStatus {
	s.healthStatesMu.Lock()
	hs, ok := s.healthStates[a.ID]
	if !ok {
		hs = &app.HealthStatus{Status: "unknown"}
		s.healthStates[a.ID] = hs
	}
	s.healthStatesMu.Unlock()

	hc := a.HealthCheck
	endpoint := hc.Endpoint
	if endpoint == "" {
		endpoint = "/health"
	}
	timeout := hc.Timeout
	if timeout <= 0 {
		timeout = 5
	}

	checkURL := fmt.Sprintf("http://localhost:%d%s", a.Ports.HostPort, endpoint)
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	resp, err := client.Get(checkURL)

	s.healthStatesMu.Lock()
	defer s.healthStatesMu.Unlock()

	hs.TotalChecks++
	hs.LastCheck = time.Now()

	if err != nil {
		hs.ConsecutiveFailures++
		hs.TotalFailures++
		hs.LastError = err.Error()
		hs.Status = "unhealthy"
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			hs.ConsecutiveFailures = 0
			hs.LastSuccess = time.Now()
			hs.LastError = ""
			hs.Status = "healthy"
		} else {
			hs.ConsecutiveFailures++
			hs.TotalFailures++
			hs.LastError = fmt.Sprintf("HTTP %d", resp.StatusCode)
			hs.Status = "unhealthy"
		}
	}

	// Auto-restart if configured
	maxFailures := hc.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 3
	}
	if hc.AutoRestart && hs.ConsecutiveFailures >= maxFailures {
		log.Printf("Health check: app %s (%s) exceeded %d failures, restarting...", a.Name, a.ID, maxFailures)
		go s.restartAppForHealth(a)
		hs.ConsecutiveFailures = 0
	}

	return hs
}

// restartAppForHealth restarts an app due to health check failure
func (s *Server) restartAppForHealth(a *app.App) {
	ctx := context.Background()

	if a.Type == app.AppTypeMLX {
		return
	}

	if a.ContainerID == "" && a.Image == "" {
		return
	}

	containerName := "basepod-" + a.Name
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	volumeMounts := []string{}
	for _, v := range a.Volumes {
		if v.HostPath != "" && v.ContainerPath != "" {
			volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath))
		}
	}

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
		Memory: a.Resources.Memory,
		CPUs:   a.Resources.CPUs,
	})
	if err != nil {
		log.Printf("Health check restart failed for %s: %v", a.Name, err)
		return
	}

	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		log.Printf("Health check restart failed to start %s: %v", a.Name, err)
		return
	}

	a.ContainerID = containerID
	a.Status = app.StatusRunning
	s.storage.UpdateApp(a)
	log.Printf("Health check: successfully restarted app %s", a.Name)
}

// runHealthChecker runs the background health check loop
func (s *Server) runHealthChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.healthStop:
			return
		case <-ticker.C:
			s.runHealthChecks()
		}
	}
}

// runHealthChecks performs health checks on all configured apps
func (s *Server) runHealthChecks() {
	apps, err := s.storage.ListApps()
	if err != nil {
		return
	}

	for i := range apps {
		a := &apps[i]
		if a.HealthCheck == nil || a.Status != app.StatusRunning {
			continue
		}
		if a.Ports.HostPort == 0 {
			continue
		}

		// Check if enough time has elapsed since last check
		interval := a.HealthCheck.Interval
		if interval <= 0 {
			interval = 30
		}

		s.healthStatesMu.RLock()
		hs := s.healthStates[a.ID]
		s.healthStatesMu.RUnlock()

		if hs != nil && time.Since(hs.LastCheck) < time.Duration(interval)*time.Second {
			continue
		}

		s.checkAppHealth(a)
	}
}

// handleListContainerImages returns all container images
func (s *Server) handleListContainerImages(w http.ResponseWriter, r *http.Request) {
	if s.podman == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Podman not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	images, err := s.podman.ListImages(ctx)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to list images: "+err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, images)
}

// handleDeleteContainerImage deletes a container image
func (s *Server) handleDeleteContainerImage(w http.ResponseWriter, r *http.Request) {
	if s.podman == nil {
		errorResponse(w, http.StatusServiceUnavailable, "Podman not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		errorResponse(w, http.StatusBadRequest, "Image ID required")
		return
	}

	force := r.URL.Query().Get("force") == "true"

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := s.podman.RemoveImage(ctx, id, force); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to remove image: "+err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
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
// Backup Handlers
// ============================================================================

// handleListBackups returns all available backups
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.backup.List()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Format response with human-readable sizes
	type backupResponse struct {
		ID        string          `json:"id"`
		CreatedAt time.Time       `json:"created_at"`
		Size      int64           `json:"size"`
		SizeHuman string          `json:"size_human"`
		Path      string          `json:"path"`
		Contents  backup.Contents `json:"contents"`
	}

	response := make([]backupResponse, 0, len(backups))
	for _, b := range backups {
		// Ensure arrays are never null
		contents := b.Contents
		if contents.StaticSites == nil {
			contents.StaticSites = []string{}
		}
		if contents.Volumes == nil {
			contents.Volumes = []string{}
		}
		response = append(response, backupResponse{
			ID:        b.ID,
			CreatedAt: b.CreatedAt,
			Size:      b.Size,
			SizeHuman: backup.FormatSize(b.Size),
			Path:      b.Path,
			Contents:  contents,
		})
	}

	jsonResponse(w, http.StatusOK, response)
}

// handleCreateBackup creates a new backup
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse options from request body (optional)
	var req struct {
		IncludeVolumes bool   `json:"include_volumes"`
		IncludeBuilds  bool   `json:"include_builds"`
		OutputDir      string `json:"output_dir"`
	}
	// Set defaults
	req.IncludeVolumes = true
	req.IncludeBuilds = false

	// Try to decode body, ignore if empty
	json.NewDecoder(r.Body).Decode(&req)

	opts := backup.Options{
		IncludeVolumes: req.IncludeVolumes,
		IncludeBuilds:  req.IncludeBuilds,
		OutputDir:      req.OutputDir,
	}

	// Create backup
	b, err := s.backup.Create(ctx, opts)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create backup: "+err.Error())
		return
	}

	// Ensure arrays are never null
	contents := b.Contents
	if contents.StaticSites == nil {
		contents.StaticSites = []string{}
	}
	if contents.Volumes == nil {
		contents.Volumes = []string{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"id":         b.ID,
		"created_at": b.CreatedAt,
		"size":       b.Size,
		"size_human": backup.FormatSize(b.Size),
		"path":       b.Path,
		"contents":   contents,
	})
}

// handleGetBackup returns details of a specific backup
func (s *Server) handleGetBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		errorResponse(w, http.StatusBadRequest, "backup ID required")
		return
	}

	b, err := s.backup.Get(id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	// Ensure arrays are never null
	contents := b.Contents
	if contents.StaticSites == nil {
		contents.StaticSites = []string{}
	}
	if contents.Volumes == nil {
		contents.Volumes = []string{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"id":         b.ID,
		"created_at": b.CreatedAt,
		"size":       b.Size,
		"size_human": backup.FormatSize(b.Size),
		"path":       b.Path,
		"contents":   contents,
	})
}

// handleDownloadBackup streams the backup file to the client
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		errorResponse(w, http.StatusBadRequest, "backup ID required")
		return
	}

	b, err := s.backup.Get(id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	// Open backup file
	file, err := os.Open(b.Path)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to open backup file")
		return
	}
	defer file.Close()

	// Set headers for file download
	filename := filepath.Base(b.Path)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", b.Size))

	// Stream file to response
	io.Copy(w, file)
}

// handleDeleteBackup deletes a backup
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		errorResponse(w, http.StatusBadRequest, "backup ID required")
		return
	}

	if err := s.backup.Delete(id); err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleRestoreBackup restores from a backup
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id == "" {
		errorResponse(w, http.StatusBadRequest, "backup ID required")
		return
	}

	// Parse options from request body (optional)
	var req struct {
		RestoreDatabase bool `json:"restore_database"`
		RestoreConfig   bool `json:"restore_config"`
		RestoreApps     bool `json:"restore_apps"`
		RestoreVolumes  bool `json:"restore_volumes"`
	}
	// Set defaults - restore everything
	req.RestoreDatabase = true
	req.RestoreConfig = true
	req.RestoreApps = true
	req.RestoreVolumes = true

	// Try to decode body, ignore if empty
	json.NewDecoder(r.Body).Decode(&req)

	opts := backup.RestoreOptions{
		RestoreDatabase: req.RestoreDatabase,
		RestoreConfig:   req.RestoreConfig,
		RestoreApps:     req.RestoreApps,
		RestoreVolumes:  req.RestoreVolumes,
	}

	// Perform restore
	result, err := s.backup.Restore(ctx, id, opts)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Restore failed: "+err.Error())
		return
	}

	// Ensure arrays are never null
	configFiles := result.ConfigFiles
	if configFiles == nil {
		configFiles = []string{}
	}
	staticSites := result.StaticSites
	if staticSites == nil {
		staticSites = []string{}
	}
	volumes := result.Volumes
	if volumes == nil {
		volumes = []string{}
	}
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"database":     result.Database,
		"config_files": configFiles,
		"static_sites": staticSites,
		"volumes":      volumes,
		"warnings":     warnings,
		"message":      "Restore completed. Please restart basepod for changes to take effect.",
	})
}

// handleListVolumes returns detailed volume information
func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	volumes, err := s.podman.ListVolumes(ctx)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to list volumes: "+err.Error())
		return
	}

	type VolumeInfo struct {
		Name       string `json:"name"`
		Driver     string `json:"driver"`
		Mountpoint string `json:"mountpoint"`
		Size       int64  `json:"size"`
		Formatted  string `json:"formatted"`
		CreatedAt  string `json:"created_at"`
	}

	var result []VolumeInfo
	for _, vol := range volumes {
		var size int64
		if vol.Mountpoint != "" {
			size = diskutil.DirSize(vol.Mountpoint)
		}
		result = append(result, VolumeInfo{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			Size:       size,
			Formatted:  diskutil.FormatBytes(size),
			CreatedAt:  vol.CreatedAt,
		})
	}

	jsonResponse(w, http.StatusOK, result)
}

// WebSocket upgrader for terminal connections
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// handleTerminal provides WebSocket-based terminal access to a container
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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
		errorResponse(w, http.StatusBadRequest, "App has no container")
		return
	}
	if a.Status != "running" {
		errorResponse(w, http.StatusBadRequest, "App is not running")
		return
	}

	// Create exec session - try bash, fall back to sh
	execID, err := s.podman.ExecCreate(ctx, a.ContainerID, []string{
		"/bin/sh", "-c", "command -v bash >/dev/null && exec bash || exec sh",
	})
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "failed to create exec session: "+err.Error())
		return
	}

	// Upgrade to WebSocket
	wsConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer wsConn.Close()

	// Start exec session via raw HTTP hijack to get bidirectional stream
	socketPath := s.podman.GetSocketPath()
	baseURL := s.podman.GetBaseURL()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte("Error: failed to connect to Podman: "+err.Error()))
		return
	}
	defer conn.Close()

	// Send HTTP request to start exec
	startBody := `{"Detach":false,"Tty":true}`
	reqStr := fmt.Sprintf("POST %s/exec/%s/start HTTP/1.1\r\nHost: d\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n%s",
		baseURL, execID, len(startBody), startBody)

	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte("Error: failed to start exec: "+err.Error()))
		return
	}

	// Read the HTTP response header
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte("Error: failed to read exec response: "+err.Error()))
		return
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: exec start failed (status %d): %s", resp.StatusCode, string(body))))
		return
	}

	// At this point, conn is the raw bidirectional stream to the exec session.
	// Any buffered data from br needs to be handled too.
	done := make(chan struct{})

	// Goroutine: exec stdout → WebSocket
	go func() {
		defer func() { close(done) }()
		buf := make([]byte, 4096)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if werr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Goroutine: WebSocket → exec stdin
	go func() {
		for {
			msgType, msg, err := wsConn.ReadMessage()
			if err != nil {
				conn.Close()
				return
			}
			if msgType == websocket.TextMessage {
				text := string(msg)
				if strings.HasPrefix(text, "resize:") {
					// Parse resize:cols,rows
					parts := strings.Split(strings.TrimPrefix(text, "resize:"), ",")
					if len(parts) == 2 {
						cols, _ := strconv.Atoi(parts[0])
						rows, _ := strconv.Atoi(parts[1])
						if cols > 0 && rows > 0 {
							_ = s.podman.ExecResize(ctx, execID, rows, cols)
						}
					}
					continue
				}
			}
			// Write terminal input data
			if _, err := conn.Write(msg); err != nil {
				return
			}
		}
	}()

	<-done
}

// validateGitHubSignature validates the HMAC-SHA256 signature from GitHub webhooks
func validateGitHubSignature(body []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// handleWebhookSetup sets up a webhook for an app
func (s *Server) handleWebhookSetup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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

	var req struct {
		GitURL string `json:"git_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.GitURL == "" {
		errorResponse(w, http.StatusBadRequest, "git_url is required")
		return
	}

	// Generate random webhook secret (32 bytes hex)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to generate secret")
		return
	}
	secret := hex.EncodeToString(secretBytes)

	a.Deployment.GitURL = req.GitURL
	a.Deployment.WebhookSecret = secret
	a.Deployment.AutoDeploy = true
	if a.Deployment.Branch == "" {
		a.Deployment.Branch = "main"
	}

	if err := s.storage.UpdateApp(a); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build webhook URL
	webhookURL := fmt.Sprintf("/api/apps/%s/webhook", a.ID)
	if a.Domain != "" {
		webhookURL = fmt.Sprintf("https://%s/api/apps/%s/webhook", a.Domain, a.ID)
	} else if s.config != nil && s.config.Domain.Base != "" {
		webhookURL = fmt.Sprintf("https://bp.%s/api/apps/%s/webhook", s.config.Domain.Base, a.ID)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"webhook_url": webhookURL,
		"secret":      secret,
		"branch":      a.Deployment.Branch,
	})
}

// handleWebhook handles incoming webhook requests from GitHub
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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

	if a.Deployment.WebhookSecret == "" {
		errorResponse(w, http.StatusForbidden, "Webhook not configured for this app")
		return
	}

	// Read body for HMAC verification
	body, err := io.ReadAll(r.Body)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Validate signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		errorResponse(w, http.StatusForbidden, "Missing signature")
		return
	}
	if !validateGitHubSignature(body, signature, a.Deployment.WebhookSecret) {
		errorResponse(w, http.StatusForbidden, "Invalid signature")
		return
	}

	// Parse event type
	event := r.Header.Get("X-GitHub-Event")

	deliveryID := uuid.New().String()

	// Handle ping event
	if event == "ping" {
		delivery := &app.WebhookDelivery{
			ID:        deliveryID,
			AppID:     a.ID,
			Event:     "ping",
			Status:    "success",
			Message:   "Webhook configured successfully",
			CreatedAt: time.Now(),
		}
		s.storage.SaveWebhookDelivery(delivery)
		jsonResponse(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}

	// Handle push event
	if event != "push" {
		delivery := &app.WebhookDelivery{
			ID:        deliveryID,
			AppID:     a.ID,
			Event:     event,
			Status:    "skipped",
			Message:   "Unsupported event type: " + event,
			CreatedAt: time.Now(),
		}
		s.storage.SaveWebhookDelivery(delivery)
		jsonResponse(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "unsupported event"})
		return
	}

	// Parse push payload
	var payload struct {
		Ref        string `json:"ref"`
		HeadCommit struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		errorResponse(w, http.StatusBadRequest, "Failed to parse payload")
		return
	}

	// Extract branch from ref (refs/heads/main -> main)
	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
	commitHash := payload.HeadCommit.ID
	if len(commitHash) > 7 {
		commitHash = commitHash[:7]
	}
	commitMsg := payload.HeadCommit.Message
	// Truncate commit message to first line
	if idx := strings.Index(commitMsg, "\n"); idx > 0 {
		commitMsg = commitMsg[:idx]
	}

	// Check branch matches
	if branch != a.Deployment.Branch {
		delivery := &app.WebhookDelivery{
			ID:        deliveryID,
			AppID:     a.ID,
			Event:     "push",
			Branch:    branch,
			Commit:    commitHash,
			Message:   commitMsg,
			Status:    "skipped",
			Error:     fmt.Sprintf("Branch %s does not match configured branch %s", branch, a.Deployment.Branch),
			CreatedAt: time.Now(),
		}
		s.storage.SaveWebhookDelivery(delivery)
		jsonResponse(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "branch mismatch"})
		return
	}

	// Check auto_deploy is enabled
	if !a.Deployment.AutoDeploy {
		delivery := &app.WebhookDelivery{
			ID:        deliveryID,
			AppID:     a.ID,
			Event:     "push",
			Branch:    branch,
			Commit:    commitHash,
			Message:   commitMsg,
			Status:    "skipped",
			Error:     "Auto-deploy is disabled",
			CreatedAt: time.Now(),
		}
		s.storage.SaveWebhookDelivery(delivery)
		jsonResponse(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "auto_deploy disabled"})
		return
	}

	// Save delivery as deploying
	delivery := &app.WebhookDelivery{
		ID:        deliveryID,
		AppID:     a.ID,
		Event:     "push",
		Branch:    branch,
		Commit:    commitHash,
		Message:   commitMsg,
		Status:    "deploying",
		CreatedAt: time.Now(),
	}
	s.storage.SaveWebhookDelivery(delivery)

	// Start async deploy
	go s.deployFromGit(a, commitHash, commitMsg, branch, deliveryID)

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deploying"})
}

// deployFromGit clones a git repo and builds+deploys the app
func (s *Server) deployFromGit(a *app.App, commitHash, commitMsg, branch, deliveryID string) {
	ctx := context.Background()
	var buildLog strings.Builder

	log.Printf("Webhook deploy %s: branch=%s commit=%s msg=%s", a.Name, branch, commitHash, commitMsg)

	paths, _ := config.GetPaths()
	buildDir := fmt.Sprintf("%s/builds/%s", paths.Base, a.ID)
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		log.Printf("Webhook deploy %s: failed to create build dir: %v", a.Name, err)
		s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", err.Error())
		return
	}

	sourceDir := buildDir + "/source"
	// Clean up old source if exists
	os.RemoveAll(sourceDir)

	// Clone the repo
	gitURL := a.Deployment.GitURL
	log.Printf("Webhook deploy %s: cloning %s branch %s", a.Name, gitURL, branch)

	cloneCmd := fmt.Sprintf("git clone --depth 1 --branch %s %s %s", branch, gitURL, sourceDir)
	output, err := execCommand(ctx, "sh", "-c", cloneCmd)
	buildLog.WriteString("$ " + cloneCmd + "\n" + output + "\n")
	if err != nil {
		errMsg := fmt.Sprintf("Git clone failed: %v\n%s", err, output)
		log.Printf("Webhook deploy %s: %s", a.Name, errMsg)
		s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
		return
	}

	// Read .basepod config if present
	basepodCfgPath := sourceDir + "/basepod.yaml"
	if cfgData, err := os.ReadFile(basepodCfgPath); err == nil {
		var repoCfg struct {
			Dockerfile string `yaml:"dockerfile" json:"dockerfile"`
			Port       int    `yaml:"port" json:"port"`
		}
		if err := yaml.Unmarshal(cfgData, &repoCfg); err != nil {
			_ = json.Unmarshal(cfgData, &repoCfg)
		}
		if repoCfg.Dockerfile != "" && a.Deployment.Dockerfile == "" {
			a.Deployment.Dockerfile = repoCfg.Dockerfile
		}
		if repoCfg.Port > 0 && a.Ports.ContainerPort == 0 {
			a.Ports.ContainerPort = repoCfg.Port
		}
		log.Printf("Webhook deploy %s: found basepod.yaml config", a.Name)
	}

	// Check for Dockerfile
	dockerfile := "Dockerfile"
	if a.Deployment.Dockerfile != "" {
		dockerfile = a.Deployment.Dockerfile
	}
	dockerfilePath := sourceDir + "/" + dockerfile
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		log.Printf("Webhook deploy %s: no Dockerfile found, auto-detecting stack", a.Name)
		port := a.Ports.ContainerPort
		if port == 0 {
			port = 8080
		}
		generated := generateDockerfile(sourceDir, port)
		if generated == "" {
			errMsg := "No Dockerfile found and could not auto-detect project type"
			log.Printf("Webhook deploy %s: %s", a.Name, errMsg)
			s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
			return
		}
		if err := os.WriteFile(dockerfilePath, []byte(generated), 0644); err != nil {
			errMsg := fmt.Sprintf("Failed to write generated Dockerfile: %v", err)
			s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
			return
		}
		buildLog.WriteString("Auto-generated Dockerfile for detected stack\n")
	}

	// Build image
	imageName := fmt.Sprintf("basepod/%s:latest", a.Name)
	log.Printf("Webhook deploy %s: building image %s", a.Name, imageName)

	a.Status = app.StatusDeploying
	s.storage.UpdateApp(a)

	podmanPath := "podman"
	if _, err := exec.LookPath("podman"); err != nil {
		for _, p := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman"} {
			if _, err := os.Stat(p); err == nil {
				podmanPath = p
				break
			}
		}
	}

	buildCmd := fmt.Sprintf("cd %s && %s build -t %s -f %s .", sourceDir, podmanPath, imageName, dockerfile)
	output, err = execCommand(ctx, "sh", "-c", buildCmd)
	buildLog.WriteString("$ " + buildCmd + "\n" + output + "\n")
	if err != nil {
		errMsg := fmt.Sprintf("Build failed: %v\n%s", err, output)
		log.Printf("Webhook deploy %s: %s", a.Name, errMsg)
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
		return
	}

	// Remove old container
	containerName := "basepod-" + a.Name
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	// Assign host port if not set
	if a.Ports.HostPort == 0 {
		a.Ports.HostPort = assignHostPort(a.ID)
	}

	// Build volume mounts
	volumeMounts := []string{}
	for _, v := range a.Volumes {
		volumeName := fmt.Sprintf("basepod-%s-%s", a.Name, v.Name)
		volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", volumeName, v.ContainerPath))
	}

	// Create new container
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
		Memory: a.Resources.Memory * 1024 * 1024,
		CPUs:   a.Resources.CPUs,
	})
	if err != nil {
		errMsg := fmt.Sprintf("Failed to create container: %v", err)
		log.Printf("Webhook deploy %s: %s", a.Name, errMsg)
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
		return
	}

	// Start container
	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		errMsg := fmt.Sprintf("Failed to start container: %v", err)
		log.Printf("Webhook deploy %s: %s", a.Name, errMsg)
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		s.storage.UpdateWebhookDeliveryStatus(deliveryID, "failed", errMsg)
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
		Image:      imageName,
		CommitHash: commitHash,
		CommitMsg:  commitMsg,
		Branch:     branch,
		Status:     "success",
		BuildLog:   buildLog.String(),
		DeployedAt: time.Now(),
	}
	a.Deployments = append([]app.DeploymentRecord{deployRecord}, a.Deployments...)
	if len(a.Deployments) > 10 {
		a.Deployments = a.Deployments[:10]
	}

	s.storage.UpdateApp(a)

	// Configure Caddy
	if a.Domain != "" && s.caddy != nil {
		_ = s.caddy.AddRoute(caddy.Route{
			ID:        "basepod-" + a.Name,
			Domain:    a.Domain,
			Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
			EnableSSL: a.SSL.Enabled,
		})
		for _, alias := range a.Aliases {
			_ = s.caddy.AddRoute(caddy.Route{
				ID:        fmt.Sprintf("alias-%s-%s", a.ID[:8], alias),
				Domain:    alias,
				Upstream:  fmt.Sprintf("localhost:%d", a.Ports.HostPort),
				EnableSSL: a.SSL.Enabled,
			})
		}
	}

	// Clean up build directory
	os.RemoveAll(buildDir)

	s.storage.UpdateWebhookDeliveryStatus(deliveryID, "success", "")

	// Log activity and notify
	s.logActivity("webhook", "deploy", "app", a.ID, a.Name, "success", "")
	s.sendNotifications("deploy_success", a.ID, a.Name, map[string]string{
		"commit": commitHash,
		"branch": branch,
	})

	log.Printf("Webhook deploy %s: completed successfully (commit: %s)", a.Name, commitHash)
}

// handleWebhookDeliveries returns recent webhook deliveries for an app
func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	a, err := s.storage.GetApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
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

	deliveries, err := s.storage.ListWebhookDeliveries(a.ID, 20)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	if deliveries == nil {
		deliveries = []app.WebhookDelivery{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deliveries": deliveries,
	})
}

// --- Helper: resolve app by ID or name ---
func (s *Server) resolveApp(id string) (*app.App, error) {
	a, err := s.storage.GetApp(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		a, err = s.storage.GetAppByName(id)
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

// --- Activity Logging ---

func (s *Server) logActivity(actorType, action, targetType, targetID, targetName, status, details string) {
	entry := &app.ActivityLog{
		ID:         uuid.New().String(),
		ActorType:  actorType,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Status:     status,
		Details:    details,
		CreatedAt:  time.Now(),
	}
	if err := s.storage.SaveActivityLog(entry); err != nil {
		log.Printf("Failed to save activity log: %v", err)
	}
}

// --- Notification Dispatch ---

func (s *Server) sendNotifications(event, appID, appName string, details map[string]string) {
	configs, err := s.storage.ListNotificationConfigs(event, appID)
	if err != nil || len(configs) == 0 {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"event":    event,
		"app_id":   appID,
		"app_name": appName,
		"details":  details,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})

	for _, cfg := range configs {
		go s.dispatchNotification(&cfg, payload)
	}
}

func (s *Server) dispatchNotification(cfg *app.NotificationConfig, payload []byte) {
	var targetURL string
	switch cfg.Type {
	case "webhook":
		targetURL = cfg.WebhookURL
	case "slack":
		targetURL = cfg.SlackWebhookURL
	case "discord":
		targetURL = cfg.DiscordWebhook
	default:
		return
	}
	if targetURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, strings.NewReader(string(payload)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Notification dispatch failed for %s: %v", cfg.Name, err)
		return
	}
	resp.Body.Close()
}

// --- Deployment Logs Handler ---

func (s *Server) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deployID := r.PathValue("deployId")

	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	for _, d := range a.Deployments {
		if d.ID == deployID {
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"deployment_id": d.ID,
				"status":        d.Status,
				"build_log":     d.BuildLog,
				"deployed_at":   d.DeployedAt,
			})
			return
		}
	}

	errorResponse(w, http.StatusNotFound, "Deployment not found")
}

// --- Rollback Handler ---

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	var req struct {
		DeploymentID string `json:"deployment_id"` // Optional: specific deployment to rollback to
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Find the deployment to rollback to
	if len(a.Deployments) < 2 && req.DeploymentID == "" {
		errorResponse(w, http.StatusBadRequest, "No previous deployment to rollback to")
		return
	}

	var targetDeploy *app.DeploymentRecord
	if req.DeploymentID != "" {
		for i := range a.Deployments {
			if a.Deployments[i].ID == req.DeploymentID {
				targetDeploy = &a.Deployments[i]
				break
			}
		}
		if targetDeploy == nil {
			errorResponse(w, http.StatusNotFound, "Deployment not found")
			return
		}
	} else {
		// Rollback to previous deployment (index 1)
		targetDeploy = &a.Deployments[1]
	}

	if targetDeploy.Image == "" {
		errorResponse(w, http.StatusBadRequest, "Previous deployment has no image to rollback to")
		return
	}

	ctx := r.Context()
	containerName := "basepod-" + a.Name

	// Stop and remove current container
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}
	_ = s.podman.StopContainer(ctx, containerName, 10)
	_ = s.podman.RemoveContainer(ctx, containerName, true)

	// Build volume mounts
	volumeMounts := []string{}
	for _, v := range a.Volumes {
		volumeName := fmt.Sprintf("basepod-%s-%s", a.Name, v.Name)
		volumeMounts = append(volumeMounts, fmt.Sprintf("%s:%s", volumeName, v.ContainerPath))
	}

	// Create new container from the rollback image
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:     containerName,
		Image:    targetDeploy.Image,
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
		Memory: a.Resources.Memory,
		CPUs:   a.Resources.CPUs,
	})
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create container: "+err.Error())
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	if err := s.podman.StartContainer(ctx, containerID); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to start container: "+err.Error())
		a.Status = app.StatusFailed
		s.storage.UpdateApp(a)
		return
	}

	// Update app
	a.ContainerID = containerID
	a.Image = targetDeploy.Image
	a.Status = app.StatusRunning
	a.UpdatedAt = time.Now()

	// Add rollback deployment record
	rollbackRecord := app.DeploymentRecord{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		Image:      targetDeploy.Image,
		CommitHash: targetDeploy.CommitHash,
		CommitMsg:  "Rollback to " + targetDeploy.ID,
		Branch:     targetDeploy.Branch,
		Status:     "success",
		DeployedAt: time.Now(),
	}
	a.Deployments = append([]app.DeploymentRecord{rollbackRecord}, a.Deployments...)
	if len(a.Deployments) > 10 {
		a.Deployments = a.Deployments[:10]
	}

	s.storage.UpdateApp(a)

	s.logActivity("user", "rollback", "app", a.ID, a.Name, "success", "")
	s.sendNotifications("deploy_success", a.ID, a.Name, map[string]string{
		"action": "rollback",
		"image":  targetDeploy.Image,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":    "Rollback successful",
		"deployment": rollbackRecord,
	})
}

// --- Cron Job Handlers ---

func (s *Server) handleListCronJobs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	jobs, err := s.storage.ListCronJobs(a.ID)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if jobs == nil {
		jobs = []app.CronJob{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"jobs": jobs})
}

func (s *Server) handleCreateCronJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	var req struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Name == "" || req.Schedule == "" || req.Command == "" {
		errorResponse(w, http.StatusBadRequest, "name, schedule, and command are required")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now()
	job := &app.CronJob{
		ID:        uuid.New().String(),
		AppID:     a.ID,
		Name:      req.Name,
		Schedule:  req.Schedule,
		Command:   req.Command,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.storage.CreateCronJob(job); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "cron_create", "app", a.ID, a.Name, "success", job.Name)
	jsonResponse(w, http.StatusCreated, job)
}

func (s *Server) handleUpdateCronJob(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	jobID := r.PathValue("jobId")

	a, err := s.resolveApp(appID)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	job, err := s.storage.GetCronJob(jobID)
	if err != nil || job == nil || job.AppID != a.ID {
		errorResponse(w, http.StatusNotFound, "Cron job not found")
		return
	}

	var req struct {
		Name     *string `json:"name"`
		Schedule *string `json:"schedule"`
		Command  *string `json:"command"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != nil {
		job.Name = *req.Name
	}
	if req.Schedule != nil {
		job.Schedule = *req.Schedule
	}
	if req.Command != nil {
		job.Command = *req.Command
	}
	if req.Enabled != nil {
		job.Enabled = *req.Enabled
	}

	if err := s.storage.UpdateCronJob(job); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, job)
}

func (s *Server) handleDeleteCronJob(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	jobID := r.PathValue("jobId")

	a, err := s.resolveApp(appID)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	job, err := s.storage.GetCronJob(jobID)
	if err != nil || job == nil || job.AppID != a.ID {
		errorResponse(w, http.StatusNotFound, "Cron job not found")
		return
	}

	if err := s.storage.DeleteCronJob(jobID); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "cron_delete", "app", a.ID, a.Name, "success", job.Name)
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Cron job deleted"})
}

func (s *Server) handleRunCronJob(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	jobID := r.PathValue("jobId")

	a, err := s.resolveApp(appID)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	job, err := s.storage.GetCronJob(jobID)
	if err != nil || job == nil || job.AppID != a.ID {
		errorResponse(w, http.StatusNotFound, "Cron job not found")
		return
	}

	if a.ContainerID == "" {
		errorResponse(w, http.StatusBadRequest, "App has no running container")
		return
	}

	// Execute the command in the container
	ctx := context.Background()
	execID, err := s.podman.ExecCreateDetached(ctx, a.ContainerID, []string{"/bin/sh", "-c", job.Command})
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create exec: "+err.Error())
		return
	}

	// Record execution
	cronExec := &app.CronExecution{
		ID:        uuid.New().String(),
		CronJobID: job.ID,
		StartedAt: time.Now(),
		Status:    "running",
	}
	s.storage.CreateCronExecution(cronExec)

	// Start exec and capture output asynchronously
	go func() {
		output, cmdErr := s.podman.ExecStart(ctx, execID)
		now := time.Now()
		cronExec.EndedAt = &now
		cronExec.Output = output
		if cmdErr != nil {
			cronExec.Status = "failed"
			cronExec.ExitCode = 1
		} else {
			cronExec.Status = "success"
			cronExec.ExitCode = 0
		}
		s.storage.UpdateCronExecution(cronExec)

		// Update job last run info
		job.LastRun = &now
		job.LastStatus = cronExec.Status
		if cronExec.Status == "failed" {
			job.LastError = output
		} else {
			job.LastError = ""
		}
		s.storage.UpdateCronJob(job)
	}()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":      "Cron job started",
		"execution_id": cronExec.ID,
	})
}

func (s *Server) handleListCronExecutions(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	jobID := r.PathValue("jobId")

	a, err := s.resolveApp(appID)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	job, err := s.storage.GetCronJob(jobID)
	if err != nil || job == nil || job.AppID != a.ID {
		errorResponse(w, http.StatusNotFound, "Cron job not found")
		return
	}

	execs, err := s.storage.ListCronExecutions(jobID, 50)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if execs == nil {
		execs = []app.CronExecution{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"executions": execs})
}

// --- Activity Log Handlers ---

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	targetID := r.URL.Query().Get("target_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	logs, err := s.storage.ListActivityLogs(targetID, action, limit)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []app.ActivityLog{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"activities": logs})
}

func (s *Server) handleListAppActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil || a == nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	logs, err := s.storage.ListActivityLogs(a.ID, "", 50)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []app.ActivityLog{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"activities": logs})
}

// --- Notification Handlers ---

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	configs, err := s.storage.ListNotificationConfigs("", "")
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if configs == nil {
		configs = []app.NotificationConfig{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"notifications": configs})
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) {
	var req app.NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Name == "" || req.Type == "" {
		errorResponse(w, http.StatusBadRequest, "name and type are required")
		return
	}
	if len(req.Events) == 0 {
		errorResponse(w, http.StatusBadRequest, "at least one event is required")
		return
	}

	now := time.Now()
	req.ID = uuid.New().String()
	req.Enabled = true
	if req.Scope == "" {
		req.Scope = "global"
	}
	req.CreatedAt = now
	req.UpdatedAt = now

	if err := s.storage.CreateNotificationConfig(&req); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "notification_create", "config", req.ID, req.Name, "success", "")
	jsonResponse(w, http.StatusCreated, req)
}

func (s *Server) handleUpdateNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.storage.GetNotificationConfig(id)
	if err != nil || existing == nil {
		errorResponse(w, http.StatusNotFound, "Notification config not found")
		return
	}

	var req app.NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Type != "" {
		existing.Type = req.Type
	}
	if req.WebhookURL != "" {
		existing.WebhookURL = req.WebhookURL
	}
	if req.SlackWebhookURL != "" {
		existing.SlackWebhookURL = req.SlackWebhookURL
	}
	if req.DiscordWebhook != "" {
		existing.DiscordWebhook = req.DiscordWebhook
	}
	if len(req.Events) > 0 {
		existing.Events = req.Events
	}
	existing.Enabled = req.Enabled

	if err := s.storage.UpdateNotificationConfig(existing); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.storage.GetNotificationConfig(id)
	if err != nil || existing == nil {
		errorResponse(w, http.StatusNotFound, "Notification config not found")
		return
	}

	if err := s.storage.DeleteNotificationConfig(id); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "notification_delete", "config", id, existing.Name, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Notification config deleted"})
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, err := s.storage.GetNotificationConfig(id)
	if err != nil || cfg == nil {
		errorResponse(w, http.StatusNotFound, "Notification config not found")
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"event":    "test",
		"app_name": "test-app",
		"details":  map[string]string{"message": "This is a test notification from Basepod"},
		"time":     time.Now().UTC().Format(time.RFC3339),
	})

	s.dispatchNotification(cfg, payload)
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Test notification sent"})
}

// --- Deploy Token Handlers ---

func (s *Server) handleListDeployTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.storage.ListDeployTokens()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tokens == nil {
		tokens = []app.DeployToken{}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{"tokens": tokens})
}

func (s *Server) handleCreateDeployToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Name == "" {
		errorResponse(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"deploy:*"}
	}

	// Generate a random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)
	prefix := rawToken[:8]

	// Hash the token for storage
	h := sha256.New()
	h.Write([]byte(rawToken))
	tokenHash := hex.EncodeToString(h.Sum(nil))

	now := time.Now()
	token := &app.DeployToken{
		ID:        uuid.New().String(),
		Name:      req.Name,
		TokenHash: tokenHash,
		Prefix:    prefix,
		Scopes:    req.Scopes,
		CreatedAt: now,
	}

	if err := s.storage.CreateDeployToken(token); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "token_create", "config", token.ID, req.Name, "success", "")

	// Return the raw token only on creation
	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":      token.ID,
		"name":    token.Name,
		"token":   rawToken,
		"prefix":  prefix,
		"scopes":  token.Scopes,
		"message": "Save this token - it won't be shown again",
	})
}

func (s *Server) handleDeleteDeployToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.storage.DeleteDeployToken(id); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logActivity("user", "token_delete", "config", id, "", "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"message": "Deploy token deleted"})
}

// --- Status Badge ---

func (s *Server) handleStatusBadge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, _ := s.resolveApp(id)

	status := "unknown"
	color := "#9e9e9e"
	if a != nil {
		status = string(a.Status)
		switch a.Status {
		case app.StatusRunning:
			color = "#4caf50"
		case app.StatusFailed:
			color = "#f44336"
		case app.StatusBuilding, app.StatusDeploying:
			color = "#2196f3"
		case app.StatusStopped:
			color = "#ff9800"
		}
	}

	badge := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="20">
  <rect width="60" height="20" fill="#555" rx="3"/>
  <rect x="60" width="60" height="20" fill="%s" rx="3"/>
  <rect width="120" height="20" fill="url(#g)" rx="3"/>
  <g fill="#fff" font-family="Verdana,sans-serif" font-size="11">
    <text x="6" y="14">basepod</text>
    <text x="66" y="14">%s</text>
  </g>
</svg>`, color, status)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(badge))
}

// --- Metrics ---

func (s *Server) handleAppMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	// Parse period: 1h, 24h, 7d (default 1h)
	period := r.URL.Query().Get("period")
	var since time.Time
	switch period {
	case "24h":
		since = time.Now().Add(-24 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	default:
		since = time.Now().Add(-1 * time.Hour)
	}

	metrics, err := s.storage.ListAppMetrics(a.ID, since, 500)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to fetch metrics")
		return
	}

	// Also get current live stats if container is running
	var current *podman.ContainerStatsResult
	if a.ContainerID != "" && a.Status == app.StatusRunning {
		current, _ = s.podman.ContainerStats(r.Context(), a.ContainerID)
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"app_id":  a.ID,
		"period":  period,
		"metrics": metrics,
		"current": current,
	})
}

// runMetricsCollector periodically collects container stats for all running apps
func (s *Server) runMetricsCollector() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Clean old metrics on startup
	s.storage.CleanOldMetrics(time.Now().Add(-7 * 24 * time.Hour))

	for {
		select {
		case <-ticker.C:
			s.collectMetrics()
		case <-s.healthStop:
			return
		}
	}
}

func (s *Server) collectMetrics() {
	apps, _ := s.storage.ListApps()
	ctx := context.Background()

	for _, a := range apps {
		if a.ContainerID == "" || a.Status != app.StatusRunning {
			continue
		}

		stats, err := s.podman.ContainerStats(ctx, a.ContainerID)
		if err != nil {
			continue
		}

		metric := &app.AppMetric{
			AppID:      a.ID,
			CPUPercent: stats.CPUPercent,
			MemUsage:   stats.MemUsage,
			MemLimit:   stats.MemLimit,
			NetInput:   stats.NetInput,
			NetOutput:  stats.NetOutput,
			RecordedAt: time.Now(),
		}
		s.storage.SaveAppMetric(metric)
	}

	// Clean metrics older than 7 days periodically
	s.storage.CleanOldMetrics(time.Now().Add(-7 * 24 * time.Hour))
}

// --- Database Provisioning ---

// handleLinkDatabase links a database app to another app by injecting connection env vars
func (s *Server) handleLinkDatabase(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	dbID := r.PathValue("dbId")

	a, err := s.resolveApp(appID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	dbApp, err := s.resolveApp(dbID)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "Database app not found")
		return
	}

	// Generate connection string based on database type
	connStr := ""
	dbHost := fmt.Sprintf("basepod-%s", dbApp.Name)
	dbPort := dbApp.Ports.ContainerPort

	if dbApp.Env != nil {
		switch {
		case dbApp.Env["POSTGRES_PASSWORD"] != "":
			user := dbApp.Env["POSTGRES_USER"]
			if user == "" {
				user = "postgres"
			}
			db := dbApp.Env["POSTGRES_DB"]
			if db == "" {
				db = user
			}
			connStr = fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=disable", user, dbApp.Env["POSTGRES_PASSWORD"], dbHost, dbPort, db)
		case dbApp.Env["MYSQL_ROOT_PASSWORD"] != "":
			user := dbApp.Env["MYSQL_USER"]
			pass := dbApp.Env["MYSQL_PASSWORD"]
			if user == "" {
				user = "root"
				pass = dbApp.Env["MYSQL_ROOT_PASSWORD"]
			}
			db := dbApp.Env["MYSQL_DATABASE"]
			if db == "" {
				db = user
			}
			connStr = fmt.Sprintf("mysql://%s:%s@%s:%d/%s", user, pass, dbHost, dbPort, db)
		case dbApp.Env["REDIS_PASSWORD"] != "":
			connStr = fmt.Sprintf("redis://:%s@%s:%d", dbApp.Env["REDIS_PASSWORD"], dbHost, dbPort)
		default:
			connStr = fmt.Sprintf("%s:%d", dbHost, dbPort)
		}
	}

	if connStr == "" {
		errorResponse(w, http.StatusBadRequest, "Could not generate connection string for this database")
		return
	}

	// Inject DATABASE_URL into the app's env
	if a.Env == nil {
		a.Env = make(map[string]string)
	}
	a.Env["DATABASE_URL"] = connStr
	a.UpdatedAt = time.Now()

	if err := s.storage.UpdateApp(a); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to update app: "+err.Error())
		return
	}

	s.logActivity("user", "link_database", "app", a.ID, a.Name, "success", fmt.Sprintf("linked to %s", dbApp.Name))

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"database_url": connStr,
		"linked_db":    dbApp.Name,
		"message":      "DATABASE_URL has been set. Restart the app for changes to take effect.",
	})
}

// handleConnectionInfo returns connection details for a database app
func (s *Server) handleConnectionInfo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.resolveApp(id)
	if err != nil {
		errorResponse(w, http.StatusNotFound, "App not found")
		return
	}

	info := map[string]interface{}{
		"host":           fmt.Sprintf("basepod-%s", a.Name),
		"port":           a.Ports.ContainerPort,
		"internal_host":  fmt.Sprintf("basepod-%s:%d", a.Name, a.Ports.ContainerPort),
	}

	if a.Env != nil {
		switch {
		case a.Env["POSTGRES_PASSWORD"] != "":
			user := a.Env["POSTGRES_USER"]
			if user == "" {
				user = "postgres"
			}
			db := a.Env["POSTGRES_DB"]
			if db == "" {
				db = user
			}
			info["type"] = "postgresql"
			info["user"] = user
			info["password"] = a.Env["POSTGRES_PASSWORD"]
			info["database"] = db
			info["connection_url"] = fmt.Sprintf("postgresql://%s:%s@basepod-%s:%d/%s?sslmode=disable", user, a.Env["POSTGRES_PASSWORD"], a.Name, a.Ports.ContainerPort, db)
		case a.Env["MYSQL_ROOT_PASSWORD"] != "":
			user := a.Env["MYSQL_USER"]
			pass := a.Env["MYSQL_PASSWORD"]
			if user == "" {
				user = "root"
				pass = a.Env["MYSQL_ROOT_PASSWORD"]
			}
			db := a.Env["MYSQL_DATABASE"]
			if db == "" {
				db = user
			}
			info["type"] = "mysql"
			info["user"] = user
			info["password"] = pass
			info["database"] = db
			info["connection_url"] = fmt.Sprintf("mysql://%s:%s@basepod-%s:%d/%s", user, pass, a.Name, a.Ports.ContainerPort, db)
		case a.Env["REDIS_PASSWORD"] != "":
			info["type"] = "redis"
			info["password"] = a.Env["REDIS_PASSWORD"]
			info["connection_url"] = fmt.Sprintf("redis://:%s@basepod-%s:%d", a.Env["REDIS_PASSWORD"], a.Name, a.Ports.ContainerPort)
		}
	}

	jsonResponse(w, http.StatusOK, info)
}

// --- User Management ---

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.storage.ListUsers()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to list users")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"users": users})
}

func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if req.Email == "" {
		errorResponse(w, http.StatusBadRequest, "Email is required")
		return
	}
	if req.Role == "" {
		req.Role = "viewer"
	}
	if req.Role != "admin" && req.Role != "deployer" && req.Role != "viewer" {
		errorResponse(w, http.StatusBadRequest, "Role must be admin, deployer, or viewer")
		return
	}

	// Check if user already exists
	existing, _ := s.storage.GetUserByEmail(req.Email)
	if existing != nil {
		errorResponse(w, http.StatusConflict, "User with this email already exists")
		return
	}

	// Generate invite token
	inviteToken := generateRandomString(32)

	user := &app.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		PasswordHash: "", // Will be set when invite is accepted
		Role:         req.Role,
		InviteToken:  inviteToken,
		CreatedAt:    time.Now(),
	}

	if err := s.storage.CreateUser(user); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create user: "+err.Error())
		return
	}

	s.logActivity("user", "invite_user", "user", user.ID, req.Email, "success", fmt.Sprintf("role: %s", req.Role))

	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"user":         user,
		"invite_token": inviteToken,
		"invite_url":   fmt.Sprintf("/setup?invite=%s", inviteToken),
	})
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InviteToken string `json:"invite_token"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if req.InviteToken == "" || req.Password == "" {
		errorResponse(w, http.StatusBadRequest, "Invite token and password are required")
		return
	}

	if len(req.Password) < 8 {
		errorResponse(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	user, err := s.storage.GetUserByInviteToken(req.InviteToken)
	if err != nil || user == nil {
		errorResponse(w, http.StatusNotFound, "Invalid or expired invite token")
		return
	}

	// Set password and clear invite token
	passwordHash := auth.HashPassword(req.Password)
	s.storage.UpdateUserPassword(user.ID, passwordHash)
	s.storage.ClearInviteToken(user.ID)

	// Create session
	session, err := s.auth.CreateUserSession(user.ID, user.Email, user.Role)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "basepod_token",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"token":     session.Token,
		"expiresAt": session.ExpiresAt,
		"user": map[string]string{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if req.Role != "admin" && req.Role != "deployer" && req.Role != "viewer" {
		errorResponse(w, http.StatusBadRequest, "Role must be admin, deployer, or viewer")
		return
	}

	if err := s.storage.UpdateUserRole(userID, req.Role); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to update role")
		return
	}

	s.logActivity("user", "update_role", "user", userID, "", "success", fmt.Sprintf("role: %s", req.Role))
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")

	user, err := s.storage.GetUserByID(userID)
	if err != nil || user == nil {
		errorResponse(w, http.StatusNotFound, "User not found")
		return
	}

	if err := s.storage.DeleteUser(userID); err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	s.logActivity("user", "delete_user", "user", userID, user.Email, "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- AI Deploy Assistant ---

func (s *Server) handleAIAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoURL string `json:"repo_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if req.RepoURL == "" {
		errorResponse(w, http.StatusBadRequest, "repo_url is required")
		return
	}

	ctx := r.Context()

	// Clone repo to temp directory
	tmpDir, err := os.MkdirTemp("", "basepod-analyze-*")
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "Failed to create temp dir")
		return
	}
	defer os.RemoveAll(tmpDir)

	cloneCmd := fmt.Sprintf("git clone --depth 1 %s %s", req.RepoURL, tmpDir+"/repo")
	if output, err := execCommand(ctx, "sh", "-c", cloneCmd); err != nil {
		errorResponse(w, http.StatusBadRequest, fmt.Sprintf("Failed to clone repo: %v\n%s", err, output))
		return
	}

	repoDir := tmpDir + "/repo"

	// Scan repo structure
	files := scanRepoStructure(repoDir)

	// Detect stack using heuristics
	stack := detectStack(repoDir)

	// Build analysis result
	result := map[string]interface{}{
		"repo_url":   req.RepoURL,
		"stack":      stack,
		"files":      files,
		"has_docker": fileExists(repoDir + "/Dockerfile"),
	}

	// Generate config suggestion
	suggestion := map[string]interface{}{
		"port": 8080,
		"env":  map[string]string{},
	}

	switch stack {
	case "nodejs":
		suggestion["port"] = 3000
		suggestion["env"] = map[string]string{"NODE_ENV": "production"}
	case "go":
		suggestion["port"] = 8080
	case "python":
		suggestion["port"] = 8000
		suggestion["env"] = map[string]string{"PYTHONUNBUFFERED": "1"}
	case "ruby":
		suggestion["port"] = 3000
		suggestion["env"] = map[string]string{"RAILS_ENV": "production"}
	case "rust":
		suggestion["port"] = 8080
	}

	// If no Dockerfile exists, generate one
	if !fileExists(repoDir + "/Dockerfile") {
		dockerfile := generateDockerfile(repoDir, suggestion["port"].(int))
		if dockerfile != "" {
			suggestion["dockerfile"] = dockerfile
		}
	} else {
		// Read existing Dockerfile
		if content, err := os.ReadFile(repoDir + "/Dockerfile"); err == nil {
			result["dockerfile_content"] = string(content)
		}
	}

	result["suggestion"] = suggestion

	// Try AI analysis via MLX if available
	svc := mlx.GetService()
	status := svc.GetStatus()
	if status.Running && status.ActiveModel != "" {
		aiAnalysis := s.analyzeWithLLM(ctx, status, files, stack)
		if aiAnalysis != "" {
			result["ai_analysis"] = aiAnalysis
		}
	}

	jsonResponse(w, http.StatusOK, result)
}

func scanRepoStructure(dir string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip hidden dirs and common non-essential dirs
		name := info.Name()
		if info.IsDir() && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" || name == "target") {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(dir, path)
			files = append(files, relPath)
		}
		return nil
	})
	// Limit to first 100 files
	if len(files) > 100 {
		files = files[:100]
	}
	return files
}

func detectStack(dir string) string {
	if fileExists(dir + "/package.json") {
		return "nodejs"
	}
	if fileExists(dir + "/go.mod") {
		return "go"
	}
	if fileExists(dir + "/requirements.txt") || fileExists(dir + "/pyproject.toml") || fileExists(dir + "/setup.py") {
		return "python"
	}
	if fileExists(dir + "/Gemfile") {
		return "ruby"
	}
	if fileExists(dir + "/Cargo.toml") {
		return "rust"
	}
	if fileExists(dir + "/pom.xml") || fileExists(dir + "/build.gradle") {
		return "java"
	}
	if fileExists(dir + "/composer.json") {
		return "php"
	}
	return "unknown"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Server) analyzeWithLLM(ctx context.Context, status mlx.Status, files []string, stack string) string {
	// Build prompt for LLM
	fileList := strings.Join(files, "\n")
	if len(fileList) > 2000 {
		fileList = fileList[:2000] + "\n..."
	}

	prompt := fmt.Sprintf(`Analyze this project structure and give a brief deployment recommendation. The detected stack is: %s

Files:
%s

In 2-3 sentences, suggest:
1. The best way to deploy this app
2. Any environment variables needed
3. The likely port it runs on`, stack, fileList)

	// Call MLX endpoint
	payload := map[string]interface{}{
		"model": status.ActiveModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 256,
	}

	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("http://localhost:%d/v1/chat/completions", status.Port)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content
	}
	return ""
}
