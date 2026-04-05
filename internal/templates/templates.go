// Package templates provides predefined app templates for one-click installs
package templates

import "runtime"

// VolumeConfig defines a volume mount for persistent data
type VolumeConfig struct {
	Name          string `json:"name"`           // e.g., "data"
	ContainerPath string `json:"container_path"` // e.g., "/var/lib/mysql"
}

// Template represents a predefined app configuration
type Template struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Image          string            `json:"image"`                     // Base image without tag, e.g., "mysql"
	Versions       []string          `json:"versions,omitempty"`        // Available versions, e.g., ["8", "8.4", "5.7"]
	DefaultVersion string            `json:"default_version,omitempty"` // Default version (first in list if empty)
	HasAlpine      bool              `json:"has_alpine,omitempty"`      // Whether alpine variants exist
	ImageARM       string            `json:"image_arm,omitempty"`       // ARM64 specific image if different
	Port           int               `json:"port"`
	Env            map[string]string `json:"env"`
	Command        []string          `json:"command,omitempty"` // Custom command to run
	Volumes        []VolumeConfig    `json:"volumes,omitempty"` // Persistent volume mounts
	Category       string            `json:"category"`
	Icon           string            `json:"icon"`
	Arch           []string          `json:"arch,omitempty"` // Supported architectures: amd64, arm64. Empty means all
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

// GetImage returns the default image with default version
func (t *Template) GetImage() string {
	if GetArch() == "arm64" && t.ImageARM != "" {
		return t.ImageARM
	}
	// If no versions defined, return image as-is (backwards compat)
	if len(t.Versions) == 0 {
		return t.Image
	}
	// Build image with default version
	version := t.DefaultVersion
	if version == "" && len(t.Versions) > 0 {
		version = t.Versions[0]
	}
	return t.Image + ":" + version
}

