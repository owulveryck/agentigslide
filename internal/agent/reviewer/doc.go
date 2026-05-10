// Package reviewer implements the Reviewer agent for the presentation
// generation pipeline.
//
// The Reviewer validates the assembled GenerationPlan against quality
// rules: content overflow, duplicate content, missing fields, template
// mismatches, incoherence with user request, and invented content. It
// supports both full plan review and incremental subset review of
// corrected slides.
//
// When extended thinking is enabled (thinkingBudget > 0), the reviewer
// uses deeper reasoning at temperature 1.0 for more thorough analysis.
//
// As an A2A agent, it implements [a2asrv.AgentExecutor].
package reviewer
