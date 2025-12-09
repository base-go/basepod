// Package api provides the REST API for deployer.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"time"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/caddy"
	"github.com/deployer/deployer/internal/podman"
	"github.com/deployer/deployer/internal/storage"
	"github.com/deployer/deployer/internal/templates"
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
	storage *storage.Storage
	podman  podman.Client
	caddy   *caddy.Client
	router  *http.ServeMux
}

// NewServer creates a new API server
func NewServer(store *storage.Storage, pm podman.Client, caddyClient *caddy.Client) *Server {
	s := &Server{
		storage: store,
		podman:  pm,
		caddy:   caddyClient,
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

	// Templates
	s.router.HandleFunc("GET /api/templates", s.handleListTemplates)
	s.router.HandleFunc("POST /api/templates/{id}/deploy", s.handleDeployTemplate)
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

	// Auto-assign .pod domain if not specified
	domain := req.Domain
	if domain == "" {
		domain = req.Name + ".pod"
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

	// Check if app already exists
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

	// Auto-assign .pod domain if not specified
	domain := req.Domain
	if domain == "" {
		domain = name + ".pod"
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
