package formatter

import (
	"testing"

	slides "google.golang.org/api/slides/v1"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func ptr[T any](v T) *T { return &v }

// makeTextRun builds a slides.TextElement that contains a TextRun.
func makeTextRun(start, end int64, content, fontFamily string, fontSize float64, bold, italic bool) *slides.TextElement {
	te := &slides.TextElement{
		StartIndex: start,
		EndIndex:   end,
		TextRun: &slides.TextRun{
			Content: content,
			Style: &slides.TextStyle{
				FontFamily: fontFamily,
				Bold:       bold,
				Italic:     italic,
			},
		},
	}
	if fontSize > 0 {
		te.TextRun.Style.FontSize = &slides.Dimension{
			Magnitude: fontSize,
			Unit:      "PT",
		}
	}
	return te
}

// makeParagraphMarker builds a slides.TextElement that contains a ParagraphMarker.
func makeParagraphMarker(start, end int64, lineSpacing float64, spaceAbove, spaceBelow *float64) *slides.TextElement {
	style := &slides.ParagraphStyle{
		LineSpacing: lineSpacing,
	}
	if spaceAbove != nil {
		style.SpaceAbove = &slides.Dimension{Magnitude: *spaceAbove, Unit: "PT"}
	}
	if spaceBelow != nil {
		style.SpaceBelow = &slides.Dimension{Magnitude: *spaceBelow, Unit: "PT"}
	}
	return &slides.TextElement{
		StartIndex: start,
		EndIndex:   end,
		ParagraphMarker: &slides.ParagraphMarker{
			Style: style,
		},
	}
}

// makeShape builds a PageElement containing a Shape with the given text elements.
func makeShape(objectID, shapeType string, width, height, left, top float64, textElements []*slides.TextElement) *slides.PageElement {
	return &slides.PageElement{
		ObjectId: objectID,
		Size: &slides.Size{
			Width:  &slides.Dimension{Magnitude: width, Unit: "EMU"},
			Height: &slides.Dimension{Magnitude: height, Unit: "EMU"},
		},
		Transform: &slides.AffineTransform{
			TranslateX: left,
			TranslateY: top,
		},
		Shape: &slides.Shape{
			ShapeType: shapeType,
			Text: &slides.TextContent{
				TextElements: textElements,
			},
		},
	}
}

// makeTable builds a PageElement containing a Table.
func makeTable(objectID string, rows [][]string) *slides.PageElement {
	var tableRows []*slides.TableRow
	for _, row := range rows {
		var cells []*slides.TableCell
		for _, cellText := range row {
			var tc *slides.TableCell
			if cellText == "" {
				tc = &slides.TableCell{} // nil Text
			} else {
				tc = &slides.TableCell{
					Text: &slides.TextContent{
						TextElements: []*slides.TextElement{
							makeTextRun(0, int64(len(cellText)), cellText, "Arial", 12, false, false),
						},
					},
				}
			}
			cells = append(cells, tc)
		}
		tableRows = append(tableRows, &slides.TableRow{TableCells: cells})
	}
	return &slides.PageElement{
		ObjectId: objectID,
		Size: &slides.Size{
			Width:  &slides.Dimension{Magnitude: 127000, Unit: "EMU"},
			Height: &slides.Dimension{Magnitude: 254000, Unit: "EMU"},
		},
		Transform: &slides.AffineTransform{
			TranslateX: 0,
			TranslateY: 0,
		},
		Table: &slides.Table{
			Rows:      int64(len(rows)),
			Columns:   int64(len(rows[0])),
			TableRows: tableRows,
		},
	}
}

// ---------------------------------------------------------------------------
// ExtractStructure tests (migrated from fixfonts)
// ---------------------------------------------------------------------------

func TestExtractStructure_EmptyPresentation(t *testing.T) {
	pres := &slides.Presentation{}
	result := ExtractStructure(pres)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty presentation, got %d slide(s)", len(result))
	}
}

