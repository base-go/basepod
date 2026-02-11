// Package ai provides the AI assistant engine for basepod.
// It uses FunctionGemma to parse natural language into basepod operations.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/base-go/basepod/internal/app"
	"github.com/base-go/basepod/internal/config"
	"github.com/base-go/basepod/internal/diskutil"
	"github.com/base-go/basepod/internal/mlx"
	"github.com/base-go/basepod/internal/podman"
	"github.com/base-go/basepod/internal/storage"
)

// AssistantFunc defines a function the AI can call.
type AssistantFunc struct {
	Name        string
	Description string
	Parameters  map[string]ParamDef
}

// ParamDef describes a function parameter.
type ParamDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// FunctionCall represents a parsed function call from the model.
type FunctionCall struct {
	Name       string                 `json:"name"`
	Parameters map[string]interface{} `json:"parameters"`
}

// AskResult is the response from the assistant.
type AskResult struct {
	Response string       `json:"response"`
	Action   *ActionInfo  `json:"action,omitempty"`
}

// ActionInfo describes what action was executed.
type ActionInfo struct {
	Function   string                 `json:"function"`
	Parameters map[string]interface{} `json:"parameters"`
	Success    bool                   `json:"success"`
}

// Caller represents the user making a request, used for access control.
type Caller struct {
	UserID   string // empty for legacy admin sessions
	UserRole string // "admin", "deployer", "viewer"
}

// Assistant is the AI assistant engine.
type Assistant struct {
	storage *storage.Storage
	podman  podman.Client
	client  *http.Client
	port    int // MLX assistant port
}

// The FunctionGemma model ID used for the assistant.
const AssistantModelID = "mlx-community/functiongemma-270m-it-4bit"

// Default assistant MLX port (separate from primary chat model).
const AssistantPort = 11435

// New creates a new Assistant instance.
func New(store *storage.Storage, pm podman.Client) *Assistant {
	return &Assistant{
		storage: store,
		podman:  pm,
		client:  &http.Client{Timeout: 60 * time.Second},
		port:    AssistantPort,
	}
}

// assistantFunctions defines all operations the assistant can perform.
var assistantFunctions = []AssistantFunc{
	{Name: "list_apps", Description: "List all deployed applications", Parameters: nil},
	{Name: "get_app", Description: "Get details of a specific app", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "start_app", Description: "Start a stopped application", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "stop_app", Description: "Stop a running application", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "restart_app", Description: "Restart an application", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "deploy_app", Description: "Deploy or redeploy an application", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "get_logs", Description: "Get recent logs for an app", Parameters: map[string]ParamDef{
		"name":  {Type: "string", Description: "app name"},
		"lines": {Type: "integer", Description: "number of log lines, default 50"},
	}},
	{Name: "create_app", Description: "Create a new application", Parameters: map[string]ParamDef{
		"name":  {Type: "string", Description: "app name"},
		"image": {Type: "string", Description: "container image"},
		"port":  {Type: "integer", Description: "container port"},
	}},
	{Name: "delete_app", Description: "Delete an application (requires confirmation)", Parameters: map[string]ParamDef{
		"name": {Type: "string", Description: "app name"},
	}},
	{Name: "storage_info", Description: "Show disk and storage usage overview", Parameters: nil},
	{Name: "system_info", Description: "Show system info including version, containers, and images", Parameters: nil},
	{Name: "list_models", Description: "List available LLM models", Parameters: nil},
	{Name: "prune_images", Description: "Clean up unused container images to free space", Parameters: nil},
}

// buildPrompt constructs the FunctionGemma prompt with function declarations.
func (a *Assistant) buildPrompt(userMessage string) string {
	var sb strings.Builder

	sb.WriteString("<start_of_turn>user\n")

	// Add function declarations
	for _, fn := range assistantFunctions {
		sb.WriteString("<start_function_declaration>\n")

		params := map[string]interface{}{}
		required := []string{}
		if fn.Parameters != nil {
			props := map[string]map[string]string{}
			for name, p := range fn.Parameters {
				props[name] = map[string]string{
					"type":        p.Type,
					"description": p.Description,
				}
				required = append(required, name)
			}
			params["type"] = "object"
			params["properties"] = props
			params["required"] = required
		} else {
			params["type"] = "object"
			params["properties"] = map[string]interface{}{}
		}

		decl := map[string]interface{}{
			"name":        fn.Name,
			"description": fn.Description,
			"parameters":  params,
		}
		data, _ := json.Marshal(decl)
		sb.Write(data)
		sb.WriteString("\n<end_function_declaration>\n")
	}

	sb.WriteString("\n")
	sb.WriteString(userMessage)
	sb.WriteString("\n<end_of_turn>\n")
	sb.WriteString("<start_of_turn>model\n")

	return sb.String()
}

