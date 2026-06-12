package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
	"github.com/owulveryck/agentigslide/internal/revision"
	islides "github.com/owulveryck/agentigslide/internal/slides"
	"github.com/owulveryck/agentigslide/internal/trace"
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
func ExecuteEditPlan(ctx context.Context, plan *model.EditPlan, slidesAPI SlidesAPI, templatePresID string, templateIndex *model.TemplateIndex) (*EditResult, *revision.Log, error) {
	revLog := revision.New(plan.PresentationID)

	pres, err := slidesAPI.GetPresentation(plan.PresentationID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get presentation: %w", err)
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
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: deleteRequests,
			}, revLog, "edit_delete_slides")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to delete slides: %w", err)
			}
		}
	}

	if len(modifyOps) > 0 {
		baseStyles := extractBaseStyles(pres)
		needsAutofit := buildNeedsAutofitMap(pres)

		var updateRequests []*slides.Request
		var autofitRequests []*slides.Request
		for _, iop := range modifyOps {
			if iop.op.SlideIndex >= 0 && iop.op.SlideIndex < len(pageIDs) {
				pid := pageIDs[iop.op.SlideIndex]
				if !affectedSet[pid] {
					affectedSet[pid] = true
					affectedPageIDs = append(affectedPageIDs, pid)
				}
				pageIDToOpIndex[pid] = iop.index
			}
			modTexts := make(map[string][]string)
			var modOrder []string
			for _, mod := range iop.op.Modifications {
				objectID := mod.VariableName
				if !shapeSet[objectID] {
					slog.Warn("modify_content: element not found or not a SHAPE/TABLE", "objectId", objectID, "slideIndex", iop.op.SlideIndex)
					continue
				}
				if _, exists := modTexts[objectID]; !exists {
					modOrder = append(modOrder, objectID)
				}
				modTexts[objectID] = append(modTexts[objectID], mod.NewText)
			}
			for _, objectID := range modOrder {
				combinedText := strings.Join(modTexts[objectID], "\n")
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
				insertReqs := markdown.InsertMarkdownContent(combinedText, objectID)
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
				if needsAutofit[objectID] {
					autofitRequests = append(autofitRequests, &slides.Request{
						UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
							ObjectId: objectID,
							ShapeProperties: &slides.ShapeProperties{
								Autofit: &slides.Autofit{AutofitType: "TEXT_AUTOFIT"},
							},
							Fields: "autofit.autofitType",
						},
					})
				}
			}
		}

		markdown.SortRequests(updateRequests)

		if len(updateRequests) > 0 {
			slog.Info("updating text elements", "count", len(updateRequests))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: updateRequests,
			}, revLog, "edit_modify_content")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to update text content: %w", err)
			}
		}
		if len(autofitRequests) > 0 {
			slog.Info("applying autofit", "count", len(autofitRequests))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: autofitRequests,
			}, revLog, "edit_modify_autofit")
			if err != nil {
				slog.Warn("autofit batch failed, text content was applied successfully", "error", err)
			}
		}
	}

	// Batched replace/insert: read template once, prepare all imports,
	// then execute in phases to minimize API round-trips.
	type pendingImport struct {
		indexedOp
		sourceSlideID string
		varNameMap    map[string]string
		isReplace     bool
	}

	var pendingImports []pendingImport

	for _, iop := range replaceOps {
		if iop.op.SlideIndex < 0 || iop.op.SlideIndex >= len(pageIDs) {
			slog.Warn("replace_slide: index out of range", "index", iop.op.SlideIndex, "total", len(pageIDs))
			continue
		}
		sid := resolveTemplateSlideID(templateIndex, iop.op.NewSourceSlide)
		if sid == "" {
			slog.Warn("replace_slide: template slide not found", "slideNumber", iop.op.NewSourceSlide)
			continue
		}
		pendingImports = append(pendingImports, pendingImport{
			indexedOp:     iop,
			sourceSlideID: sid,
			varNameMap:    buildVarNameMap(templateIndex, iop.op.NewSourceSlide),
			isReplace:     true,
		})
	}

	sort.Slice(insertOps, func(i, j int) bool {
		return insertOps[i].op.InsertPosition < insertOps[j].op.InsertPosition
	})

	for _, iop := range insertOps {
		sid := resolveTemplateSlideID(templateIndex, iop.op.NewSourceSlide)
		if sid == "" {
			slog.Warn("insert_slide: template slide not found", "slideNumber", iop.op.NewSourceSlide)
			continue
		}
		pendingImports = append(pendingImports, pendingImport{
			indexedOp:     iop,
			sourceSlideID: sid,
			varNameMap:    buildVarNameMap(templateIndex, iop.op.NewSourceSlide),
			isReplace:     false,
		})
	}

	if len(pendingImports) > 0 {
		// Phase 1: read template presentation once.
		templatePres, err := slidesAPI.GetPresentation(templatePresID)
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to get template presentation: %w", err)
		}
		templatePageMap := make(map[string]*slides.Page)
		for _, p := range templatePres.Slides {
			templatePageMap[p.ObjectId] = p
		}

		// Phase 2: prepare all import plans (no API calls).
		type importEntry struct {
			pending pendingImport
			plan    *slideImportPlan
		}
		var replaceEntries, insertEntries []importEntry

		for _, pi := range pendingImports {
			sourcePage := templatePageMap[pi.sourceSlideID]
			if sourcePage == nil {
				slog.Warn("template slide not found in presentation", "slideID", pi.sourceSlideID)
				continue
			}

			var insertionIndex int
			if pi.isReplace {
				insertionIndex = pi.op.SlideIndex
			} else {
				insertionIndex = pi.op.SlideIndex + len(insertEntries)
			}

			imp := prepareSlideImport(sourcePage, pi.sourceSlideID, insertionIndex)

			entry := importEntry{pending: pi, plan: imp}
			if pi.isReplace {
				replaceEntries = append(replaceEntries, entry)
			} else {
				insertEntries = append(insertEntries, entry)
			}
		}

		// Phase 3: batch CreateSlide for replaces.
		var replaceCreateReqs []*slides.Request
		for _, e := range replaceEntries {
			replaceCreateReqs = append(replaceCreateReqs, e.plan.createSlideReqs...)
		}
		if len(replaceCreateReqs) > 0 {
			slog.Info("creating replacement slides", "count", len(replaceCreateReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: replaceCreateReqs,
			}, revLog, "edit_batch_create_replace_slides")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to create replacement slides: %w", err)
			}
		}

		// Phase 4: batch CreateSlide for inserts.
		var insertCreateReqs []*slides.Request
		for _, e := range insertEntries {
			insertCreateReqs = append(insertCreateReqs, e.plan.createSlideReqs...)
		}
		if len(insertCreateReqs) > 0 {
			slog.Info("creating inserted slides", "count", len(insertCreateReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: insertCreateReqs,
			}, revLog, "edit_batch_create_insert_slides")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to create inserted slides: %w", err)
			}
		}

		// Phase 5: batch all element imports.
		allEntries := append(replaceEntries, insertEntries...)
		var allElementReqs []*slides.Request
		for _, e := range allEntries {
			allElementReqs = append(allElementReqs, e.plan.elementReqs...)
		}
		if len(allElementReqs) > 0 {
			slog.Info("importing elements for all slides", "count", len(allElementReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: allElementReqs,
			}, revLog, "edit_batch_import_elements")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to import elements: %w", err)
			}
		}

		// Phase 6: batch delete originals for replace_slide.
		var replaceDeleteReqs []*slides.Request
		for _, e := range replaceEntries {
			replaceDeleteReqs = append(replaceDeleteReqs, &slides.Request{
				DeleteObject: &slides.DeleteObjectRequest{
					ObjectId: pageIDs[e.pending.op.SlideIndex],
				},
			})
		}
		if len(replaceDeleteReqs) > 0 {
			slog.Info("deleting replaced originals", "count", len(replaceDeleteReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: replaceDeleteReqs,
			}, revLog, "edit_batch_delete_for_replace")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to delete replaced slides: %w", err)
			}
		}

		// Phase 7: read presentation once for text presence.
		freshPres, err := slidesAPI.GetPresentation(plan.PresentationID)
		if err != nil {
			return nil, revLog, fmt.Errorf("failed to re-read presentation: %w", err)
		}
		freshTextPresence := islides.BuildTextPresenceMap(freshPres)
		freshBaseStyles := extractBaseStyles(freshPres)
		freshNeedsAutofit := buildNeedsAutofitMap(freshPres)

		// Phase 8: batch all content application.
		var allContentReqs []*slides.Request
		var allAutofitReqs []*slides.Request
		for _, e := range allEntries {
			cReqs, aReqs := prepareSlideContentRequests(e.plan.elementMap, e.pending.varNameMap, e.pending.op.SlideContent, freshTextPresence, freshBaseStyles, freshNeedsAutofit)
			allContentReqs = append(allContentReqs, cReqs...)
			allAutofitReqs = append(allAutofitReqs, aReqs...)
		}
		markdown.SortRequests(allContentReqs)
		if len(allContentReqs) > 0 {
			slog.Info("applying content to all imported slides", "count", len(allContentReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: allContentReqs,
			}, revLog, "edit_batch_apply_content")
			if err != nil {
				return nil, revLog, fmt.Errorf("failed to apply content: %w", err)
			}
		}
		if len(allAutofitReqs) > 0 {
			slog.Info("applying autofit to imported slides", "count", len(allAutofitReqs))
			_, err := revision.BatchUpdate(slidesAPI, plan.PresentationID, &slides.BatchUpdatePresentationRequest{
				Requests: allAutofitReqs,
			}, revLog, "edit_batch_apply_autofit")
			if err != nil {
				slog.Warn("autofit batch failed, content was applied successfully", "error", err)
			}
		}

		// Track affected pages.
		for _, e := range allEntries {
			pid := e.plan.newPageID
			if !affectedSet[pid] {
				affectedSet[pid] = true
				affectedPageIDs = append(affectedPageIDs, pid)
			}
			pageIDToOpIndex[pid] = e.pending.index
		}
	}

	slog.Info(revLog.Summary())
	return &EditResult{AffectedPageIDs: affectedPageIDs, PageIDToOpIndex: pageIDToOpIndex}, revLog, nil
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

