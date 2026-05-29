package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/owulveryck/agentigslide/internal/revision"
	"google.golang.org/api/slides/v1"
)

var importCounter int

// slideImportPlan holds the prepared API requests for importing a single
// template slide. Created by prepareSlideImport, executed in batch.
type slideImportPlan struct {
	sourceSlideID   string
	newPageID       string
	insertionIndex  int
	elementMap      map[string]string
	createSlideReqs []*slides.Request
	elementReqs     []*slides.Request
}

// prepareSlideImport builds import requests without calling the API.
// It reuses importPageElement and the existing import chain.
// Must be called sequentially (relies on the global importCounter).
func prepareSlideImport(sourcePage *slides.Page, sourceSlideID string, insertionIndex int) *slideImportPlan {
	importCounter++
	newPageID := fmt.Sprintf("imp%d_%s", importCounter, sourceSlideID)
	elementMap := make(map[string]string)

	createSlideReqs := []*slides.Request{{
		CreateSlide: &slides.CreateSlideRequest{
			ObjectId:       newPageID,
			InsertionIndex: int64(insertionIndex),
		},
	}}

	var elementReqs []*slides.Request
	counter := 0
	for _, el := range sourcePage.PageElements {
		reqs := importPageElement(newPageID, el, &counter, elementMap)
		elementReqs = append(elementReqs, reqs...)
	}

	return &slideImportPlan{
		sourceSlideID:   sourceSlideID,
		newPageID:       newPageID,
		insertionIndex:  insertionIndex,
		elementMap:      elementMap,
		createSlideReqs: createSlideReqs,
		elementReqs:     elementReqs,
	}
}

// ImportTemplateSlide reads a template slide from the template presentation
// and recreates its visual elements in the target presentation. It returns
// the new page's ObjectID and a mapping from original to new element ObjectIDs.
func ImportTemplateSlide(
	ctx context.Context,
	slidesAPI SlidesAPI,
	templatePresID string,
	sourceSlideID string,
	targetPresID string,
	insertionIndex int,
	revLog *revision.Log,
) (newPageID string, elementMap map[string]string, err error) {
	templatePres, err := slidesAPI.GetPresentation(templatePresID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get template presentation: %w", err)
	}

	var sourcePage *slides.Page
	for _, p := range templatePres.Slides {
		if p.ObjectId == sourceSlideID {
			sourcePage = p
			break
		}
	}
	if sourcePage == nil {
		return "", nil, fmt.Errorf("slide %s not found in template %s", sourceSlideID, templatePresID)
	}

	importCounter++
	newPageID = fmt.Sprintf("imp%d_%s", importCounter, sourceSlideID)
	elementMap = make(map[string]string)

	createSlideReqs := []*slides.Request{{
		CreateSlide: &slides.CreateSlideRequest{
			ObjectId:       newPageID,
			InsertionIndex: int64(insertionIndex),
		},
	}}

	slog.Info("creating imported slide", "sourceSlide", sourceSlideID, "position", insertionIndex)
	_, err = revision.BatchUpdate(slidesAPI, targetPresID, &slides.BatchUpdatePresentationRequest{
		Requests: createSlideReqs,
	}, revLog, "import_create_slide")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create slide: %w", err)
	}

	var elementReqs []*slides.Request
	counter := 0

	for _, el := range sourcePage.PageElements {
		reqs := importPageElement(newPageID, el, &counter, elementMap)
		elementReqs = append(elementReqs, reqs...)
	}

	if len(elementReqs) > 0 {
		slog.Info("importing elements", "count", len(elementReqs), "sourceSlide", sourceSlideID)
		_, err = revision.BatchUpdate(slidesAPI, targetPresID, &slides.BatchUpdatePresentationRequest{
			Requests: elementReqs,
		}, revLog, "import_elements")
		if err != nil {
			return "", nil, fmt.Errorf("failed to import elements: %w", err)
		}
	}

	return newPageID, elementMap, nil
}

func importPageElement(pageID string, el *slides.PageElement, counter *int, elementMap map[string]string) []*slides.Request {
	var reqs []*slides.Request

	if el.Shape != nil {
		reqs = append(reqs, importShape(pageID, el, counter, elementMap)...)
	} else if el.Image != nil {
		reqs = append(reqs, importImage(pageID, el, counter, elementMap)...)
	} else if el.Table != nil {
		reqs = append(reqs, importTable(pageID, el, counter, elementMap)...)
	} else if el.Line != nil {
		reqs = append(reqs, importLine(pageID, el, counter, elementMap)...)
	} else if el.ElementGroup != nil {
		// Google Slides API does not support creating groups via BatchUpdate,
		// so children are imported individually (ungrouped).
		for _, child := range el.ElementGroup.Children {
			reqs = append(reqs, importPageElement(pageID, child, counter, elementMap)...)
		}
	}

	return reqs
}