// parseFunctionCall extracts a function call from the model response.
func parseFunctionCall(response string) (*FunctionCall, string) {
	startTag := "<start_function_call>"
	endTag := "<end_function_call>"

	startIdx := strings.Index(response, startTag)
	if startIdx == -1 {
		return nil, response
	}

	endIdx := strings.Index(response, endTag)
	if endIdx == -1 {
		return nil, response
	}

	jsonStr := strings.TrimSpace(response[startIdx+len(startTag) : endIdx])

	var call FunctionCall
	if err := json.Unmarshal([]byte(jsonStr), &call); err != nil {
		return nil, response
	}

	// Extract any text before the function call
	textBefore := strings.TrimSpace(response[:startIdx])
	return &call, textBefore
}

// EnsureRunning makes sure the assistant model is downloaded and running.
func (a *Assistant) EnsureRunning() error {
	svc := mlx.GetService()

	// Check if FunctionGemma is downloaded
	models := svc.ListModels()
	downloaded := false
	for _, m := range models {
		if m.ID == AssistantModelID && m.Downloaded {
			downloaded = true
			break
		}
	}

	if !downloaded {
		return fmt.Errorf("assistant model not downloaded. Download it first: the model %s (150MB) is required", AssistantModelID)
	}

	// Check if assistant is already running on its port
	if a.isAssistantRunning() {
		return nil
	}

	// Start the assistant model on the dedicated port
	return svc.RunOnPort(AssistantModelID, a.port)
}

// isAssistantRunning checks if the assistant MLX server is responding.
func (a *Assistant) isAssistantRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost:%d/v1/models", a.port)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := a.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Ask processes a natural language message and returns a result.
// The caller parameter enforces role-based and per-app access control.
func (a *Assistant) Ask(message string, caller *Caller) (*AskResult, error) {
	if err := a.EnsureRunning(); err != nil {
		return nil, err
	}

	// Build the prompt
	prompt := a.buildPrompt(message)

	// Call the MLX server
	modelResponse, err := a.callMLX(prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI response: %w", err)
	}

	// Parse the response for function calls
	call, textResponse := parseFunctionCall(modelResponse)

	if call == nil {
		// No function call — return text as-is (conversational response)
		resp := textResponse
		if resp == "" {
			resp = modelResponse
		}
		// Clean up any trailing special tokens
		resp = cleanResponse(resp)
		if resp == "" {
			resp = "I can help you manage your apps, check system status, view logs, and more. Try asking me to list your apps or check storage usage."
		}
		return &AskResult{Response: resp}, nil
	}

	// Execute the function with access control
	result, err := a.executeFunction(call, caller)
	if err != nil {
		return &AskResult{
			Response: fmt.Sprintf("Error: %s", err.Error()),
			Action: &ActionInfo{
				Function:   call.Name,
				Parameters: call.Parameters,
				Success:    false,
			},
		}, nil
	}

	return &AskResult{
		Response: result,
		Action: &ActionInfo{
			Function:   call.Name,
			Parameters: call.Parameters,
			Success:    true,
		},
	}, nil
}

// callMLX sends a prompt to the assistant's MLX server and returns the response.
func (a *Assistant) callMLX(prompt string) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", a.port)

	reqBody := map[string]interface{}{
		"model": AssistantModelID,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  512,
		"temperature": 0.1,
	}

	data, _ := json.Marshal(reqBody)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("MLX server not responding: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MLX server error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse MLX response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from model")
	}

	return result.Choices[0].Message.Content, nil
}

// executeFunction runs a parsed function call against basepod internals.
func (a *Assistant) executeFunction(call *FunctionCall, caller *Caller) (string, error) {
	// Write operations are blocked for viewers
	writeOps := map[string]bool{
		"start_app": true, "stop_app": true, "restart_app": true,
		"deploy_app": true, "create_app": true, "delete_app": true, "prune_images": true,
	}
	if caller != nil && caller.UserRole == "viewer" && writeOps[call.Name] {
		return "", fmt.Errorf("permission denied: viewers cannot perform %s", call.Name)
	}

	switch call.Name {
	case "list_apps":
		return a.execListApps(caller)
	case "get_app":
		return a.execGetApp(call.Parameters, caller)
	case "start_app":
		return a.execStartApp(call.Parameters, caller)
	case "stop_app":
		return a.execStopApp(call.Parameters, caller)
	case "restart_app":
		return a.execRestartApp(call.Parameters, caller)
	case "deploy_app":
		return a.execDeployApp(call.Parameters, caller)
	case "get_logs":
		return a.execGetLogs(call.Parameters, caller)
	case "create_app":
		return a.execCreateApp(call.Parameters)
	case "delete_app":
		return a.execDeleteApp(call.Parameters, caller)
	case "storage_info":
		return a.execStorageInfo()
	case "system_info":
		return a.execSystemInfo()
	case "list_models":
		return a.execListModels()
	case "prune_images":
		return a.execPruneImages()
	default:
		return "", fmt.Errorf("unknown function: %s", call.Name)
	}
}

