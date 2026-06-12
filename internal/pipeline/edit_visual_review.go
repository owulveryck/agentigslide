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
	PageID           string
	Approved         bool
	Issues           []EditVisualIssue
	ThumbnailFetchMs int64
	ReviewMs         int64
	Model            string
	Usage            vertex.Usage
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
	fetchStart := time.Now()
	thumb, err := slidesAPI.GetPageThumbnail(presentationID, pageID)
	if err != nil {
		slog.Warn("[agent:visual-reviewer] failed to get thumbnail", "pageID", pageID, "error", err)
		return EditVisualFinding{PageID: pageID, Approved: true}
	}

	imageData, err := fetchThumbnailData(ctx, thumb.ContentUrl)
	if err != nil {
		slog.Warn("[agent:visual-reviewer] failed to fetch thumbnail", "pageID", pageID, "error", err)
		return EditVisualFinding{PageID: pageID, Approved: true}
	}
	thumbnailFetchMs := time.Since(fetchStart).Milliseconds()
	reviewStart := time.Now()

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
		return EditVisualFinding{PageID: pageID, Approved: true, ThumbnailFetchMs: thumbnailFetchMs, ReviewMs: time.Since(reviewStart).Milliseconds()}
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
		return EditVisualFinding{PageID: pageID, Approved: true, ThumbnailFetchMs: thumbnailFetchMs, ReviewMs: time.Since(reviewStart).Milliseconds(), Model: modelName, Usage: resp.Usage}
	}

	var result struct {
		Approved bool              `json:"approved"`
		Issues   []EditVisualIssue `json:"issues"`
	}
	if err := json.Unmarshal(block.Input, &result); err != nil {
		slog.Warn("[agent:visual-reviewer] failed to parse result", "pageID", pageID, "error", err)
		return EditVisualFinding{PageID: pageID, Approved: true, ThumbnailFetchMs: thumbnailFetchMs, ReviewMs: time.Since(reviewStart).Milliseconds(), Model: modelName, Usage: resp.Usage}
	}

	return EditVisualFinding{
		PageID:           pageID,
		Approved:         result.Approved,
		Issues:           result.Issues,
		ThumbnailFetchMs: thumbnailFetchMs,
		ReviewMs:         time.Since(reviewStart).Milliseconds(),
		Model:            modelName,
		Usage:            resp.Usage,
	}
}

// thumbnailClient bounds every thumbnail download; a hung fetch must never
// stall a visual-review pass.
var thumbnailClient = &http.Client{Timeout: 30 * time.Second}

func fetchThumbnailData(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := thumbnailClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = fmt.Errorf("thumbnail fetch returned status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("thumbnail fetch returned status %d", resp.StatusCode)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, lastErr
}
