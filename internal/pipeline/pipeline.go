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
	"github.com/owulveryck/agentigslide/internal/revision"
	islides "github.com/owulveryck/agentigslide/internal/slides"
	"github.com/owulveryck/agentigslide/internal/trace"
	"github.com/owulveryck/agentigslide/internal/vertex"
	"github.com/owulveryck/agentigslide/markdown"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/slides/v1"
)

type execOptions struct {
	tracer *trace.Tracer
}

// ExecOption configures ExecutePlan behavior.
type ExecOption func(*execOptions)

// WithExecTracer attaches a debug tracer to the execution phase.
func WithExecTracer(t *trace.Tracer) ExecOption {
	return func(o *execOptions) { o.tracer = t }
}

// ExecutionResult holds the output of ExecutePlan.
type ExecutionResult struct {
	PresentationID string
	PageIDs        []string // all created slide page IDs (duplicated + diagram)
}

// AmendPromptData holds the data for rendering the amend prompt template.
type AmendPromptData struct {
	ExistingPlan      string
	TemplateIndex     string
	AmendmentRequest  string
	ExtraInstructions string
}

// BuildAmendPrompt renders the embedded amend prompt template with the given data.
func BuildAmendPrompt(data AmendPromptData) (string, error) {
	var buf strings.Builder
	if err := amendPromptTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("amend prompt template render failed: %w", err)
	}
	return buf.String(), nil
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

// LoadAgentMemory loads agent-specific memory guidelines from
// {AGENT}_MEMORY.md in the given template directory. Returns an empty string
// if the file does not exist.
func LoadAgentMemory(templateDir, agentName string) string {
	filename := strings.ToUpper(agentName) + "_MEMORY.md"
	p := filepath.Join(templateDir, filename)
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	slog.Info("loaded agent memory", "agent", agentName, "path", p)
	return content
}