// --- Access Control Helpers ---

// listAppsForCaller returns apps the caller has access to.
func (a *Assistant) listAppsForCaller(caller *Caller) ([]app.App, error) {
	if caller != nil && caller.UserRole == "deployer" && caller.UserID != "" {
		return a.storage.ListAppsForUser(caller.UserID)
	}
	return a.storage.ListApps()
}

// getAppWithAccessCheck retrieves an app by name and checks the caller's access.
func (a *Assistant) getAppWithAccessCheck(name string, caller *Caller) (*app.App, error) {
	ap, err := a.storage.GetAppByName(name)
	if err != nil {
		return nil, err
	}
	if ap == nil {
		return nil, nil
	}

	// Deployers can only access apps they have permission for
	if caller != nil && caller.UserRole == "deployer" && caller.UserID != "" {
		hasAccess, err := a.storage.UserHasAppAccess(caller.UserID, ap.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check access: %w", err)
		}
		if !hasAccess {
			return nil, fmt.Errorf("you don't have access to app '%s'", name)
		}
	}

	return ap, nil
}

// --- Function Executors ---

func (a *Assistant) execListApps(caller *Caller) (string, error) {
	apps, err := a.listAppsForCaller(caller)
	if err != nil {
		return "", err
	}
	if len(apps) == 0 {
		return "No apps deployed yet.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d app(s):\n", len(apps)))
	for _, ap := range apps {
		sb.WriteString(fmt.Sprintf("  %-20s %-10s %s\n", ap.Name, ap.Status, ap.Image))
	}
	return sb.String(), nil
}

func (a *Assistant) execGetApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	return fmt.Sprintf("App: %s\n  Status: %s\n  Image: %s\n  Domain: %s\n  Type: %s\n  Created: %s",
		ap.Name, ap.Status, ap.Image, ap.Domain, ap.Type, ap.CreatedAt.Format("2006-01-02 15:04")), nil
}

func (a *Assistant) execStartApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	if ap.ContainerID == "" {
		return fmt.Sprintf("App '%s' has not been deployed yet.", name), nil
	}

	ctx := context.Background()
	if err := a.podman.StartContainer(ctx, ap.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start %s: %w", name, err)
	}

	ap.Status = app.StatusRunning
	a.storage.UpdateApp(ap)

	return fmt.Sprintf("Started app %s.", name), nil
}

func (a *Assistant) execStopApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	if ap.ContainerID == "" {
		return fmt.Sprintf("App '%s' has not been deployed yet.", name), nil
	}

	ctx := context.Background()
	if err := a.podman.StopContainer(ctx, ap.ContainerID, 30); err != nil {
		return "", fmt.Errorf("failed to stop %s: %w", name, err)
	}

	ap.Status = app.StatusStopped
	a.storage.UpdateApp(ap)

	return fmt.Sprintf("Stopped app %s.", name), nil
}

func (a *Assistant) execRestartApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	if ap.ContainerID == "" {
		return fmt.Sprintf("App '%s' has not been deployed yet.", name), nil
	}

	ctx := context.Background()
	if err := a.podman.StopContainer(ctx, ap.ContainerID, 30); err != nil {
		return "", fmt.Errorf("failed to stop %s: %w", name, err)
	}
	if err := a.podman.StartContainer(ctx, ap.ContainerID); err != nil {
		return "", fmt.Errorf("failed to start %s: %w", name, err)
	}

	ap.Status = app.StatusRunning
	a.storage.UpdateApp(ap)

	return fmt.Sprintf("Restarted app %s.", name), nil
}

