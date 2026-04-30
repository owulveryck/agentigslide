// Package config provides shared configuration structs loaded from environment
// variables using the kelseyhightower/envconfig library. All CLI tools in this
// project load their shared configuration (template ID, credentials path)
// through this package with the "SLIDES" prefix.
package config

import (
	"fmt"
	"os"

	"github.com/kelseyhightower/envconfig"
)

// SlidesConfig holds shared configuration used by most CLI tools.
// Environment variables use the prefix "SLIDES" (e.g. SLIDES_TEMPLATE_ID).
type SlidesConfig struct {
	TemplateID    string `envconfig:"TEMPLATE_ID" required:"true" desc:"Google Slides template presentation ID"`
	TemplateIndex string `envconfig:"TEMPLATE_INDEX" default:"template_index.json" desc:"Path to template index JSON"`
	Credentials   string `envconfig:"CREDENTIALS" desc:"Path to OAuth2 client credentials JSON"`
}

// LoadSlidesConfig loads the SlidesConfig from environment variables with the
// "SLIDES" prefix.
func LoadSlidesConfig() (SlidesConfig, error) {
	var cfg SlidesConfig
	if err := envconfig.Process("SLIDES", &cfg); err != nil {
		return cfg, fmt.Errorf("loading SLIDES config: %w", err)
	}
	return cfg, nil
}

const rowFormat = "{{range .}}  {{usage_key .}}	{{usage_type .}}	{{usage_default .}}	{{usage_required .}}	{{usage_description .}}\n{{end}}"

// PrintAllUsage prints a combined usage section with a single header and all
// config tables to stderr.
func PrintAllUsage(configs ...struct {
	Prefix string
	Spec   any
}) {
	fmt.Fprintln(os.Stderr, "\nEnvironment variables:")
	fmt.Fprintln(os.Stderr, "  KEY\tTYPE\tDEFAULT\tREQUIRED\tDESCRIPTION")
	for _, c := range configs {
		_ = envconfig.Usagef(c.Prefix, c.Spec, os.Stderr, rowFormat)
	}
}
