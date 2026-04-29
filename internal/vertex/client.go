package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"example.com/internal/auth"
)

type Client struct {
	HTTPClient *http.Client
	ProjectID  string
	Region     string
}

func NewClient(ctx context.Context) (*Client, error) {
	projectID := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("ANTHROPIC_VERTEX_PROJECT_ID environment variable must be set")
	}

	region := os.Getenv("CLOUD_ML_REGION")
	if region == "" {
		region = "us-east5"
	}

	httpClient, err := auth.CreateVertexAIClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vertex AI client: %w", err)
	}

	return &Client{
		HTTPClient: httpClient,
		ProjectID:  projectID,
		Region:     region,
	}, nil
}

func NewClientWithHTTP(httpClient *http.Client, projectID, region string) *Client {
	return &Client{
		HTTPClient: httpClient,
		ProjectID:  projectID,
		Region:     region,
	}
}

func (c *Client) RawPredict(ctx context.Context, model string, messages []Message, opts ...Option) (string, error) {
	o := &options{
		MaxTokens:   32768,
		Temperature: 0.0,
	}
	for _, opt := range opts {
		opt(o)
	}

	requestBody := map[string]any{
		"anthropic_version": "vertex-2023-10-16",
		"messages":          messages,
		"max_tokens":        o.MaxTokens,
		"temperature":       o.Temperature,
	}
	if o.System != "" {
		requestBody["system"] = o.System
	}

	reqJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict",
		c.Region, c.ProjectID, c.Region, model)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
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
