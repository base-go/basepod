// Package proxy provides the main proxy manager that coordinates routing.
package proxy

import (
	"context"
	"fmt"
	"sync"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/config"
	"github.com/deployer/deployer/internal/storage"
)

// Manager manages the reverse proxy configuration
type Manager struct {
	caddy   *CaddyManager
	storage *storage.Storage
	config  *config.Config
	mu      sync.RWMutex
}

// NewManager creates a new proxy manager
func NewManager(store *storage.Storage, cfg *config.Config) (*Manager, error) {
	caddy, err := NewCaddyManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create caddy manager: %w", err)
	}

	return &Manager{
		caddy:   caddy,
		storage: store,
		config:  cfg,
	}, nil
}

// SyncRoutes syncs all app routes to Caddy
func (m *Manager) SyncRoutes(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	apps, err := m.storage.ListApps()
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	routes := make([]AppRoute, 0)
	for _, a := range apps {
		if a.Domain == "" || a.Status != app.StatusRunning {
			continue
		}

		// Determine upstream based on container
		upstream := fmt.Sprintf("localhost:%d", a.Ports.HostPort)
		if a.Ports.HostPort == 0 {
			// Default to container port + app-specific offset
			upstream = fmt.Sprintf("localhost:%d", 10000+a.Ports.ContainerPort)
		}

		routes = append(routes, AppRoute{
			App:      &a,
			Upstream: upstream,
		})
	}

	// Generate and apply configuration
	if err := m.caddy.GenerateCaddyfile(routes, m.config.Domain.Root, m.config.Domain.Email); err != nil {
		return fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	if err := m.caddy.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload Caddy: %w", err)
	}

	return nil
}

// AddAppRoute adds routing for a specific app
func (m *Manager) AddAppRoute(ctx context.Context, a *app.App, containerPort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if a.Domain == "" {
		return fmt.Errorf("app has no domain configured")
	}

	upstream := fmt.Sprintf("localhost:%d", containerPort)
	return m.caddy.AddRoute(ctx, a, upstream)
}

// RemoveAppRoute removes routing for a specific app
func (m *Manager) RemoveAppRoute(ctx context.Context, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.caddy.RemoveRoute(ctx, domain)
}

// IsReady checks if the proxy is ready
func (m *Manager) IsReady(ctx context.Context) bool {
	return m.caddy.IsRunning(ctx)
}

// GetCaddyManager returns the underlying Caddy manager
func (m *Manager) GetCaddyManager() *CaddyManager {
	return m.caddy
}