func TestExtractStructure_NoSlides(t *testing.T) {
	pres := &slides.Presentation{Slides: []*slides.Page{}}
	result := ExtractStructure(pres)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestExtractStructure_ShapeWithTextRuns(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX",
						127000, 254000, 381000, 508000,
						[]*slides.TextElement{
							makeTextRun(0, 5, "Hello", "Roboto", 18, true, false),
							makeTextRun(5, 11, " World", "Roboto", 18, false, true),
						},
					),
				},
			},
		},
	}

	result := ExtractStructure(pres)

	if len(result) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(result))
	}
	slide := result[0]
	if slide.SlideIndex != 0 {
		t.Errorf("expected SlideIndex 0, got %d", slide.SlideIndex)
	}
	if slide.PageID != "page1" {
		t.Errorf("expected PageID page1, got %s", slide.PageID)
	}
	if len(slide.Elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(slide.Elements))
	}

	elem := slide.Elements[0]
	if elem.ObjectID != "shape1" {
		t.Errorf("expected ObjectID shape1, got %s", elem.ObjectID)
	}
	if elem.ShapeType != "TEXT_BOX" {
		t.Errorf("expected ShapeType TEXT_BOX, got %s", elem.ShapeType)
	}

	// BoundingBox: EMU / 12700
	const emu = 12700.0
	wantWidth := 127000 / emu  // 10
	wantHeight := 254000 / emu // 20
	wantLeft := 381000 / emu   // 30
	wantTop := 508000 / emu    // 40
	if elem.BoundingBox.WidthPt != wantWidth {
		t.Errorf("WidthPt: want %f, got %f", wantWidth, elem.BoundingBox.WidthPt)
	}
	if elem.BoundingBox.HeightPt != wantHeight {
		t.Errorf("HeightPt: want %f, got %f", wantHeight, elem.BoundingBox.HeightPt)
	}
	if elem.BoundingBox.LeftPt != wantLeft {
		t.Errorf("LeftPt: want %f, got %f", wantLeft, elem.BoundingBox.LeftPt)
	}
	if elem.BoundingBox.TopPt != wantTop {
		t.Errorf("TopPt: want %f, got %f", wantTop, elem.BoundingBox.TopPt)
	}

	// TextRuns
	if len(elem.TextRuns) != 2 {
		t.Fatalf("expected 2 text runs, got %d", len(elem.TextRuns))
	}
	tr0 := elem.TextRuns[0]
	if tr0.Content != "Hello" || tr0.FontFamily != "Roboto" || tr0.FontSizePt != 18 || !tr0.Bold || tr0.Italic {
		t.Errorf("text run 0 mismatch: %+v", tr0)
	}
	tr1 := elem.TextRuns[1]
	if tr1.Content != " World" || tr1.FontFamily != "Roboto" || tr1.FontSizePt != 18 || tr1.Bold || !tr1.Italic {
		t.Errorf("text run 1 mismatch: %+v", tr1)
	}
	if tr1.StartIndex != 5 || tr1.EndIndex != 11 {
		t.Errorf("text run 1 indices: want 5-11, got %d-%d", tr1.StartIndex, tr1.EndIndex)
	}
}

func TestExtractStructure_ShapeWithNoText(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					{
						ObjectId: "notext",
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 100},
							Height: &slides.Dimension{Magnitude: 100},
						},
						Transform: &slides.AffineTransform{},
						Shape:     &slides.Shape{ShapeType: "RECTANGLE"},
					},
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 0 {
		t.Errorf("expected no slides (shape has no text), got %d", len(result))
	}
}

func TestExtractStructure_ShapeWithEmptyTextContent(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					{
						ObjectId: "emptytext",
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 100},
							Height: &slides.Dimension{Magnitude: 100},
						},
						Transform: &slides.AffineTransform{},
						Shape: &slides.Shape{
							ShapeType: "TEXT_BOX",
							Text:      &slides.TextContent{}, // no text elements
						},
					},
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 0 {
		t.Errorf("expected no slides (shape has empty text), got %d", len(result))
	}
}

func TestExtractStructure_TableWithCells(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeTable("table1", [][]string{
						{"Cell A", "Cell B"},
						{"", "Cell D"},
					}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(result))
	}

	// 3 cells with text (Cell A, Cell B, Cell D), 1 empty cell skipped
	if len(result[0].Elements) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result[0].Elements))
	}

	for _, elem := range result[0].Elements {
		if elem.ShapeType != "TABLE_CELL" {
			t.Errorf("expected ShapeType TABLE_CELL, got %s", elem.ShapeType)
		}
		if elem.CellLocation == nil {
			t.Error("expected CellLocation to be set for table cell")
		}
	}

	// Check specific cell locations
	e0 := result[0].Elements[0]
	if e0.CellLocation.RowIndex != 0 || e0.CellLocation.ColumnIndex != 0 {
		t.Errorf("first cell: want row=0 col=0, got row=%d col=%d",
			e0.CellLocation.RowIndex, e0.CellLocation.ColumnIndex)
	}
	e2 := result[0].Elements[2]
	if e2.CellLocation.RowIndex != 1 || e2.CellLocation.ColumnIndex != 1 {
		t.Errorf("third cell: want row=1 col=1, got row=%d col=%d",
			e2.CellLocation.RowIndex, e2.CellLocation.ColumnIndex)
	}
}

