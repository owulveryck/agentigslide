package pipeline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/owulveryck/agentigslide/internal/vertex"
)

// EditVisualFinding holds the visual review result for a single edited slide.
type EditVisualFinding struct {
	PageID   string
	Approved bool
	Issues   []EditVisualIssue
}

// EditVisualIssue describes a visual problem detected on an edited slide.
type EditVisualIssue struct {
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

var editVisualReviewTool = vertex.Tool{
	Name:        "submit_edit_visual_review",
	Description: "Soumet le résultat de la review visuelle d'un slide édité.",
	InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"approved": {
			"type": "boolean",
			"description": "true si le slide est visuellement correct après édition"
		},
		"issues": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"issueType": {
						"type": "string",
						"enum": ["text_overflow", "text_truncated", "empty_field", "misalignment", "font_issue", "layout_broken"],
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

// VisualReviewEditedSlides exports a thumbnail for each affected slide and
// sends it to Claude Vision for visual quality assessment. Reviews run in
// parallel, bounded by maxParallel.
func VisualReviewEditedSlides(
	ctx context.Context,
	vc *vertex.Client,
	modelName string,
	slidesAPI SlidesAPI,
	presentationID string,
	pageIDs []string,
	maxParallel int,
) []EditVisualFinding {
	if len(pageIDs) == 0 {
		return nil
	}

	slog.Info("[agent:visual-reviewer] starting", "slides", len(pageIDs), "model", modelName)
	start := time.Now()

	if maxParallel <= 0 {
		maxParallel = 3
	}

	findings := make([]EditVisualFinding, len(pageIDs))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup

	for i, pageID := range pageIDs {
		wg.Add(1)
		go func(idx int, pid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			finding := reviewSingleSlide(ctx, vc, modelName, slidesAPI, presentationID, pid)
			findings[idx] = finding
		}(i, pageID)
	}

	wg.Wait()

	approved := 0
	for _, f := range findings {
		if f.Approved {
			approved++
		}
	}

	slog.Info("[agent:visual-reviewer] done",
		"total", len(findings),
		"approved", approved,
		"withIssues", len(findings)-approved,
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return findings
}

func reviewSingleSlide(ctx context.Context, vc *vertex.Client, modelName string, slidesAPI SlidesAPI, presentationID, pageID string) EditVisualFinding {
	var imageData []byte
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(delays); attempt++ {
		thumb, err := slidesAPI.GetPageThumbnail(presentationID, pageID)
		if err != nil {
			if attempt < len(delays) {
				slog.Warn("[agent:visual-reviewer] thumbnail API error, retrying", "pageID", pageID, "attempt", attempt+1, "error", err)
				time.Sleep(delays[attempt])
				continue
			}
			slog.Warn("[agent:visual-reviewer] failed to get thumbnail after retries", "pageID", pageID, "error", err)
			return EditVisualFinding{PageID: pageID, Approved: true}
		}

		imageData, err = fetchThumbnailWithRetry(ctx, thumb.ContentUrl)
		if err != nil {
			if attempt < len(delays) {
				slog.Warn("[agent:visual-reviewer] thumbnail fetch error, retrying", "pageID", pageID, "attempt", attempt+1, "error", err)
				time.Sleep(delays[attempt])
				continue
			}
			slog.Warn("[agent:visual-reviewer] failed to fetch thumbnail after retries", "pageID", pageID, "error", err)
			return EditVisualFinding{PageID: pageID, Approved: true}
		}
		break
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
				Text: `Analyse ce slide Google Slides qui vient d'être édité.
Vérifie :
- Pas de texte qui déborde de sa zone (overflow/troncature)
- Pas de champ de texte visiblement vide alors qu'il devrait contenir du contenu
- Éléments correctement alignés et positionnés
- Polices lisibles et de taille appropriée pour leurs conteneurs
- Layout général cohérent et professionnel

Si tout est correct, approuve. Sinon, liste les problèmes détectés.`,
			},
		},
	}}

	resp, err := vc.RawPredictFull(ctx, modelName, messages,
		vertex.WithTools([]vertex.Tool{editVisualReviewTool}),
		vertex.WithToolChoice(map[string]any{"type": "tool", "name": "submit_edit_visual_review"}),
		vertex.WithTemperature(0.1),
		vertex.WithMaxTokens(2048),
	)
	if err != nil {
		slog.Warn("[agent:visual-reviewer] API call failed", "pageID", pageID, "error", err)
		return EditVisualFinding{PageID: pageID, Approved: true}
	}

	slog.Info("[agent:visual-reviewer] API usage",
		"pageID", pageID,
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
	)

	block := resp.ToolUseBlock()
	if block != nil {
		slog.Debug("[agent:visual-reviewer] raw tool response", "pageID", pageID, "input", string(block.Input))
	}
	if block == nil {
		slog.Warn("[agent:visual-reviewer] no tool_use block", "pageID", pageID)
		return EditVisualFinding{PageID: pageID, Approved: true}
	}

	var result struct {
		Approved bool              `json:"approved"`
		Issues   []EditVisualIssue `json:"issues"`
	}
	if err := json.Unmarshal(block.Input, &result); err != nil {
		slog.Warn("[agent:visual-reviewer] failed to parse result", "pageID", pageID, "error", err)
		return EditVisualFinding{PageID: pageID, Approved: true}
	}

	return EditVisualFinding{
		PageID:   pageID,
		Approved: result.Approved,
		Issues:   result.Issues,
	}
}

func fetchThumbnailWithRetry(_ context.Context, url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("thumbnail fetch returned status %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("thumbnail fetch returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
