package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/vertex"
)

func generatePresentationSummary(ctx context.Context, client *vertex.Client, modelName string, plan *model.PresentationPlan, userRequest string) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Titre : %s\n", plan.PresentationTitle))
	sb.WriteString(fmt.Sprintf("Slides : %d\n\n", len(plan.Slides)))

	for i, slide := range plan.Slides {
		sb.WriteString(fmt.Sprintf("--- Slide %d (template %d) ---\n", i+1, slide.SourceSlideNumber))
		if slide.Intention != "" {
			sb.WriteString(fmt.Sprintf("  Intention: %s\n", slide.Intention))
		}
		for _, obj := range slide.EditableObjects {
			if obj.NewValue != nil && *obj.NewValue != "" {
				sb.WriteString(fmt.Sprintf("  %s: %s\n", obj.VariableName, truncate(*obj.NewValue, 200)))
			}
		}
	}

	prompt := fmt.Sprintf(`Voici le contenu d'une présentation Google Slides générée automatiquement.

DEMANDE ORIGINALE :
%s

CONTENU GÉNÉRÉ :
%s

Produis une synthèse concise (5-10 lignes) de cette présentation :
- Quel est le sujet principal
- Quels sont les points clés abordés
- Comment la présentation est structurée
- Tout point d'attention notable

Réponds directement avec la synthèse, sans préambule.`, userRequest, sb.String())

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	resp, err := client.RawPredict(ctx, modelName, messages,
		vertex.WithMaxTokens(2048),
		vertex.WithTemperature(0.3),
	)
	if err != nil {
		return "", fmt.Errorf("summary API call failed: %w", err)
	}

	return resp, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func readUserRequestOrEmpty(filePath string) []byte {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}
		return data
	}
	return nil
}
