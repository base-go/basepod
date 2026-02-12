// Package ai provides the AI assistant engine for basepod.
// It uses FunctionGemma on a dedicated port to parse natural language into basepod operations.
// The assistant model runs independently from the user's chat model.
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
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

// AskResult is the response from the assistant.
type AskResult struct {
	Response string      `json:"response"`
	Action   *ActionInfo `json:"action,omitempty"`
}

// ActionInfo describes what action was executed.
type ActionInfo struct {
	Function   string         `json:"function"`
	Parameters map[string]any `json:"parameters"`
	Success    bool           `json:"success"`
}

// Caller represents the user making a request, used for access control.
type Caller struct {
	UserID   string // empty for legacy admin sessions
	UserRole string // "admin", "deployer", "viewer"
}

// Assistant is the AI assistant engine.
type Assistant struct {
	storage  *storage.Storage
	podman   podman.Client
	client   *http.Client
	port     int
	warmedUp bool // true after first successful completions call
}

// AssistantModelID is the FunctionGemma model used by the assistant.
const AssistantModelID = "mlx-community/functiongemma-270m-it-4bit"

// AssistantPort is the dedicated MLX port for the assistant (separate from chat).
const AssistantPort = 11435

// New creates a new Assistant instance.
func New(store *storage.Storage, pm podman.Client) *Assistant {
	return &Assistant{
		storage: store,
		podman:  pm,
		client:  &http.Client{Timeout: 120 * time.Second},
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

// buildPrompt constructs the FunctionGemma prompt using the model's native control tokens.
// FunctionGemma requires: developer turn with <start_function_declaration> blocks,
// user turn with the query, then model turn for the response.
// See: https://ai.google.dev/gemma/docs/functiongemma/formatting-and-best-practices
func (a *Assistant) buildPrompt(userMessage string, pageContext string) string {
	var sb strings.Builder

	// Developer turn: declare available functions
	sb.WriteString("<start_of_turn>developer\n")
	sb.WriteString("You are a model that can do function calling with the following functions\n")

	for _, fn := range assistantFunctions {
		sb.WriteString("<start_function_declaration>")
		sb.WriteString(fmt.Sprintf("declaration:%s{description:<escape>%s<escape>,parameters:{", fn.Name, fn.Description))

		if fn.Parameters != nil {
			sb.WriteString("properties:{")
			first := true
			var required []string
			for name, p := range fn.Parameters {
				if !first {
					sb.WriteString(",")
				}
				sb.WriteString(fmt.Sprintf("%s:{description:<escape>%s<escape>,type:<escape>%s<escape>}", name, p.Description, p.Type))
				required = append(required, name)
				first = false
			}
			sb.WriteString("},required:[")
			for i, r := range required {
				if i > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(fmt.Sprintf("<escape>%s<escape>", r))
			}
			sb.WriteString("],type:<escape>OBJECT<escape>")
		}

		sb.WriteString("}}")
		sb.WriteString("<end_function_declaration>\n")
	}

	sb.WriteString("<end_of_turn>\n")

	// User turn
	sb.WriteString("<start_of_turn>user\n")
	if pageContext != "" {
		sb.WriteString(fmt.Sprintf("(I'm on the %s page) ", pageContext))
	}
	sb.WriteString(userMessage)
	sb.WriteString("\n<end_of_turn>\n")

	// Model turn (prompt for response)
	sb.WriteString("<start_of_turn>model\n")

	return sb.String()
}

// parseFunctionCall extracts a function call from the model response.
// FunctionGemma outputs: <start_function_call>call:FUNC_NAME{key:<escape>value<escape>}<end_function_call>
// Falls back to JSON parsing if native format not found.
func parseFunctionCall(response string) (*FunctionCall, string) {
	response = strings.TrimSpace(response)

	// Try FunctionGemma native format first
	startTag := "<start_function_call>"
	endTag := "<end_function_call>"

	startIdx := strings.Index(response, startTag)
	endIdx := strings.Index(response, endTag)

	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		callStr := strings.TrimSpace(response[startIdx+len(startTag) : endIdx])

		// Parse "call:FUNC_NAME{param:<escape>value<escape>,...}"
		if strings.HasPrefix(callStr, "call:") {
			callStr = callStr[5:] // Remove "call:"

			braceIdx := strings.Index(callStr, "{")
			if braceIdx != -1 {
				funcName := callStr[:braceIdx]
				paramsStr := callStr[braceIdx+1:]

				// Remove trailing }
				if strings.HasSuffix(paramsStr, "}") {
					paramsStr = paramsStr[:len(paramsStr)-1]
				}

				// Validate function name
				if isKnownFunction(funcName) {
					params := parseFunctionParams(paramsStr)
					textBefore := strings.TrimSpace(response[:startIdx])
					return &FunctionCall{Name: funcName, Parameters: params}, textBefore
				}
			}
		}
	}

	// Fallback: try JSON format {"name": "...", "arguments": {...}}
	return parseFunctionCallJSON(response)
}

// parseFunctionCallJSON is a fallback parser for JSON-formatted function calls.
func parseFunctionCallJSON(response string) (*FunctionCall, string) {
	startIdx := strings.Index(response, "{")
	if startIdx == -1 {
		return nil, response
	}

	depth := 0
	endIdx := -1
	for i := startIdx; i < len(response); i++ {
		switch response[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endIdx = i + 1
			}
		}
		if endIdx != -1 {
			break
		}
	}

	if endIdx == -1 {
		return nil, response
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(response[startIdx:endIdx]), &raw); err != nil {
		return nil, response
	}

	name, _ := raw["name"].(string)
	if name == "" {
		return nil, response
	}

	params, _ := raw["arguments"].(map[string]any)
	if params == nil {
		params, _ = raw["parameters"].(map[string]any)
	}
	if params == nil {
		params = map[string]any{}
	}

	if !isKnownFunction(name) {
		return nil, response
	}

	textBefore := strings.TrimSpace(response[:startIdx])
	return &FunctionCall{Name: name, Parameters: params}, textBefore
}

