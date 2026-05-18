// Package pipeline provides the shared presentation generation logic used by
// both the slidegen CLI and the MCP server. It handles prompt construction,
// Claude API communication via Vertex AI, and plan execution against the
// Google Slides and Drive APIs.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/owulveryck/agentigslide/internal/diagram"
	"github.com/owulveryck/agentigslide/internal/model"
	islides "github.com/owulveryck/agentigslide/internal/slides"
	"github.com/owulveryck/agentigslide/internal/vertex"
	"github.com/owulveryck/agentigslide/markdown"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/slides/v1"
)

// AmendPromptData holds the data for rendering the amend prompt template.
type AmendPromptData struct {
	ExistingPlan      string
	TemplateIndex     string
	AmendmentRequest  string
	ExtraInstructions string
}

// BuildAmendPrompt renders the embedded amend prompt template with the given data.
func BuildAmendPrompt(data AmendPromptData) string {
	var buf strings.Builder
	if err := amendPromptTmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("amend prompt template render failed: %v", err))
	}
	return buf.String()
}

// BuildAmendPromptCustom renders a user-provided amend prompt template string.
func BuildAmendPromptCustom(tmplContent string, data AmendPromptData) (string, error) {
	if err := validateTemplate(tmplContent, amendRequiredFields); err != nil {
		return "", fmt.Errorf("custom amend prompt template: %w", err)
	}
	t, err := template.New("custom-amend").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("invalid amend prompt template: %w", err)
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("amend prompt template render failed: %w", err)
	}
	return buf.String(), nil
}

