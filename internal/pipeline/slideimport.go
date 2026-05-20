package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/api/slides/v1"
)

var importCounter int

// ImportTemplateSlide reads a template slide from the template presentation
// and recreates its visual elements in the target presentation. It returns
// the new page's ObjectID and a mapping from original to new element ObjectIDs.
func ImportTemplateSlide(
	ctx context.Context,
	slidesSrv *slides.Service,
	templatePresID string,
	sourceSlideID string,
	targetPresID string,
	insertionIndex int,
) (newPageID string, elementMap map[string]string, err error) {
	templatePres, err := slidesSrv.Presentations.Get(templatePresID).Do()
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
	_, err = slidesSrv.Presentations.BatchUpdate(targetPresID, &slides.BatchUpdatePresentationRequest{
		Requests: createSlideReqs,
	}).Do()
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
		_, err = slidesSrv.Presentations.BatchUpdate(targetPresID, &slides.BatchUpdatePresentationRequest{
			Requests: elementReqs,
		}).Do()
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

	var reqs []*slides.Request
	insertIdx := int64(0)

	for _, te := range shape.Text.TextElements {
		if te.TextRun == nil || te.TextRun.Content == "" {
			continue
		}

		reqs = append(reqs, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId:       objectID,
				Text:           te.TextRun.Content,
				InsertionIndex: insertIdx,
			},
		})

		if te.TextRun.Style != nil {
			styleFields := buildTextStyleFields(te.TextRun.Style)
			if len(styleFields) > 0 {
				endIdx := insertIdx + int64(len([]rune(te.TextRun.Content)))
				startIdx := insertIdx
				reqs = append(reqs, &slides.Request{
					UpdateTextStyle: &slides.UpdateTextStyleRequest{
						ObjectId: objectID,
						Style:    te.TextRun.Style,
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
			insertIdx := int64(0)
			for _, te := range cell.Text.TextElements {
				if te.TextRun == nil || te.TextRun.Content == "" {
					continue
				}
				reqs = append(reqs, &slides.Request{
					InsertText: &slides.InsertTextRequest{
						ObjectId:       newID,
						Text:           te.TextRun.Content,
						InsertionIndex: insertIdx,
						CellLocation:   cellLoc,
					},
				})
				insertIdx += int64(len([]rune(te.TextRun.Content)))
			}
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
	if style.Bold {
		fields = append(fields, "bold")
	}
	if style.Italic {
		fields = append(fields, "italic")
	}
	if style.Underline {
		fields = append(fields, "underline")
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
