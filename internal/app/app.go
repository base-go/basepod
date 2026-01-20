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
)

// App represents a deployed application
type App struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        AppType           `json:"type"`        // container or mlx
	Domain      string            `json:"domain"`      // e.g., myapp.basepod.example.com
	ContainerID string            `json:"container_id"`
	Image       string            `json:"image"`
	Status      AppStatus         `json:"status"`
	Env         map[string]string `json:"env"`
	Ports       PortConfig        `json:"ports"`
	Volumes     []VolumeMount     `json:"volumes"`
	Resources   ResourceConfig    `json:"resources"`
	Deployment  DeploymentConfig  `json:"deployment"`
	SSL         SSLConfig         `json:"ssl"`
	MLX         *MLXConfig        `json:"mlx,omitempty"` // MLX LLM configuration
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
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
	Source       DeploymentSource `json:"source"`
	Dockerfile   string           `json:"dockerfile"`    // Path to Dockerfile (default: Dockerfile)
	BuildContext string           `json:"build_context"` // Build context path (default: .)
	Branch       string           `json:"branch"`        // Git branch
	AutoDeploy   bool             `json:"auto_deploy"`   // Deploy on git push
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
	Image          *string            `json:"image,omitempty"`
	Env            *map[string]string `json:"env,omitempty"`
	Port           *int               `json:"port,omitempty"`
	Memory         *int64             `json:"memory,omitempty"`
	CPUs           *float64           `json:"cpus,omitempty"`
	EnableSSL      *bool              `json:"enable_ssl,omitempty"`
	ExposeExternal *bool              `json:"expose_external,omitempty"`
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
