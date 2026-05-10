// Package writer implements the Writer agent for the presentation
// generation pipeline.
//
// The Writer generates text content for a single slide by mapping content
// items from the outline to the template's editable fields. It respects
// field roles (titre, sous-titre, contenu), maximum character limits, and
// supports markdown formatting (bold, italic, bullet lists).
//
// Multiple Writer instances run in parallel during the pipeline, one per
// slide. The writer model is selected based on slide complexity: a simpler
// model for slides with 2 or fewer fields, a more capable model otherwise.
//
// As an A2A agent, it implements [a2asrv.AgentExecutor].
package writer
