// Package storage provides data persistence for deployer using SQLite.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/deployer/deployer/internal/app"
	"github.com/deployer/deployer/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

// Storage provides data persistence operations
type Storage struct {
	db *sql.DB
}

// New creates a new storage instance
func New() (*Storage, error) {
	paths, err := config.GetPaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get paths: %w", err)
	}

	dbPath := filepath.Join(paths.Data, "deployer.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return s, nil
}

// Close closes the database connection
func (s *Storage) Close() error {
	return s.db.Close()
}

// migrate runs database migrations
func (s *Storage) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS apps (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			domain TEXT UNIQUE,
			container_id TEXT,
			image TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			env TEXT,
			ports TEXT,
			volumes TEXT,
			resources TEXT,
			deployment TEXT,
			ssl TEXT,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_apps_name ON apps(name)`,
		`CREATE INDEX IF NOT EXISTS idx_apps_domain ON apps(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_apps_status ON apps(status)`,
		`CREATE TABLE IF NOT EXISTS deployments (
			id TEXT PRIMARY KEY,
			app_id TEXT NOT NULL,
			status TEXT NOT NULL,
			source TEXT,
			image TEXT,
			logs TEXT,
			started_at DATETIME NOT NULL,
			finished_at DATETIME,
			FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_deployments_app_id ON deployments(app_id)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS image_tags (
			image TEXT PRIMARY KEY,
			tags TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		// Fix empty domain strings to NULL (for database apps)
		`UPDATE apps SET domain = NULL WHERE domain = ''`,
		// Add type column for MLX support
		`ALTER TABLE apps ADD COLUMN type TEXT DEFAULT 'container'`,
		// Add mlx config column for MLX apps
		`ALTER TABLE apps ADD COLUMN mlx TEXT`,
	}

	for _, migration := range migrations {
		_, err := s.db.Exec(migration)
		// Ignore "duplicate column" errors for ALTER TABLE migrations
		if err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// isDuplicateColumnError checks if the error is a duplicate column error (safe to ignore)
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "duplicate column") || contains(errStr, "already exists")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// CreateApp creates a new app in the database
func (s *Storage) CreateApp(a *app.App) error {
	envJSON, _ := json.Marshal(a.Env)
	portsJSON, _ := json.Marshal(a.Ports)
	volumesJSON, _ := json.Marshal(a.Volumes)
	resourcesJSON, _ := json.Marshal(a.Resources)
	deploymentJSON, _ := json.Marshal(a.Deployment)
	sslJSON, _ := json.Marshal(a.SSL)
	mlxJSON, _ := json.Marshal(a.MLX)

	// Convert empty domain to NULL (for database apps without domains)
	var domain interface{} = a.Domain
	if a.Domain == "" {
		domain = nil
	}

	// Default type to container if not set
	appType := string(a.Type)
	if appType == "" {
		appType = "container"
	}

	_, err := s.db.Exec(`
		INSERT INTO apps (id, name, domain, container_id, image, status, env, ports, volumes, resources, deployment, ssl, type, mlx, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, a.ID, a.Name, domain, a.ContainerID, a.Image, a.Status,
		string(envJSON), string(portsJSON), string(volumesJSON),
		string(resourcesJSON), string(deploymentJSON), string(sslJSON),
		appType, string(mlxJSON),
		a.CreatedAt, a.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	return nil
}

// GetApp retrieves an app by ID
func (s *Storage) GetApp(id string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, container_id, image, status, env, ports, volumes, resources, deployment, ssl, type, mlx, created_at, updated_at
		FROM apps WHERE id = ?
	`, id)

	return s.scanApp(row)
}

// GetAppByName retrieves an app by name
func (s *Storage) GetAppByName(name string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, container_id, image, status, env, ports, volumes, resources, deployment, ssl, type, mlx, created_at, updated_at
		FROM apps WHERE name = ?
	`, name)

	return s.scanApp(row)
}

// GetAppByDomain retrieves an app by domain
func (s *Storage) GetAppByDomain(domain string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, container_id, image, status, env, ports, volumes, resources, deployment, ssl, type, mlx, created_at, updated_at
		FROM apps WHERE domain = ?
	`, domain)

	return s.scanApp(row)
}

// scanApp scans a row into an App struct
func (s *Storage) scanApp(row *sql.Row) (*app.App, error) {
	var a app.App
	var envJSON, portsJSON, volumesJSON, resourcesJSON, deploymentJSON, sslJSON string
	var domain, containerID, image, appType, mlxJSON sql.NullString

	err := row.Scan(
		&a.ID, &a.Name, &domain, &containerID, &image, &a.Status,
		&envJSON, &portsJSON, &volumesJSON, &resourcesJSON, &deploymentJSON, &sslJSON,
		&appType, &mlxJSON,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan app: %w", err)
	}

	a.Domain = domain.String
	a.ContainerID = containerID.String
	a.Image = image.String
	a.Type = app.AppType(appType.String)
	if a.Type == "" {
		a.Type = app.AppTypeContainer
	}

	json.Unmarshal([]byte(envJSON), &a.Env)
	json.Unmarshal([]byte(portsJSON), &a.Ports)
	json.Unmarshal([]byte(volumesJSON), &a.Volumes)
	json.Unmarshal([]byte(resourcesJSON), &a.Resources)
	json.Unmarshal([]byte(deploymentJSON), &a.Deployment)
	json.Unmarshal([]byte(sslJSON), &a.SSL)
	if mlxJSON.Valid && mlxJSON.String != "" {
		json.Unmarshal([]byte(mlxJSON.String), &a.MLX)
	}

	return &a, nil
}

// ListApps retrieves all apps
func (s *Storage) ListApps() ([]app.App, error) {
	rows, err := s.db.Query(`
		SELECT id, name, domain, container_id, image, status, env, ports, volumes, resources, deployment, ssl, type, mlx, created_at, updated_at
		FROM apps ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}
	defer rows.Close()

	var apps []app.App
	for rows.Next() {
		var a app.App
		var envJSON, portsJSON, volumesJSON, resourcesJSON, deploymentJSON, sslJSON string
		var domain, containerID, image, appType, mlxJSON sql.NullString

		err := rows.Scan(
			&a.ID, &a.Name, &domain, &containerID, &image, &a.Status,
			&envJSON, &portsJSON, &volumesJSON, &resourcesJSON, &deploymentJSON, &sslJSON,
			&appType, &mlxJSON,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan app: %w", err)
		}

		a.Domain = domain.String
		a.ContainerID = containerID.String
		a.Image = image.String
		a.Type = app.AppType(appType.String)
		if a.Type == "" {
			a.Type = app.AppTypeContainer
		}

		json.Unmarshal([]byte(envJSON), &a.Env)
		json.Unmarshal([]byte(portsJSON), &a.Ports)
		json.Unmarshal([]byte(volumesJSON), &a.Volumes)
		json.Unmarshal([]byte(resourcesJSON), &a.Resources)
		json.Unmarshal([]byte(deploymentJSON), &a.Deployment)
		json.Unmarshal([]byte(sslJSON), &a.SSL)
		if mlxJSON.Valid && mlxJSON.String != "" {
			json.Unmarshal([]byte(mlxJSON.String), &a.MLX)
		}

		apps = append(apps, a)
	}

	return apps, nil
}

// UpdateApp updates an app in the database
func (s *Storage) UpdateApp(a *app.App) error {
	a.UpdatedAt = time.Now()

	envJSON, _ := json.Marshal(a.Env)
	portsJSON, _ := json.Marshal(a.Ports)
	volumesJSON, _ := json.Marshal(a.Volumes)
	resourcesJSON, _ := json.Marshal(a.Resources)
	deploymentJSON, _ := json.Marshal(a.Deployment)
	sslJSON, _ := json.Marshal(a.SSL)
	mlxJSON, _ := json.Marshal(a.MLX)

	// Convert empty domain to NULL (for database apps without domains)
	var domain interface{} = a.Domain
	if a.Domain == "" {
		domain = nil
	}

	// Default type to container if not set
	appType := string(a.Type)
	if appType == "" {
		appType = "container"
	}

	_, err := s.db.Exec(`
		UPDATE apps SET
			name = ?, domain = ?, container_id = ?, image = ?, status = ?,
			env = ?, ports = ?, volumes = ?, resources = ?, deployment = ?, ssl = ?,
			type = ?, mlx = ?,
			updated_at = ?
		WHERE id = ?
	`, a.Name, domain, a.ContainerID, a.Image, a.Status,
		string(envJSON), string(portsJSON), string(volumesJSON),
		string(resourcesJSON), string(deploymentJSON), string(sslJSON),
		appType, string(mlxJSON),
		a.UpdatedAt, a.ID)

	if err != nil {
		return fmt.Errorf("failed to update app: %w", err)
	}

	return nil
}

// DeleteApp deletes an app from the database
func (s *Storage) DeleteApp(id string) error {
	_, err := s.db.Exec("DELETE FROM apps WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}
	return nil
}

// GetSetting retrieves a setting value
func (s *Storage) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get setting: %w", err)
	}
	return value, nil
}

// SetSetting sets a setting value
func (s *Storage) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, time.Now(), value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}
	return nil
}

// GetImageTags retrieves cached tags for an image
func (s *Storage) GetImageTags(image string) ([]string, time.Time, error) {
	var tagsJSON string
	var updatedAt time.Time
	err := s.db.QueryRow("SELECT tags, updated_at FROM image_tags WHERE image = ?", image).Scan(&tagsJSON, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to get image tags: %w", err)
	}

	var tags []string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to unmarshal tags: %w", err)
	}
	return tags, updatedAt, nil
}

// SaveImageTags saves tags for an image
func (s *Storage) SaveImageTags(image string, tags []string) error {
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO image_tags (image, tags, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(image) DO UPDATE SET tags = ?, updated_at = ?
	`, image, string(tagsJSON), time.Now(), string(tagsJSON), time.Now())
	if err != nil {
		return fmt.Errorf("failed to save image tags: %w", err)
	}
	return nil
}

// GetAllImageTags retrieves all cached image tags
func (s *Storage) GetAllImageTags() (map[string][]string, error) {
	rows, err := s.db.Query("SELECT image, tags FROM image_tags")
	if err != nil {
		return nil, fmt.Errorf("failed to query image tags: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var image, tagsJSON string
		if err := rows.Scan(&image, &tagsJSON); err != nil {
			continue
		}
		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			continue
		}
		result[image] = tags
	}
	return result, nil
}
