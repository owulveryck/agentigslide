package formatter

import (
	"google.golang.org/api/slides/v1"
)

const emuToPoints = 12700.0

// ExtractStructure extracts text element structural information from all slides
// in a presentation, including bounding boxes, text runs with styling,
// paragraph spacing data, colors, alignment, and shape properties.
func ExtractStructure(pres *slides.Presentation) []SlideInfo {
	var result []SlideInfo

	for i, page := range pres.Slides {
		slide := SlideInfo{
			SlideIndex: i,
			PageID:     page.ObjectId,
		}

		for _, el := range page.PageElements {
			extractElement(&slide, el, nil)
		}

		if len(slide.Elements) > 0 {
			result = append(result, slide)
		}
	}

	return result
}

// ExtractStructureForPages extracts structural information only for slides
// whose PageObjectID is in the provided set.
func ExtractStructureForPages(pres *slides.Presentation, pageIDs map[string]bool) []SlideInfo {
	var result []SlideInfo

	for i, page := range pres.Slides {
		if !pageIDs[page.ObjectId] {
			continue
		}

		slide := SlideInfo{
			SlideIndex: i,
			PageID:     page.ObjectId,
		}

		for _, el := range page.PageElements {
			extractElement(&slide, el, nil)
		}

		if len(slide.Elements) > 0 {
			result = append(result, slide)
		}
	}

	return result
}

func extractElement(slide *SlideInfo, el *slides.PageElement, cellLoc *CellRef) {
	bb := computeBoundingBox(el)

	if el.Shape != nil && el.Shape.Text != nil {
		elem := ElementInfo{
			ObjectID:     el.ObjectId,
			BoundingBox:  bb,
			CellLocation: cellLoc,
		}

		if el.Shape.ShapeType != "" {
			elem.ShapeType = el.Shape.ShapeType
		}
		if el.Shape.Placeholder != nil {
			elem.PlaceholderType = el.Shape.Placeholder.Type
		}

		extractShapeProperties(el.Shape, &elem)
		extractTextElements(el.Shape.Text, &elem)

		if len(elem.TextRuns) > 0 {
			slide.Elements = append(slide.Elements, elem)
		}
	}

	if el.Table != nil {
		for rowIdx, row := range el.Table.TableRows {
			for colIdx, cell := range row.TableCells {
				if cell.Text == nil {
					continue
				}
				ref := &CellRef{RowIndex: rowIdx, ColumnIndex: colIdx}
				elem := ElementInfo{
					ObjectID:     el.ObjectId,
					ShapeType:    "TABLE_CELL",
					BoundingBox:  bb,
					CellLocation: ref,
				}
				extractTextElements(cell.Text, &elem)
				if len(elem.TextRuns) > 0 {
					slide.Elements = append(slide.Elements, elem)
				}
			}
		}
	}

	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			extractElement(slide, child, cellLoc)
		}
	}
}

func extractShapeProperties(shape *slides.Shape, elem *ElementInfo) {
	if shape.ShapeProperties == nil {
		return
	}
	props := shape.ShapeProperties

	if props.ContentAlignment != "" {
		elem.ContentAlignment = props.ContentAlignment
	}

	if props.ShapeBackgroundFill != nil &&
		props.ShapeBackgroundFill.SolidFill != nil &&
		props.ShapeBackgroundFill.SolidFill.Color != nil &&
		props.ShapeBackgroundFill.SolidFill.Color.RgbColor != nil {
		rgb := props.ShapeBackgroundFill.SolidFill.Color.RgbColor
		elem.BackgroundColor = &RGBColor{
			Red:   rgb.Red,
			Green: rgb.Green,
			Blue:  rgb.Blue,
		}
	}

	if props.Outline != nil &&
		props.Outline.OutlineFill != nil &&
		props.Outline.OutlineFill.SolidFill != nil &&
		props.Outline.OutlineFill.SolidFill.Color != nil &&
		props.Outline.OutlineFill.SolidFill.Color.RgbColor != nil {
		rgb := props.Outline.OutlineFill.SolidFill.Color.RgbColor
		elem.OutlineColor = &RGBColor{
			Red:   rgb.Red,
			Green: rgb.Green,
			Blue:  rgb.Blue,
		}
		if props.Outline.Weight != nil {
			elem.OutlineWeightPt = props.Outline.Weight.Magnitude
		}
	}
}

func computeBoundingBox(el *slides.PageElement) BoundingBox {
	var bb BoundingBox
	if el.Size != nil {
		if el.Size.Width != nil {
			bb.WidthPt = el.Size.Width.Magnitude / emuToPoints
		}
		if el.Size.Height != nil {
			bb.HeightPt = el.Size.Height.Magnitude / emuToPoints
		}
	}
	if el.Transform != nil {
		bb.LeftPt = el.Transform.TranslateX / emuToPoints
		bb.TopPt = el.Transform.TranslateY / emuToPoints
	}
	return bb
}

func extractTextElements(text *slides.TextContent, elem *ElementInfo) {
	for _, te := range text.TextElements {
		startIdx := int(te.StartIndex)
		endIdx := int(te.EndIndex)

		if te.TextRun != nil {
			tr := TextRunInfo{
				StartIndex: startIdx,
				EndIndex:   endIdx,
				Content:    te.TextRun.Content,
			}
			if te.TextRun.Style != nil {
				s := te.TextRun.Style
				if s.FontFamily != "" {
					tr.FontFamily = s.FontFamily
				}
				if s.FontSize != nil {
					tr.FontSizePt = s.FontSize.Magnitude
				}
				tr.Bold = s.Bold
				tr.Italic = s.Italic
				tr.Underline = s.Underline
				tr.Strikethrough = s.Strikethrough

				if s.ForegroundColor != nil &&
					s.ForegroundColor.OpaqueColor != nil &&
					s.ForegroundColor.OpaqueColor.RgbColor != nil {
					rgb := s.ForegroundColor.OpaqueColor.RgbColor
					tr.ForegroundColor = &RGBColor{
						Red:   rgb.Red,
						Green: rgb.Green,
						Blue:  rgb.Blue,
					}
				}
			}
			elem.TextRuns = append(elem.TextRuns, tr)
		}

		if te.ParagraphMarker != nil && te.ParagraphMarker.Style != nil {
			pi := ParagraphInfo{
				StartIndex: startIdx,
				EndIndex:   endIdx,
			}
			style := te.ParagraphMarker.Style
			pi.LineSpacing = style.LineSpacing
			if style.SpaceAbove != nil {
				pi.SpaceAbovePt = style.SpaceAbove.Magnitude
			}
			if style.SpaceBelow != nil {
				pi.SpaceBelowPt = style.SpaceBelow.Magnitude
			}
			if style.Alignment != "" {
				pi.Alignment = style.Alignment
			}
			if style.IndentStart != nil {
				pi.IndentStartPt = style.IndentStart.Magnitude
			}
			if style.IndentEnd != nil {
				pi.IndentEndPt = style.IndentEnd.Magnitude
			}
			if style.IndentFirstLine != nil {
				pi.IndentFirstPt = style.IndentFirstLine.Magnitude
			}
			elem.Paragraphs = append(elem.Paragraphs, pi)
		}
	}
}