// BuildImage constructs the full image tag with the selected version
// The useAlpine parameter is kept for API compatibility but no longer adds suffixes
// (alpine filtering is now done in the frontend dropdown)
func (t *Template) BuildImage(version string, useAlpine bool) string {
	if GetArch() == "arm64" && t.ImageARM != "" {
		return t.ImageARM
	}
	// If no version provided, use default
	if version == "" {
		if t.DefaultVersion != "" {
			version = t.DefaultVersion
		} else if len(t.Versions) > 0 {
			version = t.Versions[0]
		}
	}
	// If still no version (no versions defined), return base image
	if version == "" {
		return t.Image
	}
	// Just use the selected version directly - alpine filtering is done in frontend
	return t.Image + ":" + version
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
		ID:             "mysql",
		Name:           "MySQL",
		Description:    "Popular open-source relational database",
		Image:          "mysql",
		Versions:       []string{"8.4", "8.0", "5.7", "latest"},
		DefaultVersion: "8.4",
		HasAlpine:      false,
		Port:           3306,
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "changeme",
			"MYSQL_DATABASE":      "app",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/lib/mysql"},
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:             "mariadb",
		Name:           "MariaDB",
		Description:    "Community-developed MySQL fork",
		Image:          "mariadb",
		Versions:       []string{"11", "10.11", "10.6", "latest"},
		DefaultVersion: "11",
		HasAlpine:      false,
		Port:           3306,
		Env: map[string]string{
			"MARIADB_ROOT_PASSWORD": "changeme",
			"MARIADB_DATABASE":      "app",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/lib/mysql"},
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:             "postgres",
		Name:           "PostgreSQL",
		Description:    "Advanced open-source relational database",
		Image:          "postgres",
		Versions:       []string{"17", "16", "15", "14", "latest"},
		DefaultVersion: "17",
		HasAlpine:      true,
		Port:           5432,
		Env: map[string]string{
			"POSTGRES_PASSWORD": "changeme",
			"POSTGRES_DB":       "app",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/lib/postgresql/data"},
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:             "mongodb",
		Name:           "MongoDB",
		Description:    "NoSQL document database",
		Image:          "mongo",
		Versions:       []string{"7", "6", "5", "latest"},
		DefaultVersion: "7",
		HasAlpine:      false,
		Port:           27017,
		Env: map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": "admin",
			"MONGO_INITDB_ROOT_PASSWORD": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/data/db"},
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	{
		ID:             "redis",
		Name:           "Redis",
		Description:    "In-memory data structure store",
		Image:          "redis",
		Versions:       []string{"7", "6", "latest"},
		DefaultVersion: "7",
		HasAlpine:      true,
		Port:           6379,
		Env: map[string]string{
			"REDIS_PASSWORD": "changeme",
		},
		Command: []string{"redis-server", "--requirepass", "${REDIS_PASSWORD}", "--appendonly", "yes"},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/data"},
		},
		Category: "database",
		Icon:     "i-lucide-database",
	},
	// Admin Tools
	{
		ID:             "phpmyadmin",
		Name:           "phpMyAdmin",
		Description:    "Web-based MySQL administration tool",
		Image:          "phpmyadmin",
		Versions:       []string{"5", "latest"},
		DefaultVersion: "5",
		Port:           80,
		Env: map[string]string{
			"PMA_ARBITRARY": "1",
		},
		Category: "admin",
		Icon:     "i-lucide-settings",
	},
	{
		ID:             "adminer",
		Name:           "Adminer",
		Description:    "Database management in a single PHP file",
		Image:          "adminer",
		Versions:       []string{"4", "latest"},
		DefaultVersion: "4",
		Port:           8080,
		Env:            map[string]string{},
		Category:       "admin",
		Icon:           "i-lucide-settings",
	},
	{
		ID:             "pgadmin",
		Name:           "pgAdmin",
		Description:    "PostgreSQL administration and development platform",
		Image:          "dpage/pgadmin4",
		Versions:       []string{"8", "7", "latest"},
		DefaultVersion: "8",
		Port:           80,
		Env: map[string]string{
			"PGADMIN_DEFAULT_EMAIL":                     "admin@example.com",
			"PGADMIN_DEFAULT_PASSWORD":                  "changeme",
			"PGADMIN_CONFIG_PROXY_X_FOR_COUNT":          "1",
			"PGADMIN_CONFIG_PROXY_X_PROTO_COUNT":        "1",
			"PGADMIN_CONFIG_PROXY_X_HOST_COUNT":         "1",
			"PGADMIN_CONFIG_PROXY_X_PORT_COUNT":         "1",
			"PGADMIN_CONFIG_PROXY_X_PREFIX_COUNT":       "1",
			"PGADMIN_CONFIG_WTF_CSRF_SSL_STRICT":        "False",
			"PGADMIN_CONFIG_ENHANCED_COOKIE_PROTECTION": "False",
		},
		Category: "admin",
		Icon:     "i-lucide-settings",
	},
	// CMS / Apps
	{
		ID:             "ghost",
		Name:           "Ghost",
		Description:    "Professional publishing platform",
		Image:          "ghost",
		Versions:       []string{"5", "4", "latest"},
		DefaultVersion: "5",
		HasAlpine:      true,
		Port:           2368,
		Env: map[string]string{
			"database__client":               "sqlite3",
			"database__connection__filename": "/var/lib/ghost/content/data/ghost.db",
			"url":                            "http://localhost:2368",
		},
		Volumes: []VolumeConfig{
			{Name: "content", ContainerPath: "/var/lib/ghost/content"},
		},
		Category: "cms",
		Icon:     "i-lucide-file-text",
	},
	{
		ID:          "strapi",
		Name:        "Strapi",
		Description: "Headless CMS to easily build APIs (amd64 only)",
		Image:       "strapi/strapi",
		Versions:    []string{"latest"},
		Port:        1337,
		Env:         map[string]string{},
		Category:    "cms",
		Icon:        "i-lucide-file-text",
		Arch:        []string{"amd64"}, // No official ARM64 image
	},
	{
		ID:             "nextcloud",
		Name:           "Nextcloud",
		Description:    "Self-hosted file sync and sharing",
		Image:          "nextcloud",
		Versions:       []string{"30", "29", "28", "latest"},
		DefaultVersion: "30",
		Port:           80,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/www/html"},
		},
		Category: "cms",
		Icon:     "i-lucide-cloud",
	},
	{
		ID:             "directus",
		Name:           "Directus",
		Description:    "Modern headless CMS with REST and GraphQL APIs",
		Image:          "directus/directus",
		Versions:       []string{"11", "10", "latest"},
		DefaultVersion: "11",
		Port:           8055,
		Env: map[string]string{
			"KEY":            "replace-with-random-key",
			"SECRET":         "replace-with-random-secret",
			"ADMIN_EMAIL":    "admin@example.com",
			"ADMIN_PASSWORD": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "uploads", ContainerPath: "/directus/uploads"},
			{Name: "database", ContainerPath: "/directus/database"},
		},
		Category: "cms",
		Icon:     "i-lucide-database",
	},
	{
		ID:             "drupal",
		Name:           "Drupal",
		Description:    "Enterprise-grade CMS (requires MySQL/PostgreSQL)",
		Image:          "drupal",
		Versions:       []string{"11", "10", "9", "latest"},
		DefaultVersion: "11",
		Port:           80,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "modules", ContainerPath: "/var/www/html/modules"},
			{Name: "themes", ContainerPath: "/var/www/html/themes"},
			{Name: "sites", ContainerPath: "/var/www/html/sites"},
		},
		Category: "cms",
		Icon:     "i-lucide-file-text",
	},
	{
		ID:             "mediawiki",
		Name:           "MediaWiki",
		Description:    "Wiki software powering Wikipedia",
		Image:          "mediawiki",
		Versions:       []string{"1.43", "1.42", "1.41", "latest"},
		DefaultVersion: "1.43",
		Port:           80,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "images", ContainerPath: "/var/www/html/images"},
		},
		Category: "cms",
		Icon:     "i-lucide-book-open",
	},
	{
		ID:          "pocketbase",
		Name:        "PocketBase",
		Description: "Open source backend in a single file",
		Image:       "ghcr.io/muchobien/pocketbase",
		Versions:    []string{"latest"},
		Port:        8080,
		Env:         map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/pb_data"},
		},
		Category: "cms",
		Icon:     "i-lucide-pocket",
	},
	// Dev Tools
	{
		ID:             "gitea",
		Name:           "Gitea",
		Description:    "Lightweight self-hosted Git service",
		Image:          "gitea/gitea",
		Versions:       []string{"1.22", "1.21", "latest"},
		DefaultVersion: "1.22",
		Port:           3000,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/data"},
		},
		Category: "devtools",
		Icon:     "i-lucide-git-branch",
	},
	{
		ID:             "uptime-kuma",
		Name:           "Uptime Kuma",
		Description:    "Self-hosted monitoring tool",
		Image:          "louislam/uptime-kuma",
		Versions:       []string{"1", "latest"},
		DefaultVersion: "1",
		Port:           3001,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/app/data"},
		},
		Category: "devtools",
		Icon:     "i-lucide-activity",
	},
	// Communication
	{
		ID:          "mattermost",
		Name:        "Mattermost",
		Description: "Open-source Slack alternative (amd64 only)",
		Image:       "mattermost/mattermost-team-edition",
		Versions:    []string{"latest"},
		Port:        8065,
		Env:         map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/mattermost/data"},
			{Name: "config", ContainerPath: "/mattermost/config"},
		},
		Category: "communication",
		Icon:     "i-lucide-message-circle",
		Arch:     []string{"amd64"}, // No ARM64 image available
	},
	{
		ID:             "n8n",
		Name:           "n8n",
		Description:    "Workflow automation tool",
		Image:          "n8nio/n8n",
		Versions:       []string{"1", "latest"},
		DefaultVersion: "1",
		Port:           5678,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/home/node/.n8n"},
		},
		Category: "automation",
		Icon:     "i-lucide-workflow",
	},
	// Analytics & Monitoring
	{
		ID:             "grafana",
		Name:           "Grafana",
		Description:    "Open-source analytics and monitoring",
		Image:          "grafana/grafana",
		Versions:       []string{"11", "10", "latest"},
		DefaultVersion: "11",
		Port:           3000,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/lib/grafana"},
		},
		Category: "analytics",
		Icon:     "i-lucide-bar-chart",
	},
	// File Storage
	{
		ID:          "minio",
		Name:        "MinIO",
		Description: "S3-compatible object storage",
		Image:       "minio/minio",
		Versions:    []string{"latest"},
		Port:        9000,
		Env: map[string]string{
			"MINIO_ROOT_USER":     "admin",
			"MINIO_ROOT_PASSWORD": "changeme123",
		},
		Command: []string{"server", "/data", "--console-address", ":9001"},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/data"},
		},
		Category: "storage",
		Icon:     "i-lucide-hard-drive",
	},
	{
		ID:          "filebrowser",
		Name:        "File Browser",
		Description: "Web-based file manager",
		Image:       "filebrowser/filebrowser",
		Versions:    []string{"latest", "v2"},
		Port:        80,
		Env:         map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/srv"},
			{Name: "db", ContainerPath: "/database"},
		},
		Category: "storage",
		Icon:     "i-lucide-folder",
	},
	// Code Server
	{
		ID:             "code-server",
		Name:           "Code Server",
		Description:    "VS Code in the browser",
		Image:          "codercom/code-server",
		Versions:       []string{"4.95", "4.94", "latest"},
		DefaultVersion: "4.95",
		Port:           8080,
		Env: map[string]string{
			"PASSWORD":             "changeme",
			"CS_DISABLE_TELEMETRY": "true",
		},
		Command: []string{"--bind-addr", "0.0.0.0:8080", "--disable-telemetry"},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/home/coder"},
		},
		Category: "devtools",
		Icon:     "i-lucide-code",
	},
	// Business Tools
	{
		ID:          "nocodb",
		Name:        "NocoDB",
		Description: "Open-source Airtable alternative",
		Image:       "nocodb/nocodb",
		Versions:    []string{"latest"},
		Port:        8080,
		Env: map[string]string{
			"NC_DB": "sqlite:///data/noco.db",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/usr/app/data"},
		},
		Category: "business",
		Icon:     "i-lucide-table",
	},
	{
		ID:          "listmonk",
		Name:        "Listmonk",
		Description: "Self-hosted newsletter and mailing list manager",
		Image:       "listmonk/listmonk",
		Versions:    []string{"latest"},
		Port:        9000,
		Env: map[string]string{
			"TZ": "UTC",
		},
		Command: []string{"./listmonk", "--static-dir=/listmonk/static"},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/listmonk/uploads"},
		},
		Category: "business",
		Icon:     "i-lucide-mail",
	},
	// AI / Machine Learning
	{
		ID:          "ollama",
		Name:        "Ollama",
		Description: "Run LLMs locally (Llama, Mistral, etc.)",
		Image:       "ollama/ollama",
		Versions:    []string{"latest"},
		Port:        11434,
		Env:         map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "models", ContainerPath: "/root/.ollama"},
		},
		Category: "ai",
		Icon:     "i-lucide-brain",
	},
	{
		ID:          "flowise",
		Name:        "Flowise",
		Description: "Build LLM apps with drag-and-drop UI",
		Image:       "flowiseai/flowise",
		Versions:    []string{"latest"},
		Port:        3000,
		Env: map[string]string{
			"FLOWISE_USERNAME": "admin",
			"FLOWISE_PASSWORD": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/root/.flowise"},
		},
		Category: "ai",
		Icon:     "i-lucide-workflow",
	},
	// Security
	{
		ID:             "vaultwarden",
		Name:           "Vaultwarden",
		Description:    "Lightweight Bitwarden-compatible password manager",
		Image:          "vaultwarden/server",
		Versions:       []string{"latest"},
		DefaultVersion: "latest",
		Port:           80,
		Env: map[string]string{
			"ADMIN_TOKEN": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/data"},
		},
		Category: "security",
		Icon:     "i-lucide-lock",
	},
	// Media
	{
		ID:             "jellyfin",
		Name:           "Jellyfin",
		Description:    "Free media streaming server (Plex alternative)",
		Image:          "jellyfin/jellyfin",
		Versions:       []string{"latest"},
		DefaultVersion: "latest",
		Port:           8096,
		Env:            map[string]string{},
		Volumes: []VolumeConfig{
			{Name: "config", ContainerPath: "/config"},
			{Name: "cache", ContainerPath: "/cache"},
			{Name: "media", ContainerPath: "/media"},
		},
		Category: "media",
		Icon:     "i-lucide-play-circle",
	},
	// Search
	{
		ID:             "meilisearch",
		Name:           "Meilisearch",
		Description:    "Lightning-fast search engine",
		Image:          "getmeili/meilisearch",
		Versions:       []string{"v1.12", "v1.11", "latest"},
		DefaultVersion: "v1.12",
		Port:           7700,
		Env: map[string]string{
			"MEILI_MASTER_KEY": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/meili_data"},
		},
		Category: "search",
		Icon:     "i-lucide-search",
	},
	// Messaging
	{
		ID:             "rabbitmq",
		Name:           "RabbitMQ",
		Description:    "Message broker with management UI",
		Image:          "rabbitmq",
		Versions:       []string{"3-management", "3", "latest"},
		DefaultVersion: "3-management",
		Port:           15672,
		Env: map[string]string{
			"RABBITMQ_DEFAULT_USER": "admin",
			"RABBITMQ_DEFAULT_PASS": "changeme",
		},
		Volumes: []VolumeConfig{
			{Name: "data", ContainerPath: "/var/lib/rabbitmq"},
		},
		Category: "messaging",
		Icon:     "i-lucide-mail",
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

// GetTemplateByImage returns a template by its base image name
func GetTemplateByImage(image string) *Template {
	for _, t := range Templates {
		if t.Image == image {
			return &t
		}
	}
	return nil
}
