package agent

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds the per-agent model names and orchestrator parameters. All
// fields are loaded from environment variables with the "AGENT" prefix.
type Config struct {
	OutlinerModel              string        `envconfig:"OUTLINER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Outliner agent (structural analysis)"`
	SelectorModel              string        `envconfig:"SELECTOR_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Selector agent (template matching)"`
	WriterModel                string        `envconfig:"WRITER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Writer agent (complex slides, >2 fields)"`
	WriterSimpleModel          string        `envconfig:"WRITER_SIMPLE_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for the Writer agent (simple slides, <=2 fields)"`
	OutlinerMaxTokens          int           `envconfig:"OUTLINER_MAX_TOKENS" default:"32768" desc:"Max output tokens for the Outliner agent"`
	DesignerModel              string        `envconfig:"DESIGNER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the Designer agent (diagram creation)"`
	DiagramVisualReviewModel   string        `envconfig:"DIAGRAM_VISUAL_REVIEW_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for visual review of diagram slides"`
	MaxDiagramVisualRetries    int           `envconfig:"MAX_DIAGRAM_VISUAL_RETRIES" default:"1" desc:"Max visual review iterations for diagram slides (0 to disable)"`
	EditPlannerModel           string        `envconfig:"EDIT_PLANNER_MODEL" default:"claude-opus-4-6" desc:"Claude model for the EditPlanner agent (edit planning)"`
	EditPlannerMaxTokens       int           `envconfig:"EDIT_PLANNER_MAX_TOKENS" default:"16384" desc:"Max output tokens for the EditPlanner agent"`
	EditWriterModel            string        `envconfig:"EDIT_WRITER_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for the EditWriter agent (complex edits, >2 modifications)"`
	EditWriterSimpleModel      string        `envconfig:"EDIT_WRITER_SIMPLE_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for the EditWriter agent (simple edits, <=2 modifications)"`
	EditReviewerModel          string        `envconfig:"EDIT_REVIEWER_MODEL" default:"claude-opus-4-6" desc:"Claude model for the EditReviewer agent (edit quality validation)"`
	EditReviewerThinkingBudget int           `envconfig:"EDIT_REVIEWER_THINKING_BUDGET" default:"5120" desc:"Thinking budget tokens for EditReviewer (0 to disable)"`
	MaxEditReviewRetries       int           `envconfig:"MAX_EDIT_REVIEW_RETRIES" default:"1" desc:"Max review-correction iterations for edit pipeline"`
	EditReviewEnabled          bool          `envconfig:"EDIT_REVIEW_ENABLED" default:"false" desc:"Enable the EditReviewer step in the edit pipeline"`
	EditVisualReviewEnabled    bool          `envconfig:"EDIT_VISUAL_REVIEW_ENABLED" default:"true" desc:"Enable visual review of edited slides after execution"`
	EditVisualReviewModel      string        `envconfig:"EDIT_VISUAL_REVIEW_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for visual review of edited slides"`
	MaxEditVisualRetries       int           `envconfig:"MAX_EDIT_VISUAL_RETRIES" default:"1" desc:"Max visual feedback iterations for edit pipeline (0 to disable)"`
	FormatterEnabled           bool          `envconfig:"FORMATTER_ENABLED" default:"true" desc:"Enable the Formatter agent for formatting consistency checks"`
	EditFormatterEnabled       bool          `envconfig:"EDIT_FORMATTER_ENABLED" default:"true" desc:"Enable the Formatter agent on modified slides after edit execution"`
	VisualReviewEnabled        bool          `envconfig:"VISUAL_REVIEW_ENABLED" default:"true" desc:"Enable visual review of generated slides after creation"`
	VisualReviewModel          string        `envconfig:"VISUAL_REVIEW_MODEL" default:"claude-sonnet-4-6" desc:"Claude model for visual review of generated slides"`
	MaxVisualRetries           int           `envconfig:"MAX_VISUAL_RETRIES" default:"1" desc:"Max visual feedback iterations for generated slides (0 = review only)"`
	ReviewerModel              string        `envconfig:"REVIEWER_MODEL" default:"claude-opus-4-6" desc:"Claude model for the Reviewer agent (quality validation)"`
	ReviewerThinkingBudget     int           `envconfig:"REVIEWER_THINKING_BUDGET" default:"5120" desc:"Thinking budget tokens for Reviewer (0 to disable)"`
	MaxParallel                int           `envconfig:"MAX_PARALLEL" default:"5" desc:"Max concurrent Writer goroutines"`
	MaxReviewRetries           int           `envconfig:"MAX_REVIEW_RETRIES" default:"2" desc:"Max review-correction iterations"`
	MaxSelectorRetries         int           `envconfig:"MAX_SELECTOR_RETRIES" default:"2" desc:"Max selector retry attempts on validation failure"`
	MemoryEnabled              bool          `envconfig:"MEMORY_ENABLED" default:"true" desc:"Enable loading and synthesizing agent memory from past runs"`
	MemoryModel                string        `envconfig:"MEMORY_MODEL" default:"claude-haiku-4-5@20251001" desc:"Claude model for synthesizing memory guidelines (fast/cheap)"`
	PipelineTimeout            time.Duration `envconfig:"PIPELINE_TIMEOUT" default:"10m" desc:"Max total duration for the generation pipeline (0 to disable)"`
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
