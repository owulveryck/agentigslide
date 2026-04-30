package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- helpers ---

func ptrStr(s string) *string { return &s }

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	return data
}

func assertJSONRoundTrip[T any](t *testing.T, name string, original T) T {
	t.Helper()
	data := mustMarshal(t, original)
	var decoded T
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("%s: Unmarshal failed: %v", name, err)
	}
	// Re-marshal to compare canonical JSON
	data2 := mustMarshal(t, decoded)
	if string(data) != string(data2) {
		t.Errorf("%s: round-trip mismatch\n  original JSON: %s\n  decoded  JSON: %s", name, data, data2)
	}
	return decoded
}

// assertFieldAbsent checks that a given key is NOT present in the JSON output.
func assertFieldAbsent(t *testing.T, data []byte, key string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, ok := m[key]; ok {
		t.Errorf("expected key %q to be absent due to omitempty, but it was present", key)
	}
}

// assertFieldPresent checks that a given key IS present in the JSON output.
func assertFieldPresent(t *testing.T, data []byte, key string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if _, ok := m[key]; !ok {
		t.Errorf("expected key %q to be present, but it was absent", key)
	}
}

// ============================================================
// analysis.go tests
// ============================================================

func TestSlideAnalysis_RoundTrip(t *testing.T) {
	original := SlideAnalysis{
		SlideNumber: 5,
		SlideID:     "g1234abcd",
		Intention:   "Title slide for introduction",
		Description: "A slide with main title and subtitle",
		EditableElements: []EditableElement{
			{
				ObjectID:    "obj_001",
				Type:        "title",
				Placeholder: ptrStr("Enter title here"),
				Content:     "Welcome",
				Description: "Main title text",
				Location:    "center-top",
			},
			{
				ObjectID:    "obj_002",
				Type:        "subtitle",
				Placeholder: nil,
				Content:     "Subtitle line",
				Description: "Subtitle under main title",
				Location:    "center-middle",
			},
		},
		VisualElements: []VisualElement{
			{
				ObjectID:    ptrStr("vis_001"),
				Type:        "logo",
				Description: "Company logo",
				Purpose:     "branding",
				Reusable:    true,
			},
			{
				ObjectID:    nil,
				Type:        "decoration",
				Description: "Background gradient",
			},
		},
	}

	decoded := assertJSONRoundTrip(t, "SlideAnalysis", original)

	if decoded.SlideNumber != 5 {
		t.Errorf("SlideNumber: got %d, want 5", decoded.SlideNumber)
	}
	if decoded.SlideID != "g1234abcd" {
		t.Errorf("SlideID: got %q, want %q", decoded.SlideID, "g1234abcd")
	}
	if len(decoded.EditableElements) != 2 {
		t.Fatalf("EditableElements length: got %d, want 2", len(decoded.EditableElements))
	}
	if decoded.EditableElements[0].ObjectID != "obj_001" {
		t.Errorf("EditableElement[0].ObjectID mismatch")
	}
	if decoded.EditableElements[1].Placeholder != nil {
		t.Errorf("EditableElement[1].Placeholder should be nil")
	}
	if len(decoded.VisualElements) != 2 {
		t.Fatalf("VisualElements length: got %d, want 2", len(decoded.VisualElements))
	}
	if decoded.VisualElements[0].Reusable != true {
		t.Errorf("VisualElement[0].Reusable: got false, want true")
	}
	if decoded.VisualElements[1].ObjectID != nil {
		t.Errorf("VisualElement[1].ObjectID should be nil")
	}
}

func TestEditableElement_PlaceholderNil(t *testing.T) {
	elem := EditableElement{
		ObjectID:    "obj_x",
		Type:        "body",
		Placeholder: nil,
		Content:     "some text",
		Description: "body text",
		Location:    "left",
	}
	data := mustMarshal(t, elem)
	// placeholder is NOT omitempty, so it should be present as null
	assertFieldPresent(t, data, "placeholder")
	if !strings.Contains(string(data), `"placeholder":null`) {
		t.Errorf("expected placeholder to be null in JSON, got: %s", data)
	}
}

func TestVisualElement_Omitempty(t *testing.T) {
	// Zero-value fields with omitempty should be absent
	elem := VisualElement{
		Type:        "icon",
		Description: "An icon",
		// ObjectID nil, Purpose empty, Reusable false
	}
	data := mustMarshal(t, elem)
	assertFieldAbsent(t, data, "objectId")
	assertFieldAbsent(t, data, "purpose")
	assertFieldAbsent(t, data, "reusable")
	assertFieldPresent(t, data, "type")
	assertFieldPresent(t, data, "description")
}

func TestVisualElement_OmitemptyPopulated(t *testing.T) {
	elem := VisualElement{
		ObjectID:    ptrStr("v1"),
		Type:        "image",
		Description: "Photo",
		Purpose:     "illustration",
		Reusable:    true,
	}
	data := mustMarshal(t, elem)
	assertFieldPresent(t, data, "objectId")
	assertFieldPresent(t, data, "purpose")
	assertFieldPresent(t, data, "reusable")
}

func TestVisionResponse_RoundTrip(t *testing.T) {
	original := VisionResponse{
		Intention:   "Cover page",
		Description: "Cover page with company branding",
		EditableElements: []EditableElement{
			{
				ObjectID: "e1", Type: "title", Placeholder: ptrStr("Title"),
				Content: "Hello", Description: "Title", Location: "top",
			},
		},
		VisualElements: []VisualElement{
			{Type: "logo", Description: "Logo", Purpose: "branding", Reusable: true, ObjectID: ptrStr("v1")},
		},
	}
	assertJSONRoundTrip(t, "VisionResponse", original)
}

