// Package templates provides predefined app templates for one-click installs
package templates

import "runtime"

// Template represents a predefined app configuration
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Image       string            `json:"image"`
	ImageARM    string            `json:"image_arm,omitempty"` // ARM64 specific image if different
	Port        int               `json:"port"`
	Env         map[string]string `json:"env"`
	Command     []string          `json:"command,omitempty"` // Custom command to run
	Category    string            `json:"category"`
	Icon        string            `json:"icon"`
	Arch        []string          `json:"arch,omitempty"` // Supported architectures: amd64, arm64. Empty means all
}

// GetArch returns the current system architecture
func GetArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "amd64":
		return "amd64"
	default:
		return runtime.GOARCH
	}
}

// GetImage returns the appropriate image for the current architecture
func (t *Template) GetImage() string {
	if GetArch() == "arm64" && t.ImageARM != "" {
		return t.ImageARM
	}
	return t.Image
}

// IsArchSupported checks if template supports current architecture
func (t *Template) IsArchSupported() bool {
	if len(t.Arch) == 0 {
		return true // No restriction means all architectures
	}
	currentArch := GetArch()
	for _, a := range t.Arch {
		if a == currentArch {
			return true
		}
	}
	return false
}

// GetTemplatesForArch returns only templates supported on current architecture
func GetTemplatesForArch() []Template {
	result := make([]Template, 0)
	for _, t := range Templates {
		if t.IsArchSupported() {
			result = append(result, t)
		}
	}
	return result
}

// SystemInfo returns architecture info for the frontend
type SystemInfo struct {
	Arch     string `json:"arch"`
	OS       string `json:"os"`
	Platform string `json:"platform"` // e.g., "darwin/arm64"
}

