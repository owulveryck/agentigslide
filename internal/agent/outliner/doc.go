// Package outliner implements the Outliner agent for the presentation
// generation pipeline.
//
// The Outliner analyzes a user request and produces a structured
// [agent.PresentationOutline] independently of available templates. It
// identifies sections, determines slide needs (intent, content items, type),
// and establishes the logical structure of the presentation.
//
// The agent supports two execution modes:
//   - [Agent.Run]: single-shot outline generation
//   - [Agent.RunInteractive]: multi-turn refinement loop where the user
//     reviews and adjusts the outline before proceeding
//
// As an A2A agent, it implements [a2asrv.AgentExecutor]. The interactive
// mode maps to A2A's TaskStateInputRequired — the task is suspended and
// resumed when the client sends a new message on the same task ID.
package outliner