// LoadAllAgentMemories loads memory files for all known agents and returns a
// map keyed by agent name.
func LoadAllAgentMemories(templateDir string) map[string]string {
	agents := []string{
		"outliner", "selector", "writer", "reviewer", "designer",
		"editplanner", "editwriter", "editreviewer", "formatter",
	}
	memories := make(map[string]string)
	for _, a := range agents {
		if m := LoadAgentMemory(templateDir, a); m != "" {
			memories[a] = m
		}
	}
	return memories
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
func ExecutePlan(ctx context.Context, plan *model.PresentationPlan, slidesAPI SlidesAPI, driveAPI DriveAPI, opts ...ExecOption) (result *ExecutionResult, revLog *revision.Log, err error) {
	var eopts execOptions
	for _, o := range opts {
		o(&eopts)
	}
	tracer := eopts.tracer
	slog.Info("copying template", "templateID", plan.TemplateID)
	copiedFile, err := driveAPI.CopyFile(ctx, plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to copy template: %w", err)
	}
	presId := copiedFile.Id
	revLog = revision.New(presId)
	slog.Info("presentation created", "presentationID", presId)

	pres, err := slidesAPI.GetPresentation(presId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get presentation: %w", err)
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
		newPageId := fmt.Sprintf("dup%d_%s", dupCounter, srcId)
		objectIds[srcId] = newPageId

		for _, elId := range islides.CollectElementIds(srcPage) {
			objectIds[elId] = fmt.Sprintf("dup%d_%s", dupCounter, elId)
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
		_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
			Requests: dupRequests,
		}, revLog, "duplicate_slides")
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to duplicate slides: %w", err)
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
		_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
			Requests: deleteRequests,
		}, revLog, "delete_originals")
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to delete original slides: %w", err)
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
		_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
			Requests: reorderRequests,
		}, revLog, "reorder_slides")
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to reorder slides: %w", err)
		}
	}

	freshPres, err := slidesAPI.GetPresentation(presId)
	if err != nil {
		return nil, revLog, fmt.Errorf("failed to re-read presentation: %w", err)
	}
	textPresence := islides.BuildTextPresenceMap(freshPres)
	shapeSet := islides.BuildShapeSet(freshPres)
	baseStyles := extractBaseStyles(freshPres)
	needsAutofit := buildNeedsAutofitMap(freshPres)

	execTrace := trace.ExecutionTrace{PresentationID: presId}

	var updateRequests []*slides.Request
	for i, spec := range plan.Slides {
		ref, ok := refsByPlanIndex[i]
		if !ok {
			continue
		}

		slideTrace := trace.SlideExecutionTrace{
			PlanIndex:     i,
			SourceSlideID: spec.SourceSlideID,
			NewPageID:     ref.PageObjectID,
			ElementMap:    ref.ElementMap,
			BaseStyles:    make(map[string]trace.BaseStyleTrace),
		}
		dupElemSet := make(map[string]bool, len(ref.ElementMap))
		for _, dupID := range ref.ElementMap {
			dupElemSet[dupID] = true
		}
		for elemID, bs := range baseStyles {
			if dupElemSet[elemID] {
				slideTrace.BaseStyles[elemID] = baseStyleToTrace(bs)
			}
		}

		// Group shape editable objects by actual element ID so that
		// fields sharing the same ObjectID (e.g. title + body in one
		// text box) produce a single DeleteText + InsertText sequence.
		shapeTexts := make(map[string][]string)
		var shapeOrder []string

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
				if _, exists := shapeTexts[actualId]; !exists {
					shapeOrder = append(shapeOrder, actualId)
				}
				shapeTexts[actualId] = append(shapeTexts[actualId], *obj.NewValue)
			}
		}

		for _, actualId := range shapeOrder {
			combinedText := strings.Join(shapeTexts[actualId], "\n")
			hadText := textPresence[actualId]
			if hadText {
				updateRequests = append(updateRequests, &slides.Request{
					DeleteText: &slides.DeleteTextRequest{
						ObjectId: actualId,
						TextRange: &slides.Range{
							Type: "ALL",
						},
					},
				})
			} else {
				slog.Warn("shape has no detected text, skipping DeleteText before insert",
					"objectId", actualId, "newTextLen", len(combinedText))
			}
			insertReqs := markdown.InsertMarkdownContent(combinedText, actualId)
			if style, ok := baseStyles[actualId]; ok {
				textLen := int64(computeInsertedLength(insertReqs))
				if textLen > 0 {
					start := int64(0)
					updateRequests = append(updateRequests, &slides.Request{
						UpdateTextStyle: &slides.UpdateTextStyleRequest{
							ObjectId: actualId,
							TextRange: &slides.Range{
								Type:       "FIXED_RANGE",
								StartIndex: &start,
								EndIndex:   &textLen,
							},
							Style:  style.style,
							Fields: style.fields,
						},
					})
				}
			}
			updateRequests = append(updateRequests, insertReqs...)
			if needsAutofit[actualId] {
				updateRequests = append(updateRequests, &slides.Request{
					UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
						ObjectId: actualId,
						ShapeProperties: &slides.ShapeProperties{
							Autofit: &slides.Autofit{AutofitType: "TEXT_AUTOFIT"},
						},
						Fields: "autofit.autofitType",
					},
				})
			}
			slideTrace.TextInsertions = append(slideTrace.TextInsertions, trace.TextInsertionTrace{
				ElementID:       actualId,
				TextLength:      len([]rune(combinedText)),
				HadExistingText: hadText,
			})
		}
		execTrace.PerSlide = append(execTrace.PerSlide, slideTrace)
	}
	markdown.SortRequests(updateRequests)

	if len(updateRequests) > 0 {
		slog.Info("updating text elements", "count", len(updateRequests))
		_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}, revLog, "update_text")
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to update text content: %w", err)
		}
	}

	// Phase: create diagram slides and shapes.
	diagramPageIDs := make(map[int]string)
	if len(diagramSlideIndices) > 0 {
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
			_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
				Requests: createSlideRequests,
			}, revLog, "create_diagram_slides")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to create diagram slides: %w", err)
			}
		}

		// Re-read to find auto-added placeholders on diagram slides, then delete them.
		diagPres, err := slidesAPI.GetPresentation(presId)
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to re-read presentation for diagram cleanup: %w", err)
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
			_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
				Requests: cleanupRequests,
			}, revLog, "cleanup_diagram_placeholders")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to clean diagram slides: %w", err)
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
			_, err := revision.BatchUpdate(slidesAPI, presId, &slides.BatchUpdatePresentationRequest{
				Requests: shapeRequests,
			}, revLog, "create_diagram_shapes")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to create diagram shapes: %w", err)
			}
		}
	}

	var pageIDs []string
	for i := 0; i < len(plan.Slides); i++ {
		if ref, ok := refsByPlanIndex[i]; ok {
			pageIDs = append(pageIDs, ref.PageObjectID)
		}
	}
	for i := 0; i < len(plan.Slides); i++ {
		if pid, ok := diagramPageIDs[i]; ok {
			pageIDs = append(pageIDs, pid)
		}
	}

	execTrace.SlidesCreated = len(pageIDs)
	tracer.RecordExecution(execTrace)

	slog.Info("presentation complete", "url", fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId))
	slog.Info(revLog.Summary())
	return &ExecutionResult{PresentationID: presId, PageIDs: pageIDs}, revLog, nil
}
