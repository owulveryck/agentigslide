package agent

import "github.com/owulveryck/agentigslide/internal/vertex"

// BuildSystemBlocks constructs the system prompt as an array of content blocks
// with a cache_control breakpoint on the last block. This enables Anthropic's
// prompt caching: all content up to the breakpoint is cached and reused across
// calls with the same prefix, reducing token costs for parallel Writers.
//
// agentMemory contains guidelines learned from previous pipeline runs. It is
// placed between the base system prompt and template instructions so that it
// benefits from prompt caching (its content rarely changes within a run).
func BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory string) []vertex.ContentBlock {
	var blocks []vertex.ContentBlock

	blocks = append(blocks, vertex.ContentBlock{
		Type: "text",
		Text: systemPrompt,
	})

	if agentMemory != "" {
		blocks = append(blocks, vertex.ContentBlock{
			Type: "text",
			Text: "MÉMOIRE DE L'AGENT (guidelines issues des exécutions précédentes — respecte ces consignes) :\n" +
				"Ces guidelines sont SUBORDONNÉES au catalogue et à la configuration du template : " +
				"en cas de contradiction avec le catalogue, la configuration ou les données fournies dans cette requête, ignore la guideline.\n" +
				agentMemory,
		})
	}

	if templateInstructions != "" {
		blocks = append(blocks, vertex.ContentBlock{
			Type: "text",
			Text: "INSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions,
		})
	}

	blocks[len(blocks)-1].CacheControl = &vertex.CacheControl{Type: "ephemeral"}
	return blocks
}
