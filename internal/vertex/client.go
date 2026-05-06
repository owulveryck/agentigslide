package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/owulveryck/slideAppScripter/internal/auth"
)

const (
	maxRetries     = 5
	baseRetryDelay = 3 * time.Second
)

// Config holds Vertex AI connection parameters loaded from environment
// variables with the "VERTEX" prefix (e.g. VERTEX_PROJECT_ID, VERTEX_REGION).
type Config struct {
	ProjectID string `envconfig:"PROJECT_ID" required:"true" desc:"Google Cloud project ID for Vertex AI"`
	Region    string `envconfig:"REGION" default:"us-east5" desc:"Vertex AI region"`
}

// LoadConfig loads the Vertex AI Config from environment variables with the
// "VERTEX" prefix.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := envconfig.Process("VERTEX", &cfg); err != nil {
		return cfg, fmt.Errorf("loading VERTEX config: %w", err)
	}
	return cfg, nil
}

// Client is a Vertex AI client for making Claude API predictions. It holds
// the authenticated HTTP client, Google Cloud project ID, and the Vertex AI
// region to use for API requests.
type Client struct {
	HTTPClient *http.Client
	ProjectID  string
	Region     string
}

// NewClient creates a new Vertex AI Client from the provided Config. It
// authenticates via Google Cloud application default credentials.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	httpClient, err := auth.CreateVertexAIClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vertex AI client: %w", err)
	}

	return &Client{
		HTTPClient: httpClient,
		ProjectID:  cfg.ProjectID,
		Region:     cfg.Region,
	}, nil
}

// NewClientWithHTTP creates a new Vertex AI Client with an explicitly provided
// HTTP client, project ID, and region.
func NewClientWithHTTP(httpClient *http.Client, projectID, region string) *Client {
	return &Client{
		HTTPClient: httpClient,
		ProjectID:  projectID,
		Region:     region,
	}
}

// doRequest sends a request to the Vertex AI rawPredict endpoint with
// exponential backoff retry on transient errors (429, 529, 5xx).
func (c *Client) doRequest(ctx context.Context, model string, o *options) ([]byte, error) {
	requestBody := map[string]any{
		"anthropic_version": "vertex-2023-10-16",
		"messages":          o.Messages,
		"max_tokens":        o.MaxTokens,
		"temperature":       o.Temperature,
	}
	if len(o.SystemBlocks) > 0 {
		requestBody["system"] = o.SystemBlocks
	} else if o.System != "" {
		requestBody["system"] = o.System
	}
	if len(o.Tools) > 0 {
		requestBody["tools"] = o.Tools
	}
	if o.ToolChoice != nil {
		requestBody["tool_choice"] = o.ToolChoice
	}
	if o.Thinking != nil {
		requestBody["thinking"] = o.Thinking
	}

	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict",
		c.Region, c.ProjectID, c.Region, model)

	var lastErr error
	for attempt := range maxRetries {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		if isRetryable(resp.StatusCode) && attempt < maxRetries-1 {
			delay := baseRetryDelay * time.Duration(math.Pow(2, float64(attempt)))
			slog.Warn("[vertex] retryable error, backing off",
				"status", resp.StatusCode,
				"attempt", attempt+1,
				"delay", delay,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			lastErr = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
			continue
		}

		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func isRetryable(statusCode int) bool {
	return statusCode == 429 || statusCode == 529 || statusCode >= 500
}

// RawPredict sends a list of messages to the specified Claude model via the
// Vertex AI rawPredict endpoint and returns the concatenated text response.
// It strips any markdown code fences from the response for easier JSON parsing.
func (c *Client) RawPredict(ctx context.Context, model string, messages []Message, opts ...Option) (string, error) {
	o := defaultOptions(messages)
	for _, opt := range opts {
		opt(o)
	}

	body, err := c.doRequest(ctx, model, o)
	if err != nil {
		return "", err
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w\nResponse: %s", err, string(body))
	}

	var responseText string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	responseText = strings.TrimSpace(responseText)
	if after, found := strings.CutPrefix(responseText, "```json"); found {
		responseText = strings.TrimSuffix(strings.TrimSpace(after), "```")
	} else if after, found := strings.CutPrefix(responseText, "```"); found {
		responseText = strings.TrimSuffix(strings.TrimSpace(after), "```")
	}

	return responseText, nil
}

// RawPredictFull sends a list of messages to the specified Claude model via
// Vertex AI and returns the full structured response, including tool_use
// content blocks. Use this method when working with tool_use for structured
// outputs.
func (c *Client) RawPredictFull(ctx context.Context, model string, messages []Message, opts ...Option) (*FullResponse, error) {
	o := defaultOptions(messages)
	for _, opt := range opts {
		opt(o)
	}

	body, err := c.doRequest(ctx, model, o)
	if err != nil {
		return nil, err
	}

	var fullResp FullResponse
	if err := json.Unmarshal(body, &fullResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w\nResponse: %s", err, string(body))
	}

	return &fullResp, nil
}
