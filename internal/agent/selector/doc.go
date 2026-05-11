// Package selector implements the Selector agent for the presentation
// generation pipeline.
//
// The Selector maps each [agent.SlideNeed] from the outline to the best
// matching template slide from the catalog. It analyzes the content
// requirements (item count, field types, visual style) and selects the
// template that provides the best fit.
//
// As an A2A agent, it implements [a2asrv.AgentExecutor]. It accepts a
// JSON payload containing the outline and catalog, and returns a
// [agent.SelectionPlan] as an artifact.
package selector
