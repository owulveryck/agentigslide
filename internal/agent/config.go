package agent

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// Config holds the per-agent model names and orchestrator parameters. All
// fields are loaded from environment variables with the "AGENT" prefix.
type Config struct {
	OutlinerModel     string `envconfig:"OUTLINER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Outliner agent (structural analysis)"`
	SelectorModel     string `envconfig:"SELECTOR_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Selector agent (template matching)"`
	WriterModel       string `envconfig:"WRITER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Writer agent (complex slides, >2 fields)"`
	WriterSimpleModel string `envconfig:"WRITER_SIMPLE_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for the Writer agent (simple slides, <=2 fields)"`
	ReviewerModel     string `envconfig:"REVIEWER_MODEL" default:"claude-opus-4-6" desc:"Claude model for the Reviewer agent (quality validation)"`
	MaxParallel       int    `envconfig:"MAX_PARALLEL" default:"5" desc:"Max concurrent Writer goroutines"`
	MaxReviewRetries  int    `envconfig:"MAX_REVIEW_RETRIES" default:"2" desc:"Max review-correction iterations"`
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