func TestExtractStructure_ElementGroup(t *testing.T) {
	child := makeShape("child1", "TEXT_BOX",
		127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 4, "test", "Arial", 14, false, false),
		},
	)
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					{
						ObjectId: "group1",
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 500000},
							Height: &slides.Dimension{Magnitude: 500000},
						},
						Transform: &slides.AffineTransform{},
						ElementGroup: &slides.Group{
							Children: []*slides.PageElement{child},
						},
					},
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(result))
	}
	if len(result[0].Elements) != 1 {
		t.Fatalf("expected 1 element from group child, got %d", len(result[0].Elements))
	}
	if result[0].Elements[0].ObjectID != "child1" {
		t.Errorf("expected child1, got %s", result[0].Elements[0].ObjectID)
	}
}

func TestExtractStructure_PlaceholderType(t *testing.T) {
	el := makeShape("ph1", "TEXT_BOX",
		127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 5, "Title", "Arial", 24, false, false),
		},
	)
	el.Shape.Placeholder = &slides.Placeholder{Type: "TITLE"}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{el},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure length")
	}
	if result[0].Elements[0].PlaceholderType != "TITLE" {
		t.Errorf("expected PlaceholderType TITLE, got %s", result[0].Elements[0].PlaceholderType)
	}
}

func TestExtractStructure_ParagraphInfo(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX",
						127000, 127000, 0, 0,
						[]*slides.TextElement{
							makeParagraphMarker(0, 10, 115.0, ptr(5.0), ptr(3.0)),
							makeTextRun(0, 10, "Some text.", "Arial", 12, false, false),
						},
					),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	elem := result[0].Elements[0]
	if len(elem.Paragraphs) != 1 {
		t.Fatalf("expected 1 paragraph, got %d", len(elem.Paragraphs))
	}
	p := elem.Paragraphs[0]
	if p.LineSpacing != 115.0 {
		t.Errorf("LineSpacing: want 115, got %f", p.LineSpacing)
	}
	if p.SpaceAbovePt != 5.0 {
		t.Errorf("SpaceAbovePt: want 5, got %f", p.SpaceAbovePt)
	}
	if p.SpaceBelowPt != 3.0 {
		t.Errorf("SpaceBelowPt: want 3, got %f", p.SpaceBelowPt)
	}
}