// PlanToGenerationSummary converts an enriched PresentationPlan back to a
// GenerationPlan JSON string. This is used to show Claude the current state of
// a plan when amending it.
func PlanToGenerationSummary(p *model.PresentationPlan) string {
	summary := model.GenerationPlan{
		PresentationTitle: p.PresentationTitle,
	}
	for _, s := range p.Slides {
		sr := model.SlideRequest{
			SourceSlide: s.SourceSlideNumber,
		}
		for _, obj := range s.EditableObjects {
			if obj.Modified && obj.NewValue != nil {
				sr.Modifications = append(sr.Modifications, model.TextModification{
					VariableName: obj.VariableName,
					NewText:      *obj.NewValue,
				})
			}
		}
		summary.Slides = append(summary.Slides, sr)
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	return string(data)
}

// LoadTemplateInstructions loads additional template-specific instructions from
// PROMPT.md in the given template directory. Returns an empty string if the
// file does not exist.
func LoadTemplateInstructions(templateDir string) string {
	data, err := os.ReadFile(filepath.Join(templateDir, "PROMPT.md"))
	if err != nil {
		return ""
	}
	slog.Info("loaded template-specific instructions", "path", filepath.Join(templateDir, "PROMPT.md"))
	return strings.TrimSpace(string(data))
}

// SendPrompt sends a prompt to Claude via Vertex AI and parses the JSON response
// into a GenerationPlan.
func SendPrompt(ctx context.Context, vc *vertex.Client, modelName, prompt string) (*model.GenerationPlan, error) {
	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	responseText, err := vc.RawPredict(ctx, modelName, messages, vertex.WithTemperature(0.2))
	if err != nil {
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	var p model.GenerationPlan
	if err := json.Unmarshal([]byte(responseText), &p); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &p, nil
}

// ExecutePlan creates a Google Slides presentation by duplicating template slides
// according to the plan, then applies text modifications with markdown formatting.
func ExecutePlan(ctx context.Context, plan *model.PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (presId string, err error) {
	slog.Info("copying template", "templateID", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to copy template: %w", err)
	}
	presId = copiedFile.Id
	slog.Info("presentation created", "presentationID", presId)

	pres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get presentation: %w", err)
	}

	pageMap := make(map[string]*slides.Page, len(pres.Slides))
	for _, page := range pres.Slides {
		pageMap[page.ObjectId] = page
	}

	refsByPlanIndex := make(map[int]model.SlideRef, len(plan.Slides))
	diagramSlideIndices := make(map[int]bool)
	var dupRefs []model.SlideRef
	var dupRequests []*slides.Request
	dupCounter := 0

	for i, spec := range plan.Slides {
		if spec.Diagram != nil {
			diagramSlideIndices[i] = true
			continue
		}

		srcId := spec.SourceSlideID
		srcPage, ok := pageMap[srcId]
		if !ok {
			slog.Warn("slide not found in presentation, skipping", "slideNumber", spec.SourceSlideNumber, "slideID", srcId)
			continue
		}

		dupCounter++
		objectIds := make(map[string]string)
		newPageId := fmt.Sprintf("d%d_%s", dupCounter, srcId)
		objectIds[srcId] = newPageId

		for _, elId := range islides.CollectElementIds(srcPage) {
			objectIds[elId] = fmt.Sprintf("d%d_%s", dupCounter, elId)
		}

		dupRequests = append(dupRequests, &slides.Request{
			DuplicateObject: &slides.DuplicateObjectRequest{
				ObjectId:  srcId,
				ObjectIds: objectIds,
			},
		})

		ref := model.SlideRef{PageObjectID: newPageId, ElementMap: objectIds}
		refsByPlanIndex[i] = ref
		dupRefs = append(dupRefs, ref)
	}

	if len(dupRequests) > 0 {
		slog.Info("duplicating slides", "count", len(dupRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: dupRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to duplicate slides: %w", err)
		}
	}

	var deleteRequests []*slides.Request
	for _, page := range pres.Slides {
		deleteRequests = append(deleteRequests, &slides.Request{
			DeleteObject: &slides.DeleteObjectRequest{
				ObjectId: page.ObjectId,
			},
		})
	}

	if len(deleteRequests) > 0 {
		slog.Info("deleting original template slides", "count", len(deleteRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: deleteRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to delete original slides: %w", err)
		}
	}

	// Reorder slides to match plan order.
	// DuplicateObject places copies next to sources, not at the end.
	// Moving each slide to position 0 in reverse plan order produces the correct order.
	var reorderRequests []*slides.Request
	for i := len(dupRefs) - 1; i >= 0; i-- {
		reorderRequests = append(reorderRequests, &slides.Request{
			UpdateSlidesPosition: &slides.UpdateSlidesPositionRequest{
				SlideObjectIds:  []string{dupRefs[i].PageObjectID},
				InsertionIndex:  0,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})
	}

	if len(reorderRequests) > 0 {
		slog.Info("reordering slides", "count", len(dupRefs))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: reorderRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to reorder slides: %w", err)
		}
	}

	freshPres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to re-read presentation: %w", err)
	}
	textPresence := islides.BuildTextPresenceMap(freshPres)
	shapeSet := islides.BuildShapeSet(freshPres)

	var updateRequests []*slides.Request
	for i, spec := range plan.Slides {
		ref, ok := refsByPlanIndex[i]
		if !ok {
			continue
		}
		for _, obj := range spec.EditableObjects {
			if !obj.Modified || obj.NewValue == nil || obj.ObjectID == "" {
				continue
			}
			actualId := ref.ElementMap[obj.ObjectID]
			if actualId == "" {
				actualId = obj.ObjectID
			}

			if obj.CellLocation != nil {
				cellLoc := &slides.TableCellLocation{
					RowIndex:    int64(obj.CellLocation.RowIndex),
					ColumnIndex: int64(obj.CellLocation.ColumnIndex),
				}
				cellKey := fmt.Sprintf("%s_%d_%d", actualId, obj.CellLocation.RowIndex, obj.CellLocation.ColumnIndex)
				if textPresence[cellKey] {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId:     actualId,
							CellLocation: cellLoc,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				updateRequests = append(updateRequests, markdown.InsertMarkdownContentInCell(*obj.NewValue, actualId, cellLoc)...)
			} else {
				if !shapeSet[actualId] {
					slog.Warn("skipping text update for non-SHAPE element",
						"objectId", actualId,
						"variableName", obj.VariableName,
						"elementType", obj.ElementType)
					continue
				}
				if textPresence[actualId] {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId: actualId,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				updateRequests = append(updateRequests, markdown.InsertMarkdownContent(*obj.NewValue, actualId)...)
			}
		}
	}
	markdown.SortRequests(updateRequests)

	if len(updateRequests) > 0 {
		slog.Info("updating text elements", "count", len(updateRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to update text content: %w", err)
		}
	}

	// Phase: create diagram slides and shapes.
	if len(diagramSlideIndices) > 0 {
		diagramPageIDs := make(map[int]string)
		var createSlideRequests []*slides.Request
		for i, spec := range plan.Slides {
			if !diagramSlideIndices[i] || spec.Diagram == nil {
				continue
			}
			pageID := fmt.Sprintf("diag_page_%d", i)
			diagramPageIDs[i] = pageID
			createSlideRequests = append(createSlideRequests, &slides.Request{
				CreateSlide: &slides.CreateSlideRequest{ObjectId: pageID},
			})
		}

		if len(createSlideRequests) > 0 {
			slog.Info("creating blank diagram slides", "count", len(createSlideRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
				Requests: createSlideRequests,
			}).Do()
			if err != nil {
				return "", fmt.Errorf("failed to create diagram slides: %w", err)
			}
		}

		// Re-read to find auto-added placeholders on diagram slides, then delete them.
		diagPres, err := slidesSrv.Presentations.Get(presId).Do()
		if err != nil {
			return "", fmt.Errorf("failed to re-read presentation for diagram cleanup: %w", err)
		}
		diagramPageSet := make(map[string]bool, len(diagramPageIDs))
		for _, pid := range diagramPageIDs {
			diagramPageSet[pid] = true
		}
		var cleanupRequests []*slides.Request
		for _, page := range diagPres.Slides {
			if !diagramPageSet[page.ObjectId] {
				continue
			}
			for _, el := range page.PageElements {
				cleanupRequests = append(cleanupRequests, &slides.Request{
					DeleteObject: &slides.DeleteObjectRequest{ObjectId: el.ObjectId},
				})
			}
		}
		if len(cleanupRequests) > 0 {
			slog.Info("removing placeholders from diagram slides", "count", len(cleanupRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
				Requests: cleanupRequests,
			}).Do()
			if err != nil {
				return "", fmt.Errorf("failed to clean diagram slides: %w", err)
			}
		}

		// Now add the diagram shapes.
		var shapeRequests []*slides.Request
		for i, spec := range plan.Slides {
			pageID, ok := diagramPageIDs[i]
			if !ok || spec.Diagram == nil {
				continue
			}
			positioned, err := diagram.Layout(spec.Diagram, pageID)
			if err != nil {
				slog.Warn("diagram layout failed", "slideIndex", i, "error", err)
				continue
			}
			shapeRequests = append(shapeRequests, diagram.Render(positioned)...)
		}
		if len(shapeRequests) > 0 {
			slog.Info("creating diagram shapes", "count", len(shapeRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
				Requests: shapeRequests,
			}).Do()
			if err != nil {
				return "", fmt.Errorf("failed to create diagram shapes: %w", err)
			}
		}
	}

	slog.Info("presentation complete", "url", fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId))
	return presId, nil
}
