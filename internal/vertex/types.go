// Package vertex provides a client for calling Anthropic Claude models through
// Google Cloud Vertex AI endpoints. It handles request construction, authentication
// via Google Cloud credentials, and response parsing for Claude model predictions.
package vertex

import "encoding/json"

// Message represents a conversation message sent to the Claude API. Each message
// has a role ("user" or "assistant") and one or more content blocks.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a single content block within a message. It can be
// a text block (Type "text" with Text field), an image block (Type "image"
// with Source field), or a document block (Type "document" with Source field).
type ContentBlock struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Source *DataSource `json:"source,omitempty"`
}

// DataSource holds base64-encoded media data for image or document content blocks.
// Type is always "base64", MediaType specifies the MIME type (e.g., "image/png",
// "application/pdf"), and Data contains the base64-encoded content.
type DataSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// Tool defines a tool available to Claude during a conversation.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ContentBlockFull represents a content block in a Claude response that may
// be text, tool_use, or thinking.
type ContentBlockFull struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Text  string          `json:"text,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// FullResponse represents the complete parsed response from a Claude API call,
// including all content block types and the stop reason.
type FullResponse struct {
	Content    []ContentBlockFull `json:"content"`
	StopReason string             `json:"stop_reason"`
}

// ToolUseBlock extracts the first tool_use content block from the response.
// Returns nil if no tool_use block is found.
func (r *FullResponse) ToolUseBlock() *ContentBlockFull {
	for i := range r.Content {
		if r.Content[i].Type == "tool_use" {
			return &r.Content[i]
		}
	}
	return nil
}

type options struct {
	MaxTokens   int
	Temperature float64
	System      string
	Tools       []Tool
	ToolChoice  map[string]any
}

// Option is a functional option for configuring RawPredict requests.
type Option func(*options)

// WithMaxTokens sets the maximum number of tokens in the Claude response.
func WithMaxTokens(n int) Option {
	return func(o *options) { o.MaxTokens = n }
}

// WithTemperature sets the sampling temperature for the Claude response.
func WithTemperature(t float64) Option {
	return func(o *options) { o.Temperature = t }
}

// WithSystem sets the system prompt for the Claude request.
func WithSystem(s string) Option {
	return func(o *options) { o.System = s }
}

// WithTools sets the tools available to Claude for the request.
func WithTools(tools []Tool) Option {
	return func(o *options) { o.Tools = tools }
}

// WithToolChoice forces Claude to use a specific tool selection strategy.
// Common values: {"type": "tool", "name": "tool_name"} or {"type": "any"}.
func WithToolChoice(choice map[string]any) Option {
	return func(o *options) { o.ToolChoice = choice }
}
