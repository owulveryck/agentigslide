package vertex

type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Source *DataSource `json:"source,omitempty"`
}

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

type Option func(*options)

func WithMaxTokens(n int) Option {
	return func(o *options) { o.MaxTokens = n }
}

func WithTemperature(t float64) Option {
	return func(o *options) { o.Temperature = t }
}

func WithSystem(s string) Option {
	return func(o *options) { o.System = s }
}