// parseFunctionParams parses FunctionGemma-style parameters: key:<escape>value<escape>,key2:<escape>value2<escape>
func parseFunctionParams(s string) map[string]any {
	params := map[string]any{}
	s = strings.TrimSpace(s)
	if s == "" {
		return params
	}

	// Split by comma but respect <escape> boundaries
	var parts []string
	var current strings.Builder
	inEscape := false

	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "<escape>") {
			inEscape = !inEscape
			current.WriteString("<escape>")
			i += len("<escape>") - 1
			continue
		}
		if s[i] == ',' && !inEscape {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(s[i])
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		colonIdx := strings.Index(part, ":")
		if colonIdx == -1 {
			continue
		}
		key := strings.TrimSpace(part[:colonIdx])
		value := strings.TrimSpace(part[colonIdx+1:])
		// Remove <escape> tags
		value = strings.ReplaceAll(value, "<escape>", "")
		value = strings.TrimSpace(value)
		params[key] = value
	}

	return params
}

// isKnownFunction checks if a function name is in our registry.
func isKnownFunction(name string) bool {
	for _, fn := range assistantFunctions {
		if fn.Name == name {
			return true
		}
	}
	return false
}

// EnsureRunning makes sure FunctionGemma is downloaded and running on its dedicated port.
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

	// Check if assistant is already running
	if !a.isAssistantRunning() {
		if err := svc.RunOnPort(AssistantModelID, a.port); err != nil {
			return err
		}
	}

	// Warmup: pre-load model weights so first real request isn't slow.
	// This is needed even if the server was already running (e.g. after basepod restart)
	// because model weights load lazily on first inference.
	if !a.warmedUp {
		if err := a.warmup(); err != nil {
			// Warmup failed (stuck process). Kill and restart.
			svc.StopAssistant()
			if err := svc.RunOnPort(AssistantModelID, a.port); err != nil {
				return fmt.Errorf("failed to restart assistant after stuck process: %w", err)
			}
			// Try warmup once more after restart
			if err := a.warmup(); err != nil {
				return fmt.Errorf("assistant warmup failed after restart: %w", err)
			}
		}
	}
	return nil
}

