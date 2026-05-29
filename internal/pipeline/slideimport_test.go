package pipeline

import (
	"testing"

	"google.golang.org/api/slides/v1"
)

func TestJoinFields(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a,b,c"},
	}
	for _, tt := range tests {
		got := joinFields(tt.input)
		if got != tt.want {
			t.Errorf("joinFields(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildTextStyleFields(t *testing.T) {
	style := &slides.TextStyle{
		FontFamily: "Arial",
		FontSize:   &slides.Dimension{Magnitude: 12, Unit: "PT"},
		ForegroundColor: &slides.OptionalColor{
			OpaqueColor: &slides.OpaqueColor{
				RgbColor: &slides.RgbColor{Red: 1},
			},
		},
		Bold:   true,
		Italic: true,
	}
	fields := buildTextStyleFields(style)
	expected := map[string]bool{
		"fontFamily":      true,
		"fontSize":        true,
		"foregroundColor": true,
		"bold":            true,
		"italic":          true,
		"underline":       true,
	}
	for _, f := range fields {
		if !expected[f] {
			t.Errorf("unexpected field %q", f)
		}
		delete(expected, f)
	}
	for f := range expected {
		t.Errorf("missing expected field %q", f)
	}
}

func TestBuildTextStyleFields_Minimal(t *testing.T) {
	style := &slides.TextStyle{}
	fields := buildTextStyleFields(style)
	if len(fields) != 3 {
		t.Errorf("expected 3 fields (bold, italic, underline), got %d: %v", len(fields), fields)
	}
}

func TestBuildParagraphStyleFields(t *testing.T) {
	style := &slides.ParagraphStyle{
		Alignment:   "CENTER",
		LineSpacing: 150,
		SpaceAbove:  &slides.Dimension{Magnitude: 10, Unit: "PT"},
	}
	fields := buildParagraphStyleFields(style)
	expected := map[string]bool{"alignment": true, "lineSpacing": true, "spaceAbove": true}
	for _, f := range fields {
		if !expected[f] {
			t.Errorf("unexpected field %q", f)
		}
		delete(expected, f)
	}
	for f := range expected {
		t.Errorf("missing expected field %q", f)
	}
}

func TestBuildParagraphStyleFields_Empty(t *testing.T) {
	style := &slides.ParagraphStyle{}
	fields := buildParagraphStyleFields(style)
	if len(fields) != 0 {
		t.Errorf("expected 0 fields for empty style, got %d: %v", len(fields), fields)
	}
}

func TestBuildLinePropertiesFields(t *testing.T) {
	props := &slides.LineProperties{
		Weight:    &slides.Dimension{Magnitude: 2, Unit: "PT"},
		DashStyle: "DASH",
		EndArrow:  "OPEN_ARROW",
	}
	fields := buildLinePropertiesFields(props)
	expected := map[string]bool{"weight": true, "dashStyle": true, "endArrow": true}
	for _, f := range fields {
		if !expected[f] {
			t.Errorf("unexpected field %q", f)
		}
		delete(expected, f)
	}
	for f := range expected {
		t.Errorf("missing expected field %q", f)
	}
}

func TestPrepareSlideImport(t *testing.T) {
	page := &slides.Page{
		ObjectId: "source_page",
		PageElements: []*slides.PageElement{
			{
				ObjectId: "shape1",
				Size:     &slides.Size{Width: &slides.Dimension{Magnitude: 100}},
				Shape: &slides.Shape{
					ShapeType: "TEXT_BOX",
				},
			},
		},
	}

	plan := prepareSlideImport(page, "source_slide_1", 5)

	if plan.sourceSlideID != "source_slide_1" {
		t.Errorf("sourceSlideID = %q, want %q", plan.sourceSlideID, "source_slide_1")
	}
	if plan.insertionIndex != 5 {
		t.Errorf("insertionIndex = %d, want 5", plan.insertionIndex)
	}
	if plan.newPageID == "" {
		t.Error("newPageID should not be empty")
	}
	if len(plan.createSlideReqs) != 1 {
		t.Errorf("createSlideReqs = %d, want 1", len(plan.createSlideReqs))
	}
	if plan.createSlideReqs[0].CreateSlide.InsertionIndex != 5 {
		t.Errorf("createSlide insertionIndex = %d, want 5", plan.createSlideReqs[0].CreateSlide.InsertionIndex)
	}
	if len(plan.elementReqs) == 0 {
		t.Error("expected element requests for the shape")
	}
	if len(plan.elementMap) == 0 {
		t.Error("expected element map entries")
	}
}
