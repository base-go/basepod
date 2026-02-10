// Package app provides application management for basepod.
package app

import (
	"time"
)

// AppType represents the type of application
type AppType string

const (
	AppTypeContainer AppType = "container" // Default: runs in Podman container
	AppTypeMLX       AppType = "mlx"       // MLX LLM: runs natively with Metal acceleration
	AppTypeStatic    AppType = "static"    // Static site: served directly by Caddy
)

// App represents a deployed application
type App struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Type        AppType            `json:"type"`        // container or mlx
	Domain      string             `json:"domain"`      // e.g., myapp.basepod.example.com
	Aliases     []string           `json:"aliases"`     // Additional domains (e.g., ["duxt.dev", "blog.example.com"])
	ContainerID string             `json:"container_id"`
	Image       string             `json:"image"`
	Status      AppStatus          `json:"status"`
	Env         map[string]string  `json:"env"`
	Ports       PortConfig         `json:"ports"`
	Volumes     []VolumeMount      `json:"volumes"`
	Resources   ResourceConfig     `json:"resources"`
	Deployment  DeploymentConfig   `json:"deployment"`
	Deployments []DeploymentRecord `json:"deployments,omitempty"` // Deployment history
	SSL         SSLConfig          `json:"ssl"`
	MLX          *MLXConfig          `json:"mlx,omitempty"`          // MLX LLM configuration
	HealthCheck  *HealthCheckConfig  `json:"health_check,omitempty"` // Health check configuration
	Health       *HealthStatus       `json:"health,omitempty"`       // Runtime health status (not persisted)
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// DeploymentRecord represents a single deployment
type DeploymentRecord struct {
	ID         string    `json:"id"`
	Image      string    `json:"image,omitempty"`       // Docker image used for this deploy
	CommitHash string    `json:"commit_hash,omitempty"` // Git commit hash (short)
	CommitMsg  string    `json:"commit_msg,omitempty"`  // Git commit message (first line)
	Branch     string    `json:"branch,omitempty"`      // Git branch
	Status     string    `json:"status"`                // success, failed, building
	BuildLog   string    `json:"build_log,omitempty"`   // Build output log
	DeployedAt time.Time `json:"deployed_at"`
}

// MLXConfig holds MLX LLM configuration
type MLXConfig struct {
	Model       string `json:"model"`        // HuggingFace model ID (e.g., mlx-community/Llama-3.2-3B-Instruct-4bit)
	MaxTokens   int    `json:"max_tokens"`   // Max tokens for generation (default: 4096)
	ContextSize int    `json:"context_size"` // Context window size (default: 8192)
	Temperature float64 `json:"temperature"` // Default temperature (default: 0.7)
	VenvPath    string `json:"venv_path"`    // Path to Python venv
	PID         int    `json:"pid"`          // Process ID when running
}

// HealthCheckConfig holds health check configuration for an app
type HealthCheckConfig struct {
	Endpoint    string `json:"endpoint"`     // e.g. "/health" (default)
	Interval    int    `json:"interval"`     // seconds between checks (default: 30)
	Timeout     int    `json:"timeout"`      // seconds per check (default: 5)
	MaxFailures int    `json:"max_failures"` // consecutive failures before restart (default: 3)
	AutoRestart bool   `json:"auto_restart"` // restart on failure (default: true)
}