// warmup sends a trivial request to pre-load model weights after starting.
// Returns an error if the completions endpoint doesn't respond within 60s (stuck process).
func (a *Assistant) warmup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	url := fmt.Sprintf("http://localhost:%d/v1/completions", a.port)
	body := map[string]any{"model": AssistantModelID, "prompt": "hi", "max_tokens": 1}
	data, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("warmup failed (process may be stuck): %w", err)
	}
	resp.Body.Close()
	a.warmedUp = true
	return nil
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

// greetingResponse handles greetings and help requests without hitting the model.
// FunctionGemma is a function-calling model, not a chatbot — it gives robotic text
// responses to greetings. We handle those in code for a better UX.
func greetingResponse(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))

	// Greetings
	greetings := []string{"hi", "hello", "hey", "yo", "sup", "howdy", "hola", "greetings"}
	for _, g := range greetings {
		if lower == g || strings.HasPrefix(lower, g+" ") || strings.HasPrefix(lower, g+"!") || strings.HasPrefix(lower, g+",") {
			return "Hey! I'm your Basepod assistant. Here's what I can help with:\n\n" +
				"- **List apps** — see all your deployed applications\n" +
				"- **Start/stop/restart** an app by name\n" +
				"- **Deploy** an app to pull the latest image\n" +
				"- **View logs** for any app\n" +
				"- **Create** a new app from a container image\n" +
				"- **Storage info** — check disk usage\n" +
				"- **System info** — containers, images, MLX status\n" +
				"- **List models** — see downloaded LLM models\n" +
				"- **Prune images** — clean up unused container images\n\n" +
				"Just ask in plain English!"
		}
	}

	// Help requests
	helpPhrases := []string{"help", "what can you do", "what do you do", "capabilities", "commands", "functions", "what are you"}
	for _, h := range helpPhrases {
		if lower == h || strings.Contains(lower, h) {
			return "I can manage your Basepod apps. Try:\n\n" +
				"- \"list my apps\"\n" +
				"- \"start myapp\" / \"stop myapp\" / \"restart myapp\"\n" +
				"- \"deploy myapp\"\n" +
				"- \"show logs for myapp\"\n" +
				"- \"create an app called myapp with image nginx\"\n" +
				"- \"storage info\" or \"system info\"\n" +
				"- \"list models\"\n" +
				"- \"prune images\""
		}
	}

	return ""
}

// Ask processes a natural language message and returns a result.
// The caller parameter enforces role-based and per-app access control.
// pageContext is the current page the user is on (e.g. "Apps", "Dashboard").
func (a *Assistant) Ask(message string, caller *Caller, pageContext string) (*AskResult, error) {
	// Handle greetings and help without calling the model
	if greeting := greetingResponse(message); greeting != "" {
		return &AskResult{Response: greeting}, nil
	}

	if err := a.EnsureRunning(); err != nil {
		return nil, err
	}

	// Build prompt and call FunctionGemma
	prompt := a.buildPrompt(message, pageContext)
	modelResponse, err := a.callMLX(prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI response: %w", err)
	}

	// Clean up special tokens from response
	modelResponse = cleanResponse(modelResponse)

	// Parse the response for function calls
	call, textResponse := parseFunctionCall(modelResponse)

	if call == nil {
		resp := textResponse
		if resp == "" {
			resp = modelResponse
		}
		resp = strings.TrimSpace(resp)
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

// callMLX sends a raw prompt to the FunctionGemma MLX server on the dedicated port.
// Uses /v1/completions (not chat/completions) because we build the full prompt ourselves.
func (a *Assistant) callMLX(prompt string) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/completions", a.port)

	reqBody := map[string]any{
		"model":             AssistantModelID,
		"prompt":            prompt,
		"max_tokens":        256,
		"temperature":       0.1,
		"repetition_penalty": 1.2,
		"stop":              []string{"<end_of_turn>", "<end_function_call>"},
	}

	data, _ := json.Marshal(reqBody)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse MLX response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from model")
	}

	return result.Choices[0].Text, nil
}

// cleanResponse removes special tokens from output but preserves function call tokens for parsing.
func cleanResponse(s string) string {
	s = strings.ReplaceAll(s, "<end_of_turn>", "")
	s = strings.ReplaceAll(s, "<start_of_turn>", "")
	return strings.TrimSpace(s)
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