// ============================================================
// content.go tests
// ============================================================

func TestMagnitude_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		mag  Magnitude
	}{
		{"with unit", Magnitude{Magnitude: 914400.0, Unit: "EMU"}},
		{"zero magnitude", Magnitude{Magnitude: 0, Unit: "PT"}},
		{"no unit (omitempty)", Magnitude{Magnitude: 42.5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertJSONRoundTrip(t, tc.name, tc.mag)
		})
	}
}

func TestMagnitude_UnitOmitempty(t *testing.T) {
	mag := Magnitude{Magnitude: 100.0}
	data := mustMarshal(t, mag)
	assertFieldAbsent(t, data, "unit")

	mag2 := Magnitude{Magnitude: 100.0, Unit: "EMU"}
	data2 := mustMarshal(t, mag2)
	assertFieldPresent(t, data2, "unit")
}

func TestSize_RoundTrip(t *testing.T) {
	s := Size{
		Height: Magnitude{Magnitude: 200, Unit: "EMU"},
		Width:  Magnitude{Magnitude: 400, Unit: "EMU"},
	}
	assertJSONRoundTrip(t, "Size", s)
}

func TestTransform_RoundTrip(t *testing.T) {
	tr := Transform{
		TranslateX: 100.5,
		TranslateY: 200.3,
		ScaleX:     1.0,
		ScaleY:     1.0,
		Unit:       "EMU",
	}
	assertJSONRoundTrip(t, "Transform", tr)
}

func TestTransform_Omitempty(t *testing.T) {
	tr := Transform{
		TranslateX: 50,
		TranslateY: 60,
		// ScaleX, ScaleY, Unit all zero/empty
	}
	data := mustMarshal(t, tr)
	assertFieldAbsent(t, data, "scaleX")
	assertFieldAbsent(t, data, "scaleY")
	assertFieldAbsent(t, data, "unit")
	assertFieldPresent(t, data, "translateX")
	assertFieldPresent(t, data, "translateY")
}

func TestTextRunStyle_RoundTrip(t *testing.T) {
	style := TextRunStyle{
		FontSize: &Magnitude{Magnitude: 14, Unit: "PT"},
	}
	assertJSONRoundTrip(t, "TextRunStyle", style)
}

func TestTextRunStyle_NilFontSize(t *testing.T) {
	style := TextRunStyle{FontSize: nil}
	data := mustMarshal(t, style)
	assertFieldAbsent(t, data, "fontSize")
}

func TestTextRun_RoundTrip(t *testing.T) {
	tr := TextRun{
		Content: "Hello world",
		Style:   &TextRunStyle{FontSize: &Magnitude{Magnitude: 12, Unit: "PT"}},
	}
	decoded := assertJSONRoundTrip(t, "TextRun", tr)
	if decoded.Content != "Hello world" {
		t.Errorf("Content mismatch")
	}
}

func TestTextRun_NilStyle(t *testing.T) {
	tr := TextRun{Content: "No style"}
	data := mustMarshal(t, tr)
	assertFieldAbsent(t, data, "style")
}

func TestTextContent_RoundTrip(t *testing.T) {
	tc := TextContent{
		TextElements: []TextElement{
			{TextRun: &TextRun{Content: "First"}},
			{TextRun: nil},
			{TextRun: &TextRun{Content: "Third", Style: &TextRunStyle{FontSize: &Magnitude{Magnitude: 10, Unit: "PT"}}}},
		},
	}
	assertJSONRoundTrip(t, "TextContent", tc)
}

func TestShape_RoundTrip(t *testing.T) {
	shape := Shape{
		ShapeType: "TEXT_BOX",
		Text: &TextContent{
			TextElements: []TextElement{
				{TextRun: &TextRun{Content: "Shape text"}},
			},
		},
		Placeholder: &Placeholder{Type: "TITLE", Index: 0},
	}
	assertJSONRoundTrip(t, "Shape", shape)
}

func TestShape_Omitempty(t *testing.T) {
	shape := Shape{}
	data := mustMarshal(t, shape)
	assertFieldAbsent(t, data, "shapeType")
	assertFieldAbsent(t, data, "text")
	assertFieldAbsent(t, data, "placeholder")
}

func TestPlaceholder_RoundTrip(t *testing.T) {
	p := Placeholder{Type: "BODY", Index: 2}
	assertJSONRoundTrip(t, "Placeholder", p)
}

func TestPlaceholder_IndexOmitempty(t *testing.T) {
	p := Placeholder{Type: "TITLE"}
	data := mustMarshal(t, p)
	assertFieldAbsent(t, data, "index")
}

func TestImage_RoundTrip(t *testing.T) {
	img := Image{ContentURL: "https://example.com/image.png"}
	assertJSONRoundTrip(t, "Image", img)
}

func TestImage_Omitempty(t *testing.T) {
	img := Image{}
	data := mustMarshal(t, img)
	assertFieldAbsent(t, data, "contentUrl")
}