func (a *Assistant) execDeployApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	if ap.Image == "" {
		return fmt.Sprintf("App '%s' has no image configured.", name), nil
	}

	ctx := context.Background()

	// Pull latest image
	if err := a.podman.PullImage(ctx, ap.Image); err != nil {
		return "", fmt.Errorf("failed to pull image for %s: %w", name, err)
	}

	// Stop and remove old container if exists
	if ap.ContainerID != "" {
		a.podman.StopContainer(ctx, ap.ContainerID, 10)
		a.podman.RemoveContainer(ctx, ap.ContainerID, true)
	}

	// Create new container
	containerPort := 80
	if ap.Ports.ContainerPort > 0 {
		containerPort = ap.Ports.ContainerPort
	}

	hostPort := assignHostPort(ap.ID)
	opts := podman.CreateContainerOpts{
		Name:  "basepod-" + ap.Name,
		Image: ap.Image,
		Ports: map[string]string{
			fmt.Sprintf("%d", containerPort): fmt.Sprintf("%d", hostPort),
		},
		Env: ap.Env,
	}

	containerID, err := a.podman.CreateContainer(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("failed to create container for %s: %w", name, err)
	}

	if err := a.podman.StartContainer(ctx, containerID); err != nil {
		return "", fmt.Errorf("failed to start container for %s: %w", name, err)
	}

	ap.ContainerID = containerID
	ap.Status = app.StatusRunning
	a.storage.UpdateApp(ap)

	return fmt.Sprintf("Deployed app %s successfully.", name), nil
}

func (a *Assistant) execGetLogs(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	if ap.ContainerID == "" {
		return fmt.Sprintf("App '%s' has not been deployed yet.", name), nil
	}

	lines := 50
	if v, ok := params["lines"]; ok {
		if f, ok := v.(float64); ok {
			lines = int(f)
		}
	}

	ctx := context.Background()
	logsReader, err := a.podman.ContainerLogs(ctx, ap.ContainerID, podman.LogOpts{
		Stdout: true,
		Stderr: true,
		Tail:   strconv.Itoa(lines),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get logs for %s: %w", name, err)
	}
	defer logsReader.Close()

	logData, err := io.ReadAll(logsReader)
	if err != nil {
		return "", err
	}

	logStr := stripPodmanHeaders(logData)
	if logStr == "" {
		return fmt.Sprintf("No recent logs for %s.", name), nil
	}

	// Truncate if very long
	if len(logStr) > 2000 {
		logStr = logStr[len(logStr)-2000:]
		logStr = "...(truncated)\n" + logStr
	}

	return fmt.Sprintf("Logs for %s (last %d lines):\n%s", name, lines, logStr), nil
}

func (a *Assistant) execCreateApp(params map[string]interface{}) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	image, _ := getStringParam(params, "image")
	if image == "" {
		image = "nginx:latest"
	}

	port := 80
	if v, ok := params["port"]; ok {
		if f, ok := v.(float64); ok {
			port = int(f)
		}
	}

	// Check if app already exists
	existing, _ := a.storage.GetAppByName(name)
	if existing != nil {
		return fmt.Sprintf("App '%s' already exists.", name), nil
	}

	newApp := &app.App{
		Name:   name,
		Image:  image,
		Type:   app.AppTypeContainer,
		Status: app.StatusPending,
		Ports: app.PortConfig{
			ContainerPort: port,
		},
		Env: make(map[string]string),
	}

	if err := a.storage.CreateApp(newApp); err != nil {
		return "", fmt.Errorf("failed to create app: %w", err)
	}

	return fmt.Sprintf("Created app '%s' with image %s on port %d.", name, image, port), nil
}

func (a *Assistant) execDeleteApp(params map[string]interface{}, caller *Caller) (string, error) {
	name, err := getStringParam(params, "name")
	if err != nil {
		return "", err
	}

	ap, err := a.getAppWithAccessCheck(name, caller)
	if err != nil {
		return "", err
	}
	if ap == nil {
		return fmt.Sprintf("App '%s' not found.", name), nil
	}

	// Don't auto-delete — return a confirmation message
	return fmt.Sprintf("To delete app '%s', please use the web dashboard or CLI: bp delete %s", name, name), nil
}

func (a *Assistant) execStorageInfo() (string, error) {
	ctx := context.Background()

	paths, err := config.GetPaths()
	if err != nil {
		return "", err
	}

	du, err := diskutil.GetDiskUsage(paths.Base)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Disk: %s of %s used (%.0f%%)\n",
		diskutil.FormatBytes(int64(du.Used)),
		diskutil.FormatBytes(int64(du.Total)),
		du.Percent))

	// Container images
	images, err := a.podman.ListImages(ctx)
	if err == nil {
		var total int64
		for _, img := range images {
			total += img.Size
		}
		sb.WriteString(fmt.Sprintf("  Container Images:   %s (%d images)\n",
			diskutil.FormatBytes(total), len(images)))
	}

	// Volumes
	volumes, err := a.podman.ListVolumes(ctx)
	if err == nil {
		var total int64
		for _, vol := range volumes {
			if vol.Mountpoint != "" {
				total += diskutil.DirSize(vol.Mountpoint)
			}
		}
		sb.WriteString(fmt.Sprintf("  Volumes:            %s (%d volumes)\n",
			diskutil.FormatBytes(total), len(volumes)))
	}

	return sb.String(), nil
}