func importShape(pageID string, el *slides.PageElement, counter *int, elementMap map[string]string) []*slides.Request {
	*counter++
	newID := fmt.Sprintf("imp%d_%d_%s", importCounter, *counter, el.ObjectId)
	elementMap[el.ObjectId] = newID

	reqs := []*slides.Request{{
		CreateShape: &slides.CreateShapeRequest{
			ObjectId:  newID,
			ShapeType: el.Shape.ShapeType,
			ElementProperties: &slides.PageElementProperties{
				PageObjectId: pageID,
				Size:         el.Size,
				Transform:    el.Transform,
			},
		},
	}}

	reqs = append(reqs, importShapeProperties(newID, el.Shape)...)
	reqs = append(reqs, importShapeText(newID, el.Shape)...)

	return reqs
}

func importShapeProperties(objectID string, shape *slides.Shape) []*slides.Request {
	if shape.ShapeProperties == nil {
		return nil
	}
	props := shape.ShapeProperties

	sp := &slides.ShapeProperties{}
	var fields []string

	if props.ShapeBackgroundFill != nil && props.ShapeBackgroundFill.SolidFill != nil &&
		props.ShapeBackgroundFill.PropertyState != "INHERIT" && props.ShapeBackgroundFill.PropertyState != "NOT_RENDERED" {
		sp.ShapeBackgroundFill = props.ShapeBackgroundFill
		fields = append(fields, "shapeBackgroundFill")
	}

	if props.Outline != nil &&
		props.Outline.PropertyState != "INHERIT" && props.Outline.PropertyState != "NOT_RENDERED" {
		sp.Outline = props.Outline
		fields = append(fields, "outline")
	}

	if props.ContentAlignment != "" {
		sp.ContentAlignment = props.ContentAlignment
		fields = append(fields, "contentAlignment")
	}

	if props.Shadow != nil &&
		props.Shadow.PropertyState != "INHERIT" && props.Shadow.PropertyState != "NOT_RENDERED" {
		sp.Shadow = props.Shadow
		fields = append(fields, "shadow")
	}

	if len(fields) == 0 {
		return nil
	}

	return []*slides.Request{{
		UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
			ObjectId:        objectID,
			ShapeProperties: sp,
			Fields:          joinFields(fields),
		},
	}}
}

func importShapeText(objectID string, shape *slides.Shape) []*slides.Request {
	if shape.Text == nil {
		return nil
	}
	return importTextContent(objectID, shape.Text, nil)
}

func importTextContent(objectID string, text *slides.TextContent, cellLoc *slides.TableCellLocation) []*slides.Request {
	var reqs []*slides.Request
	insertIdx := int64(0)

	for _, te := range text.TextElements {
		if te.TextRun == nil || te.TextRun.Content == "" {
			continue
		}

		reqs = append(reqs, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId:       objectID,
				Text:           te.TextRun.Content,
				InsertionIndex: insertIdx,
				CellLocation:   cellLoc,
			},
		})

		if te.TextRun.Style != nil {
			styleFields := buildTextStyleFields(te.TextRun.Style)
			if len(styleFields) > 0 {
				endIdx := insertIdx + int64(len([]rune(te.TextRun.Content)))
				startIdx := insertIdx
				style := *te.TextRun.Style
				style.ForceSendFields = []string{"Bold", "Italic", "Underline"}
				reqs = append(reqs, &slides.Request{
					UpdateTextStyle: &slides.UpdateTextStyleRequest{
						ObjectId:     objectID,
						CellLocation: cellLoc,
						Style:        &style,
						TextRange: &slides.Range{
							Type:       "FIXED_RANGE",
							StartIndex: &startIdx,
							EndIndex:   &endIdx,
						},
						Fields: joinFields(styleFields),
					},
				})
			}
		}

		insertIdx += int64(len([]rune(te.TextRun.Content)))
	}

	for _, te := range text.TextElements {
		if te.ParagraphMarker == nil || te.ParagraphMarker.Style == nil {
			continue
		}
		paraFields := buildParagraphStyleFields(te.ParagraphMarker.Style)
		if len(paraFields) == 0 {
			continue
		}
		startIdx := te.StartIndex
		endIdx := te.EndIndex
		if endIdx > insertIdx {
			endIdx = insertIdx
		}
		if startIdx >= endIdx {
			continue
		}
		pStyle := *te.ParagraphMarker.Style
		if pStyle.LineSpacing != 0 {
			pStyle.ForceSendFields = append(pStyle.ForceSendFields, "LineSpacing")
		}
		reqs = append(reqs, &slides.Request{
			UpdateParagraphStyle: &slides.UpdateParagraphStyleRequest{
				ObjectId:     objectID,
				CellLocation: cellLoc,
				Style:        &pStyle,
				TextRange: &slides.Range{
					Type:       "FIXED_RANGE",
					StartIndex: &startIdx,
					EndIndex:   &endIdx,
				},
				Fields: joinFields(paraFields),
			},
		})
	}

	return reqs
}