func TestTable_RoundTrip(t *testing.T) {
	tbl := Table{
		Rows:    2,
		Columns: 3,
		TableRows: []TableRow{
			{
				TableCells: []TableCell{
					{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "Cell 1"}}}}},
					{Text: nil},
					{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "Cell 3"}}}}},
				},
			},
			{
				TableCells: []TableCell{
					{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "Cell 4"}}}}},
				},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "Table", tbl)
	if decoded.Rows != 2 || decoded.Columns != 3 {
		t.Errorf("Table dimensions mismatch")
	}
	if len(decoded.TableRows) != 2 {
		t.Fatalf("TableRows length: got %d, want 2", len(decoded.TableRows))
	}
	if decoded.TableRows[0].TableCells[1].Text != nil {
		t.Errorf("expected nil Text in cell [0][1]")
	}
}

func TestTable_NoRows_Omitempty(t *testing.T) {
	tbl := Table{Rows: 1, Columns: 1}
	data := mustMarshal(t, tbl)
	assertFieldAbsent(t, data, "tableRows")
}

func TestElementGroup_RoundTrip(t *testing.T) {
	eg := ElementGroup{
		Children: []PageElement{
			{
				ObjectID: "child_1",
				Shape: &Shape{
					ShapeType: "RECTANGLE",
					Text:      &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "Inside group"}}}},
				},
			},
			{
				ObjectID: "child_2",
				Image:    &Image{ContentURL: "https://example.com/nested.png"},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "ElementGroup", eg)
	if len(decoded.Children) != 2 {
		t.Fatalf("Children length: got %d, want 2", len(decoded.Children))
	}
}

func TestPageElement_RoundTrip(t *testing.T) {
	pe := PageElement{
		ObjectID: "pe_001",
		Shape: &Shape{
			ShapeType: "TEXT_BOX",
			Text: &TextContent{
				TextElements: []TextElement{{TextRun: &TextRun{Content: "hello"}}},
			},
		},
		Size: &Size{
			Height: Magnitude{Magnitude: 914400, Unit: "EMU"},
			Width:  Magnitude{Magnitude: 1828800, Unit: "EMU"},
		},
		Transform: &Transform{
			TranslateX: 100,
			TranslateY: 200,
			ScaleX:     1,
			ScaleY:     1,
			Unit:       "EMU",
		},
	}
	assertJSONRoundTrip(t, "PageElement", pe)
}

func TestPageElement_AllNilOptionals(t *testing.T) {
	pe := PageElement{ObjectID: "pe_empty"}
	data := mustMarshal(t, pe)
	assertFieldAbsent(t, data, "shape")
	assertFieldAbsent(t, data, "image")
	assertFieldAbsent(t, data, "table")
	assertFieldAbsent(t, data, "elementGroup")
	assertFieldAbsent(t, data, "size")
	assertFieldAbsent(t, data, "transform")
	assertFieldPresent(t, data, "objectId")
}

func TestPageElement_WithImage(t *testing.T) {
	pe := PageElement{
		ObjectID: "img_pe",
		Image:    &Image{ContentURL: "https://example.com/pic.jpg"},
	}
	decoded := assertJSONRoundTrip(t, "PageElement-Image", pe)
	if decoded.Image == nil {
		t.Fatal("Image should not be nil")
	}
	if decoded.Image.ContentURL != "https://example.com/pic.jpg" {
		t.Errorf("ContentURL mismatch")
	}
	if decoded.Shape != nil {
		t.Errorf("Shape should be nil")
	}
}

func TestPageElement_WithTable(t *testing.T) {
	pe := PageElement{
		ObjectID: "tbl_pe",
		Table:    &Table{Rows: 3, Columns: 2},
	}
	decoded := assertJSONRoundTrip(t, "PageElement-Table", pe)
	if decoded.Table == nil {
		t.Fatal("Table should not be nil")
	}
	if decoded.Table.Rows != 3 || decoded.Table.Columns != 2 {
		t.Errorf("Table dimensions mismatch")
	}
}

func TestPageElement_WithElementGroup(t *testing.T) {
	pe := PageElement{
		ObjectID: "grp_pe",
		ElementGroup: &ElementGroup{
			Children: []PageElement{
				{ObjectID: "c1"},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "PageElement-ElementGroup", pe)
	if decoded.ElementGroup == nil {
		t.Fatal("ElementGroup should not be nil")
	}
	if len(decoded.ElementGroup.Children) != 1 {
		t.Fatalf("Children length: got %d, want 1", len(decoded.ElementGroup.Children))
	}
}

func TestSlideContent_RoundTrip(t *testing.T) {
	sc := SlideContent{
		ObjectID: "slide_01",
		PageElements: []PageElement{
			{
				ObjectID: "pe1",
				Shape: &Shape{
					ShapeType: "TEXT_BOX",
					Text: &TextContent{
						TextElements: []TextElement{
							{TextRun: &TextRun{Content: "Title text", Style: &TextRunStyle{FontSize: &Magnitude{Magnitude: 24, Unit: "PT"}}}},
						},
					},
					Placeholder: &Placeholder{Type: "TITLE", Index: 0},
				},
				Size: &Size{
					Height: Magnitude{Magnitude: 500000, Unit: "EMU"},
					Width:  Magnitude{Magnitude: 8000000, Unit: "EMU"},
				},
				Transform: &Transform{TranslateX: 100000, TranslateY: 50000, ScaleX: 1, ScaleY: 1, Unit: "EMU"},
			},
			{
				ObjectID: "pe2",
				Image:    &Image{ContentURL: "https://cdn.example.com/logo.png"},
			},
			{
				ObjectID: "pe3",
				Table: &Table{
					Rows:    2,
					Columns: 2,
					TableRows: []TableRow{
						{TableCells: []TableCell{
							{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "A"}}}}},
							{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "B"}}}}},
						}},
						{TableCells: []TableCell{
							{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "C"}}}}},
							{Text: &TextContent{TextElements: []TextElement{{TextRun: &TextRun{Content: "D"}}}}},
						}},
					},
				},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "SlideContent", sc)
	if decoded.ObjectID != "slide_01" {
		t.Errorf("ObjectID mismatch")
	}
	if len(decoded.PageElements) != 3 {
		t.Fatalf("PageElements length: got %d, want 3", len(decoded.PageElements))
	}
}

