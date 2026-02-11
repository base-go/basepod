// Package podman provides an OS-agnostic client for interacting with Podman.
package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/base-go/basepod/internal/config"
)

// Client is the interface for Podman operations
type Client interface {
	// Health check
	Ping(ctx context.Context) error

	// Container operations
	CreateContainer(ctx context.Context, opts CreateContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout int) error
	RemoveContainer(ctx context.Context, id string, force bool) error
	ListContainers(ctx context.Context, all bool) ([]Container, error)
	InspectContainer(ctx context.Context, id string) (*ContainerInspect, error)
	ContainerLogs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error)

	// Image operations
	PullImage(ctx context.Context, image string) error
	BuildImage(ctx context.Context, opts BuildOpts) (string, error)
	ListImages(ctx context.Context) ([]Image, error)
	RemoveImage(ctx context.Context, id string, force bool) error

	// Network operations
	CreateNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	ListNetworks(ctx context.Context) ([]Network, error)

	// Volume operations
	CreateVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string, force bool) error
	ListVolumes(ctx context.Context) ([]Volume, error)

	// Exec operations
	ExecCreate(ctx context.Context, containerID string, cmd []string) (string, error)
	ExecCreateDetached(ctx context.Context, containerID string, cmd []string) (string, error)
	ExecStart(ctx context.Context, execID string) (string, error)
	ExecResize(ctx context.Context, execID string, height, width int) error

	// Stats
	ContainerStats(ctx context.Context, id string) (*ContainerStatsResult, error)

	// Access underlying HTTP client (for raw hijack)
	GetHTTPClient() *http.Client
	GetBaseURL() string
	GetSocketPath() string
}

// ContainerStatsResult holds resource usage stats for a container
type ContainerStatsResult struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   int64   `json:"mem_usage"`   // bytes
	MemLimit   int64   `json:"mem_limit"`   // bytes
	NetInput   int64   `json:"net_input"`   // bytes
	NetOutput  int64   `json:"net_output"`  // bytes
}

// CreateContainerOpts holds options for creating a container
type CreateContainerOpts struct {
	Name           string
	Image          string
	Env            map[string]string
	Ports          map[string]string // container:host
	ExposeExternal bool              // If true, bind to 0.0.0.0; if false, bind to 127.0.0.1
	Volumes        []string          // host:container or volume:container
	Networks       []string
	Command        []string
	WorkingDir     string
	Labels         map[string]string
	Memory         int64 // Memory limit in bytes
	CPUs           float64
}

// FlexibleTime handles Podman's Created field which can be int64 or string
type FlexibleTime int64

func (f *FlexibleTime) UnmarshalJSON(data []byte) error {
	// Try int64 first
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexibleTime(i)
		return nil
	}
	// Try string (ISO format or Unix timestamp string)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		// Try parsing as Unix timestamp string
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			*f = FlexibleTime(i)
			return nil
		}
		// Try parsing as ISO 8601 time
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			*f = FlexibleTime(t.Unix())
			return nil
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			*f = FlexibleTime(t.Unix())
			return nil
		}
	}
	*f = 0
	return nil
}

// Container represents a Podman container
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created FlexibleTime      `json:"Created"`
	Ports   []PortMapping     `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
}

// PortMapping represents a port mapping
type PortMapping struct {
	HostIP        string `json:"host_ip"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Protocol      string `json:"protocol"`
}

