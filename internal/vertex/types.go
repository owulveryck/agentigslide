// Package vertex provides a client for calling Anthropic Claude models through
// Google Cloud Vertex AI endpoints. It handles request construction, authentication
// via Google Cloud credentials, and response parsing for Claude model predictions.
package vertex

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

type options struct {
	MaxTokens   int
	Temperature float64
	System      string
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