func TestSlideContent_EmptyPageElements(t *testing.T) {
	sc := SlideContent{ObjectID: "empty_slide", PageElements: nil}
	data := mustMarshal(t, sc)
	// PageElements is not omitempty, so null is fine
	var decoded SlideContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.ObjectID != "empty_slide" {
		t.Errorf("ObjectID mismatch")
	}
}

// ============================================================
// plan.go tests
// ============================================================

func TestCellLocation_RoundTrip(t *testing.T) {
	cl := CellLocation{RowIndex: 2, ColumnIndex: 5}
	decoded := assertJSONRoundTrip(t, "CellLocation", cl)
	if decoded.RowIndex != 2 || decoded.ColumnIndex != 5 {
		t.Errorf("CellLocation mismatch: got (%d,%d), want (2,5)", decoded.RowIndex, decoded.ColumnIndex)
	}
}

func TestEditableObject_RoundTrip(t *testing.T) {
	eo := EditableObject{
		ObjectID:     "eo_001",
		VariableName: "titleMainShape",
		Role:         "title",
		ElementType:  "text",
		Placeholder:  ptrStr("Enter your title"),
		Description:  "Main title element",
		Location:     "top-center",
		CurrentValue: "Default Title",
		NewValue:     ptrStr("My Awesome Title"),
		Modified:     true,
		CellLocation: &CellLocation{RowIndex: 0, ColumnIndex: 1},
	}
	decoded := assertJSONRoundTrip(t, "EditableObject", eo)
	if *decoded.Placeholder != "Enter your title" {
		t.Errorf("Placeholder mismatch")
	}
	if *decoded.NewValue != "My Awesome Title" {
		t.Errorf("NewValue mismatch")
	}
	if decoded.CellLocation == nil || decoded.CellLocation.RowIndex != 0 {
		t.Errorf("CellLocation mismatch")
	}
}

func TestEditableObject_Omitempty(t *testing.T) {
	eo := EditableObject{
		ObjectID:     "eo_002",
		VariableName: "bodyShape",
		Role:         "body",
		ElementType:  "text",
		Placeholder:  nil,
		Description:  "Body text",
		Location:     "center",
		CurrentValue: "Old text",
		NewValue:     nil,
		Modified:     false,
		CellLocation: nil,
	}
	data := mustMarshal(t, eo)
	assertFieldAbsent(t, data, "newValue")
	assertFieldAbsent(t, data, "cellLocation")
	// placeholder is NOT omitempty, so should be present as null
	assertFieldPresent(t, data, "placeholder")
}

func TestVisualObject_RoundTrip(t *testing.T) {
	vo := VisualObject{
		ObjectID:    ptrStr("vo_001"),
		Type:        "icon",
		Description: "Star icon",
		Purpose:     "decoration",
		Reusable:    true,
	}
	assertJSONRoundTrip(t, "VisualObject", vo)
}

func TestVisualObject_NilObjectID(t *testing.T) {
	vo := VisualObject{
		ObjectID:    nil,
		Type:        "background",
		Description: "Gradient",
		Purpose:     "visual flair",
		Reusable:    false,
	}
	data := mustMarshal(t, vo)
	assertFieldAbsent(t, data, "objectId")
}

func TestSlideSpec_RoundTrip(t *testing.T) {
	ss := SlideSpec{
		Position:          1,
		SourceSlideNumber: 5,
		SourceSlideID:     "g_abc123",
		Intention:         "Introduction",
		Description:       "Opening slide",
		PreviewImage:      "template/1MycsjRBQ67mWJ0SxlAgY4A_J04RluDsH8kgsCpixVwI/5/slide.png",
		EditableObjects: []EditableObject{
			{
				ObjectID:     "eo1",
				VariableName: "titleShape",
				Role:         "title",
				ElementType:  "text",
				Placeholder:  ptrStr("Title"),
				Description:  "Title",
				Location:     "top",
				CurrentValue: "Default",
				NewValue:     ptrStr("Hello World"),
				Modified:     true,
			},
		},
		VisualObjects: []VisualObject{
			{ObjectID: ptrStr("vo1"), Type: "logo", Description: "Logo", Purpose: "branding", Reusable: true},
		},
	}
	decoded := assertJSONRoundTrip(t, "SlideSpec", ss)
	if decoded.Position != 1 {
		t.Errorf("Position mismatch")
	}
	if len(decoded.EditableObjects) != 1 {
		t.Fatalf("EditableObjects length mismatch")
	}
	if len(decoded.VisualObjects) != 1 {
		t.Fatalf("VisualObjects length mismatch")
	}
}

func TestSlideSpec_VisualObjectsOmitempty(t *testing.T) {
	ss := SlideSpec{
		Position:          1,
		SourceSlideNumber: 2,
		SourceSlideID:     "g_xyz",
		Intention:         "Content",
		Description:       "A content slide",
		PreviewImage:      "image.png",
		EditableObjects:   []EditableObject{},
		VisualObjects:     nil,
	}
	data := mustMarshal(t, ss)
	assertFieldAbsent(t, data, "visualObjects")
}

