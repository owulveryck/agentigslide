package vertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// redirectTransport rewrites every outgoing request to point at the test server
// while preserving the original path and query string so we can assert on them.
type redirectTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(t.targetURL)
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	return t.base.RoundTrip(req)
}

// newTestClient creates a *Client whose HTTP traffic is redirected to the given
// httptest.Server URL. projectID and region are embedded in the client so that
// the constructed Vertex AI URL can be inspected on the server side.
func newTestClient(serverURL, projectID, region string) *Client {
	return NewClientWithHTTP(
		&http.Client{
			Transport: &redirectTransport{
				base:      http.DefaultTransport,
				targetURL: serverURL,
			},
		},
		projectID,
		region,
	)
}

// --- NewClientWithHTTP ---

func TestNewClientWithHTTP(t *testing.T) {
	httpClient := &http.Client{}
	c := NewClientWithHTTP(httpClient, "my-project", "us-central1")

	if c.HTTPClient != httpClient {
		t.Error("HTTPClient not set correctly")
	}
	if c.ProjectID != "my-project" {
		t.Errorf("ProjectID = %q, want %q", c.ProjectID, "my-project")
	}
	if c.Region != "us-central1" {
		t.Errorf("Region = %q, want %q", c.Region, "us-central1")
	}
}

// --- Options ---

func TestWithMaxTokens(t *testing.T) {
	o := &options{}
	WithMaxTokens(1024)(o)
	if o.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", o.MaxTokens)
	}
}

func TestWithTemperature(t *testing.T) {
	o := &options{}
	WithTemperature(0.7)(o)
	if o.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", o.Temperature)
	}
}

func TestWithSystem(t *testing.T) {
	o := &options{}
	WithSystem("you are helpful")(o)
	if o.System != "you are helpful" {
		t.Errorf("System = %q, want %q", o.System, "you are helpful")
	}
}

// --- LoadConfig env var validation ---

func TestLoadConfig_MissingProjectID(t *testing.T) {
	// envconfig's required check triggers only when the var is unset, not empty
	prev, hadPrev := os.LookupEnv("VERTEX_PROJECT_ID")
	if err := os.Unsetenv("VERTEX_PROJECT_ID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrev {
			if err := os.Setenv("VERTEX_PROJECT_ID", prev); err != nil {
				t.Fatal(err)
			}
		}
	})
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when VERTEX_PROJECT_ID is missing")
	}
	if !strings.Contains(err.Error(), "PROJECT_ID") {
		t.Errorf("error should mention PROJECT_ID, got: %v", err)
	}
}

func TestLoadConfig_DefaultRegion(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "my-project")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Region != "us-east5" {
		t.Errorf("Region = %q, want %q", cfg.Region, "us-east5")
	}
}

func TestLoadConfig_CustomRegion(t *testing.T) {
	t.Setenv("VERTEX_PROJECT_ID", "my-project")
	t.Setenv("VERTEX_REGION", "europe-west1")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Region != "europe-west1" {
		t.Errorf("Region = %q, want %q", cfg.Region, "europe-west1")
	}
}

// --- RawPredict ---

func TestRawPredict_SuccessfulTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "hello"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want %q", result, "hello")
	}
}

func TestRawPredict_URLConstruction(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "my-project-123", "europe-west4")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/v1/projects/my-project-123/locations/europe-west4/publishers/anthropic/models/claude-sonnet-4-20250514:rawPredict"
	if capturedPath != expected {
		t.Errorf("path = %q, want %q", capturedPath, expected)
	}
}

func TestRawPredict_RequestBody(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, ok := capturedBody["anthropic_version"].(string); !ok || v != "vertex-2023-10-16" {
		t.Errorf("anthropic_version = %v, want %q", capturedBody["anthropic_version"], "vertex-2023-10-16")
	}

	msgs, ok := capturedBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v, want 1 message", capturedBody["messages"])
	}

	// Default max_tokens should be 32768
	if v, ok := capturedBody["max_tokens"].(float64); !ok || int(v) != 32768 {
		t.Errorf("max_tokens = %v, want 32768", capturedBody["max_tokens"])
	}

	// Default temperature should be 0.0
	if v, ok := capturedBody["temperature"].(float64); !ok || v != 0.0 {
		t.Errorf("temperature = %v, want 0.0", capturedBody["temperature"])
	}

	// system should NOT be present by default
	if _, ok := capturedBody["system"]; ok {
		t.Error("system should not be present by default")
	}
}