func TestExtractStructure_TextRunWithNoStyle(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					{
						ObjectId: "s1",
						Size: &slides.Size{
							Width:  &slides.Dimension{Magnitude: 127000},
							Height: &slides.Dimension{Magnitude: 127000},
						},
						Transform: &slides.AffineTransform{},
						Shape: &slides.Shape{
							ShapeType: "TEXT_BOX",
							Text: &slides.TextContent{
								TextElements: []*slides.TextElement{
									{
										StartIndex: 0,
										EndIndex:   4,
										TextRun: &slides.TextRun{
											Content: "test",
											// Style is nil
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	tr := result[0].Elements[0].TextRuns[0]
	if tr.FontFamily != "" || tr.FontSizePt != 0 || tr.Bold || tr.Italic {
		t.Errorf("expected default values for unstyled run, got %+v", tr)
	}
}

func TestExtractStructure_BoundingBoxNoSizeNoTransform(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					{
						ObjectId: "s1",
						// No Size, no Transform
						Shape: &slides.Shape{
							ShapeType: "TEXT_BOX",
							Text: &slides.TextContent{
								TextElements: []*slides.TextElement{
									makeTextRun(0, 2, "hi", "Arial", 10, false, false),
								},
							},
						},
					},
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	bb := result[0].Elements[0].BoundingBox
	if bb.WidthPt != 0 || bb.HeightPt != 0 || bb.LeftPt != 0 || bb.TopPt != 0 {
		t.Errorf("expected all zeros for missing size/transform, got %+v", bb)
	}
}

func TestExtractStructure_MultipleSlides(t *testing.T) {
	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page0",
				PageElements: []*slides.PageElement{
					makeShape("s0", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{makeTextRun(0, 1, "A", "Arial", 10, false, false)}),
				},
			},
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{}, // no elements
			},
			{
				ObjectId: "page2",
				PageElements: []*slides.PageElement{
					makeShape("s2", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{makeTextRun(0, 1, "B", "Arial", 10, false, false)}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 2 {
		t.Fatalf("expected 2 slides (page1 has no elements), got %d", len(result))
	}
	if result[0].SlideIndex != 0 {
		t.Errorf("first slide index: want 0, got %d", result[0].SlideIndex)
	}
	if result[1].SlideIndex != 2 {
		t.Errorf("second slide index: want 2, got %d", result[1].SlideIndex)
	}
}

// ---------------------------------------------------------------------------
// NEW tests for enriched extraction fields
// ---------------------------------------------------------------------------

func TestExtractStructure_TextRunForegroundColor(t *testing.T) {
	te := makeTextRun(0, 5, "Hello", "Arial", 14, false, false)
	te.TextRun.Style.ForegroundColor = &slides.OptionalColor{
		OpaqueColor: &slides.OpaqueColor{
			RgbColor: &slides.RgbColor{
				Red:   1.0,
				Green: 0.0,
				Blue:  0.5,
			},
		},
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{te}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	tr := result[0].Elements[0].TextRuns[0]
	if tr.ForegroundColor == nil {
		t.Fatal("expected ForegroundColor to be set")
	}
	if tr.ForegroundColor.Red != 1.0 {
		t.Errorf("ForegroundColor.Red: want 1.0, got %f", tr.ForegroundColor.Red)
	}
	if tr.ForegroundColor.Green != 0.0 {
		t.Errorf("ForegroundColor.Green: want 0.0, got %f", tr.ForegroundColor.Green)
	}
	if tr.ForegroundColor.Blue != 0.5 {
		t.Errorf("ForegroundColor.Blue: want 0.5, got %f", tr.ForegroundColor.Blue)
	}
}

func TestExtractStructure_TextRunUnderlineStrikethrough(t *testing.T) {
	te := makeTextRun(0, 4, "test", "Arial", 12, false, false)
	te.TextRun.Style.Underline = true
	te.TextRun.Style.Strikethrough = true

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{te}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	tr := result[0].Elements[0].TextRuns[0]
	if !tr.Underline {
		t.Error("expected Underline to be true")
	}
	if !tr.Strikethrough {
		t.Error("expected Strikethrough to be true")
	}
}

func TestExtractStructure_ParagraphAlignment(t *testing.T) {
	pm := &slides.TextElement{
		StartIndex: 0,
		EndIndex:   10,
		ParagraphMarker: &slides.ParagraphMarker{
			Style: &slides.ParagraphStyle{
				Alignment: "CENTER",
			},
		},
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{
							pm,
							makeTextRun(0, 10, "Centered!!", "Arial", 12, false, false),
						}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	elem := result[0].Elements[0]
	if len(elem.Paragraphs) != 1 {
		t.Fatalf("expected 1 paragraph, got %d", len(elem.Paragraphs))
	}
	if elem.Paragraphs[0].Alignment != "CENTER" {
		t.Errorf("expected Alignment CENTER, got %s", elem.Paragraphs[0].Alignment)
	}
}

func TestExtractStructure_ParagraphIndentation(t *testing.T) {
	pm := &slides.TextElement{
		StartIndex: 0,
		EndIndex:   10,
		ParagraphMarker: &slides.ParagraphMarker{
			Style: &slides.ParagraphStyle{
				IndentStart:     &slides.Dimension{Magnitude: 36, Unit: "PT"},
				IndentEnd:       &slides.Dimension{Magnitude: 18, Unit: "PT"},
				IndentFirstLine: &slides.Dimension{Magnitude: 72, Unit: "PT"},
			},
		},
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{
							pm,
							makeTextRun(0, 10, "Indented!!", "Arial", 12, false, false),
						}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	p := result[0].Elements[0].Paragraphs[0]
	if p.IndentStartPt != 36 {
		t.Errorf("IndentStartPt: want 36, got %f", p.IndentStartPt)
	}
	if p.IndentEndPt != 18 {
		t.Errorf("IndentEndPt: want 18, got %f", p.IndentEndPt)
	}
	if p.IndentFirstPt != 72 {
		t.Errorf("IndentFirstPt: want 72, got %f", p.IndentFirstPt)
	}
}

func TestExtractStructure_ShapeBackgroundColor(t *testing.T) {
	el := makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 4, "test", "Arial", 12, false, false),
		})
	el.Shape.ShapeProperties = &slides.ShapeProperties{
		ShapeBackgroundFill: &slides.ShapeBackgroundFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red:   0.2,
						Green: 0.4,
						Blue:  0.6,
					},
				},
			},
		},
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{el},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	bg := result[0].Elements[0].BackgroundColor
	if bg == nil {
		t.Fatal("expected BackgroundColor to be set")
	}
	if bg.Red != 0.2 {
		t.Errorf("BackgroundColor.Red: want 0.2, got %f", bg.Red)
	}
	if bg.Green != 0.4 {
		t.Errorf("BackgroundColor.Green: want 0.4, got %f", bg.Green)
	}
	if bg.Blue != 0.6 {
		t.Errorf("BackgroundColor.Blue: want 0.6, got %f", bg.Blue)
	}
}

func TestExtractStructure_ShapeContentAlignment(t *testing.T) {
	el := makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 4, "test", "Arial", 12, false, false),
		})
	el.Shape.ShapeProperties = &slides.ShapeProperties{
		ContentAlignment: "MIDDLE",
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{el},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	if result[0].Elements[0].ContentAlignment != "MIDDLE" {
		t.Errorf("expected ContentAlignment MIDDLE, got %s", result[0].Elements[0].ContentAlignment)
	}
}

func TestExtractStructure_ShapeOutline(t *testing.T) {
	el := makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 4, "test", "Arial", 12, false, false),
		})
	el.Shape.ShapeProperties = &slides.ShapeProperties{
		Outline: &slides.Outline{
			OutlineFill: &slides.OutlineFill{
				SolidFill: &slides.SolidFill{
					Color: &slides.OpaqueColor{
						RgbColor: &slides.RgbColor{
							Red:   0.1,
							Green: 0.2,
							Blue:  0.3,
						},
					},
				},
			},
			Weight: &slides.Dimension{Magnitude: 2.5, Unit: "PT"},
		},
	}

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{el},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	elem := result[0].Elements[0]
	if elem.OutlineColor == nil {
		t.Fatal("expected OutlineColor to be set")
	}
	if elem.OutlineColor.Red != 0.1 {
		t.Errorf("OutlineColor.Red: want 0.1, got %f", elem.OutlineColor.Red)
	}
	if elem.OutlineColor.Green != 0.2 {
		t.Errorf("OutlineColor.Green: want 0.2, got %f", elem.OutlineColor.Green)
	}
	if elem.OutlineColor.Blue != 0.3 {
		t.Errorf("OutlineColor.Blue: want 0.3, got %f", elem.OutlineColor.Blue)
	}
	if elem.OutlineWeightPt != 2.5 {
		t.Errorf("OutlineWeightPt: want 2.5, got %f", elem.OutlineWeightPt)
	}
}

