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

// EditResult contains the outcome of executing an edit plan.
type EditResult struct {
	// AffectedPageIDs lists the PageObjectIDs of all slides that were
	// created or modified. Deleted slides are not included.
	// IDs refer to the final presentation state.
	AffectedPageIDs []string

	// PageIDToOpIndex maps each affected PageObjectID to its index in the
	// original plan.Operations slice. Used by the visual feedback loop to
	// trace visual issues back to specific operations and skeleton entries.
	PageIDToOpIndex map[string]int
}

// ExecuteEditPlan applies a set of edit operations to an existing presentation.
// It handles modify_content, delete_slide, replace_slide, and insert_slide
// operations. templatePresID and templateIndex are required for replace_slide
// and insert_slide to resolve template slide numbers to IDs.
func ExecuteEditPlan(ctx context.Context, plan *model.EditPlan, slidesSrv *slides.Service, templatePresID string, templateIndex *model.TemplateIndex) (*EditResult, error) {
	pres, err := slidesSrv.Presentations.Get(plan.PresentationID).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get presentation: %w", err)
	}

	pageIDs := make([]string, len(pres.Slides))
	for i, page := range pres.Slides {
		pageIDs[i] = page.ObjectId
	}

	textPresence := islides.BuildTextPresenceMap(pres)
	shapeSet := islides.BuildShapeSet(pres)

	var affectedPageIDs []string
	affectedSet := make(map[string]bool)
	pageIDToOpIndex := make(map[string]int)

	type indexedOp struct {
		op    model.EditOperation
		index int
	}

	var deleteOps []indexedOp
	var modifyOps []indexedOp
	var replaceOps []indexedOp
	var insertOps []indexedOp

	for i, op := range plan.Operations {
		switch op.Type {
		case "delete_slide":
			deleteOps = append(deleteOps, indexedOp{op, i})
		case "modify_content":
			modifyOps = append(modifyOps, indexedOp{op, i})
		case "replace_slide":
			replaceOps = append(replaceOps, indexedOp{op, i})
		case "insert_slide":
			insertOps = append(insertOps, indexedOp{op, i})
		default:
			slog.Warn("unsupported edit operation type", "type", op.Type)
		}
	}

	// Delete slides in reverse index order to avoid index shifts.
	sort.Slice(deleteOps, func(i, j int) bool {
		return deleteOps[i].op.SlideIndex > deleteOps[j].op.SlideIndex
	})

	if len(deleteOps) > 0 {
		var deleteRequests []*slides.Request
		for _, iop := range deleteOps {
			if iop.op.SlideIndex < 0 || iop.op.SlideIndex >= len(pageIDs) {
				slog.Warn("delete_slide: index out of range", "index", iop.op.SlideIndex, "total", len(pageIDs))
				continue
			}
			deleteRequests = append(deleteRequests, &slides.Request{
				DeleteObject: &slides.DeleteObjectRequest{
					ObjectId: pageIDs[iop.op.SlideIndex],
				},
			})
		}
		if len(deleteRequests) > 0 {
			slog.Info("deleting slides", "count", len(deleteRequests))
			_, err := slidesSrv.Presentations.BatchUpdate(plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: deleteRequests,
			}).Do()
			if err != nil {
				return nil, fmt.Errorf("failed to delete slides: %w", err)
			}
		}
	}

	if len(modifyOps) > 0 {
		baseStyles := extractBaseStyles(pres)

		var updateRequests []*slides.Request
		for _, iop := range modifyOps {
			if iop.op.SlideIndex >= 0 && iop.op.SlideIndex < len(pageIDs) {
				pid := pageIDs[iop.op.SlideIndex]
				if !affectedSet[pid] {
					affectedSet[pid] = true
					affectedPageIDs = append(affectedPageIDs, pid)
				}
				pageIDToOpIndex[pid] = iop.index
			}
			for _, mod := range iop.op.Modifications {
				objectID := mod.VariableName

				if !shapeSet[objectID] {
					slog.Warn("modify_content: element not found or not a SHAPE/TABLE", "objectId", objectID, "slideIndex", iop.op.SlideIndex)
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
				return nil, fmt.Errorf("failed to update text content: %w", err)
			}
		}
	}

	// Replace slides: delete old slide, import template slide at same position,
	// then apply content.
	for _, iop := range replaceOps {
		if iop.op.SlideIndex < 0 || iop.op.SlideIndex >= len(pageIDs) {
			slog.Warn("replace_slide: index out of range", "index", iop.op.SlideIndex, "total", len(pageIDs))
			continue
		}
		sourceSlideID := resolveTemplateSlideID(templateIndex, iop.op.NewSourceSlide)
		if sourceSlideID == "" {
			slog.Warn("replace_slide: template slide not found", "slideNumber", iop.op.NewSourceSlide)
			continue
		}

		newPageID, elementMap, importErr := ImportTemplateSlide(ctx, slidesSrv, templatePresID, sourceSlideID, plan.PresentationID, iop.op.SlideIndex)
		if importErr != nil {
			return nil, fmt.Errorf("replace_slide: failed to import template slide: %w", importErr)
		}

		if !affectedSet[newPageID] {
			affectedSet[newPageID] = true
			affectedPageIDs = append(affectedPageIDs, newPageID)
		}
		pageIDToOpIndex[newPageID] = iop.index

		// Delete the original slide (now shifted by 1 position)
		_, delErr := slidesSrv.Presentations.BatchUpdate(plan.PresentationID, &slides.BatchUpdatePresentationRequest{
			Requests: []*slides.Request{{
				DeleteObject: &slides.DeleteObjectRequest{
					ObjectId: pageIDs[iop.op.SlideIndex],
				},
			}},
		}).Do()
		if delErr != nil {
			return nil, fmt.Errorf("replace_slide: failed to delete original slide: %w", delErr)
		}

		varNameMap := buildVarNameMap(templateIndex, iop.op.NewSourceSlide)
		applySlideContent(ctx, slidesSrv, plan.PresentationID, newPageID, elementMap, varNameMap, iop.op.SlideContent)
	}

	// Insert new slides from templates.
	sort.Slice(insertOps, func(i, j int) bool {
		return insertOps[i].op.InsertPosition < insertOps[j].op.InsertPosition
	})

	for i, iop := range insertOps {
		sourceSlideID := resolveTemplateSlideID(templateIndex, iop.op.NewSourceSlide)
		if sourceSlideID == "" {
			slog.Warn("insert_slide: template slide not found", "slideNumber", iop.op.NewSourceSlide)
			continue
		}

		adjustedPos := iop.op.InsertPosition + i

		newPageID, elementMap, importErr := ImportTemplateSlide(ctx, slidesSrv, templatePresID, sourceSlideID, plan.PresentationID, adjustedPos)
		if importErr != nil {
			return nil, fmt.Errorf("insert_slide: failed to import template slide: %w", importErr)
		}

		if !affectedSet[newPageID] {
			affectedSet[newPageID] = true
			affectedPageIDs = append(affectedPageIDs, newPageID)
		}
		pageIDToOpIndex[newPageID] = iop.index

		varNameMap := buildVarNameMap(templateIndex, iop.op.NewSourceSlide)
		applySlideContent(ctx, slidesSrv, plan.PresentationID, newPageID, elementMap, varNameMap, iop.op.SlideContent)
	}

	return &EditResult{AffectedPageIDs: affectedPageIDs, PageIDToOpIndex: pageIDToOpIndex}, nil
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

	// Re-read the presentation to get fresh text presence after import.
	// Imported shapes may be empty — DeleteText on empty text fails with
	// "startIndex 0 must be less than endIndex 0".
	pres, err := slidesSrv.Presentations.Get(presID).Do()
	if err != nil {
		slog.Warn("applySlideContent: failed to read presentation", "error", err)
		return
	}
	textPresence := islides.BuildTextPresenceMap(pres)

	var reqs []*slides.Request
	for _, mod := range content {
		objectID := resolveImportedObjectID(mod.VariableName, elementMap, varNameMap)
		if objectID == "" {
			slog.Warn("applySlideContent: element not found", "variableName", mod.VariableName, "page", pageID)
			continue
		}

		if textPresence[objectID] {
			reqs = append(reqs, &slides.Request{
				DeleteText: &slides.DeleteTextRequest{
					ObjectId: objectID,
					TextRange: &slides.Range{
						Type: "ALL",
					},
				},
			})
		}
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

// ReapplyModifications re-applies modify_content operations to an existing
// presentation. It re-reads the presentation state to get fresh textPresence,
// shapeSet, and baseStyles. Used by the visual feedback loop to apply
// corrected text after the EditWriter shortens overflowing content.
func ReapplyModifications(ctx context.Context, presID string, ops []model.EditOperation, slidesSrv *slides.Service) error {
	pres, err := slidesSrv.Presentations.Get(presID).Do()
	if err != nil {
		return fmt.Errorf("failed to get presentation: %w", err)
	}

	textPresence := islides.BuildTextPresenceMap(pres)
	shapeSet := islides.BuildShapeSet(pres)
	baseStyles := extractBaseStyles(pres)

	var updateRequests []*slides.Request
	for _, op := range ops {
		if op.Type != "modify_content" {
			continue
		}
		for _, mod := range op.Modifications {
			objectID := mod.VariableName

			if !shapeSet[objectID] {
				slog.Warn("ReapplyModifications: element not found", "objectId", objectID, "slideIndex", op.SlideIndex)
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
		slog.Info("re-applying modifications", "count", len(updateRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presID, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}).Do()
		if err != nil {
			return fmt.Errorf("failed to re-apply modifications: %w", err)
		}
	}

	return nil
}
