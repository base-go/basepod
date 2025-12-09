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

	"github.com/deployer/deployer/internal/config"
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
}

// CreateContainerOpts holds options for creating a container
type CreateContainerOpts struct {
	Name       string
	Image      string
	Env        map[string]string
	Ports      map[string]string // container:host
	Volumes    []string          // host:container or volume:container
	Networks   []string
	Command    []string
	WorkingDir string
	Labels     map[string]string
	Memory     int64 // Memory limit in bytes
	CPUs       float64
}

// Container represents a Podman container
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
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
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     int64    `json:"Created"`
	Size        int64    `json:"Size"`
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
	portMappings := make([]map[string]interface{}, 0)
	for containerPort, hostPort := range opts.Ports {
		cPort, _ := strconv.Atoi(containerPort)
		hPort, _ := strconv.Atoi(hostPort)
		portMappings = append(portMappings, map[string]interface{}{
			"container_port": cPort,
			"host_port":      hPort,
		})
	}

	spec := map[string]interface{}{
		"name":         opts.Name,
		"image":        opts.Image,
		"env":          opts.Env,
		"portmappings": portMappings,
		"volumes":      opts.Volumes,
		"netns":        map[string]interface{}{"nsmode": "bridge"},
		"command":      opts.Command,
		"working_dir":  opts.WorkingDir,
		"labels":       opts.Labels,
	}

	if len(opts.Networks) > 0 {
		spec["networks"] = opts.Networks
	}

	if opts.Memory > 0 {
		spec["resource_limits"] = map[string]interface{}{
			"memory": map[string]interface{}{
				"limit": opts.Memory,
			},
		}
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
