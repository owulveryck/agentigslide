package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/owulveryck/agentigslide/internal/model"
	islides "github.com/owulveryck/agentigslide/internal/slides"
	"github.com/owulveryck/agentigslide/markdown"

	"google.golang.org/api/slides/v1"
)

// ExecuteEditPlan applies a set of edit operations to an existing presentation.
// It handles modify_content, delete_slide, replace_slide, and insert_slide
// operations. templatePresID and templateIndex are required for replace_slide
// and insert_slide to resolve template slide numbers to IDs.
func ExecuteEditPlan(ctx context.Context, plan *model.EditPlan, slidesSrv *slides.Service, templatePresID string, templateIndex *model.TemplateIndex) error {
	pres, err := slidesSrv.Presentations.Get(plan.PresentationID).Do()
	if err != nil {
		return fmt.Errorf("failed to get presentation: %w", err)
	}

	pageIDs := make([]string, len(pres.Slides))
	for i, page := range pres.Slides {
		pageIDs[i] = page.ObjectId
	}

	textPresence := islides.BuildTextPresenceMap(pres)
	shapeSet := islides.BuildShapeSet(pres)

	var deleteOps []model.EditOperation
	var modifyOps []model.EditOperation
	var replaceOps []model.EditOperation
	var insertOps []model.EditOperation

	for _, op := range plan.Operations {
		switch op.Type {
		case "delete_slide":
			deleteOps = append(deleteOps, op)
		case "modify_content":
			modifyOps = append(modifyOps, op)
		case "replace_slide":
			replaceOps = append(replaceOps, op)
		case "insert_slide":
			insertOps = append(insertOps, op)
		default:
			slog.Warn("unsupported edit operation type", "type", op.Type)
		}
	}

	// Delete slides in reverse index order to avoid index shifts.
	sort.Slice(deleteOps, func(i, j int) bool {
		return deleteOps[i].SlideIndex > deleteOps[j].SlideIndex
	})

	if len(deleteOps) > 0 {
		var deleteRequests []*slides.Request
		for _, op := range deleteOps {
			if op.SlideIndex < 0 || op.SlideIndex >= len(pageIDs) {
				slog.Warn("delete_slide: index out of range", "index", op.SlideIndex, "total", len(pageIDs))
				continue
			}
			deleteRequests = append(deleteRequests, &slides.Request{
				DeleteObject: &slides.DeleteObjectRequest{
					ObjectId: pageIDs[op.SlideIndex],
				},
			})
		}
		if len(deleteRequests) > 0 {
			slog.Info("deleting slides", "count", len(deleteRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: deleteRequests,
			}).Do()
			if err != nil {
				return fmt.Errorf("failed to delete slides: %w", err)
			}
		}
	}

	if len(modifyOps) > 0 {
		baseStyles := extractBaseStyles(pres)

		var updateRequests []*slides.Request
		for _, op := range modifyOps {
			for _, mod := range op.Modifications {
				objectID := mod.VariableName

				if !shapeSet[objectID] {
					slog.Warn("modify_content: element not found or not a SHAPE/TABLE", "objectId", objectID, "slideIndex", op.SlideIndex)
					continue
				}

				if textPresence[objectID] {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId: objectID,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				insertReqs := markdown.InsertMarkdownContent(mod.NewText, objectID)

				if style, ok := baseStyles[objectID]; ok {
					textLen := int64(computeInsertedLength(insertReqs))
					if textLen > 0 {
						start := int64(0)
						// Base style must be applied before markdown styles
						// (bold/italic) so that markdown overrides take precedence.
						updateRequests = append(updateRequests, &slides.Request{
							UpdateTextStyle: &slides.UpdateTextStyleRequest{
								ObjectId: objectID,
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
			}
		}

		markdown.SortRequests(updateRequests)

		if len(updateRequests) > 0 {
			slog.Info("updating text elements", "count", len(updateRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: updateRequests,
			}).Do()
			if err != nil {
				return fmt.Errorf("failed to update text content: %w", err)
			}
		}
	}

	// Replace slides: delete old slide, import template slide at same position,
	// then apply content.
	for _, op := range replaceOps {
		if op.SlideIndex < 0 || op.SlideIndex >= len(pageIDs) {
			slog.Warn("replace_slide: index out of range", "index", op.SlideIndex, "total", len(pageIDs))
			continue
		}
		sourceSlideID := resolveTemplateSlideID(templateIndex, op.NewSourceSlide)
		if sourceSlideID == "" {
			slog.Warn("replace_slide: template slide not found", "slideNumber", op.NewSourceSlide)
			continue
		}

		newPageID, elementMap, importErr := ImportTemplateSlide(ctx, slidesSrv, templatePresID, sourceSlideID, plan.PresentationID, op.SlideIndex)
		if importErr != nil {
			return fmt.Errorf("replace_slide: failed to import template slide: %w", importErr)
		}

		// Delete the original slide (now shifted by 1 position)
		_, delErr := slidesSrv.Presentations.BatchUpdate(plan.PresentationID, &slides.BatchUpdatePresentationRequest{
			Requests: []*slides.Request{{
				DeleteObject: &slides.DeleteObjectRequest{
					ObjectId: pageIDs[op.SlideIndex],
				},
			}},
		}).Do()
		if delErr != nil {
			return fmt.Errorf("replace_slide: failed to delete original slide: %w", delErr)
		}

		varNameMap := buildVarNameMap(templateIndex, op.NewSourceSlide)
		applySlideContent(ctx, slidesSrv, plan.PresentationID, newPageID, elementMap, varNameMap, op.SlideContent)
	}

	// Insert new slides from templates.
	sort.Slice(insertOps, func(i, j int) bool {
		return insertOps[i].InsertPosition < insertOps[j].InsertPosition
	})

	for i, op := range insertOps {
		sourceSlideID := resolveTemplateSlideID(templateIndex, op.NewSourceSlide)
		if sourceSlideID == "" {
			slog.Warn("insert_slide: template slide not found", "slideNumber", op.NewSourceSlide)
			continue
		}

		adjustedPos := op.InsertPosition + i

		newPageID, elementMap, importErr := ImportTemplateSlide(ctx, slidesSrv, templatePresID, sourceSlideID, plan.PresentationID, adjustedPos)
		if importErr != nil {
			return fmt.Errorf("insert_slide: failed to import template slide: %w", importErr)
		}

		varNameMap := buildVarNameMap(templateIndex, op.NewSourceSlide)
		applySlideContent(ctx, slidesSrv, plan.PresentationID, newPageID, elementMap, varNameMap, op.SlideContent)
	}

	return nil
}

// resolveTemplateSlideID finds the SlideID for a template slide number.
func resolveTemplateSlideID(index *model.TemplateIndex, slideNumber int) string {
	if index == nil {
		return ""
	}
	for _, ts := range index.Slides {
		if ts.SlideNumber == slideNumber {
			return ts.SlideID
		}
	}
	return ""
}

// applySlideContent applies text modifications to a newly imported slide.
// elementMap maps original template ObjectIDs to new ObjectIDs in the target.
// varNameMap maps semantic variable names (e.g. "maintitleShape") to original
// template ObjectIDs, so the chain is: variableName → ObjectID → newObjectID.
func applySlideContent(ctx context.Context, slidesSrv *slides.Service, presID, pageID string, elementMap map[string]string, varNameMap map[string]string, content []model.TextModification) {
	if len(content) == 0 {
		return
	}

	var reqs []*slides.Request
	for _, mod := range content {
		objectID := resolveImportedObjectID(mod.VariableName, elementMap, varNameMap)
		if objectID == "" {
			slog.Warn("applySlideContent: element not found", "variableName", mod.VariableName, "page", pageID)
			continue
		}

		reqs = append(reqs, &slides.Request{
			DeleteText: &slides.DeleteTextRequest{
				ObjectId: objectID,
				TextRange: &slides.Range{
					Type: "ALL",
				},
			},
		})
		reqs = append(reqs, markdown.InsertMarkdownContent(mod.NewText, objectID)...)
	}

	markdown.SortRequests(reqs)

	if len(reqs) > 0 {
		slog.Info("applying content to imported slide", "page", pageID, "modifications", len(content))
		_, err := slidesSrv.Presentations.BatchUpdate(presID, &slides.BatchUpdatePresentationRequest{
			Requests: reqs,
		}).Do()
		if err != nil {
			slog.Warn("failed to apply content to imported slide", "error", err, "page", pageID)
		}
	}
}

// resolveImportedObjectID resolves a variableName (which may be a semantic name
// like "maintitleShape" or a raw ObjectID) to the actual ObjectID in the target
// presentation after import.
func resolveImportedObjectID(variableName string, elementMap map[string]string, varNameMap map[string]string) string {
	if newID, ok := elementMap[variableName]; ok {
		return newID
	}
	if origID, ok := varNameMap[variableName]; ok {
		if newID, ok := elementMap[origID]; ok {
			return newID
		}
		return origID
	}
	return ""
}

type baseStyle struct {
	style  *slides.TextStyle
	fields string
}

// extractBaseStyles scans all shapes in the presentation and captures the
// font family, size, and foreground color from the first non-empty TextRun
// of each shape. These are used to restore styling after DeleteText+InsertText.
func extractBaseStyles(pres *slides.Presentation) map[string]baseStyle {
	m := make(map[string]baseStyle)
	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			extractBaseStylesFromElement(m, el)
		}
	}
	return m
}

func extractBaseStylesFromElement(m map[string]baseStyle, el *slides.PageElement) {
	if el.Shape != nil && el.Shape.Text != nil {
		if s, ok := firstRunStyle(el.Shape.Text); ok {
			m[el.ObjectId] = s
		}
	}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			extractBaseStylesFromElement(m, child)
		}
	}
}

func firstRunStyle(tc *slides.TextContent) (baseStyle, bool) {
	for _, te := range tc.TextElements {
		if te.TextRun == nil || te.TextRun.Style == nil {
			continue
		}
		s := te.TextRun.Style
		style := &slides.TextStyle{}
		var fields []string

		if s.FontFamily != "" {
			style.FontFamily = s.FontFamily
			fields = append(fields, "fontFamily")
		}
		if s.FontSize != nil {
			style.FontSize = s.FontSize
			fields = append(fields, "fontSize")
		}
		if s.ForegroundColor != nil {
			style.ForegroundColor = s.ForegroundColor
			fields = append(fields, "foregroundColor")
		}

		if len(fields) == 0 {
			return baseStyle{}, false
		}
		return baseStyle{style: style, fields: joinStyleFields(fields)}, true
	}
	return baseStyle{}, false
}

func joinStyleFields(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ","
		}
		result += f
	}
	return result
}

// computeInsertedLength counts the total rune length of text from InsertText requests.
func computeInsertedLength(reqs []*slides.Request) int {
	total := 0
	for _, r := range reqs {
		if r.InsertText != nil {
			total += len([]rune(r.InsertText.Text))
		}
	}
	return total
}

// buildVarNameMap builds a mapping from semantic variable names to ObjectIDs
// for a given template slide.
func buildVarNameMap(templateIndex *model.TemplateIndex, slideNumber int) map[string]string {
	if templateIndex == nil {
		return nil
	}
	m := make(map[string]string)
	for _, ts := range templateIndex.Slides {
		if ts.SlideNumber == slideNumber {
			for _, f := range ts.EditableFields {
				if f.VariableName != "" && f.ObjectID != "" {
					m[f.VariableName] = f.ObjectID
				}
			}
			break
		}
	}
	return m
}