func TestExtractStructure_NilShapeProperties(t *testing.T) {
	el := makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
		[]*slides.TextElement{
			makeTextRun(0, 4, "test", "Arial", 12, false, false),
		})
	// Explicitly set ShapeProperties to nil (makeShape does not set it)
	el.Shape.ShapeProperties = nil

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId:     "page1",
				PageElements: []*slides.PageElement{el},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	elem := result[0].Elements[0]
	if elem.BackgroundColor != nil {
		t.Errorf("expected BackgroundColor to be nil, got %+v", elem.BackgroundColor)
	}
	if elem.OutlineColor != nil {
		t.Errorf("expected OutlineColor to be nil, got %+v", elem.OutlineColor)
	}
	if elem.ContentAlignment != "" {
		t.Errorf("expected ContentAlignment to be empty, got %s", elem.ContentAlignment)
	}
	if elem.OutlineWeightPt != 0 {
		t.Errorf("expected OutlineWeightPt to be 0, got %f", elem.OutlineWeightPt)
	}
}

func TestExtractStructure_TextRunNilForegroundColor(t *testing.T) {
	te := makeTextRun(0, 5, "Hello", "Arial", 14, true, false)
	// Style is set (bold=true) but ForegroundColor is nil (default from makeTextRun)

	pres := &slides.Presentation{
		Slides: []*slides.Page{
			{
				ObjectId: "page1",
				PageElements: []*slides.PageElement{
					makeShape("shape1", "TEXT_BOX", 127000, 127000, 0, 0,
						[]*slides.TextElement{te}),
				},
			},
		},
	}

	result := ExtractStructure(pres)
	if len(result) != 1 || len(result[0].Elements) != 1 {
		t.Fatalf("unexpected structure")
	}
	tr := result[0].Elements[0].TextRuns[0]
	if tr.ForegroundColor != nil {
		t.Errorf("expected ForegroundColor to be nil, got %+v", tr.ForegroundColor)
	}
	// Verify the style fields that ARE set still work
	if !tr.Bold {
		t.Error("expected Bold to be true")
	}
}