func TestRawPredict_DefaultOptions(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	maxTokens := capturedBody["max_tokens"].(float64)
	if int(maxTokens) != 32768 {
		t.Errorf("default max_tokens = %v, want 32768", maxTokens)
	}
	temp := capturedBody["temperature"].(float64)
	if temp != 0.0 {
		t.Errorf("default temperature = %v, want 0.0", temp)
	}
}

func TestRawPredict_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden details"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermanentError, got: %T: %v", err, err)
	}
	if permErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", permErr.StatusCode)
	}
	if !strings.Contains(permErr.Body, "forbidden details") {
		t.Errorf("Body should contain response body, got: %q", permErr.Body)
	}
}

func TestRawPredict_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestRawPredict_MarkdownFenceJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "```json\n{\"key\": \"value\"}\n```"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "```") {
		t.Errorf("result should not contain markdown fences, got: %q", result)
	}
	if !strings.Contains(result, `"key"`) {
		t.Errorf("result should contain JSON content, got: %q", result)
	}
}

func TestRawPredict_MarkdownFencePlain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "```\nsome content\n```"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "```") {
		t.Errorf("result should not contain markdown fences, got: %q", result)
	}
	if !strings.Contains(result, "some content") {
		t.Errorf("result should contain inner content, got: %q", result)
	}
}

func TestRawPredict_MultipleContentBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "hello "},
				{"type": "text", "text": "world"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestRawPredict_WithSystemOption(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	}, WithSystem("you are a helpful assistant"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	system, ok := capturedBody["system"].(string)
	if !ok {
		t.Fatal("system key should be present in request body")
	}
	if system != "you are a helpful assistant" {
		t.Errorf("system = %q, want %q", system, "you are a helpful assistant")
	}
}

func TestRawPredict_WithMaxTokensOption(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	}, WithMaxTokens(4096))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	maxTokens := capturedBody["max_tokens"].(float64)
	if int(maxTokens) != 4096 {
		t.Errorf("max_tokens = %v, want 4096", maxTokens)
	}
}

func TestRawPredict_WithTemperatureOption(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	}, WithTemperature(0.9))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	temp := capturedBody["temperature"].(float64)
	if temp != 0.9 {
		t.Errorf("temperature = %v, want 0.9", temp)
	}
}

func TestRawPredict_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is done (simulating a slow server).
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := c.RawPredict(ctx, "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestRawPredict_ContentTypeHeader(t *testing.T) {
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", capturedContentType, "application/json")
	}
}

func TestRawPredict_POSTMethod(t *testing.T) {
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != "POST" {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
}

func TestRawPredict_EmptyContentBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestRawPredict_NonTextBlocksIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "tool_use", "text": "should be ignored"},
				{"type": "text", "text": "kept"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	result, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "kept" {
		t.Errorf("result = %q, want %q", result, "kept")
	}
}

func TestRawPredict_Error4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	var permErr *PermanentError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected PermanentError, got: %T: %v", err, err)
	}
	if permErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", permErr.StatusCode)
	}
}

func TestRawPredict_TransientError(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.RawPredict(ctx, "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	})
	if err == nil {
		t.Fatal("expected error for 429 status")
	}
	if attempts < 1 {
		t.Errorf("expected at least 1 attempt, got %d", attempts)
	}
}

func TestRawPredict_MultipleOptions(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "proj", "us-east5")
	_, err := c.RawPredict(context.Background(), "claude-sonnet-4-20250514", []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "test"}}},
	}, WithMaxTokens(2048), WithTemperature(0.5), WithSystem("be concise"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if int(capturedBody["max_tokens"].(float64)) != 2048 {
		t.Errorf("max_tokens = %v, want 2048", capturedBody["max_tokens"])
	}
	if capturedBody["temperature"].(float64) != 0.5 {
		t.Errorf("temperature = %v, want 0.5", capturedBody["temperature"])
	}
	if capturedBody["system"].(string) != "be concise" {
		t.Errorf("system = %v, want %q", capturedBody["system"], "be concise")
	}
}