func importImage(pageID string, el *slides.PageElement, counter *int, elementMap map[string]string) []*slides.Request {
	*counter++
	newID := fmt.Sprintf("imp%d_%d_%s", importCounter, *counter, el.ObjectId)
	elementMap[el.ObjectId] = newID

	url := el.Image.ContentUrl
	if url == "" {
		slog.Warn("image has no content URL, skipping", "objectId", el.ObjectId)
		return nil
	}

	return []*slides.Request{{
		CreateImage: &slides.CreateImageRequest{
			ObjectId: newID,
			Url:      url,
			ElementProperties: &slides.PageElementProperties{
				PageObjectId: pageID,
				Size:         el.Size,
				Transform:    el.Transform,
			},
		},
	}}
}

func importTable(pageID string, el *slides.PageElement, counter *int, elementMap map[string]string) []*slides.Request {
	*counter++
	newID := fmt.Sprintf("imp%d_%d_%s", importCounter, *counter, el.ObjectId)
	elementMap[el.ObjectId] = newID

	rows := int64(len(el.Table.TableRows))
	cols := int64(el.Table.Columns)

	reqs := []*slides.Request{{
		CreateTable: &slides.CreateTableRequest{
			ObjectId: newID,
			Rows:     rows,
			Columns:  cols,
			ElementProperties: &slides.PageElementProperties{
				PageObjectId: pageID,
				Size:         el.Size,
				Transform:    el.Transform,
			},
		},
	}}

	for ri, row := range el.Table.TableRows {
		for ci, cell := range row.TableCells {
			if cell.Text == nil {
				continue
			}
			cellLoc := &slides.TableCellLocation{
				RowIndex:    int64(ri),
				ColumnIndex: int64(ci),
			}
			reqs = append(reqs, importTextContent(newID, cell.Text, cellLoc)...)
		}
	}

	return reqs
}

func importLine(pageID string, el *slides.PageElement, counter *int, elementMap map[string]string) []*slides.Request {
	*counter++
	newID := fmt.Sprintf("imp%d_%d_%s", importCounter, *counter, el.ObjectId)
	elementMap[el.ObjectId] = newID

	category := "STRAIGHT"
	if el.Line.LineCategory != "" {
		category = el.Line.LineCategory
	}

	reqs := []*slides.Request{{
		CreateLine: &slides.CreateLineRequest{
			ObjectId: newID,
			Category: category,
			ElementProperties: &slides.PageElementProperties{
				PageObjectId: pageID,
				Size:         el.Size,
				Transform:    el.Transform,
			},
		},
	}}

	if el.Line.LineProperties != nil {
		fields := buildLinePropertiesFields(el.Line.LineProperties)
		if len(fields) > 0 {
			reqs = append(reqs, &slides.Request{
				UpdateLineProperties: &slides.UpdateLinePropertiesRequest{
					ObjectId:       newID,
					LineProperties: el.Line.LineProperties,
					Fields:         joinFields(fields),
				},
			})
		}
	}

	return reqs
}

func buildTextStyleFields(style *slides.TextStyle) []string {
	var fields []string
	if style.FontFamily != "" {
		fields = append(fields, "fontFamily")
	}
	if style.FontSize != nil {
		fields = append(fields, "fontSize")
	}
	if style.ForegroundColor != nil {
		fields = append(fields, "foregroundColor")
	}
	fields = append(fields, "bold", "italic", "underline")
	return fields
}

func buildParagraphStyleFields(style *slides.ParagraphStyle) []string {
	var fields []string
	if style.Alignment != "" {
		fields = append(fields, "alignment")
	}
	if style.LineSpacing != 0 {
		fields = append(fields, "lineSpacing")
	}
	if style.SpaceAbove != nil {
		fields = append(fields, "spaceAbove")
	}
	if style.SpaceBelow != nil {
		fields = append(fields, "spaceBelow")
	}
	if style.IndentStart != nil {
		fields = append(fields, "indentStart")
	}
	if style.IndentEnd != nil {
		fields = append(fields, "indentEnd")
	}
	if style.IndentFirstLine != nil {
		fields = append(fields, "indentFirstLine")
	}
	if style.Direction != "" {
		fields = append(fields, "direction")
	}
	if style.SpacingMode != "" {
		fields = append(fields, "spacingMode")
	}
	return fields
}

func buildLinePropertiesFields(props *slides.LineProperties) []string {
	var fields []string
	if props.LineFill != nil {
		fields = append(fields, "lineFill")
	}
	if props.Weight != nil {
		fields = append(fields, "weight")
	}
	if props.DashStyle != "" {
		fields = append(fields, "dashStyle")
	}
	if props.EndArrow != "" {
		fields = append(fields, "endArrow")
	}
	if props.StartArrow != "" {
		fields = append(fields, "startArrow")
	}
	return fields
}

func joinFields(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ","
		}
		result += f
	}
	return result
}
