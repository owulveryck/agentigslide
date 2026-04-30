package fixfonts

import (
	"slices"
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

// makeCorrection is a builder for Correction with sensible defaults.
func makeCorrection(objectID, typ string, opts ...func(*Correction)) Correction {
	c := Correction{
		ObjectID: objectID,
		Type:     typ,
		Reason:   "test reason",
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func withFontSize(pt float64) func(*Correction) {
	return func(c *Correction) { c.FontSizePt = &pt }
}
func withFontFamily(ff string) func(*Correction) {
	return func(c *Correction) { c.FontFamily = &ff }
}
func withLineSpacing(ls float64) func(*Correction) {
	return func(c *Correction) { c.LineSpacing = &ls }
}
func withSpaceAbove(pt float64) func(*Correction) {
	return func(c *Correction) { c.SpaceAbovePt = &pt }
}
func withSpaceBelow(pt float64) func(*Correction) {
	return func(c *Correction) { c.SpaceBelowPt = &pt }
}
func withRange(start, end int) func(*Correction) {
	return func(c *Correction) { c.StartIndex = &start; c.EndIndex = &end }
}
func withCellLoc(row, col int) func(*Correction) {
	return func(c *Correction) { c.CellLocation = &CellRef{RowIndex: row, ColumnIndex: col} }
}

// sampleStructure returns a minimal structure slice for validation tests.
func sampleStructure() []SlideInfo {
	return []SlideInfo{
		{
			SlideIndex: 0,
			PageID:     "p0",
			Elements: []ElementInfo{
				{ObjectID: "obj1"},
				{ObjectID: "obj2"},
				{ObjectID: "tableObj"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// ExtractStructure tests
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
// ValidateCorrections tests
// ---------------------------------------------------------------------------

func TestValidateCorrections_Empty(t *testing.T) {
	plan := &CorrectionPlan{}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestValidateCorrections_ValidTextStyle(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle", withFontSize(14)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1 valid correction, got %d", len(result))
	}
	if result[0].ObjectID != "obj1" {
		t.Errorf("expected obj1, got %s", result[0].ObjectID)
	}
}

func TestValidateCorrections_UnknownObjectID(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("unknown", "textStyle", withFontSize(14)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected 0 (unknown objectId filtered), got %d", len(result))
	}
}

func TestValidateCorrections_UnknownType(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "unknownType", withFontSize(14)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected 0 (unknown type filtered), got %d", len(result))
	}
}

func TestValidateCorrections_TextStyleNoChanges(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle"), // no FontSizePt, no FontFamily
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected 0 (textStyle with no changes filtered), got %d", len(result))
	}
}

func TestValidateCorrections_ParagraphStyleNoChanges(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "paragraphStyle"), // no LineSpacing, no SpaceAbove, no SpaceBelow
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected 0 (paragraphStyle with no changes filtered), got %d", len(result))
	}
}

func TestValidateCorrections_ValidParagraphStyle(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj2", "paragraphStyle", withLineSpacing(100)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestValidateCorrections_TextStyleWithFontFamilyOnly(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle", withFontFamily("Roboto")),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1 (fontFamily alone is sufficient), got %d", len(result))
	}
}

func TestValidateCorrections_ParagraphStyleWithSpaceAboveOnly(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "paragraphStyle", withSpaceAbove(3.0)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestValidateCorrections_ParagraphStyleWithSpaceBelowOnly(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "paragraphStyle", withSpaceBelow(2.0)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func TestValidateCorrections_MixedValidAndInvalid(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle", withFontSize(12)),         // valid
			makeCorrection("unknown", "textStyle", withFontSize(10)),      // unknown objectId
			makeCorrection("obj2", "badType", withFontSize(10)),           // unknown type
			makeCorrection("obj2", "textStyle"),                           // no changes
			makeCorrection("obj2", "paragraphStyle", withLineSpacing(90)), // valid
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 2 {
		t.Fatalf("expected 2 valid corrections, got %d", len(result))
	}
	if result[0].ObjectID != "obj1" {
		t.Errorf("first valid: want obj1, got %s", result[0].ObjectID)
	}
	if result[1].ObjectID != "obj2" {
		t.Errorf("second valid: want obj2, got %s", result[1].ObjectID)
	}
}

// ---------------------------------------------------------------------------
// BuildCorrections tests
// ---------------------------------------------------------------------------

func TestBuildCorrections_Empty(t *testing.T) {
	result := BuildCorrections(nil)
	if len(result) != 0 {
		t.Errorf("expected nil/empty, got %d", len(result))
	}
}

func TestBuildCorrections_TextStyleFontSize(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(14)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.UpdateTextStyle == nil {
		t.Fatal("expected UpdateTextStyle request")
	}
	uts := r.UpdateTextStyle
	if uts.ObjectId != "obj1" {
		t.Errorf("ObjectId: want obj1, got %s", uts.ObjectId)
	}
	if uts.Style.FontSize == nil || uts.Style.FontSize.Magnitude != 14 || uts.Style.FontSize.Unit != "PT" {
		t.Errorf("FontSize mismatch: %+v", uts.Style.FontSize)
	}
	if uts.Fields != "fontSize" {
		t.Errorf("Fields: want fontSize, got %s", uts.Fields)
	}
	// Default ALL range
	if uts.TextRange == nil || uts.TextRange.Type != "ALL" {
		t.Errorf("expected ALL text range, got %+v", uts.TextRange)
	}
}

func TestBuildCorrections_TextStyleFontFamily(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontFamily("Roboto")),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	uts := reqs[0].UpdateTextStyle
	if uts.Style.FontFamily != "Roboto" {
		t.Errorf("FontFamily: want Roboto, got %s", uts.Style.FontFamily)
	}
	if uts.Fields != "fontFamily" {
		t.Errorf("Fields: want fontFamily, got %s", uts.Fields)
	}
}

func TestBuildCorrections_TextStyleBoth(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(10), withFontFamily("Roboto")),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	uts := reqs[0].UpdateTextStyle
	if uts.Style.FontSize == nil || uts.Style.FontSize.Magnitude != 10 {
		t.Errorf("FontSize mismatch")
	}
	if uts.Style.FontFamily != "Roboto" {
		t.Errorf("FontFamily mismatch")
	}
	if uts.Fields != "fontSize,fontFamily" {
		t.Errorf("Fields: want fontSize,fontFamily, got %s", uts.Fields)
	}
}

func TestBuildCorrections_ParagraphStyleLineSpacing(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withLineSpacing(100)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.UpdateParagraphStyle == nil {
		t.Fatal("expected UpdateParagraphStyle request")
	}
	ups := r.UpdateParagraphStyle
	if ups.Style.LineSpacing != 100 {
		t.Errorf("LineSpacing: want 100, got %f", ups.Style.LineSpacing)
	}
	if ups.Fields != "lineSpacing" {
		t.Errorf("Fields: want lineSpacing, got %s", ups.Fields)
	}
}

func TestBuildCorrections_ParagraphStyleSpaceAbove(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withSpaceAbove(5)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.Style.SpaceAbove == nil || ups.Style.SpaceAbove.Magnitude != 5 || ups.Style.SpaceAbove.Unit != "PT" {
		t.Errorf("SpaceAbove mismatch: %+v", ups.Style.SpaceAbove)
	}
	if ups.Fields != "spaceAbove" {
		t.Errorf("Fields: want spaceAbove, got %s", ups.Fields)
	}
}

func TestBuildCorrections_ParagraphStyleSpaceBelow(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withSpaceBelow(3)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.Style.SpaceBelow == nil || ups.Style.SpaceBelow.Magnitude != 3 || ups.Style.SpaceBelow.Unit != "PT" {
		t.Errorf("SpaceBelow mismatch: %+v", ups.Style.SpaceBelow)
	}
	if ups.Fields != "spaceBelow" {
		t.Errorf("Fields: want spaceBelow, got %s", ups.Fields)
	}
}

func TestBuildCorrections_WithCellLocation(t *testing.T) {
	corrections := []Correction{
		makeCorrection("tbl1", "textStyle", withFontSize(10), withCellLoc(2, 3)),
	}
	reqs := BuildCorrections(corrections)
	uts := reqs[0].UpdateTextStyle
	if uts.CellLocation == nil {
		t.Fatal("expected CellLocation to be set")
	}
	if uts.CellLocation.RowIndex != 2 || uts.CellLocation.ColumnIndex != 3 {
		t.Errorf("CellLocation: want row=2 col=3, got row=%d col=%d",
			uts.CellLocation.RowIndex, uts.CellLocation.ColumnIndex)
	}
}

func TestBuildCorrections_ParagraphStyleWithCellLocation(t *testing.T) {
	corrections := []Correction{
		makeCorrection("tbl1", "paragraphStyle", withLineSpacing(100), withCellLoc(1, 0)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.CellLocation == nil {
		t.Fatal("expected CellLocation to be set on paragraph style request")
	}
	if ups.CellLocation.RowIndex != 1 || ups.CellLocation.ColumnIndex != 0 {
		t.Errorf("CellLocation: want row=1 col=0, got row=%d col=%d",
			ups.CellLocation.RowIndex, ups.CellLocation.ColumnIndex)
	}
}

func TestBuildCorrections_WithFixedRange(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(12), withRange(5, 10)),
	}
	reqs := BuildCorrections(corrections)
	uts := reqs[0].UpdateTextStyle
	if uts.TextRange == nil {
		t.Fatal("expected TextRange")
	}
	if uts.TextRange.Type != "FIXED_RANGE" {
		t.Errorf("Type: want FIXED_RANGE, got %s", uts.TextRange.Type)
	}
	if uts.TextRange.StartIndex == nil || *uts.TextRange.StartIndex != 5 {
		t.Errorf("StartIndex: want 5, got %v", uts.TextRange.StartIndex)
	}
	if uts.TextRange.EndIndex == nil || *uts.TextRange.EndIndex != 10 {
		t.Errorf("EndIndex: want 10, got %v", uts.TextRange.EndIndex)
	}
}

func TestBuildCorrections_ParagraphWithFixedRange(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withLineSpacing(110), withRange(0, 20)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.TextRange == nil || ups.TextRange.Type != "FIXED_RANGE" {
		t.Errorf("expected FIXED_RANGE, got %+v", ups.TextRange)
	}
}

func TestBuildCorrections_WithoutRange_AllRange(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(12)),
	}
	reqs := BuildCorrections(corrections)
	uts := reqs[0].UpdateTextStyle
	if uts.TextRange == nil || uts.TextRange.Type != "ALL" {
		t.Errorf("expected ALL range, got %+v", uts.TextRange)
	}
}

