package designer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/owulveryck/agentigslide/internal/vertex"

	"google.golang.org/api/slides/v1"
)

// VisualReviewResult contains the outcome of a visual review.
type VisualReviewResult struct {
	Approved bool                `json:"approved"`
	Issues   []VisualReviewIssue `json:"issues"`
}

// VisualReviewIssue describes a visual problem detected in a rendered diagram.
type VisualReviewIssue struct {
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

var visualReviewTool = vertex.Tool{
	Name:        "submit_visual_review",
	Description: "Soumet le résultat de la review visuelle d'un diagramme.",
	InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"approved": {
			"type": "boolean",
			"description": "true si le diagramme est visuellement correct"
		},
		"issues": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"issueType": {
						"type": "string",
						"enum": ["overlap", "truncated_text", "too_small", "too_large", "layout_messy", "unreadable"],
						"description": "Type de problème visuel"
					},
					"description": {
						"type": "string",
						"description": "Description du problème"
					},
					"suggestion": {
						"type": "string",
						"description": "Suggestion de correction"
					}
				},
				"required": ["issueType", "description"]
			}
		}
	},
	"required": ["approved", "issues"]
}`),
}

// ReviewDiagramVisual exports a slide thumbnail and submits it to Claude Vision
// for visual quality review.
func ReviewDiagramVisual(ctx context.Context, client *vertex.Client, modelName string, slidesSrv *slides.Service, presentationID, pageObjectID string) (*VisualReviewResult, vertex.Usage, error) {
	slog.Info("[agent:designer:visual] starting visual review",
		"model", modelName,
		"pageObjectID", pageObjectID,
	)
	start := time.Now()

	thumb, err := slidesSrv.Presentations.Pages.GetThumbnail(presentationID, pageObjectID).
		ThumbnailPropertiesThumbnailSize("LARGE").Do()
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("failed to get slide thumbnail: %w", err)
	}

	imageData, err := fetchThumbnailData(ctx, thumb.ContentUrl)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("failed to fetch thumbnail: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{
			{
				Type: "image",
				Source: &vertex.DataSource{
					Type:      "base64",
					MediaType: "image/png",
					Data:      b64,
				},
			},
			{
				Type: "text",
				Text: `Analyse ce diagramme rendu sur un slide Google Slides.
Vérifie :
- Pas de chevauchement entre les formes
- Texte lisible et non tronqué dans toutes les formes
- Formes de taille raisonnable (ni trop petites ni trop grandes)
- Layout clair et ordonné
- Connexions/flèches correctement positionnées

Si tout est correct, approuve. Sinon, liste les problèmes détectés.`,
			},
		},
	}}

	resp, err := client.RawPredictFull(ctx, modelName, messages,
		vertex.WithTools([]vertex.Tool{visualReviewTool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_visual_review"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(2048),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("visual review API call failed: %w", err)
	}

	slog.Info("[agent:designer:visual] API usage",
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
	)

	block := resp.ToolUseBlock()
	if block == nil {
		return nil, resp.Usage, fmt.Errorf("visual review: no tool_use block in response")
	}

	var result VisualReviewResult
	if err := json.Unmarshal(block.Input, &result); err != nil {
		return nil, resp.Usage, fmt.Errorf("visual review: failed to parse result: %w", err)
	}

	slog.Info("[agent:designer:visual] done",
		"approved", result.Approved,
		"issues", len(result.Issues),
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return &result, resp.Usage, nil
}

func fetchThumbnailData(_ context.Context, url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("thumbnail fetch returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
