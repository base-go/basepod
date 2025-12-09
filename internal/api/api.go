// Package api provides the REST API for deployer.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/podman"
	"github.com/deployer/deployer/internal/storage"
	"github.com/google/uuid"
)

// Server represents the API server
type Server struct {
	storage *storage.Storage
	podman  podman.Client
	router  *http.ServeMux
}

// NewServer creates a new API server
func NewServer(store *storage.Storage, pm podman.Client) *Server {
	s := &Server{
		storage: store,
		podman:  pm,
		router:  http.NewServeMux(),
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() {
	// Health check
	s.router.HandleFunc("GET /health", s.handleHealth)
	s.router.HandleFunc("GET /api/health", s.handleHealth)

	// Apps
	s.router.HandleFunc("GET /api/apps", s.handleListApps)
	s.router.HandleFunc("POST /api/apps", s.handleCreateApp)
	s.router.HandleFunc("GET /api/apps/{id}", s.handleGetApp)
	s.router.HandleFunc("PUT /api/apps/{id}", s.handleUpdateApp)
	s.router.HandleFunc("DELETE /api/apps/{id}", s.handleDeleteApp)

	// App actions
	s.router.HandleFunc("POST /api/apps/{id}/start", s.handleStartApp)
	s.router.HandleFunc("POST /api/apps/{id}/stop", s.handleStopApp)
	s.router.HandleFunc("POST /api/apps/{id}/restart", s.handleRestartApp)
	s.router.HandleFunc("POST /api/apps/{id}/deploy", s.handleDeployApp)
	s.router.HandleFunc("GET /api/apps/{id}/logs", s.handleGetAppLogs)

	// System
	s.router.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	s.router.HandleFunc("GET /api/containers", s.handleListContainers)
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

	s.router.ServeHTTP(w, r)
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

	// Check if app already exists
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

	newApp := &app.App{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Domain:    req.Domain,
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

	// Remove old container if exists
	if a.ContainerID != "" {
		_ = s.podman.StopContainer(ctx, a.ContainerID, 10)
		_ = s.podman.RemoveContainer(ctx, a.ContainerID, true)
	}

	// Create new container
	containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
		Name:    "deployer-" + a.Name,
		Image:   image,
		Env:     a.Env,
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
	}

	// Get image count
	images, err := s.podman.ListImages(ctx)
	if err == nil {
		info["images"] = len(images)
	}

	jsonResponse(w, http.StatusOK, info)
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