func TestBuildCorrections_ZeroFontSize_ForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(0)),
	}
	reqs := BuildCorrections(corrections)
	uts := reqs[0].UpdateTextStyle
	if uts.Style.FontSize == nil {
		t.Fatal("expected FontSize even for zero value")
	}
	if uts.Style.FontSize.Magnitude != 0 {
		t.Errorf("Magnitude: want 0, got %f", uts.Style.FontSize.Magnitude)
	}
	if !slices.Contains(uts.Style.FontSize.ForceSendFields, "Magnitude") {
		t.Error("expected ForceSendFields to include Magnitude for zero FontSizePt")
	}
}

func TestBuildCorrections_ZeroSpaceAbove_ForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withSpaceAbove(0)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.Style.SpaceAbove == nil {
		t.Fatal("expected SpaceAbove even for zero value")
	}
	if !slices.Contains(ups.Style.SpaceAbove.ForceSendFields, "Magnitude") {
		t.Error("expected ForceSendFields to include Magnitude for zero SpaceAbovePt")
	}
}

func TestBuildCorrections_ZeroSpaceBelow_ForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withSpaceBelow(0)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if ups.Style.SpaceBelow == nil {
		t.Fatal("expected SpaceBelow even for zero value")
	}
	if !slices.Contains(ups.Style.SpaceBelow.ForceSendFields, "Magnitude") {
		t.Error("expected ForceSendFields to include Magnitude for zero SpaceBelowPt")
	}
}

