// Package storage provides data persistence for basepod using SQLite.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/base-go/basepod/internal/app"
	"github.com/base-go/basepod/internal/config"
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

	dbPath := filepath.Join(paths.Data, "basepod.db")
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

// DB returns the underlying database connection
func (s *Storage) DB() *sql.DB {
	return s.db
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
		// MLX models table
		`CREATE TABLE IF NOT EXISTS mlx_models (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			size_bytes INTEGER DEFAULT 0,
			downloaded INTEGER DEFAULT 0,
			downloaded_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mlx_models_downloaded ON mlx_models(downloaded)`,
		// Chat messages table - stores conversations per model
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_model ON chat_messages(model_id)`,
		// Add aliases column for domain aliases
		`ALTER TABLE apps ADD COLUMN aliases TEXT`,
		// Add deployments column for deployment history
		`ALTER TABLE apps ADD COLUMN deployments TEXT`,
		// Add health_check column for health check configuration
		`ALTER TABLE apps ADD COLUMN health_check TEXT`,
		// Webhook deliveries table
		`CREATE TABLE IF NOT EXISTS webhook_deliveries (
			id TEXT PRIMARY KEY,
			app_id TEXT NOT NULL,
			event TEXT NOT NULL,
			branch TEXT,
			commit_hash TEXT,
			commit_msg TEXT,
			status TEXT NOT NULL,
			error TEXT,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_app_id ON webhook_deliveries(app_id)`,
		// Cron jobs table
		`CREATE TABLE IF NOT EXISTS cron_jobs (
			id TEXT PRIMARY KEY,
			app_id TEXT NOT NULL,
			name TEXT NOT NULL,
			schedule TEXT NOT NULL,
			command TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			last_run DATETIME,
			last_status TEXT,
			last_error TEXT,
			next_run DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cron_app_id ON cron_jobs(app_id)`,
		// Cron executions table
		`CREATE TABLE IF NOT EXISTS cron_executions (
			id TEXT PRIMARY KEY,
			cron_job_id TEXT NOT NULL,
			started_at DATETIME NOT NULL,
			ended_at DATETIME,
			status TEXT NOT NULL,
			output TEXT,
			exit_code INTEGER,
			FOREIGN KEY (cron_job_id) REFERENCES cron_jobs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_exec_cron_id ON cron_executions(cron_job_id)`,
		// Activity log table
		`CREATE TABLE IF NOT EXISTS activity_log (
			id TEXT PRIMARY KEY,
			actor_type TEXT NOT NULL,
			action TEXT NOT NULL,
			target_type TEXT,
			target_id TEXT,
			target_name TEXT,
			details TEXT,
			status TEXT,
			ip_address TEXT,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_action ON activity_log(action)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_target ON activity_log(target_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_created ON activity_log(created_at DESC)`,
		// Notification configs table
		`CREATE TABLE IF NOT EXISTS notification_configs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			scope TEXT NOT NULL,
			scope_id TEXT,
			webhook_url TEXT,
			slack_webhook_url TEXT,
			discord_webhook_url TEXT,
			events TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notif_scope ON notification_configs(scope, scope_id)`,
		// Deploy tokens table
		`CREATE TABLE IF NOT EXISTS deploy_tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			prefix TEXT NOT NULL,
			scopes TEXT NOT NULL,
			last_used_at DATETIME,
			created_at DATETIME NOT NULL,
			expires_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_token_hash ON deploy_tokens(token_hash)`,
		// App metrics table
		`CREATE TABLE IF NOT EXISTS app_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			app_id TEXT NOT NULL,
			cpu_percent REAL NOT NULL DEFAULT 0,
			mem_usage INTEGER NOT NULL DEFAULT 0,
			mem_limit INTEGER NOT NULL DEFAULT 0,
			net_input INTEGER NOT NULL DEFAULT 0,
			net_output INTEGER NOT NULL DEFAULT 0,
			recorded_at DATETIME NOT NULL,
			FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_app_time ON app_metrics(app_id, recorded_at)`,
		// Users table
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			invite_token TEXT,
			created_at DATETIME NOT NULL,
			last_login_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_users_invite ON users(invite_token)`,
		// Per-app access control for deployers
		`CREATE TABLE IF NOT EXISTS user_app_access (
			user_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			PRIMARY KEY (user_id, app_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_app_access_user ON user_app_access(user_id)`,
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
	deploymentsJSON, _ := json.Marshal(a.Deployments)
	sslJSON, _ := json.Marshal(a.SSL)
	mlxJSON, _ := json.Marshal(a.MLX)
	aliasesJSON, _ := json.Marshal(a.Aliases)
	healthCheckJSON, _ := json.Marshal(a.HealthCheck)

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
		INSERT INTO apps (id, name, domain, aliases, container_id, image, status, env, ports, volumes, resources, deployment, deployments, ssl, type, mlx, health_check, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, a.ID, a.Name, domain, string(aliasesJSON), a.ContainerID, a.Image, a.Status,
		string(envJSON), string(portsJSON), string(volumesJSON),
		string(resourcesJSON), string(deploymentJSON), string(deploymentsJSON), string(sslJSON),
		appType, string(mlxJSON), string(healthCheckJSON),
		a.CreatedAt, a.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	return nil
}

// GetApp retrieves an app by ID
func (s *Storage) GetApp(id string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, aliases, container_id, image, status, env, ports, volumes, resources, deployment, deployments, ssl, type, mlx, health_check, created_at, updated_at
		FROM apps WHERE id = ?
	`, id)

	return s.scanApp(row)
}

// GetAppByName retrieves an app by name
func (s *Storage) GetAppByName(name string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, aliases, container_id, image, status, env, ports, volumes, resources, deployment, deployments, ssl, type, mlx, health_check, created_at, updated_at
		FROM apps WHERE name = ?
	`, name)

	return s.scanApp(row)
}

// GetAppByDomain retrieves an app by domain
func (s *Storage) GetAppByDomain(domain string) (*app.App, error) {
	row := s.db.QueryRow(`
		SELECT id, name, domain, aliases, container_id, image, status, env, ports, volumes, resources, deployment, deployments, ssl, type, mlx, health_check, created_at, updated_at
		FROM apps WHERE domain = ?
	`, domain)

	return s.scanApp(row)
}

// scanApp scans a row into an App struct
func (s *Storage) scanApp(row *sql.Row) (*app.App, error) {
	var a app.App
	var envJSON, portsJSON, volumesJSON, resourcesJSON, deploymentJSON, sslJSON string
	var domain, aliasesJSON, deploymentsJSON, containerID, image, appType, mlxJSON, healthCheckJSON sql.NullString

	err := row.Scan(
		&a.ID, &a.Name, &domain, &aliasesJSON, &containerID, &image, &a.Status,
		&envJSON, &portsJSON, &volumesJSON, &resourcesJSON, &deploymentJSON, &deploymentsJSON, &sslJSON,
		&appType, &mlxJSON, &healthCheckJSON,
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
	if aliasesJSON.Valid && aliasesJSON.String != "" {
		json.Unmarshal([]byte(aliasesJSON.String), &a.Aliases)
	}
	if deploymentsJSON.Valid && deploymentsJSON.String != "" {
		json.Unmarshal([]byte(deploymentsJSON.String), &a.Deployments)
	}
	if healthCheckJSON.Valid && healthCheckJSON.String != "" {
		json.Unmarshal([]byte(healthCheckJSON.String), &a.HealthCheck)
	}

	return &a, nil
}

// ListApps retrieves all apps
func (s *Storage) ListApps() ([]app.App, error) {
	rows, err := s.db.Query(`
		SELECT id, name, domain, aliases, container_id, image, status, env, ports, volumes, resources, deployment, deployments, ssl, type, mlx, health_check, created_at, updated_at
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
		var domain, aliasesJSON, deploymentsJSON, containerID, image, appType, mlxJSON, healthCheckJSON sql.NullString

		err := rows.Scan(
			&a.ID, &a.Name, &domain, &aliasesJSON, &containerID, &image, &a.Status,
			&envJSON, &portsJSON, &volumesJSON, &resourcesJSON, &deploymentJSON, &deploymentsJSON, &sslJSON,
			&appType, &mlxJSON, &healthCheckJSON,
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
		if aliasesJSON.Valid && aliasesJSON.String != "" {
			json.Unmarshal([]byte(aliasesJSON.String), &a.Aliases)
		}
		if deploymentsJSON.Valid && deploymentsJSON.String != "" {
			json.Unmarshal([]byte(deploymentsJSON.String), &a.Deployments)
		}
		if healthCheckJSON.Valid && healthCheckJSON.String != "" {
			json.Unmarshal([]byte(healthCheckJSON.String), &a.HealthCheck)
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
	deploymentsJSON, _ := json.Marshal(a.Deployments)
	sslJSON, _ := json.Marshal(a.SSL)
	mlxJSON, _ := json.Marshal(a.MLX)
	aliasesJSON, _ := json.Marshal(a.Aliases)
	healthCheckJSON, _ := json.Marshal(a.HealthCheck)

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
			name = ?, domain = ?, aliases = ?, container_id = ?, image = ?, status = ?,
			env = ?, ports = ?, volumes = ?, resources = ?, deployment = ?, deployments = ?, ssl = ?,
			type = ?, mlx = ?, health_check = ?,
			updated_at = ?
		WHERE id = ?
	`, a.Name, domain, string(aliasesJSON), a.ContainerID, a.Image, a.Status,
		string(envJSON), string(portsJSON), string(volumesJSON),
		string(resourcesJSON), string(deploymentJSON), string(deploymentsJSON), string(sslJSON),
		appType, string(mlxJSON), string(healthCheckJSON),
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

// ChatMessage represents a chat message
type ChatMessage struct {
	ID        int64     `json:"id"`
	ModelID   string    `json:"model_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// SaveChatMessage saves a chat message
func (s *Storage) SaveChatMessage(modelID, role, content string) error {
	_, err := s.db.Exec(`
		INSERT INTO chat_messages (model_id, role, content, created_at)
		VALUES (?, ?, ?, ?)
	`, modelID, role, content, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save chat message: %w", err)
	}
	return nil
}

// GetChatMessages retrieves chat messages for a model
func (s *Storage) GetChatMessages(modelID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT id, model_id, role, content, created_at
		FROM chat_messages
		WHERE model_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, modelID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat messages: %w", err)
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.ID, &msg.ModelID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// ClearChatMessages deletes all chat messages for a model
func (s *Storage) ClearChatMessages(modelID string) error {
	_, err := s.db.Exec("DELETE FROM chat_messages WHERE model_id = ?", modelID)
	if err != nil {
		return fmt.Errorf("failed to clear chat messages: %w", err)
	}
	return nil
}

// ClearAllChatMessages deletes all chat messages
func (s *Storage) ClearAllChatMessages() error {
	_, err := s.db.Exec("DELETE FROM chat_messages")
	if err != nil {
		return fmt.Errorf("failed to clear all chat messages: %w", err)
	}
	return nil
}

// SaveWebhookDelivery saves a webhook delivery record
func (s *Storage) SaveWebhookDelivery(d *app.WebhookDelivery) error {
	_, err := s.db.Exec(`
		INSERT INTO webhook_deliveries (id, app_id, event, branch, commit_hash, commit_msg, status, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, d.ID, d.AppID, d.Event, d.Branch, d.Commit, d.Message, d.Status, d.Error, d.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to save webhook delivery: %w", err)
	}
	return nil
}

// UpdateWebhookDeliveryStatus updates the status and error of a webhook delivery
func (s *Storage) UpdateWebhookDeliveryStatus(id, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE webhook_deliveries SET status = ?, error = ? WHERE id = ?`, status, errMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update webhook delivery: %w", err)
	}
	return nil
}

// --- Cron Jobs ---

// CreateCronJob creates a new cron job
func (s *Storage) CreateCronJob(j *app.CronJob) error {
	_, err := s.db.Exec(`
		INSERT INTO cron_jobs (id, app_id, name, schedule, command, enabled, next_run, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, j.ID, j.AppID, j.Name, j.Schedule, j.Command, j.Enabled, j.NextRun, j.CreatedAt, j.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create cron job: %w", err)
	}
	return nil
}

// GetCronJob retrieves a cron job by ID
func (s *Storage) GetCronJob(id string) (*app.CronJob, error) {
	var j app.CronJob
	var lastRun, nextRun sql.NullTime
	var lastStatus, lastError sql.NullString
	err := s.db.QueryRow(`
		SELECT id, app_id, name, schedule, command, enabled, last_run, last_status, last_error, next_run, created_at, updated_at
		FROM cron_jobs WHERE id = ?
	`, id).Scan(&j.ID, &j.AppID, &j.Name, &j.Schedule, &j.Command, &j.Enabled,
		&lastRun, &lastStatus, &lastError, &nextRun, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cron job: %w", err)
	}
	if lastRun.Valid {
		j.LastRun = &lastRun.Time
	}
	j.LastStatus = lastStatus.String
	j.LastError = lastError.String
	if nextRun.Valid {
		j.NextRun = &nextRun.Time
	}
	return &j, nil
}

// ListCronJobs lists cron jobs for an app
func (s *Storage) ListCronJobs(appID string) ([]app.CronJob, error) {
	rows, err := s.db.Query(`
		SELECT id, app_id, name, schedule, command, enabled, last_run, last_status, last_error, next_run, created_at, updated_at
		FROM cron_jobs WHERE app_id = ? ORDER BY created_at DESC
	`, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cron jobs: %w", err)
	}
	defer rows.Close()

	var jobs []app.CronJob
	for rows.Next() {
		var j app.CronJob
		var lastRun, nextRun sql.NullTime
		var lastStatus, lastError sql.NullString
		if err := rows.Scan(&j.ID, &j.AppID, &j.Name, &j.Schedule, &j.Command, &j.Enabled,
			&lastRun, &lastStatus, &lastError, &nextRun, &j.CreatedAt, &j.UpdatedAt); err != nil {
			continue
		}
		if lastRun.Valid {
			j.LastRun = &lastRun.Time
		}
		j.LastStatus = lastStatus.String
		j.LastError = lastError.String
		if nextRun.Valid {
			j.NextRun = &nextRun.Time
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

// UpdateCronJob updates a cron job
func (s *Storage) UpdateCronJob(j *app.CronJob) error {
	j.UpdatedAt = time.Now()
	_, err := s.db.Exec(`
		UPDATE cron_jobs SET name=?, schedule=?, command=?, enabled=?, last_run=?, last_status=?, last_error=?, next_run=?, updated_at=?
		WHERE id = ?
	`, j.Name, j.Schedule, j.Command, j.Enabled, j.LastRun, j.LastStatus, j.LastError, j.NextRun, j.UpdatedAt, j.ID)
	if err != nil {
		return fmt.Errorf("failed to update cron job: %w", err)
	}
	return nil
}

// DeleteCronJob deletes a cron job
func (s *Storage) DeleteCronJob(id string) error {
	_, err := s.db.Exec("DELETE FROM cron_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete cron job: %w", err)
	}
	return nil
}

// CreateCronExecution creates a cron execution record
func (s *Storage) CreateCronExecution(e *app.CronExecution) error {
	_, err := s.db.Exec(`
		INSERT INTO cron_executions (id, cron_job_id, started_at, status, output, exit_code)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.ID, e.CronJobID, e.StartedAt, e.Status, e.Output, e.ExitCode)
	if err != nil {
		return fmt.Errorf("failed to create cron execution: %w", err)
	}
	return nil
}

// UpdateCronExecution updates a cron execution
func (s *Storage) UpdateCronExecution(e *app.CronExecution) error {
	_, err := s.db.Exec(`
		UPDATE cron_executions SET ended_at=?, status=?, output=?, exit_code=? WHERE id = ?
	`, e.EndedAt, e.Status, e.Output, e.ExitCode, e.ID)
	if err != nil {
		return fmt.Errorf("failed to update cron execution: %w", err)
	}
	return nil
}

// ListCronExecutions lists executions for a cron job
func (s *Storage) ListCronExecutions(cronJobID string, limit int) ([]app.CronExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, cron_job_id, started_at, ended_at, status, output, exit_code
		FROM cron_executions WHERE cron_job_id = ? ORDER BY started_at DESC LIMIT ?
	`, cronJobID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list cron executions: %w", err)
	}
	defer rows.Close()

	var execs []app.CronExecution
	for rows.Next() {
		var e app.CronExecution
		var endedAt sql.NullTime
		var output sql.NullString
		if err := rows.Scan(&e.ID, &e.CronJobID, &e.StartedAt, &endedAt, &e.Status, &output, &e.ExitCode); err != nil {
			continue
		}
		if endedAt.Valid {
			e.EndedAt = &endedAt.Time
		}
		e.Output = output.String
		execs = append(execs, e)
	}
	return execs, nil
}

// --- Activity Log ---

// SaveActivityLog saves an activity log entry
func (s *Storage) SaveActivityLog(l *app.ActivityLog) error {
	_, err := s.db.Exec(`
		INSERT INTO activity_log (id, actor_type, action, target_type, target_id, target_name, details, status, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, l.ID, l.ActorType, l.Action, l.TargetType, l.TargetID, l.TargetName, l.Details, l.Status, l.IPAddress, l.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to save activity log: %w", err)
	}
	return nil
}

// ListActivityLogs retrieves activity logs with optional filters
func (s *Storage) ListActivityLogs(targetID string, action string, limit int) ([]app.ActivityLog, error) {
	return s.ListActivityLogsPaginated(targetID, action, limit, 0)
}

func (s *Storage) ListActivityLogsPaginated(targetID string, action string, limit int, offset int) ([]app.ActivityLog, error) {
	if limit <= 0 {
		limit = 50
	}

	query := "SELECT id, actor_type, action, target_type, target_id, target_name, details, status, ip_address, created_at FROM activity_log WHERE 1=1"
	var args []interface{}

	if targetID != "" {
		query += " AND target_id = ?"
		args = append(args, targetID)
	}
	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list activity logs: %w", err)
	}
	defer rows.Close()

	var logs []app.ActivityLog
	for rows.Next() {
		var l app.ActivityLog
		var targetType, targetID, targetName, details, status, ipAddr sql.NullString
		if err := rows.Scan(&l.ID, &l.ActorType, &l.Action, &targetType, &targetID, &targetName, &details, &status, &ipAddr, &l.CreatedAt); err != nil {
			continue
		}
		l.TargetType = targetType.String
		l.TargetID = targetID.String
		l.TargetName = targetName.String
		l.Details = details.String
		l.Status = status.String
		l.IPAddress = ipAddr.String
		logs = append(logs, l)
	}
	return logs, nil
}

func (s *Storage) CountActivityLogs(targetID string, action string) (int, error) {
	query := "SELECT COUNT(*) FROM activity_log WHERE 1=1"
	var args []interface{}

	if targetID != "" {
		query += " AND target_id = ?"
		args = append(args, targetID)
	}
	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// --- Notification Configs ---

// CreateNotificationConfig creates a notification config
func (s *Storage) CreateNotificationConfig(n *app.NotificationConfig) error {
	eventsJSON, _ := json.Marshal(n.Events)
	_, err := s.db.Exec(`
		INSERT INTO notification_configs (id, name, type, enabled, scope, scope_id, webhook_url, slack_webhook_url, discord_webhook_url, events, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, n.ID, n.Name, n.Type, n.Enabled, n.Scope, n.ScopeID, n.WebhookURL, n.SlackWebhookURL, n.DiscordWebhook, string(eventsJSON), n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create notification config: %w", err)
	}
	return nil
}

// GetNotificationConfig retrieves a notification config by ID
func (s *Storage) GetNotificationConfig(id string) (*app.NotificationConfig, error) {
	var n app.NotificationConfig
	var scopeID, webhookURL, slackURL, discordURL sql.NullString
	var eventsJSON string
	err := s.db.QueryRow(`
		SELECT id, name, type, enabled, scope, scope_id, webhook_url, slack_webhook_url, discord_webhook_url, events, created_at, updated_at
		FROM notification_configs WHERE id = ?
	`, id).Scan(&n.ID, &n.Name, &n.Type, &n.Enabled, &n.Scope, &scopeID, &webhookURL, &slackURL, &discordURL, &eventsJSON, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get notification config: %w", err)
	}
	n.ScopeID = scopeID.String
	n.WebhookURL = webhookURL.String
	n.SlackWebhookURL = slackURL.String
	n.DiscordWebhook = discordURL.String
	json.Unmarshal([]byte(eventsJSON), &n.Events)
	return &n, nil
}

// ListNotificationConfigs lists notification configs, optionally filtered by event and app
func (s *Storage) ListNotificationConfigs(event string, appID string) ([]app.NotificationConfig, error) {
	query := `SELECT id, name, type, enabled, scope, scope_id, webhook_url, slack_webhook_url, discord_webhook_url, events, created_at, updated_at
		FROM notification_configs WHERE enabled = 1`
	var args []interface{}

	if appID != "" {
		query += " AND (scope = 'global' OR scope_id = ?)"
		args = append(args, appID)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list notification configs: %w", err)
	}
	defer rows.Close()

	var configs []app.NotificationConfig
	for rows.Next() {
		var n app.NotificationConfig
		var scopeID, webhookURL, slackURL, discordURL sql.NullString
		var eventsJSON string
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &n.Enabled, &n.Scope, &scopeID, &webhookURL, &slackURL, &discordURL, &eventsJSON, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		n.ScopeID = scopeID.String
		n.WebhookURL = webhookURL.String
		n.SlackWebhookURL = slackURL.String
		n.DiscordWebhook = discordURL.String
		json.Unmarshal([]byte(eventsJSON), &n.Events)

		// Filter by event if specified
		if event != "" {
			matched := false
			for _, e := range n.Events {
				if e == event {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		configs = append(configs, n)
	}
	return configs, nil
}

// UpdateNotificationConfig updates a notification config
func (s *Storage) UpdateNotificationConfig(n *app.NotificationConfig) error {
	n.UpdatedAt = time.Now()
	eventsJSON, _ := json.Marshal(n.Events)
	_, err := s.db.Exec(`
		UPDATE notification_configs SET name=?, type=?, enabled=?, scope=?, scope_id=?, webhook_url=?, slack_webhook_url=?, discord_webhook_url=?, events=?, updated_at=?
		WHERE id = ?
	`, n.Name, n.Type, n.Enabled, n.Scope, n.ScopeID, n.WebhookURL, n.SlackWebhookURL, n.DiscordWebhook, string(eventsJSON), n.UpdatedAt, n.ID)
	if err != nil {
		return fmt.Errorf("failed to update notification config: %w", err)
	}
	return nil
}

// DeleteNotificationConfig deletes a notification config
func (s *Storage) DeleteNotificationConfig(id string) error {
	_, err := s.db.Exec("DELETE FROM notification_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete notification config: %w", err)
	}
	return nil
}

// --- Deploy Tokens ---

// CreateDeployToken creates a deploy token
func (s *Storage) CreateDeployToken(t *app.DeployToken) error {
	scopesJSON, _ := json.Marshal(t.Scopes)
	_, err := s.db.Exec(`
		INSERT INTO deploy_tokens (id, name, token_hash, prefix, scopes, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, t.ID, t.Name, t.TokenHash, t.Prefix, string(scopesJSON), t.CreatedAt, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to create deploy token: %w", err)
	}
	return nil
}

// GetDeployTokenByHash retrieves a deploy token by its hash
func (s *Storage) GetDeployTokenByHash(hash string) (*app.DeployToken, error) {
	var t app.DeployToken
	var scopesJSON string
	var lastUsed, expires sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, name, token_hash, prefix, scopes, last_used_at, created_at, expires_at
		FROM deploy_tokens WHERE token_hash = ?
	`, hash).Scan(&t.ID, &t.Name, &t.TokenHash, &t.Prefix, &scopesJSON, &lastUsed, &t.CreatedAt, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get deploy token: %w", err)
	}
	json.Unmarshal([]byte(scopesJSON), &t.Scopes)
	if lastUsed.Valid {
		t.LastUsedAt = &lastUsed.Time
	}
	if expires.Valid {
		t.ExpiresAt = &expires.Time
	}
	return &t, nil
}

// ListDeployTokens lists all deploy tokens
func (s *Storage) ListDeployTokens() ([]app.DeployToken, error) {
	rows, err := s.db.Query(`
		SELECT id, name, token_hash, prefix, scopes, last_used_at, created_at, expires_at
		FROM deploy_tokens ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list deploy tokens: %w", err)
	}
	defer rows.Close()

	var tokens []app.DeployToken
	for rows.Next() {
		var t app.DeployToken
		var scopesJSON string
		var lastUsed, expires sql.NullTime
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.Prefix, &scopesJSON, &lastUsed, &t.CreatedAt, &expires); err != nil {
			continue
		}
		json.Unmarshal([]byte(scopesJSON), &t.Scopes)
		if lastUsed.Valid {
			t.LastUsedAt = &lastUsed.Time
		}
		if expires.Valid {
			t.ExpiresAt = &expires.Time
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// UpdateDeployTokenLastUsed updates the last used timestamp
func (s *Storage) UpdateDeployTokenLastUsed(id string) error {
	_, err := s.db.Exec("UPDATE deploy_tokens SET last_used_at = ? WHERE id = ?", time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update deploy token: %w", err)
	}
	return nil
}

// DeleteDeployToken deletes a deploy token
func (s *Storage) DeleteDeployToken(id string) error {
	_, err := s.db.Exec("DELETE FROM deploy_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete deploy token: %w", err)
	}
	return nil
}

// --- Users ---

func (s *Storage) CreateUser(u *app.User) error {
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, password_hash, role, invite_token, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.Role, u.InviteToken, u.CreatedAt,
	)
	return err
}

func (s *Storage) GetUserByEmail(email string) (*app.User, error) {
	var u app.User
	var lastLogin sql.NullTime
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, role, created_at, last_login_at FROM users WHERE email = ?", email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

func (s *Storage) GetUserByID(id string) (*app.User, error) {
	var u app.User
	var lastLogin sql.NullTime
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, role, created_at, last_login_at FROM users WHERE id = ?", id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

func (s *Storage) GetUserByInviteToken(token string) (*app.User, error) {
	var u app.User
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, role, invite_token, created_at FROM users WHERE invite_token = ?", token,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.InviteToken, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Storage) ListUsers() ([]app.User, error) {
	rows, err := s.db.Query(
		"SELECT id, email, password_hash, role, created_at, last_login_at FROM users ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []app.User
	for rows.Next() {
		var u app.User
		var lastLogin sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}
		if lastLogin.Valid {
			u.LastLoginAt = &lastLogin.Time
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *Storage) UpdateUserRole(id, role string) error {
	_, err := s.db.Exec("UPDATE users SET role = ? WHERE id = ?", role, id)
	return err
}

func (s *Storage) UpdateUserLogin(id string) error {
	_, err := s.db.Exec("UPDATE users SET last_login_at = ? WHERE id = ?", time.Now(), id)
	return err
}

func (s *Storage) UpdateUserPassword(id, passwordHash string) error {
	_, err := s.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
	return err
}

func (s *Storage) DeleteUser(id string) error {
	_, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func (s *Storage) ClearInviteToken(id string) error {
	_, err := s.db.Exec("UPDATE users SET invite_token = NULL WHERE id = ?", id)
	return err
}

func (s *Storage) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// --- User App Access ---

// SetUserAppAccess replaces all app access for a user
func (s *Storage) SetUserAppAccess(userID string, appIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing access
	if _, err := tx.Exec("DELETE FROM user_app_access WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("failed to clear user app access: %w", err)
	}

	// Insert new access
	now := time.Now()
	for _, appID := range appIDs {
		if _, err := tx.Exec("INSERT INTO user_app_access (user_id, app_id, created_at) VALUES (?, ?, ?)", userID, appID, now); err != nil {
			return fmt.Errorf("failed to insert user app access: %w", err)
		}
	}

	return tx.Commit()
}

// GetUserAppAccess returns list of app IDs a user can access
func (s *Storage) GetUserAppAccess(userID string) ([]string, error) {
	rows, err := s.db.Query("SELECT app_id FROM user_app_access WHERE user_id = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user app access: %w", err)
	}
	defer rows.Close()

	var appIDs []string
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			continue
		}
		appIDs = append(appIDs, appID)
	}
	return appIDs, nil
}

// ListAppsForUser returns apps filtered by user_app_access
func (s *Storage) ListAppsForUser(userID string) ([]app.App, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.name, a.domain, a.aliases, a.container_id, a.image, a.status, a.env, a.ports, a.volumes, a.resources, a.deployment, a.deployments, a.ssl, a.type, a.mlx, a.health_check, a.created_at, a.updated_at
		FROM apps a
		INNER JOIN user_app_access ua ON a.id = ua.app_id
		WHERE ua.user_id = ?
		ORDER BY a.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps for user: %w", err)
	}
	defer rows.Close()

	var apps []app.App
	for rows.Next() {
		var a app.App
		var envJSON, portsJSON, volumesJSON, resourcesJSON, deploymentJSON, sslJSON string
		var domain, aliasesJSON, deploymentsJSON, containerID, image, appType, mlxJSON, healthCheckJSON sql.NullString

		err := rows.Scan(
			&a.ID, &a.Name, &domain, &aliasesJSON, &containerID, &image, &a.Status,
			&envJSON, &portsJSON, &volumesJSON, &resourcesJSON, &deploymentJSON, &deploymentsJSON, &sslJSON,
			&appType, &mlxJSON, &healthCheckJSON,
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
		if aliasesJSON.Valid && aliasesJSON.String != "" {
			json.Unmarshal([]byte(aliasesJSON.String), &a.Aliases)
		}
		if deploymentsJSON.Valid && deploymentsJSON.String != "" {
			json.Unmarshal([]byte(deploymentsJSON.String), &a.Deployments)
		}
		if healthCheckJSON.Valid && healthCheckJSON.String != "" {
			json.Unmarshal([]byte(healthCheckJSON.String), &a.HealthCheck)
		}

		apps = append(apps, a)
	}

	return apps, nil
}

// UserHasAppAccess checks if a user has access to a specific app
func (s *Storage) UserHasAppAccess(userID, appID string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM user_app_access WHERE user_id = ? AND app_id = ?", userID, appID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check user app access: %w", err)
	}
	return count > 0, nil
}

// --- App Metrics ---

// SaveAppMetric stores a metric data point
func (s *Storage) SaveAppMetric(m *app.AppMetric) error {
	_, err := s.db.Exec(
		`INSERT INTO app_metrics (app_id, cpu_percent, mem_usage, mem_limit, net_input, net_output, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.AppID, m.CPUPercent, m.MemUsage, m.MemLimit, m.NetInput, m.NetOutput, m.RecordedAt,
	)
	return err
}

// ListAppMetrics retrieves metrics for an app within a time range
func (s *Storage) ListAppMetrics(appID string, since time.Time, limit int) ([]app.AppMetric, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, cpu_percent, mem_usage, mem_limit, net_input, net_output, recorded_at
		 FROM app_metrics WHERE app_id = ? AND recorded_at > ? ORDER BY recorded_at ASC LIMIT ?`,
		appID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []app.AppMetric
	for rows.Next() {
		var m app.AppMetric
		if err := rows.Scan(&m.ID, &m.AppID, &m.CPUPercent, &m.MemUsage, &m.MemLimit, &m.NetInput, &m.NetOutput, &m.RecordedAt); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, nil
}

// CleanOldMetrics removes metrics older than the specified duration
func (s *Storage) CleanOldMetrics(before time.Time) error {
	_, err := s.db.Exec("DELETE FROM app_metrics WHERE recorded_at < ?", before)
	return err
}

// ListWebhookDeliveries retrieves recent webhook deliveries for an app
func (s *Storage) ListWebhookDeliveries(appID string, limit int) ([]app.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT id, app_id, event, branch, commit_hash, commit_msg, status, error, created_at
		FROM webhook_deliveries
		WHERE app_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, appID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list webhook deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []app.WebhookDelivery
	for rows.Next() {
		var d app.WebhookDelivery
		var branch, commit, msg, errStr sql.NullString
		if err := rows.Scan(&d.ID, &d.AppID, &d.Event, &branch, &commit, &msg, &d.Status, &errStr, &d.CreatedAt); err != nil {
			continue
		}
		d.Branch = branch.String
		d.Commit = commit.String
		d.Message = msg.String
		d.Error = errStr.String
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}
