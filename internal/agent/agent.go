// Package agent implements the multi-agent pipeline for Google Slides
// presentation generation.
//
// The pipeline consists of four specialized agents, each living in its own
// sub-package and implementing the A2A (Agent-to-Agent) protocol via
// [github.com/a2aproject/a2a-go/v2/a2asrv.AgentExecutor]:
//
//   - outliner: structures the user request into a PresentationOutline
//   - selector: maps each SlideNeed to the best template slide from the catalog
//   - writer: generates text content for each slide's editable fields
//   - reviewer: validates the assembled plan against quality rules
//
// The orchestrator sub-package coordinates these agents into a coherent
// pipeline (Outliner → Selector → Writers → Reviewer) with validation,
// retry loops, and parallel writer execution.
//
// This parent package holds shared types (PresentationOutline, SelectionPlan,
// SlideContent, etc.), validation logic, configuration, and helper functions
// used across sub-packages.
//
// See ADR 007 (docs/adr/007-a2a-architecture.md) for the architectural
// rationale behind the A2A integration.
package agent

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
)

const (
	// ProviderOrg is the organization name used in A2A AgentCards.
	ProviderOrg = "OCTO Technology"

	// ProviderURL is the provider URL used in A2A AgentCards.
	ProviderURL = "https://octo.com"

	// AgentVersion is the current version of the agent implementations.
	AgentVersion = "0.1.0"
)

// DefaultProvider returns the shared AgentProvider for all agents.
func DefaultProvider() *a2a.AgentProvider {
	return &a2a.AgentProvider{
		Org: ProviderOrg,
		URL: ProviderURL,
	}
}

// DefaultInputModes returns the standard input MIME types accepted by agents.
func DefaultInputModes() []string {
	return []string{"application/json", "text/plain"}
}

// DefaultOutputModes returns the standard output MIME types produced by agents.
func DefaultOutputModes() []string {
	return []string{"application/json"}
}
