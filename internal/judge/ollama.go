package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOllamaHost      = "http://localhost:11434"
	defaultOllamaKeepAlive = "10m"
	defaultOllamaNumCtx    = 8192
	defaultOllamaTimeout   = 15 * time.Minute
)

// OllamaRunner executes structured local Ollama chat requests for judging.
type OllamaRunner struct {
	// BaseURL overrides the Ollama server base URL. When empty, reasond uses
	// OLLAMA_HOST or the default localhost endpoint.
	BaseURL string
	// KeepAlive overrides the keep_alive value sent on judge chat requests.
	// When empty, reasond uses OLLAMA_KEEP_ALIVE or the built-in default.
	KeepAlive string
	// HTTPClient overrides the request client used for Ollama API calls.
	// When nil, reasond uses the default timeout-configured client.
	HTTPClient *http.Client
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model     string                 `json:"model"`
	Messages  []ollamaMessage        `json:"messages"`
	Stream    bool                   `json:"stream"`
	Format    json.RawMessage        `json:"format"`
	KeepAlive string                 `json:"keep_alive,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
	Error string `json:"error"`
}

// Run executes Ollama's chat API and parses the structured findings response.
// Request options use the current OLLAMA_CONTEXT_LENGTH when it is set to a
// valid positive integer, otherwise reasond falls back to its default num_ctx.
func (r OllamaRunner) Run(ctx context.Context, rootDir, model, auditMarkdown string) (Response, error) {
	if strings.TrimSpace(model) == "" {
		return Response{}, fmt.Errorf("ollama model is required")
	}
	_ = rootDir

	endpoint, err := ollamaEndpoint(r.BaseURL, "/api/chat")
	if err != nil {
		return Response{}, err
	}

	body, err := json.Marshal(ollamaChatRequest{
		Model: model,
		Messages: []ollamaMessage{
			{Role: "user", Content: BuildPrompt(auditMarkdown)},
		},
		Stream:    false,
		Format:    json.RawMessage(Schema()),
		KeepAlive: ollamaKeepAlive(r.KeepAlive),
		Options: map[string]interface{}{
			"temperature": 0,
			"num_ctx":     ollamaNumCtx(),
		},
	})
	if err != nil {
		return Response{}, fmt.Errorf("encode ollama request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build ollama request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	responseBody, err := doOllamaRequest(ollamaHTTPClient(r.HTTPClient), request)
	if err != nil {
		return Response{}, fmt.Errorf("run ollama judge: %w", err)
	}

	var envelope ollamaChatResponse
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return Response{}, fmt.Errorf("decode ollama response: %w", err)
	}
	if strings.TrimSpace(envelope.Error) != "" {
		return Response{}, fmt.Errorf("ollama chat error: %s", strings.TrimSpace(envelope.Error))
	}
	if strings.TrimSpace(envelope.Message.Content) == "" {
		return Response{}, fmt.Errorf("ollama chat returned empty content")
	}

	var parsed Response
	if err := json.Unmarshal([]byte(envelope.Message.Content), &parsed); err != nil {
		return Response{}, fmt.Errorf("decode ollama structured output: %w", err)
	}

	return parsed, nil
}

// ListOllamaModels returns installed local models from the Ollama tags API.
// BaseURL and client follow the same override rules as OllamaRunner.
func ListOllamaModels(ctx context.Context, baseURL string, client *http.Client) ([]string, error) {
	endpoint, err := ollamaEndpoint(baseURL, "/api/tags")
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build ollama tags request: %w", err)
	}

	responseBody, err := doOllamaRequest(ollamaHTTPClient(client), request)
	if err != nil {
		return nil, fmt.Errorf("list ollama models: %w", err)
	}

	var payload ollamaTagsResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, fmt.Errorf("decode ollama tags response: %w", err)
	}
	if strings.TrimSpace(payload.Error) != "" {
		return nil, fmt.Errorf("ollama tags error: %s", strings.TrimSpace(payload.Error))
	}

	seen := map[string]struct{}{}
	models := make([]string, 0, len(payload.Models))
	for _, candidate := range payload.Models {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			name = strings.TrimSpace(candidate.Model)
		}
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("ollama has no installed models")
	}

	return models, nil
}

func ollamaHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultOllamaTimeout}
}

func ollamaKeepAlive(configured string) string {
	value := strings.TrimSpace(configured)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("OLLAMA_KEEP_ALIVE"))
	}
	if value == "" {
		return defaultOllamaKeepAlive
	}
	return value
}

func ollamaNumCtx() int {
	value := strings.TrimSpace(os.Getenv("OLLAMA_CONTEXT_LENGTH"))
	if value == "" {
		return defaultOllamaNumCtx
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultOllamaNumCtx
	}
	return parsed
}

func doOllamaRequest(client *http.Client, request *http.Request) ([]byte, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read ollama response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func ollamaEndpoint(baseURL string, apiPath string) (string, error) {
	host := strings.TrimSpace(baseURL)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	}
	if host == "" {
		host = defaultOllamaHost
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}

	parsed, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("resolve ollama host: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("resolve ollama host: missing host in %q", host)
	}

	basePath := strings.TrimRight(parsed.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/api")
	parsed.Path = basePath + apiPath
	parsed.RawPath = ""
	return parsed.String(), nil
}
