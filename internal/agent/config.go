package agent

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Config holds the per-agent model names and orchestrator parameters. All
// fields are loaded from environment variables with the "AGENT" prefix.
type Config struct {
	OutlinerModel          string `envconfig:"OUTLINER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Outliner agent (structural analysis)"`
	SelectorModel          string `envconfig:"SELECTOR_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Selector agent (template matching)"`
	WriterModel            string `envconfig:"WRITER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Writer agent (complex slides, >2 fields)"`
	WriterSimpleModel      string `envconfig:"WRITER_SIMPLE_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for the Writer agent (simple slides, <=2 fields)"`
	OutlinerMaxTokens      int    `envconfig:"OUTLINER_MAX_TOKENS" default:"32768" desc:"Max output tokens for the Outliner agent"`
	ReviewerModel          string `envconfig:"REVIEWER_MODEL" default:"claude-opus-4-6" desc:"Claude model for the Reviewer agent (quality validation)"`
	ReviewerThinkingBudget int    `envconfig:"REVIEWER_THINKING_BUDGET" default:"5120" desc:"Thinking budget tokens for Reviewer (0 to disable)"`
	MaxParallel            int    `envconfig:"MAX_PARALLEL" default:"5" desc:"Max concurrent Writer goroutines"`
	MaxReviewRetries       int    `envconfig:"MAX_REVIEW_RETRIES" default:"2" desc:"Max review-correction iterations"`
	MaxSelectorRetries     int    `envconfig:"MAX_SELECTOR_RETRIES" default:"2" desc:"Max selector retry attempts on validation failure"`
}

// LoadConfig loads the agent Config from environment variables with the
// "AGENT" prefix.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := envconfig.Process("AGENT", &cfg); err != nil {
		return cfg, fmt.Errorf("loading AGENT config: %w", err)
	}
	return cfg, nil
}
