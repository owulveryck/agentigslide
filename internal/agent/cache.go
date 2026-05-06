package agent

import "github.com/owulveryck/agentigslide/internal/vertex"

// buildSystemBlocks constructs the system prompt as an array of content blocks
// with a cache_control breakpoint on the last block. This enables Anthropic's
// prompt caching: all content up to the breakpoint is cached and reused across
// calls with the same prefix, reducing token costs for parallel Writers.
func buildSystemBlocks(systemPrompt, templateInstructions string) []vertex.ContentBlock {
	if templateInstructions == "" {
		return []vertex.ContentBlock{{
			Type:         "text",
			Text:         systemPrompt,
			CacheControl: &vertex.CacheControl{Type: "ephemeral"},
		}}
	}
	return []vertex.ContentBlock{
		{
			Type: "text",
			Text: systemPrompt,
		},
		{
			Type:         "text",
			Text:         "INSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions,
			CacheControl: &vertex.CacheControl{Type: "ephemeral"},
		},
	}
}