func (a *Assistant) execSystemInfo() (string, error) {
	ctx := context.Background()

	var sb strings.Builder

	containers, err := a.podman.ListContainers(ctx, true)
	if err == nil {
		running := 0
		for _, c := range containers {
			if c.State == "running" {
				running++
			}
		}
		sb.WriteString(fmt.Sprintf("Containers: %d total, %d running\n", len(containers), running))
	}

	images, err := a.podman.ListImages(ctx)
	if err == nil {
		sb.WriteString(fmt.Sprintf("Images: %d\n", len(images)))
	}

	svc := mlx.GetService()
	status := svc.GetStatus()
	if status.Running {
		sb.WriteString(fmt.Sprintf("MLX: running (%s)\n", status.ActiveModel))
	} else {
		sb.WriteString("MLX: stopped\n")
	}

	return sb.String(), nil
}

func (a *Assistant) execListModels() (string, error) {
	svc := mlx.GetService()
	models := svc.ListModels()

	downloaded := []mlx.Model{}
	for _, m := range models {
		if m.Downloaded {
			downloaded = append(downloaded, m)
		}
	}

	if len(downloaded) == 0 {
		return "No models downloaded. Use the web dashboard or CLI to download models.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Downloaded models (%d):\n", len(downloaded)))
	for _, m := range downloaded {
		sb.WriteString(fmt.Sprintf("  %-30s %s (%s)\n", m.Name, m.Size, m.Category))
	}

	status := svc.GetStatus()
	if status.Running {
		sb.WriteString(fmt.Sprintf("\nActive: %s", status.ActiveModel))
	}

	return sb.String(), nil
}

func (a *Assistant) execPruneImages() (string, error) {
	ctx := context.Background()

	images, err := a.podman.ListImages(ctx)
	if err != nil {
		return "", err
	}

	removed := 0
	for _, img := range images {
		dangling := true
		for _, tag := range img.RepoTags {
			if tag != "" && tag != "<none>:<none>" {
				dangling = false
				break
			}
		}
		if dangling {
			if err := a.podman.RemoveImage(ctx, img.ID, false); err == nil {
				removed++
			}
		}
	}

	if removed == 0 {
		return "No unused images to clean up.", nil
	}
	return fmt.Sprintf("Removed %d unused image(s).", removed), nil
}

// --- Helpers ---

func getStringParam(params map[string]interface{}, key string) (string, error) {
	v, ok := params[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v), nil
	}
	return s, nil
}

// assignHostPort generates a unique host port based on app ID (mirrors api.go).
func assignHostPort(appID string) int {
	h := uint32(0)
	for _, c := range appID {
		h = h*31 + uint32(c)
	}
	return 10000 + int(h%50000)
}

// stripPodmanHeaders removes Podman multiplexed stream headers from log output.
func stripPodmanHeaders(data []byte) string {
	var sb strings.Builder
	pos := 0
	for pos < len(data) {
		if pos+8 > len(data) {
			sb.Write(data[pos:])
			break
		}
		// Frame header: [stream_type(1), padding(3), size(4 big-endian)]
		frameSize := int(data[pos+4])<<24 | int(data[pos+5])<<16 | int(data[pos+6])<<8 | int(data[pos+7])
		if frameSize <= 0 || frameSize > 1<<20 {
			// Not a multiplexed stream, return as-is
			sb.Write(data[pos:])
			break
		}
		pos += 8
		end := pos + frameSize
		if end > len(data) {
			end = len(data)
		}
		sb.Write(data[pos:end])
		pos = end
	}
	return sb.String()
}

// cleanResponse removes FunctionGemma special tokens from text output.
func cleanResponse(s string) string {
	s = strings.ReplaceAll(s, "<end_of_turn>", "")
	s = strings.ReplaceAll(s, "<start_of_turn>", "")
	s = strings.ReplaceAll(s, "<start_function_call>", "")
	s = strings.ReplaceAll(s, "<end_function_call>", "")
	s = strings.ReplaceAll(s, "<start_function_declaration>", "")
	s = strings.ReplaceAll(s, "<end_function_declaration>", "")
	return strings.TrimSpace(s)
}
