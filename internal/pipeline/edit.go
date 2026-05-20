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
// It handles modify_content and delete_slide operations in-place.
func ExecuteEditPlan(ctx context.Context, plan *model.EditPlan, slidesSrv *slides.Service) error {
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

	for _, op := range plan.Operations {
		switch op.Type {
		case "delete_slide":
			deleteOps = append(deleteOps, op)
		case "modify_content":
			modifyOps = append(modifyOps, op)
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
		var updateRequests []*slides.Request
		for _, op := range modifyOps {
			if op.SlideIndex < 0 || op.SlideIndex >= len(pageIDs) {
				slog.Warn("modify_content: index out of range", "index", op.SlideIndex, "total", len(pageIDs))
				continue
			}

			page := pres.Slides[op.SlideIndex]
			objectMap := buildObjectMap(page)

			for _, mod := range op.Modifications {
				objectID := resolveObjectID(objectMap, mod.VariableName)
				if objectID == "" {
					slog.Warn("modify_content: element not found", "variableName", mod.VariableName, "slideIndex", op.SlideIndex)
					continue
				}

				if !shapeSet[objectID] {
					slog.Warn("modify_content: skipping non-SHAPE element", "objectId", objectID)
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
				updateRequests = append(updateRequests, markdown.InsertMarkdownContent(mod.NewText, objectID)...)
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

	return nil
}

// buildObjectMap creates a map from element ObjectID to the element itself
// for all elements on a slide page.
func buildObjectMap(page *slides.Page) map[string]*slides.PageElement {
	m := make(map[string]*slides.PageElement)
	for _, el := range page.PageElements {
		addToObjectMap(m, el)
	}
	return m
}

func addToObjectMap(m map[string]*slides.PageElement, el *slides.PageElement) {
	m[el.ObjectId] = el
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			addToObjectMap(m, child)
		}
	}
}

// resolveObjectID finds the actual ObjectID for a modification target.
// The variableName from the EditPlanner may be the ObjectID directly.
func resolveObjectID(objectMap map[string]*slides.PageElement, variableName string) string {
	if _, ok := objectMap[variableName]; ok {
		return variableName
	}
	return ""
}