// GetSystemInfo returns current system architecture info
func GetSystemInfo() SystemInfo {
	return SystemInfo{
		Arch:     runtime.GOARCH,
		OS:       runtime.GOOS,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// Templates is the list of all available templates
var Templates = []Template{
	// Databases
	{
		ID:          "mysql",
		Name:        "MySQL",
		Description: "Popular open-source relational database",
		Image:       "mysql:8",
		Port:        3306,
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "changeme",
			"MYSQL_DATABASE":      "app",
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:          "mariadb",
		Name:        "MariaDB",
		Description: "Community-developed MySQL fork",
		Image:       "mariadb:11",
		Port:        3306,
		Env: map[string]string{
			"MARIADB_ROOT_PASSWORD": "changeme",
			"MARIADB_DATABASE":      "app",
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:          "postgres",
		Name:        "PostgreSQL",
		Description: "Advanced open-source relational database",
		Image:       "postgres:16-alpine",
		Port:        5432,
		Env: map[string]string{
			"POSTGRES_PASSWORD": "changeme",
			"POSTGRES_DB":       "app",
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:          "mongodb",
		Name:        "MongoDB",
		Description: "NoSQL document database",
		Image:       "mongo:7",
		Port:        27017,
		Env: map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": "admin",
			"MONGO_INITDB_ROOT_PASSWORD": "changeme",
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:          "redis",
		Name:        "Redis",
		Description: "In-memory data structure store",
		Image:       "redis:7-alpine",
		Port:        6379,
		Env:         map[string]string{},
		Category:    "database",
		Icon:        "i-lucide-database",
	},
	// Admin Tools
	{
		ID:          "phpmyadmin",
		Name:        "phpMyAdmin",
		Description: "Web-based MySQL administration tool",
		Image:       "phpmyadmin:latest",
		Port:        80,
		Env: map[string]string{
			"PMA_ARBITRARY": "1",
		},
		Category: "admin",
		Icon:     "i-lucide-settings",
	},
	{
		ID:          "adminer",
		Name:        "Adminer",
		Description: "Database management in a single PHP file",
		Image:       "adminer:latest",
		Port:        8080,
		Env:         map[string]string{},
		Category:    "admin",
		Icon:        "i-lucide-settings",
	},
	{
		ID:          "pgadmin",
		Name:        "pgAdmin",
		Description: "PostgreSQL administration and development platform",
		Image:       "dpage/pgadmin4:latest",
		Port:        80,
		Env: map[string]string{
			"PGADMIN_DEFAULT_EMAIL":    "admin@example.com",
			"PGADMIN_DEFAULT_PASSWORD": "changeme",
		},
		Category: "admin",
		Icon:     "i-lucide-settings",
	},
	// Web Servers
	{
		ID:          "nginx",
		Name:        "Nginx",
		Description: "High-performance web server and reverse proxy",
		Image:       "nginx:alpine",
		Port:        80,
		Env:         map[string]string{},
		Category:    "webserver",
		Icon:        "i-lucide-globe",
	},
	{
		ID:          "apache",
		Name:        "Apache",
		Description: "Most popular web server",
		Image:       "httpd:alpine",
		Port:        80,
		Env:         map[string]string{},
		Category:    "webserver",
		Icon:        "i-lucide-globe",
	},
	{
		ID:          "caddy",
		Name:        "Caddy",
		Description: "Fast, multi-platform web server with automatic HTTPS",
		Image:       "caddy:alpine",
		Port:        80,
		Env:         map[string]string{},
		Category:    "webserver",
		Icon:        "i-lucide-globe",
	},
	// CMS / Apps
	{
		ID:          "wordpress",
		Name:        "WordPress",
		Description: "Popular content management system",
		Image:       "wordpress:latest",
		Port:        80,
		Env: map[string]string{
			"WORDPRESS_DB_HOST":     "mysql",
			"WORDPRESS_DB_USER":     "wordpress",
			"WORDPRESS_DB_PASSWORD": "changeme",
			"WORDPRESS_DB_NAME":     "wordpress",
		},
		Category: "cms",
		Icon:     "i-lucide-file-text",
	},
	{
		ID:          "ghost",
		Name:        "Ghost",
		Description: "Professional publishing platform",
		Image:       "ghost:5-alpine",
		Port:        2368,
		Env: map[string]string{
			"database__client":             "sqlite3",
			"database__connection__filename": "/var/lib/ghost/content/data/ghost.db",
			"url":                          "http://localhost:2368",
		},
		Category: "cms",
		Icon:     "i-lucide-file-text",
	},
	{
		ID:          "strapi",
		Name:        "Strapi",
		Description: "Headless CMS to easily build APIs (amd64 only)",
		Image:       "strapi/strapi:latest",
		Port:        1337,
		Env:         map[string]string{},
		Category:    "cms",
		Icon:        "i-lucide-file-text",
		Arch:        []string{"amd64"}, // No official ARM64 image
	},
	// Dev Tools
	{
		ID:          "gitea",
		Name:        "Gitea",
		Description: "Lightweight self-hosted Git service",
		Image:       "gitea/gitea:latest",
		Port:        3000,
		Env:         map[string]string{},
		Category:    "devtools",
		Icon:        "i-lucide-git-branch",
	},
	{
		ID:          "portainer",
		Name:        "Portainer",
		Description: "Container management UI",
		Image:       "portainer/portainer-ce:latest",
		Port:        9000,
		Env:         map[string]string{},
		Category:    "devtools",
		Icon:        "i-lucide-box",
	},
	{
		ID:          "uptime-kuma",
		Name:        "Uptime Kuma",
		Description: "Self-hosted monitoring tool",
		Image:       "louislam/uptime-kuma:latest",
		Port:        3001,
		Env:         map[string]string{},
		Category:    "devtools",
		Icon:        "i-lucide-activity",
	},
	// Communication
	{
		ID:          "mattermost",
		Name:        "Mattermost",
		Description: "Open-source Slack alternative (amd64 only)",
		Image:       "mattermost/mattermost-team-edition:latest",
		Port:        8065,
		Env:         map[string]string{},
		Category:    "communication",
		Icon:        "i-lucide-message-circle",
		Arch:        []string{"amd64"}, // No ARM64 image available
	},
	{
		ID:          "n8n",
		Name:        "n8n",
		Description: "Workflow automation tool",
		Image:       "n8nio/n8n:latest",
		Port:        5678,
		Env:         map[string]string{},
		Category:    "automation",
		Icon:        "i-lucide-workflow",
	},
	// Analytics & Monitoring
	{
		ID:          "plausible",
		Name:        "Plausible Analytics",
		Description: "Privacy-friendly Google Analytics alternative",
		Image:       "plausible/analytics:latest",
		Port:        8000,
		Env: map[string]string{
			"BASE_URL":       "http://localhost",
			"SECRET_KEY_BASE": "changeme_must_be_64_bytes_long_generate_with_openssl_rand_hex_64",
		},
		Category: "analytics",
		Icon:     "i-lucide-bar-chart",
	},
	{
		ID:          "grafana",
		Name:        "Grafana",
		Description: "Open-source analytics and monitoring",
		Image:       "grafana/grafana:latest",
		Port:        3000,
		Env:         map[string]string{},
		Category:    "analytics",
		Icon:        "i-lucide-bar-chart",
	},
	// File Storage
	{
		ID:          "minio",
		Name:        "MinIO",
		Description: "S3-compatible object storage",
		Image:       "minio/minio:latest",
		Port:        9000,
		Env: map[string]string{
			"MINIO_ROOT_USER":     "admin",
			"MINIO_ROOT_PASSWORD": "changeme123",
		},
		Category: "storage",
		Icon:     "i-lucide-hard-drive",
	},
	{
		ID:          "filebrowser",
		Name:        "File Browser",
		Description: "Web-based file manager",
		Image:       "filebrowser/filebrowser:latest",
		Port:        80,
		Env:         map[string]string{},
		Category:    "storage",
		Icon:        "i-lucide-folder",
	},
	// Code Server
	{
		ID:          "code-server",
		Name:        "Code Server",
		Description: "VS Code in the browser",
		Image:       "codercom/code-server:latest",
		Port:        8080,
		Env: map[string]string{
			"PASSWORD":          "changeme",
			"CS_DISABLE_TELEMETRY": "true", // Set to "false" to enable telemetry
		},
		Command:  []string{"--bind-addr", "0.0.0.0:8080", "--disable-telemetry"},
		Category: "devtools",
		Icon:     "i-lucide-code",
	},
}

// GetTemplate returns a template by ID
func GetTemplate(id string) *Template {
	for _, t := range Templates {
		if t.ID == id {
			return &t
		}
	}
	return nil
}

// GetCategories returns all unique categories
func GetCategories() []string {
	categories := make(map[string]bool)
	for _, t := range Templates {
		categories[t.Category] = true
	}
	result := make([]string, 0, len(categories))
	for c := range categories {
		result = append(result, c)
	}
	return result
}