// prepareSlideContentRequests builds text update requests for an imported slide
// without calling the API. Same logic as applySlideContent but pure.
func prepareSlideContentRequests(elementMap map[string]string, varNameMap map[string]string, content []model.TextModification, textPresence map[string]bool, baseStyles map[string]baseStyle, needsAutofit map[string]bool) (contentReqs, autofitReqs []*slides.Request) {
	modTexts := make(map[string][]string)
	var modOrder []string
	for _, mod := range content {
		objectID := resolveImportedObjectID(mod.VariableName, elementMap, varNameMap)
		if objectID == "" {
			slog.Warn("prepareSlideContentRequests: element not found", "variableName", mod.VariableName)
			continue
		}
		if _, exists := modTexts[objectID]; !exists {
			modOrder = append(modOrder, objectID)
		}
		modTexts[objectID] = append(modTexts[objectID], mod.NewText)
	}

	for _, objectID := range modOrder {
		combinedText := strings.Join(modTexts[objectID], "\n")
		if textPresence[objectID] {
			contentReqs = append(contentReqs, &slides.Request{
				DeleteText: &slides.DeleteTextRequest{
					ObjectId: objectID,
					TextRange: &slides.Range{
						Type: "ALL",
					},
				},
			})
		}
		insertReqs := markdown.InsertMarkdownContent(combinedText, objectID)
		if baseStyles != nil {
			if style, ok := baseStyles[objectID]; ok {
				textLen := int64(computeInsertedLength(insertReqs))
				if textLen > 0 {
					start := int64(0)
					contentReqs = append(contentReqs, &slides.Request{
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
		}
		contentReqs = append(contentReqs, insertReqs...)
		if needsAutofit[objectID] {
			autofitReqs = append(autofitReqs, &slides.Request{
				UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
					ObjectId: objectID,
					ShapeProperties: &slides.ShapeProperties{
						Autofit: &slides.Autofit{AutofitType: "TEXT_AUTOFIT"},
					},
					Fields: "autofit.autofitType",
				},
			})
		}
	}
	return contentReqs, autofitReqs
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

func baseStyleToTrace(bs baseStyle) trace.BaseStyleTrace {
	var t trace.BaseStyleTrace
	if bs.style == nil {
		return t
	}
	t.FontFamily = bs.style.FontFamily
	if bs.style.FontSize != nil {
		t.FontSizePt = bs.style.FontSize.Magnitude
	}
	if bs.style.ForegroundColor != nil && bs.style.ForegroundColor.OpaqueColor != nil &&
		bs.style.ForegroundColor.OpaqueColor.RgbColor != nil {
		c := bs.style.ForegroundColor.OpaqueColor.RgbColor
		t.FgColorHex = fmt.Sprintf("#%02x%02x%02x",
			int(c.Red*255), int(c.Green*255), int(c.Blue*255))
	}
	return t
}

// buildNeedsAutofitMap identifies shapes that have no TEXT_AUTOFIT enabled.
// When we replace text in these shapes with longer content, the text may
// overflow. Enabling TEXT_AUTOFIT ensures Google Slides auto-shrinks the font.
func buildNeedsAutofitMap(pres *slides.Presentation) map[string]bool {
	m := make(map[string]bool)
	for _, page := range pres.Slides {
		for _, el := range page.PageElements {
			if el.Shape == nil || el.Shape.Text == nil {
				continue
			}
			sp := el.Shape.ShapeProperties
			if sp == nil || sp.Autofit == nil {
				continue
			}
			// Only flag shapes that explicitly have autofit NONE — these
			// support autofit but have it disabled.  Shapes without any
			// Autofit property may not support it (grouped shapes, etc.)
			// and the API rejects TEXT_AUTOFIT on them.
			// Also skip placeholder types that reject autofit changes
			// (slide numbers, dates, footers).
			if sp.Autofit.AutofitType == "NONE" && !isNonAutofitPlaceholder(el.Shape) {
				m[el.ObjectId] = true
			}
		}
	}
	return m
}

func isNonAutofitPlaceholder(shape *slides.Shape) bool {
	if shape.Placeholder == nil {
		return false
	}
	switch shape.Placeholder.Type {
	case "SLIDE_NUMBER", "DATE_AND_TIME", "FOOTER", "HEADER":
		return true
	}
	return false
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
func ReapplyModifications(ctx context.Context, presID string, ops []model.EditOperation, slidesAPI SlidesAPI, revLog *revision.Log) error {
	pres, err := slidesAPI.GetPresentation(presID)
	if err != nil {
		return fmt.Errorf("failed to get presentation: %w", err)
	}

	textPresence := islides.BuildTextPresenceMap(pres)
	shapeSet := islides.BuildShapeSet(pres)
	baseStyles := extractBaseStyles(pres)
	needsAutofit := buildNeedsAutofitMap(pres)

	var updateRequests []*slides.Request
	var autofitRequests []*slides.Request
	for _, op := range ops {
		if op.Type != "modify_content" {
			continue
		}
		modTexts := make(map[string][]string)
		var modOrder []string
		for _, mod := range op.Modifications {
			objectID := mod.VariableName
			if !shapeSet[objectID] {
				slog.Warn("ReapplyModifications: element not found", "objectId", objectID, "slideIndex", op.SlideIndex)
				continue
			}
			if _, exists := modTexts[objectID]; !exists {
				modOrder = append(modOrder, objectID)
			}
			modTexts[objectID] = append(modTexts[objectID], mod.NewText)
		}
		for _, objectID := range modOrder {
			combinedText := strings.Join(modTexts[objectID], "\n")
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
			insertReqs := markdown.InsertMarkdownContent(combinedText, objectID)
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
			if needsAutofit[objectID] {
				autofitRequests = append(autofitRequests, &slides.Request{
					UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
						ObjectId: objectID,
						ShapeProperties: &slides.ShapeProperties{
							Autofit: &slides.Autofit{AutofitType: "TEXT_AUTOFIT"},
						},
						Fields: "autofit.autofitType",
					},
				})
			}
		}
	}

	markdown.SortRequests(updateRequests)

	if len(updateRequests) > 0 {
		slog.Info("re-applying modifications", "count", len(updateRequests))
		_, err := revision.BatchUpdate(slidesAPI, presID, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}, revLog, "reapply_modifications")
		if err != nil {
			return fmt.Errorf("failed to re-apply modifications: %w", err)
		}
	}
	if len(autofitRequests) > 0 {
		slog.Info("applying autofit", "count", len(autofitRequests))
		_, err := revision.BatchUpdate(slidesAPI, presID, &slides.BatchUpdatePresentationRequest{
			Requests: autofitRequests,
		}, revLog, "reapply_autofit")
		if err != nil {
			slog.Warn("autofit batch failed, modifications were applied successfully", "error", err)
		}
	}

	return nil
}
