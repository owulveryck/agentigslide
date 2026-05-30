// Package formatter implements the Formatter agent, a deterministic (no LLM)
// agent that extracts structural data from Google Slides presentations,
// applies consistency rules across all slides, and generates corrections
// via the BatchUpdate API. It replaces the fixfonts package (ADR 016).
package formatter