func TestBuildCorrections_LineSpacing_ForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withLineSpacing(0)),
	}
	reqs := BuildCorrections(corrections)
	ups := reqs[0].UpdateParagraphStyle
	if !slices.Contains(ups.Style.ForceSendFields, "LineSpacing") {
		t.Error("expected ForceSendFields to include LineSpacing")
	}
}

func TestBuildCorrections_FixedRange_ForceSendFields_StartIndex(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(12), withRange(0, 5)),
	}
	reqs := BuildCorrections(corrections)
	tr := reqs[0].UpdateTextStyle.TextRange
	if !slices.Contains(tr.ForceSendFields, "StartIndex") {
		t.Error("expected FIXED_RANGE ForceSendFields to include StartIndex (for zero start index)")
	}
}

func TestBuildCorrections_MultipleCorrections(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(10)),
		makeCorrection("obj2", "paragraphStyle", withLineSpacing(100)),
		makeCorrection("obj3", "textStyle", withFontFamily("Roboto"), withRange(0, 5)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(reqs))
	}
	if reqs[0].UpdateTextStyle == nil {
		t.Error("request 0 should be UpdateTextStyle")
	}
	if reqs[1].UpdateParagraphStyle == nil {
		t.Error("request 1 should be UpdateParagraphStyle")
	}
	if reqs[2].UpdateTextStyle == nil {
		t.Error("request 2 should be UpdateTextStyle")
	}
}

func TestBuildCorrections_NonZeroFontSize_NoForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withFontSize(14)),
	}
	reqs := BuildCorrections(corrections)
	fs := reqs[0].UpdateTextStyle.Style.FontSize
	if len(fs.ForceSendFields) != 0 {
		t.Errorf("expected no ForceSendFields for non-zero FontSize, got %v", fs.ForceSendFields)
	}
}

func TestBuildCorrections_NonZeroSpaceAbove_NoForceSendFields(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withSpaceAbove(5)),
	}
	reqs := BuildCorrections(corrections)
	sa := reqs[0].UpdateParagraphStyle.Style.SpaceAbove
	if len(sa.ForceSendFields) != 0 {
		t.Errorf("expected no ForceSendFields for non-zero SpaceAbove, got %v", sa.ForceSendFields)
	}
}