// ContainerInspect holds detailed container information
type ContainerInspect struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Created string `json:"Created"`
	State   struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		OOMKilled  bool   `json:"OOMKilled"`
		Dead       bool   `json:"Dead"`
		Pid        int    `json:"Pid"`
		ExitCode   int    `json:"ExitCode"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
	Config struct {
		Env        []string          `json:"Env"`
		Cmd        []string          `json:"Cmd"`
		Image      string            `json:"Image"`
		WorkingDir string            `json:"WorkingDir"`
		Labels     map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		IPAddress string                    `json:"IPAddress"`
		Ports     map[string][]PortBinding  `json:"Ports"`
		Networks  map[string]NetworkSetting `json:"Networks"`
	} `json:"NetworkSettings"`
}

// PortBinding represents a port binding
type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// NetworkSetting represents network settings for a container
type NetworkSetting struct {
	NetworkID string `json:"NetworkID"`
	IPAddress string `json:"IPAddress"`
	Gateway   string `json:"Gateway"`
}

// LogOpts holds options for fetching container logs
type LogOpts struct {
	Follow     bool
	Tail       string
	Since      string
	Timestamps bool
	Stdout     bool
	Stderr     bool
}

// BuildOpts holds options for building an image
type BuildOpts struct {
	ContextDir string
	Dockerfile string
	Tags       []string
	BuildArgs  map[string]string
	NoCache    bool
}

// Image represents a Podman image
type Image struct {
	ID          string       `json:"Id"`
	RepoTags    []string     `json:"RepoTags"`
	RepoDigests []string     `json:"RepoDigests"`
	Created     FlexibleTime `json:"Created"`
	Size        int64        `json:"Size"`
}

// Network represents a Podman network
type Network struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Driver  string `json:"driver"`
	Created string `json:"created"`
}

// Volume represents a Podman volume
type Volume struct {
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Mountpoint string `json:"Mountpoint"`
	CreatedAt  string `json:"CreatedAt"`
}

// client implements the Client interface
type client struct {
	httpClient *http.Client
	baseURL    string
	socketPath string
}

// NewClient creates a new Podman client
func NewClient() (Client, error) {
	socketPath := config.GetPodmanSocket()
	if socketPath == "" {
		return nil, fmt.Errorf("could not determine Podman socket path")
	}

	return NewClientWithSocket(socketPath)
}

// NewClientWithSocket creates a new Podman client with a specific socket path
func NewClientWithSocket(socketPath string) (Client, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}

	return &client{
		httpClient: httpClient,
		baseURL:    "http://d/v4.0.0/libpod", // Podman API version
		socketPath: socketPath,
	}, nil
}

// request makes an HTTP request to the Podman API
func (c *client) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// Ping checks if Podman is accessible
func (c *client) Ping(ctx context.Context) error {
	resp, err := c.request(ctx, "GET", "/_ping", nil)
	if err != nil {
		return fmt.Errorf("failed to ping Podman: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Podman ping failed with status: %d", resp.StatusCode)
	}

	return nil
}

// CreateContainer creates a new container
func (c *client) CreateContainer(ctx context.Context, opts CreateContainerOpts) (string, error) {
	// Convert port mappings
	// If ExposeExternal is true, bind to 0.0.0.0 (public), otherwise 127.0.0.1 (localhost only)
	hostIP := "127.0.0.1"
	if opts.ExposeExternal {
		hostIP = "0.0.0.0"
	}
	portMappings := make([]map[string]interface{}, 0)
	for containerPort, hostPort := range opts.Ports {
		cPort, _ := strconv.Atoi(containerPort)
		hPort, _ := strconv.Atoi(hostPort)
		portMappings = append(portMappings, map[string]interface{}{
			"container_port": cPort,
			"host_port":      hPort,
			"host_ip":        hostIP,
		})
	}

	// Convert volume strings to mount objects
	// Named volumes use "volumes" field, bind mounts use "mounts" field
	mounts := make([]map[string]interface{}, 0)
	volumes := make([]map[string]interface{}, 0)
	for _, vol := range opts.Volumes {
		parts := strings.Split(vol, ":")
		if len(parts) >= 2 {
			source := parts[0]
			destination := parts[1]

			// Check if source looks like a path (starts with / or .)
			// If so, use bind mount; otherwise use named volume
			if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") {
				// Bind mount - source is a host path
				mounts = append(mounts, map[string]interface{}{
					"destination": destination,
					"source":      source,
					"type":        "bind",
					"options":     []string{"rbind"},
				})
			} else {
				// Named volume - use "volumes" field with dest/name format
				_ = c.CreateVolume(ctx, source) // Ignore error if already exists
				volumes = append(volumes, map[string]interface{}{
					"dest": destination,
					"name": source,
				})
			}
		}
	}

	spec := map[string]interface{}{
		"name":         opts.Name,
		"image":        opts.Image,
		"env":          opts.Env,
		"portmappings": portMappings,
		"netns":        map[string]interface{}{"nsmode": "bridge"},
		"command":      opts.Command,
		"working_dir":  opts.WorkingDir,
		"labels":       opts.Labels,
	}

	// Only add mounts if there are any
	if len(mounts) > 0 {
		spec["mounts"] = mounts
	}

	// Only add volumes if there are any
	if len(volumes) > 0 {
		spec["volumes"] = volumes
	}

	if len(opts.Networks) > 0 {
		// Podman API expects networks as a map, not an array
		networksMap := make(map[string]interface{})
		for _, network := range opts.Networks {
			networksMap[network] = map[string]interface{}{}
		}
		spec["networks"] = networksMap
	}

	if opts.Memory > 0 || opts.CPUs > 0 {
		resourceLimits := map[string]interface{}{}
		if opts.Memory > 0 {
			resourceLimits["memory"] = map[string]interface{}{
				"limit": opts.Memory,
			}
		}
		if opts.CPUs > 0 {
			// Convert CPUs to CPU period/quota: 0.5 CPUs = 50000 quota / 100000 period
			cpuQuota := int64(opts.CPUs * 100000)
			resourceLimits["cpu"] = map[string]interface{}{
				"quota":  cpuQuota,
				"period": 100000,
			}
		}
		spec["resource_limits"] = resourceLimits
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal container spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", "/containers/create", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create container (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.ID, nil
}

// StartContainer starts a container
func (c *client) StartContainer(ctx context.Context, id string) error {
	resp, err := c.request(ctx, "POST", fmt.Sprintf("/containers/%s/start", id), nil)
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to start container (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// StopContainer stops a container
func (c *client) StopContainer(ctx context.Context, id string, timeout int) error {
	path := fmt.Sprintf("/containers/%s/stop?timeout=%d", id, timeout)
	resp, err := c.request(ctx, "POST", path, nil)
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to stop container (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// RemoveContainer removes a container
func (c *client) RemoveContainer(ctx context.Context, id string, force bool) error {
	path := fmt.Sprintf("/containers/%s?force=%t", id, force)
	resp, err := c.request(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove container (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ListContainers lists all containers
func (c *client) ListContainers(ctx context.Context, all bool) ([]Container, error) {
	path := fmt.Sprintf("/containers/json?all=%t", all)
	resp, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list containers (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var containers []Container
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("failed to decode containers: %w", err)
	}

	return containers, nil
}

// InspectContainer inspects a container
func (c *client) InspectContainer(ctx context.Context, id string) (*ContainerInspect, error) {
	resp, err := c.request(ctx, "GET", fmt.Sprintf("/containers/%s/json", id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to inspect container (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var inspect ContainerInspect
	if err := json.NewDecoder(resp.Body).Decode(&inspect); err != nil {
		return nil, fmt.Errorf("failed to decode container inspect: %w", err)
	}

	return &inspect, nil
}

// ContainerLogs fetches container logs
func (c *client) ContainerLogs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=%t&stderr=%t&follow=%t&timestamps=%t",
		id, opts.Stdout, opts.Stderr, opts.Follow, opts.Timestamps)

	if opts.Tail != "" {
		path += "&tail=" + opts.Tail
	}
	if opts.Since != "" {
		path += "&since=" + opts.Since
	}

	resp, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to get container logs (status %d)", resp.StatusCode)
	}

	return resp.Body, nil
}

// PullImage pulls an image from a registry
func (c *client) PullImage(ctx context.Context, image string) error {
	path := fmt.Sprintf("/images/pull?reference=%s", image)
	resp, err := c.request(ctx, "POST", path, nil)
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to pull image (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Consume the response body (streaming)
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// BuildImage builds an image from a Dockerfile
func (c *client) BuildImage(ctx context.Context, opts BuildOpts) (string, error) {
	// TODO: Implement image building with tar context
	return "", fmt.Errorf("BuildImage not yet implemented")
}

// ListImages lists all images
func (c *client) ListImages(ctx context.Context) ([]Image, error) {
	resp, err := c.request(ctx, "GET", "/images/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list images (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var images []Image
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, fmt.Errorf("failed to decode images: %w", err)
	}

	return images, nil
}

// RemoveImage removes an image
func (c *client) RemoveImage(ctx context.Context, id string, force bool) error {
	path := fmt.Sprintf("/images/%s?force=%t", id, force)
	resp, err := c.request(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove image (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// CreateNetwork creates a new network
func (c *client) CreateNetwork(ctx context.Context, name string) error {
	spec := map[string]interface{}{
		"name":   name,
		"driver": "bridge",
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal network spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", "/networks/create", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create network (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// RemoveNetwork removes a network
func (c *client) RemoveNetwork(ctx context.Context, name string) error {
	resp, err := c.request(ctx, "DELETE", fmt.Sprintf("/networks/%s", name), nil)
	if err != nil {
		return fmt.Errorf("failed to remove network: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove network (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ListNetworks lists all networks
func (c *client) ListNetworks(ctx context.Context) ([]Network, error) {
	resp, err := c.request(ctx, "GET", "/networks/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list networks (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var networks []Network
	if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
		return nil, fmt.Errorf("failed to decode networks: %w", err)
	}

	return networks, nil
}

// CreateVolume creates a new volume
func (c *client) CreateVolume(ctx context.Context, name string) error {
	spec := map[string]interface{}{
		"name": name,
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal volume spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", "/volumes/create", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create volume: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create volume (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// RemoveVolume removes a volume
func (c *client) RemoveVolume(ctx context.Context, name string, force bool) error {
	path := fmt.Sprintf("/volumes/%s?force=%t", name, force)
	resp, err := c.request(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove volume: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove volume (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ListVolumes lists all volumes
func (c *client) ListVolumes(ctx context.Context) ([]Volume, error) {
	resp, err := c.request(ctx, "GET", "/volumes/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list volumes (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var volumes []Volume
	if err := json.NewDecoder(resp.Body).Decode(&volumes); err != nil {
		return nil, fmt.Errorf("failed to decode volumes: %w", err)
	}

	return volumes, nil
}

// ExecCreate creates an exec session in a container
func (c *client) ExecCreate(ctx context.Context, containerID string, cmd []string) (string, error) {
	spec := map[string]interface{}{
		"Cmd":          cmd,
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          true,
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal exec spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", fmt.Sprintf("/containers/%s/exec", containerID), strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create exec (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode exec response: %w", err)
	}

	return result.ID, nil
}

// ExecCreateDetached creates an exec session without TTY (for capturing output)
func (c *client) ExecCreateDetached(ctx context.Context, containerID string, cmd []string) (string, error) {
	spec := map[string]interface{}{
		"Cmd":          cmd,
		"AttachStdin":  false,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
	}

	body, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal exec spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", fmt.Sprintf("/containers/%s/exec", containerID), strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create exec (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode exec response: %w", err)
	}

	return result.ID, nil
}

// ExecStart starts an exec session and returns the output
func (c *client) ExecStart(ctx context.Context, execID string) (string, error) {
	startSpec := map[string]interface{}{
		"Detach": false,
		"Tty":    false,
	}

	body, err := json.Marshal(startSpec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal exec start spec: %w", err)
	}

	resp, err := c.request(ctx, "POST", fmt.Sprintf("/exec/%s/start", execID), strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to start exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to start exec (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	output, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	return string(output), nil
}

// ExecResize resizes the TTY of an exec session
func (c *client) ExecResize(ctx context.Context, execID string, height, width int) error {
	path := fmt.Sprintf("/exec/%s/resize?h=%d&w=%d", execID, height, width)
	resp, err := c.request(ctx, "POST", path, nil)
	if err != nil {
		return fmt.Errorf("failed to resize exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to resize exec (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// ContainerStats returns resource usage stats for a container
func (c *client) ContainerStats(ctx context.Context, id string) (*ContainerStatsResult, error) {
	resp, err := c.request(ctx, "GET", fmt.Sprintf("/containers/%s/stats?stream=false", id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get stats (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var rawStats struct {
		Stats []struct {
			CPU        float64 `json:"cpu_percent"`
			MemUsage   int64   `json:"mem_usage"`
			MemLimit   int64   `json:"mem_limit"`
			NetInput   int64   `json:"net_input"`
			NetOutput  int64   `json:"net_output"`
		} `json:"stats"`
		CPUPercent float64 `json:"cpu_percent"`
		MemUsage   int64   `json:"mem_usage"`
		MemLimit   int64   `json:"mem_limit"`
		NetInput   int64   `json:"net_input"`
		NetOutput  int64   `json:"net_output"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rawStats); err != nil {
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}

	result := &ContainerStatsResult{
		CPUPercent: rawStats.CPUPercent,
		MemUsage:   rawStats.MemUsage,
		MemLimit:   rawStats.MemLimit,
		NetInput:   rawStats.NetInput,
		NetOutput:  rawStats.NetOutput,
	}

	// Podman sometimes returns stats in a stats array
	if len(rawStats.Stats) > 0 {
		s := rawStats.Stats[0]
		result.CPUPercent = s.CPU
		result.MemUsage = s.MemUsage
		result.MemLimit = s.MemLimit
		result.NetInput = s.NetInput
		result.NetOutput = s.NetOutput
	}

	return result, nil
}

// GetHTTPClient returns the underlying HTTP client
func (c *client) GetHTTPClient() *http.Client {
	return c.httpClient
}

// GetBaseURL returns the base URL for the Podman API
func (c *client) GetBaseURL() string {
	return c.baseURL
}

// GetSocketPath returns the socket path
func (c *client) GetSocketPath() string {
	return c.socketPath
}
