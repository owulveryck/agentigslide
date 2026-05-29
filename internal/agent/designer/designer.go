package designer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

// Agent generates diagram specifications for slides.
type Agent struct {
	client *vertex.Client
	model  string
}

// New creates an Agent with the given Vertex AI client and model name.
func New(client *vertex.Client, model string) *Agent {
	return &Agent{client: client, model: model}
}

var diagramTool = vertex.Tool{
	Name:        "design_diagram",
	Description: "Conçoit un diagramme en décrivant les noeuds (formes), connexions (flèches) et groupes (zones).",
	InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"title": {
			"type": "string",
			"description": "Titre du slide (optionnel, court et percutant)"
		},
		"layoutHint": {
			"type": "string",
			"enum": ["top-to-bottom", "left-to-right"],
			"description": "Direction générale du diagramme"
		},
		"nodes": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"id": {
						"type": "string",
						"description": "Identifiant unique du noeud (ex: 'api', 'db')"
					},
					"label": {
						"type": "string",
						"description": "Texte affiché dans la forme (max ~30 chars)"
					},
					"shape": {
						"type": "string",
						"enum": ["rectangle", "round_rectangle", "ellipse", "diamond", "hexagon", "text"],
						"description": "Type de forme (text = bloc texte sans bordure)"
					},
					"style": {
						"type": "string",
						"enum": ["primary", "secondary", "accent", "neutral", "highlight", "outline_only", "marine", "turquoise", "marine_light", "turquoise_light"],
						"description": "Style visuel"
					},
					"size": {
						"type": "string",
						"enum": ["small", "medium", "large", "wide"],
						"description": "Taille du noeud (small=icone, medium=defaut, large=element principal, wide=barre pleine largeur)"
					}
				},
				"required": ["id", "label"]
			}
		},
		"edges": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"from": {"type": "string", "description": "ID du noeud source"},
					"to": {"type": "string", "description": "ID du noeud destination"},
					"label": {"type": "string", "description": "Texte sur la connexion (optionnel)"},
					"lineStyle": {
						"type": "string",
						"enum": ["arrow", "line", "dashed_arrow", "dashed_line"],
						"description": "Style de ligne"
					}
				},
				"required": ["from", "to"]
			}
		},
		"groups": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "Identifiant du groupe"},
					"label": {"type": "string", "description": "Nom de la zone"},
					"nodes": {
						"type": "array",
						"items": {"type": "string"},
						"description": "IDs des noeuds dans ce groupe"
					},
					"style": {
						"type": "string",
						"enum": ["primary", "secondary", "accent", "neutral", "highlight", "outline_only", "marine", "turquoise", "marine_light", "turquoise_light"],
						"description": "Style du fond de zone"
					},
					"layoutHint": {
						"type": "string",
						"enum": ["top-to-bottom", "left-to-right"],
						"description": "Direction du layout des noeuds dans ce groupe"
					}
				},
				"required": ["id", "label", "nodes"]
			}
		}
	},
	"required": ["layoutHint", "nodes", "edges"]
}`),
}

// DesignDiagram generates a diagram specification from a slide need.
func (a *Agent) DesignDiagram(ctx context.Context, slideNeed agent.SlideNeed, templateInstructions string, agentMemory string, feedback ...agent.ReviewIssue) (*agent.DiagramSpec, vertex.Usage, error) {
	slog.Info("[agent:designer] starting",
		"model", a.model,
		"intent", slideNeed.Intent,
		"contentItems", len(slideNeed.ContentItems),
		"feedback", len(feedback),
	)
	start := time.Now()

	var contentSection string
	if len(slideNeed.ContentItems) > 0 {
		var items []string
		for i, item := range slideNeed.ContentItems {
			items = append(items, fmt.Sprintf("  [%d] %s", i, item))
		}
		contentSection = fmt.Sprintf("\n\nCONTENU DU DIAGRAMME :\n%s", strings.Join(items, "\n"))
	}

	var feedbackSection string
	if len(feedback) > 0 {
		var issues []string
		for _, fb := range feedback {
			entry := fmt.Sprintf("- [%s] %s", fb.IssueType, fb.Description)
			if fb.Suggestion != "" {
				entry += fmt.Sprintf(" → Suggestion : %s", fb.Suggestion)
			}
			issues = append(issues, entry)
		}
		feedbackSection = fmt.Sprintf("\n\nCORRECTIONS DEMANDÉES :\n%s\n\nCorrige ces problèmes dans ta réponse.", strings.Join(issues, "\n"))
	}

	prompt := fmt.Sprintf(`DIAGRAMME À CONCEVOIR
INTENTION : %s%s%s

Conçois le diagramme en identifiant les noeuds, leurs connexions et les groupes éventuels.`,
		slideNeed.Intent,
		contentSection,
		feedbackSection,
	)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystemBlocks(agent.BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory)),
		vertex.WithTools([]vertex.Tool{diagramTool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "design_diagram"}),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(8192),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("designer API call failed: %w", err)
	}

	slog.Info("[agent:designer] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
		"cacheRead", resp.Usage.CacheReadInputTokens,
		"cacheWrite", resp.Usage.CacheCreationInputTokens,
	)

	if resp.StopReason == "max_tokens" {
		return nil, resp.Usage, fmt.Errorf("designer: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("designer: no tool_use block in response")
	}

	var spec agent.DiagramSpec
	if err := json.Unmarshal(block.Input, &spec); err != nil {
		return nil, resp.Usage, fmt.Errorf("designer: failed to parse diagram spec: %w", err)
	}

	if err := validateSpec(&spec); err != nil {
		return nil, resp.Usage, fmt.Errorf("designer: invalid diagram spec: %w", err)
	}

	slog.Info("[agent:designer] done",
		"nodes", len(spec.Nodes),
		"edges", len(spec.Edges),
		"groups", len(spec.Groups),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &spec, resp.Usage, nil
}

func validateSpec(spec *agent.DiagramSpec) error {
	if len(spec.Nodes) == 0 {
		return fmt.Errorf("diagram has no nodes")
	}
	nodeIDs := make(map[string]bool, len(spec.Nodes))
	for _, n := range spec.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node has empty ID")
		}
		if nodeIDs[n.ID] {
			return fmt.Errorf("duplicate node ID: %s", n.ID)
		}
		nodeIDs[n.ID] = true
	}
	for _, e := range spec.Edges {
		if !nodeIDs[e.From] {
			return fmt.Errorf("edge references unknown source node: %s", e.From)
		}
		if !nodeIDs[e.To] {
			return fmt.Errorf("edge references unknown target node: %s", e.To)
		}
	}
	for _, g := range spec.Groups {
		for _, nid := range g.Nodes {
			if !nodeIDs[nid] {
				return fmt.Errorf("group %q references unknown node: %s", g.ID, nid)
			}
		}
	}
	return nil
}