// HealthStatus holds runtime health check status (not persisted)
type HealthStatus struct {
	Status              string    `json:"status"`                         // "healthy", "unhealthy", "unknown"
	LastCheck           time.Time `json:"last_check"`
	LastSuccess         time.Time `json:"last_success"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastError           string    `json:"last_error,omitempty"`
	TotalChecks         int       `json:"total_checks"`
	TotalFailures       int       `json:"total_failures"`
}

// AppStatus represents the current status of an app
type AppStatus string

const (
	StatusPending   AppStatus = "pending"
	StatusBuilding  AppStatus = "building"
	StatusDeploying AppStatus = "deploying"
	StatusRunning   AppStatus = "running"
	StatusStopped   AppStatus = "stopped"
	StatusFailed    AppStatus = "failed"
)

// PortConfig holds port configuration
type PortConfig struct {
	ContainerPort  int    `json:"container_port"`  // Port the app listens on inside container
	HostPort       int    `json:"host_port"`       // Port exposed on the host
	Protocol       string `json:"protocol"`        // http, https, tcp
	ExposeExternal bool   `json:"expose_external"` // Whether to expose port externally (default: false)
}

// VolumeMount represents a volume mount
type VolumeMount struct {
	Name          string `json:"name"`           // Volume name
	HostPath      string `json:"host_path"`      // Path on host
	ContainerPath string `json:"container_path"` // Path inside container
	ReadOnly      bool   `json:"read_only"`
}

// ResourceConfig holds resource limits
type ResourceConfig struct {
	Memory   int64   `json:"memory"`    // Memory limit in MB
	CPUs     float64 `json:"cpus"`      // CPU limit (e.g., 0.5 = half a core)
	Replicas int     `json:"replicas"`  // Number of replicas (future: for scaling)
}

// DeploymentConfig holds deployment settings
type DeploymentConfig struct {
	Source        DeploymentSource `json:"source"`
	Dockerfile    string           `json:"dockerfile"`              // Path to Dockerfile (default: Dockerfile)
	BuildContext  string           `json:"build_context"`           // Build context path (default: .)
	Branch        string           `json:"branch"`                  // Git branch
	AutoDeploy    bool             `json:"auto_deploy"`             // Deploy on git push
	GitURL        string           `json:"git_url,omitempty"`       // Repository clone URL for webhooks
	WebhookSecret string           `json:"webhook_secret,omitempty"` // HMAC secret for webhook validation
}

// WebhookDelivery represents a single webhook delivery from GitHub
type WebhookDelivery struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Event     string    `json:"event"`            // "push", "ping"
	Branch    string    `json:"branch,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	Message   string    `json:"message,omitempty"`
	Status    string    `json:"status"`           // "success", "failed", "skipped", "deploying"
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DeploymentSource represents the source of the deployment
type DeploymentSource string

const (
	SourceGit        DeploymentSource = "git"
	SourceDockerfile DeploymentSource = "dockerfile"
	SourceImage      DeploymentSource = "image"
	SourceUpload     DeploymentSource = "upload"
)

// SSLConfig holds SSL/TLS configuration
type SSLConfig struct {
	Enabled     bool   `json:"enabled"`
	AutoRenew   bool   `json:"auto_renew"`
	Certificate string `json:"certificate,omitempty"` // Path or empty for auto
	Key         string `json:"key,omitempty"`         // Path or empty for auto
}

// CreateAppRequest represents a request to create a new app
type CreateAppRequest struct {
	Name      string            `json:"name"`
	Type      AppType           `json:"type,omitempty"`   // container (default) or mlx
	Domain    string            `json:"domain,omitempty"` // Auto-generated if empty
	Image     string            `json:"image,omitempty"`  // For image-based deployments
	Model     string            `json:"model,omitempty"`  // For MLX: HuggingFace model ID
	Env       map[string]string `json:"env,omitempty"`
	Port      int               `json:"port,omitempty"` // Container port (default: 8080)
	Memory    int64             `json:"memory,omitempty"`
	CPUs      float64           `json:"cpus,omitempty"`
	EnableSSL bool              `json:"enable_ssl"`
	Volumes   []VolumeMount     `json:"volumes,omitempty"` // Custom volume mounts
}

// UpdateAppRequest represents a request to update an app
type UpdateAppRequest struct {
	Name           *string            `json:"name,omitempty"`
	Domain         *string            `json:"domain,omitempty"`
	Aliases        *[]string          `json:"aliases,omitempty"` // Additional domains
	Image          *string            `json:"image,omitempty"`
	Env            *map[string]string `json:"env,omitempty"`
	Port           *int               `json:"port,omitempty"`
	Memory         *int64             `json:"memory,omitempty"`
	CPUs           *float64           `json:"cpus,omitempty"`
	EnableSSL      *bool              `json:"enable_ssl,omitempty"`
	ExposeExternal *bool               `json:"expose_external,omitempty"`
	Volumes        *[]VolumeMount      `json:"volumes,omitempty"`
	HealthCheck    *HealthCheckConfig   `json:"health_check,omitempty"`
	Deployment     *DeploymentConfig    `json:"deployment,omitempty"`
}

// DeployRequest represents a request to deploy an app
type DeployRequest struct {
	// For git deployments
	GitURL string `json:"git_url,omitempty"`
	Branch string `json:"branch,omitempty"`

	// For image deployments
	Image string `json:"image,omitempty"`

	// Build options
	Dockerfile   string            `json:"dockerfile,omitempty"`
	BuildContext string            `json:"build_context,omitempty"`
	BuildArgs    map[string]string `json:"build_args,omitempty"`
}

// CronJob represents a scheduled task for an app
type CronJob struct {
	ID         string     `json:"id"`
	AppID      string     `json:"app_id"`
	Name       string     `json:"name"`
	Schedule   string     `json:"schedule"`            // cron expression: "0 2 * * *"
	Command    string     `json:"command"`              // shell command to run in container
	Enabled    bool       `json:"enabled"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastStatus string     `json:"last_status,omitempty"` // "success", "failed", "running"
	LastError  string     `json:"last_error,omitempty"`
	NextRun    *time.Time `json:"next_run,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// CronExecution records a single cron job run
type CronExecution struct {
	ID        string     `json:"id"`
	CronJobID string     `json:"cron_job_id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Status    string     `json:"status"` // "success", "failed", "running"
	Output    string     `json:"output"`
	ExitCode  int        `json:"exit_code,omitempty"`
}

// ActivityLog represents an activity/audit log entry
type ActivityLog struct {
	ID         string    `json:"id"`
	ActorType  string    `json:"actor_type"`            // "user", "system", "webhook"
	Action     string    `json:"action"`                // "deploy", "restart", "config_update", etc.
	TargetType string    `json:"target_type,omitempty"` // "app", "system", "config"
	TargetID   string    `json:"target_id,omitempty"`
	TargetName string    `json:"target_name,omitempty"`
	Details    string    `json:"details,omitempty"` // JSON metadata
	Status     string    `json:"status,omitempty"`  // "success", "failed", "in_progress"
	IPAddress  string    `json:"ip_address,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NotificationConfig represents a notification hook configuration
type NotificationConfig struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Type            string   `json:"type"`    // "webhook", "slack", "discord"
	Enabled         bool     `json:"enabled"`
	Scope           string   `json:"scope"`              // "global" or app_id
	ScopeID         string   `json:"scope_id,omitempty"` // app_id if scope="app"
	WebhookURL      string   `json:"webhook_url,omitempty"`
	SlackWebhookURL string   `json:"slack_webhook_url,omitempty"`
	DiscordWebhook  string   `json:"discord_webhook_url,omitempty"`
	Events          []string `json:"events"` // ["deploy_success", "deploy_failed", "health_check_fail"]
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DeployToken represents a scoped API key for CI/CD
type DeployToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`                      // Never expose
	Prefix     string     `json:"prefix"`                 // First 8 chars for identification
	Scopes     []string   `json:"scopes"`                 // ["deploy:*", "deploy:app-123", "status"]
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// AppMetric represents a point-in-time resource usage metric for an app
type AppMetric struct {
	ID         int64     `json:"id"`
	AppID      string    `json:"app_id"`
	CPUPercent float64   `json:"cpu_percent"`
	MemUsage   int64     `json:"mem_usage"`
	MemLimit   int64     `json:"mem_limit"`
	NetInput   int64     `json:"net_input"`
	NetOutput  int64     `json:"net_output"`
	RecordedAt time.Time `json:"recorded_at"`
}

// AppListResponse represents a list of apps
type AppListResponse struct {
	Apps  []App `json:"apps"`
	Total int   `json:"total"`
}

// AppLog represents a log entry
type AppLog struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"` // stdout, stderr
	Message   string    `json:"message"`
}
