package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/vertex"
)

const selectorSystemPrompt = `Tu es un expert en sélection de slides template pour des présentations professionnelles.
Ton rôle est de faire correspondre chaque besoin de slide (décrit dans le plan structuré) avec le template le plus adapté du catalogue disponible.

LECTURE DU CATALOGUE :
Le catalogue indique pour chaque slide ses champs catégorisés : [T titre, S sous-titre, C contenu].
- "titre" = champ titre principal
- "sous-titre" = champ sous-titre
- "contenu" = zones de contenu (quadrants, colonnes, cartes, blocs...)

CRITÈRES DE SÉLECTION :
1. ADÉQUATION DISPOSITION : La disposition de la slide doit correspondre au contenu. 3 éléments parallèles → slide 3 colonnes. Préfère les templates dont le nombre de zones contenu est proche de itemCount.
2. ADÉQUATION TAILLE : La longueur maximale d'un élément (maxItemLength) doit rentrer dans les capacités ~N caractères des champs contenu. Choisis des champs suffisamment grands.
3. TYPE DE SLIDE : Le type (cover, section_divider, content, data, conclusion) doit correspondre au slideType demandé.
4. DIVERSITÉ : Utilise des slides variées. Ne réutilise pas toujours les mêmes templates.
5. TITRE : Si needsTitle=true, préfère un template avec au moins 1 champ "titre".
6. SOUS-TITRE : Si needsSubtitle=false, préfère un template sans champs "sous-titre".

Tu ne dois PAS mapper les champs — le Writer s'en chargera. Tu choisis uniquement quel template utiliser.`

// SelectorAgent maps each SlideNeed from the outline to the best template
// slide from the catalog.
type SelectorAgent struct {
	client *vertex.Client
	model  string
}

// NewSelectorAgent creates a SelectorAgent.
func NewSelectorAgent(client *vertex.Client, model string) *SelectorAgent {
	return &SelectorAgent{client: client, model: model}
}

func (a *SelectorAgent) selectorTool() vertex.Tool {
	return vertex.Tool{
		Name:        "select_templates",
		Description: "Sélectionne les templates les plus adaptés pour chaque besoin de slide.",
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"selections": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"outlineIndex": {
						"type": "integer",
						"description": "Index global du SlideNeed dans le plan (0-based, en comptant tous les slideNeeds de toutes les sections dans l'ordre)"
					},
					"sourceSlide": {
						"type": "integer",
						"description": "Numéro du slide template choisi dans le catalogue"
					},
					"rationale": {
						"type": "string",
						"description": "Justification du choix de ce template"
					}
				},
				"required": ["outlineIndex", "sourceSlide", "rationale"]
			}
		}
	},
	"required": ["selections"]
}`),
	}
}

// Run executes the Selector agent: sends the outline and catalog to Claude
// and returns the template selection plan.
func (a *SelectorAgent) Run(ctx context.Context, outline *PresentationOutline, compactCatalog string, templateInstructions string, previousErrors ...string) (*SelectionPlan, error) {
	slog.Info("[agent:selector] mapping outline to templates", "model", a.model)
	start := time.Now()

	outlineJSON, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("selector: failed to marshal outline: %w", err)
	}

	prompt := fmt.Sprintf(`PLAN STRUCTURÉ DE LA PRÉSENTATION :
%s

CATALOGUE DES SLIDES TEMPLATE DISPONIBLES :
%s

Pour chaque SlideNeed du plan, sélectionne le template le plus adapté et mappe les champs.
L'outlineIndex est l'index global du SlideNeed en parcourant toutes les sections dans l'ordre (0-based).`, string(outlineJSON), compactCatalog)

	if len(previousErrors) > 0 && previousErrors[0] != "" {
		slog.Info("[agent:selector] retrying with validation feedback", "model", a.model)
		prompt += fmt.Sprintf(`

ERREURS DE VALIDATION DE LA TENTATIVE PRÉCÉDENTE :
%s

CORRIGE ces erreurs en choisissant des templates qui existent dans le catalogue.
Vérifie que chaque sourceSlide correspond bien à un numéro de SLIDE listé dans le catalogue.`, previousErrors[0])
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	systemPrompt := selectorSystemPrompt
	if templateInstructions != "" {
		systemPrompt += "\n\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions
	}

	tool := a.selectorTool()
	resp, err := a.client.RawPredictFull(ctx, a.model, messages,
		vertex.WithSystem(systemPrompt),
		vertex.WithTools([]vertex.Tool{tool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "select_templates"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(16384),
	)
	if err != nil {
		return nil, fmt.Errorf("selector API call failed: %w", err)
	}

	if resp.StopReason == "max_tokens" {
		return nil, fmt.Errorf("selector: response truncated (max_tokens reached)")
	}

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, fmt.Errorf("selector: no tool_use block in response")
	}

	var selPlan SelectionPlan
	if err := json.Unmarshal(block.Input, &selPlan); err != nil {
		return nil, fmt.Errorf("selector: failed to parse selection plan: %w", err)
	}

	for i, sel := range selPlan.Selections {
		slog.Info("[agent:selector]   slide mapped",
			"position", i+1,
			"sourceSlide", sel.SourceSlide,
			"rationale", sel.Rationale,
		)
	}

	slog.Info("[agent:selector] done",
		"selections", len(selPlan.Selections),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &selPlan, nil
}