func TestPresentationPlan_RoundTrip(t *testing.T) {
	pp := PresentationPlan{
		PresentationTitle: "Q4 Strategy Review",
		TemplateID:        "1MycsjRBQ67mWJ0SxlAgY4A_J04RluDsH8kgsCpixVwI",
		GeneratedAt:       "2026-04-30T10:00:00Z",
		SourceRequest:     "Create a strategy deck for Q4",
		Slides: []SlideSpec{
			{
				Position:          1,
				SourceSlideNumber: 1,
				SourceSlideID:     "g_slide1",
				Intention:         "Title",
				Description:       "Title slide",
				PreviewImage:      "slide1.png",
				EditableObjects: []EditableObject{
					{
						ObjectID: "eo1", VariableName: "titleShape", Role: "title",
						ElementType: "text", Placeholder: ptrStr("Title"),
						Description: "Title", Location: "top",
						CurrentValue: "Default", NewValue: ptrStr("Q4 Strategy"),
						Modified: true,
					},
				},
			},
			{
				Position:          2,
				SourceSlideNumber: 10,
				SourceSlideID:     "g_slide10",
				Intention:         "Agenda",
				Description:       "Agenda items",
				PreviewImage:      "slide10.png",
				EditableObjects:   []EditableObject{},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "PresentationPlan", pp)
	if decoded.PresentationTitle != "Q4 Strategy Review" {
		t.Errorf("PresentationTitle mismatch")
	}
	if len(decoded.Slides) != 2 {
		t.Fatalf("Slides length: got %d, want 2", len(decoded.Slides))
	}
}

func TestTextModification_RoundTrip(t *testing.T) {
	tm := TextModification{
		VariableName: "titleMainShape",
		NewText:      "Updated Title",
	}
	decoded := assertJSONRoundTrip(t, "TextModification", tm)
	if decoded.VariableName != "titleMainShape" || decoded.NewText != "Updated Title" {
		t.Errorf("TextModification mismatch")
	}
}

func TestSlideRequest_RoundTrip(t *testing.T) {
	sr := SlideRequest{
		SourceSlide: 3,
		Modifications: []TextModification{
			{VariableName: "titleShape", NewText: "Slide Title"},
			{VariableName: "bodyShape", NewText: "Body content with **bold** text"},
		},
	}
	decoded := assertJSONRoundTrip(t, "SlideRequest", sr)
	if decoded.SourceSlide != 3 {
		t.Errorf("SourceSlide: got %d, want 3", decoded.SourceSlide)
	}
	if len(decoded.Modifications) != 2 {
		t.Fatalf("Modifications length: got %d, want 2", len(decoded.Modifications))
	}
}

func TestGenerationPlan_RoundTrip(t *testing.T) {
	gp := GenerationPlan{
		PresentationTitle: "Innovation Deck",
		Slides: []SlideRequest{
			{SourceSlide: 1, Modifications: []TextModification{{VariableName: "title", NewText: "Innovation"}}},
			{SourceSlide: 5, Modifications: []TextModification{{VariableName: "body", NewText: "Key points"}}},
		},
	}
	decoded := assertJSONRoundTrip(t, "GenerationPlan", gp)
	if decoded.PresentationTitle != "Innovation Deck" {
		t.Errorf("PresentationTitle mismatch")
	}
	if len(decoded.Slides) != 2 {
		t.Fatalf("Slides length: got %d, want 2", len(decoded.Slides))
	}
}

// ============================================================
// template.go tests
// ============================================================

func TestEditableFieldSummary_RoundTrip(t *testing.T) {
	efs := EditableFieldSummary{
		ObjectID:     "efs_001",
		Role:         "title",
		Placeholder:  ptrStr("Enter title"),
		Content:      "Default Title",
		RawContent:   "Default Title\n",
		VariableName: "titleMainShape",
		CellLocation: &CellLocation{RowIndex: 1, ColumnIndex: 2},
		WidthPt:      300.5,
		HeightPt:     50.0,
		MaxChars:     120,
	}
	decoded := assertJSONRoundTrip(t, "EditableFieldSummary", efs)
	if *decoded.Placeholder != "Enter title" {
		t.Errorf("Placeholder mismatch")
	}
	if decoded.CellLocation == nil {
		t.Fatal("CellLocation should not be nil")
	}
	if decoded.CellLocation.RowIndex != 1 || decoded.CellLocation.ColumnIndex != 2 {
		t.Errorf("CellLocation mismatch")
	}
}

func TestEditableFieldSummary_Omitempty(t *testing.T) {
	efs := EditableFieldSummary{
		ObjectID:     "efs_002",
		Role:         "body",
		Placeholder:  nil,
		VariableName: "bodyShape",
		// Content, RawContent, CellLocation, WidthPt, HeightPt, MaxChars all zero
	}
	data := mustMarshal(t, efs)
	assertFieldAbsent(t, data, "content")
	assertFieldAbsent(t, data, "rawContent")
	assertFieldAbsent(t, data, "cellLocation")
	assertFieldAbsent(t, data, "widthPt")
	assertFieldAbsent(t, data, "heightPt")
	assertFieldAbsent(t, data, "maxChars")
	// placeholder is NOT omitempty
	assertFieldPresent(t, data, "placeholder")
}

func TestVisualElementSummary_RoundTrip(t *testing.T) {
	ves := VisualElementSummary{
		ObjectID: ptrStr("ves_001"),
		Type:     "icon",
		Purpose:  "navigation",
	}
	assertJSONRoundTrip(t, "VisualElementSummary", ves)
}

func TestVisualElementSummary_Omitempty(t *testing.T) {
	ves := VisualElementSummary{
		Type: "decoration",
	}
	data := mustMarshal(t, ves)
	assertFieldAbsent(t, data, "objectId")
	assertFieldAbsent(t, data, "purpose")
	assertFieldPresent(t, data, "type")
}

func TestTemplateSlide_RoundTrip(t *testing.T) {
	ts := TemplateSlide{
		SlideNumber: 3,
		SlideID:     "g_slide3",
		Intention:   "Content slide with bullet points",
		Keywords:    []string{"content", "bullets", "list"},
		EditableFields: []EditableFieldSummary{
			{
				ObjectID: "ef1", Role: "title", Placeholder: ptrStr("Title"),
				VariableName: "titleShape", WidthPt: 600, HeightPt: 40, MaxChars: 80,
			},
			{
				ObjectID: "ef2", Role: "body", Placeholder: nil,
				Content: "Bullet 1\nBullet 2", RawContent: "Bullet 1\nBullet 2\n",
				VariableName: "bodyShape", WidthPt: 500, HeightPt: 300, MaxChars: 500,
			},
		},
		VisualElements: []VisualElementSummary{
			{ObjectID: ptrStr("ve1"), Type: "icon", Purpose: "decoration"},
		},
	}
	decoded := assertJSONRoundTrip(t, "TemplateSlide", ts)
	if decoded.SlideNumber != 3 {
		t.Errorf("SlideNumber mismatch")
	}
	if len(decoded.Keywords) != 3 {
		t.Fatalf("Keywords length: got %d, want 3", len(decoded.Keywords))
	}
	if len(decoded.EditableFields) != 2 {
		t.Fatalf("EditableFields length mismatch")
	}
	if len(decoded.VisualElements) != 1 {
		t.Fatalf("VisualElements length mismatch")
	}
}

func TestTemplateSlide_VisualElementsOmitempty(t *testing.T) {
	ts := TemplateSlide{
		SlideNumber:    1,
		SlideID:        "g_s1",
		Intention:      "Simple slide",
		Keywords:       []string{"simple"},
		EditableFields: []EditableFieldSummary{},
		VisualElements: nil,
	}
	data := mustMarshal(t, ts)
	assertFieldAbsent(t, data, "visualElements")
}

func TestTemplateIndex_RoundTrip(t *testing.T) {
	ti := TemplateIndex{
		TemplateID: "1MycsjRBQ67mWJ0SxlAgY4A_J04RluDsH8kgsCpixVwI",
		Slides: []TemplateSlide{
			{
				SlideNumber: 1,
				SlideID:     "g_s1",
				Intention:   "Title slide",
				Keywords:    []string{"title", "intro"},
				EditableFields: []EditableFieldSummary{
					{ObjectID: "e1", Role: "title", Placeholder: ptrStr("Title"), VariableName: "titleShape"},
				},
				VisualElements: []VisualElementSummary{
					{Type: "logo", Purpose: "branding", ObjectID: ptrStr("v1")},
				},
			},
			{
				SlideNumber:    2,
				SlideID:        "g_s2",
				Intention:      "Agenda slide",
				Keywords:       []string{"agenda", "overview"},
				EditableFields: []EditableFieldSummary{},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "TemplateIndex", ti)
	if decoded.TemplateID != "1MycsjRBQ67mWJ0SxlAgY4A_J04RluDsH8kgsCpixVwI" {
		t.Errorf("TemplateID mismatch")
	}
	if len(decoded.Slides) != 2 {
		t.Fatalf("Slides length: got %d, want 2", len(decoded.Slides))
	}
}

func TestTemplateIndex_Empty(t *testing.T) {
	ti := TemplateIndex{
		TemplateID: "empty_template",
		Slides:     nil,
	}
	data := mustMarshal(t, ti)
	var decoded TemplateIndex
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.TemplateID != "empty_template" {
		t.Errorf("TemplateID mismatch")
	}
}

// ============================================================
// slideref.go tests
// ============================================================

func TestSlideRef_StructCreation(t *testing.T) {
	// SlideRef has no JSON tags; test struct creation and field access
	sr := SlideRef{
		PageObjectID: "page_001",
		ElementMap: map[string]string{
			"original_id_1": "d0_original_id_1",
			"original_id_2": "d0_original_id_2",
			"original_id_3": "d0_original_id_3",
		},
	}
	if sr.PageObjectID != "page_001" {
		t.Errorf("PageObjectID: got %q, want %q", sr.PageObjectID, "page_001")
	}
	if len(sr.ElementMap) != 3 {
		t.Fatalf("ElementMap length: got %d, want 3", len(sr.ElementMap))
	}
	if sr.ElementMap["original_id_1"] != "d0_original_id_1" {
		t.Errorf("ElementMap lookup mismatch")
	}
	// Verify missing key returns zero value
	if v, ok := sr.ElementMap["nonexistent"]; ok {
		t.Errorf("Expected missing key, got %q", v)
	}
}

func TestSlideRef_EmptyElementMap(t *testing.T) {
	sr := SlideRef{
		PageObjectID: "page_empty",
		ElementMap:   map[string]string{},
	}
	if sr.PageObjectID != "page_empty" {
		t.Errorf("PageObjectID mismatch")
	}
	if len(sr.ElementMap) != 0 {
		t.Errorf("ElementMap should be empty")
	}
}

func TestSlideRef_NilElementMap(t *testing.T) {
	sr := SlideRef{
		PageObjectID: "page_nil",
		ElementMap:   nil,
	}
	if sr.ElementMap != nil {
		t.Errorf("ElementMap should be nil")
	}
}

// ============================================================
// Table-driven tests for JSON unmarshal from raw strings
// ============================================================

func TestUnmarshalFromJSON_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		target   interface{}
		validate func(t *testing.T, v interface{})
	}{
		{
			name:    "EditableElement with null placeholder",
			jsonStr: `{"objectId":"o1","type":"text","placeholder":null,"content":"hi","description":"d","location":"l"}`,
			target:  &EditableElement{},
			validate: func(t *testing.T, v interface{}) {
				ee := v.(*EditableElement)
				if ee.Placeholder != nil {
					t.Errorf("expected nil placeholder")
				}
				if ee.ObjectID != "o1" {
					t.Errorf("ObjectID mismatch")
				}
			},
		},
		{
			name:    "VisualElement minimal (only required fields)",
			jsonStr: `{"type":"bg","description":"gradient"}`,
			target:  &VisualElement{},
			validate: func(t *testing.T, v interface{}) {
				ve := v.(*VisualElement)
				if ve.ObjectID != nil {
					t.Errorf("expected nil ObjectID")
				}
				if ve.Purpose != "" {
					t.Errorf("expected empty Purpose")
				}
				if ve.Reusable != false {
					t.Errorf("expected false Reusable")
				}
			},
		},
		{
			name:    "Transform with only required fields",
			jsonStr: `{"translateX":10,"translateY":20}`,
			target:  &Transform{},
			validate: func(t *testing.T, v interface{}) {
				tr := v.(*Transform)
				if tr.TranslateX != 10 || tr.TranslateY != 20 {
					t.Errorf("translate mismatch")
				}
				if tr.ScaleX != 0 || tr.ScaleY != 0 {
					t.Errorf("expected zero scale")
				}
				if tr.Unit != "" {
					t.Errorf("expected empty unit")
				}
			},
		},
		{
			name:    "PageElement with only objectId",
			jsonStr: `{"objectId":"minimal"}`,
			target:  &PageElement{},
			validate: func(t *testing.T, v interface{}) {
				pe := v.(*PageElement)
				if pe.ObjectID != "minimal" {
					t.Errorf("ObjectID mismatch")
				}
				if pe.Shape != nil || pe.Image != nil || pe.Table != nil || pe.ElementGroup != nil {
					t.Errorf("all optional pointers should be nil")
				}
			},
		},
		{
			name:    "SlideSpec with empty editableObjects",
			jsonStr: `{"position":1,"sourceSlideNumber":2,"sourceSlideId":"s2","intention":"i","description":"d","previewImage":"p","editableObjects":[]}`,
			target:  &SlideSpec{},
			validate: func(t *testing.T, v interface{}) {
				ss := v.(*SlideSpec)
				if ss.Position != 1 {
					t.Errorf("Position mismatch")
				}
				if len(ss.EditableObjects) != 0 {
					t.Errorf("expected empty EditableObjects")
				}
				if ss.VisualObjects != nil {
					t.Errorf("expected nil VisualObjects")
				}
			},
		},
		{
			name:    "GenerationPlan with no slides",
			jsonStr: `{"presentationTitle":"Empty","slides":[]}`,
			target:  &GenerationPlan{},
			validate: func(t *testing.T, v interface{}) {
				gp := v.(*GenerationPlan)
				if gp.PresentationTitle != "Empty" {
					t.Errorf("PresentationTitle mismatch")
				}
				if len(gp.Slides) != 0 {
					t.Errorf("expected empty Slides")
				}
			},
		},
		{
			name:    "EditableObject with cellLocation",
			jsonStr: `{"objectId":"x","variableName":"v","role":"r","elementType":"table_cell","placeholder":null,"description":"d","location":"l","currentValue":"c","modified":false,"cellLocation":{"rowIndex":3,"columnIndex":4}}`,
			target:  &EditableObject{},
			validate: func(t *testing.T, v interface{}) {
				eo := v.(*EditableObject)
				if eo.CellLocation == nil {
					t.Fatal("CellLocation should not be nil")
				}
				if eo.CellLocation.RowIndex != 3 || eo.CellLocation.ColumnIndex != 4 {
					t.Errorf("CellLocation mismatch: got (%d,%d)", eo.CellLocation.RowIndex, eo.CellLocation.ColumnIndex)
				}
				if eo.NewValue != nil {
					t.Errorf("NewValue should be nil")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.jsonStr), tc.target); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			tc.validate(t, tc.target)
		})
	}
}

// ============================================================
// Deep nesting test: SlideContent with ElementGroup hierarchy
// ============================================================

func TestSlideContent_DeeplyNested(t *testing.T) {
	sc := SlideContent{
		ObjectID: "deep_slide",
		PageElements: []PageElement{
			{
				ObjectID: "group_outer",
				ElementGroup: &ElementGroup{
					Children: []PageElement{
						{
							ObjectID: "group_inner",
							ElementGroup: &ElementGroup{
								Children: []PageElement{
									{
										ObjectID: "leaf_shape",
										Shape: &Shape{
											ShapeType: "RECTANGLE",
											Text: &TextContent{
												TextElements: []TextElement{
													{TextRun: &TextRun{
														Content: "Deep text",
														Style:   &TextRunStyle{FontSize: &Magnitude{Magnitude: 8, Unit: "PT"}},
													}},
												},
											},
										},
										Size: &Size{
											Height: Magnitude{Magnitude: 100, Unit: "EMU"},
											Width:  Magnitude{Magnitude: 200, Unit: "EMU"},
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
	decoded := assertJSONRoundTrip(t, "SlideContent-DeeplyNested", sc)

	// Navigate the nested structure
	outer := decoded.PageElements[0].ElementGroup
	if outer == nil {
		t.Fatal("outer ElementGroup is nil")
	}
	inner := outer.Children[0].ElementGroup
	if inner == nil {
		t.Fatal("inner ElementGroup is nil")
	}
	leaf := inner.Children[0]
	if leaf.Shape == nil {
		t.Fatal("leaf Shape is nil")
	}
	if leaf.Shape.Text == nil {
		t.Fatal("leaf Shape.Text is nil")
	}
	if len(leaf.Shape.Text.TextElements) != 1 {
		t.Fatalf("TextElements length: got %d, want 1", len(leaf.Shape.Text.TextElements))
	}
	if leaf.Shape.Text.TextElements[0].TextRun.Content != "Deep text" {
		t.Errorf("Deep text content mismatch")
	}
}

// ============================================================
// Unicode and special characters
// ============================================================

func TestUnicodeContent_RoundTrip(t *testing.T) {
	sa := SlideAnalysis{
		SlideNumber: 1,
		SlideID:     "unicode_slide",
		Intention:   "Slide avec des caracteres speciaux",
		Description: "Contains emoji and special chars",
		EditableElements: []EditableElement{
			{
				ObjectID:    "u1",
				Type:        "text",
				Placeholder: ptrStr("Entrez le texte ici"),
				Content:     "Bonjour le monde! Les donnees sont la.",
				Description: "French text with accents",
				Location:    "center",
			},
		},
		VisualElements: []VisualElement{},
	}
	decoded := assertJSONRoundTrip(t, "UnicodeContent", sa)
	if decoded.EditableElements[0].Content != "Bonjour le monde! Les donnees sont la." {
		t.Errorf("Unicode content mismatch")
	}
}

// ============================================================
// Comprehensive full pipeline test
// ============================================================

func TestFullPipeline_TemplateToPresentation(t *testing.T) {
	// Simulate the full data flow: TemplateIndex -> GenerationPlan -> PresentationPlan

	// Step 1: Template index
	templateIdx := TemplateIndex{
		TemplateID: "tmpl_001",
		Slides: []TemplateSlide{
			{
				SlideNumber: 1,
				SlideID:     "s1",
				Intention:   "Title page",
				Keywords:    []string{"title", "cover"},
				EditableFields: []EditableFieldSummary{
					{ObjectID: "e1", Role: "title", Placeholder: ptrStr("Title"), VariableName: "titleShape", WidthPt: 600, HeightPt: 50, MaxChars: 100},
					{ObjectID: "e2", Role: "subtitle", Placeholder: ptrStr("Subtitle"), VariableName: "subtitleShape", WidthPt: 500, HeightPt: 30, MaxChars: 80},
				},
			},
		},
	}
	assertJSONRoundTrip(t, "Pipeline-TemplateIndex", templateIdx)

	// Step 2: Generation plan (Claude's output)
	genPlan := GenerationPlan{
		PresentationTitle: "Test Presentation",
		Slides: []SlideRequest{
			{
				SourceSlide: 1,
				Modifications: []TextModification{
					{VariableName: "titleShape", NewText: "My Presentation"},
					{VariableName: "subtitleShape", NewText: "A great subtitle"},
				},
			},
		},
	}
	assertJSONRoundTrip(t, "Pipeline-GenerationPlan", genPlan)

	// Step 3: Presentation plan (final plan)
	presPlan := PresentationPlan{
		PresentationTitle: "Test Presentation",
		TemplateID:        "tmpl_001",
		GeneratedAt:       "2026-04-30T12:00:00Z",
		SourceRequest:     "Create a test presentation",
		Slides: []SlideSpec{
			{
				Position:          1,
				SourceSlideNumber: 1,
				SourceSlideID:     "s1",
				Intention:         "Title page",
				Description:       "Opening title",
				PreviewImage:      "slide1.png",
				EditableObjects: []EditableObject{
					{
						ObjectID: "e1", VariableName: "titleShape", Role: "title",
						ElementType: "text", Placeholder: ptrStr("Title"),
						Description: "Title field", Location: "top",
						CurrentValue: "Title", NewValue: ptrStr("My Presentation"),
						Modified: true,
					},
					{
						ObjectID: "e2", VariableName: "subtitleShape", Role: "subtitle",
						ElementType: "text", Placeholder: ptrStr("Subtitle"),
						Description: "Subtitle field", Location: "center",
						CurrentValue: "Subtitle", NewValue: ptrStr("A great subtitle"),
						Modified: true,
					},
				},
			},
		},
	}
	decoded := assertJSONRoundTrip(t, "Pipeline-PresentationPlan", presPlan)
	if len(decoded.Slides) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(decoded.Slides))
	}
	if len(decoded.Slides[0].EditableObjects) != 2 {
		t.Fatalf("expected 2 editable objects, got %d", len(decoded.Slides[0].EditableObjects))
	}
}
