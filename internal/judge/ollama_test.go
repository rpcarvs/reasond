package judge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListOllamaModelsUsesTagsAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected tags path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{
				{"name": "glm4:9b", "model": "glm4:9b"},
				{"name": "qwen3:8b", "model": "qwen3:8b"},
			},
		})
	}))
	defer server.Close()

	models, err := ListOllamaModels(context.Background(), server.URL, server.Client())
	if err != nil {
		t.Fatalf("list ollama models: %v", err)
	}
	if len(models) != 2 || models[0] != "glm4:9b" || models[1] != "qwen3:8b" {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestListOllamaModelsRejectsEmptyCatalog(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{}})
	}))
	defer server.Close()

	if _, err := ListOllamaModels(context.Background(), server.URL, server.Client()); err == nil {
		t.Fatalf("expected empty ollama catalog to fail")
	}
}

func TestOllamaRunnerParsesStructuredOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected chat path %q", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["model"] != "glm4:9b" {
			t.Fatalf("unexpected request model: %+v", request["model"])
		}
		if request["stream"] != false {
			t.Fatalf("expected stream=false, got %+v", request["stream"])
		}
		if request["keep_alive"] != "10m" {
			t.Fatalf("expected default keep_alive 10m, got %+v", request["keep_alive"])
		}
		options, ok := request["options"].(map[string]any)
		if !ok {
			t.Fatalf("expected options object, got %+v", request["options"])
		}
		if options["num_ctx"] != float64(8192) {
			t.Fatalf("expected num_ctx 8192, got %+v", options["num_ctx"])
		}
		if _, ok := request["format"].(map[string]any); !ok {
			t.Fatalf("expected schema format object, got %+v", request["format"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"findings":[{"title":"Issue","issue":"Problem","why":"Because","how":"Prompt","score":0.7}]}`,
			},
		})
	}))
	defer server.Close()

	response, err := (OllamaRunner{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}).Run(context.Background(), "", "glm4:9b", "# User Prompt\n\nTest\n")
	if err != nil {
		t.Fatalf("run ollama judge: %v", err)
	}
	if len(response.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", response)
	}
	if response.Findings[0].Title != "Issue" || response.Findings[0].Score != 0.7 {
		t.Fatalf("unexpected finding: %+v", response.Findings[0])
	}
}

func TestOllamaRunnerUsesEnvKeepAlive(t *testing.T) {
	t.Setenv("OLLAMA_KEEP_ALIVE", "25m")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["keep_alive"] != "25m" {
			t.Fatalf("expected env keep_alive 25m, got %+v", request["keep_alive"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"findings":[]}`,
			},
		})
	}))
	defer server.Close()

	if _, err := (OllamaRunner{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}).Run(context.Background(), "", "glm4:9b", "# User Prompt\n\nTest\n"); err != nil {
		t.Fatalf("run ollama judge: %v", err)
	}
}

func TestOllamaRunnerUsesEnvContextLength(t *testing.T) {
	t.Setenv("OLLAMA_CONTEXT_LENGTH", "16384")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		options, ok := request["options"].(map[string]any)
		if !ok {
			t.Fatalf("expected options object, got %+v", request["options"])
		}
		if options["num_ctx"] != float64(16384) {
			t.Fatalf("expected env num_ctx 16384, got %+v", options["num_ctx"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"findings":[]}`,
			},
		})
	}))
	defer server.Close()

	if _, err := (OllamaRunner{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}).Run(context.Background(), "", "glm4:9b", "# User Prompt\n\nTest\n"); err != nil {
		t.Fatalf("run ollama judge: %v", err)
	}
}

func TestOllamaNumCtxFallsBackOnInvalidEnv(t *testing.T) {
	t.Setenv("OLLAMA_CONTEXT_LENGTH", "invalid")

	if got := ollamaNumCtx(); got != 8192 {
		t.Fatalf("expected fallback num_ctx 8192, got %d", got)
	}
}

func TestOllamaHTTPClientDefaultsToFifteenMinutes(t *testing.T) {
	t.Parallel()

	client := ollamaHTTPClient(nil)
	if client.Timeout != 15*time.Minute {
		t.Fatalf("expected default timeout 15m, got %s", client.Timeout)
	}
}

func TestOllamaEndpointHonorsOLLAMAHostWithoutScheme(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "127.0.0.1:11434")

	endpoint, err := ollamaEndpoint("", "/api/tags")
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:11434/api/tags") {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
}
