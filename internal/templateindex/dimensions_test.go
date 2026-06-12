package templateindex

import (
	"math"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestComputeElementSize_NilElement(t *testing.T) {
	w, h := ComputeElementSize(nil)
	if w != 0 || h != 0 {
		t.Errorf("expected (0, 0) for nil element, got (%f, %f)", w, h)
	}
}

func TestComputeElementSize_NilSize(t *testing.T) {
	el := &model.PageElement{}
	w, h := ComputeElementSize(el)
	if w != 0 || h != 0 {
		t.Errorf("expected (0, 0) for nil size, got (%f, %f)", w, h)
	}
}

func TestComputeElementSize_NoTransform(t *testing.T) {
	el := &model.PageElement{
		Size: &model.Size{
			Width:  model.Magnitude{Magnitude: 127000},
			Height: model.Magnitude{Magnitude: 254000},
		},
	}
	w, h := ComputeElementSize(el)
	if math.Abs(w-10.0) > 0.01 {
		t.Errorf("width = %f, want 10.0", w)
	}
	if math.Abs(h-20.0) > 0.01 {
		t.Errorf("height = %f, want 20.0", h)
	}
}

func TestComputeElementSize_WithScale(t *testing.T) {
	el := &model.PageElement{
		Size: &model.Size{
			Width:  model.Magnitude{Magnitude: 127000},
			Height: model.Magnitude{Magnitude: 254000},
		},
		Transform: &model.Transform{
			ScaleX: 2.0,
			ScaleY: 0.5,
		},
	}
	w, h := ComputeElementSize(el)
	if math.Abs(w-20.0) > 0.01 {
		t.Errorf("width = %f, want 20.0", w)
	}
	if math.Abs(h-10.0) > 0.01 {
		t.Errorf("height = %f, want 10.0", h)
	}
}

func TestEstimateMaxChars_ZeroDimensions(t *testing.T) {
	font := FontInfo{SizePt: 12.0, Family: "Arial"}
	if got := EstimateMaxChars(0, 100, font); got != 0 {
		t.Errorf("expected 0 for zero width, got %d", got)
	}
	if got := EstimateMaxChars(100, 0, font); got != 0 {
		t.Errorf("expected 0 for zero height, got %d", got)
	}
}

func TestEstimateMaxChars_ZeroFontSize(t *testing.T) {
	font := FontInfo{SizePt: 0}
	if got := EstimateMaxChars(100, 50, font); got != 0 {
		t.Errorf("expected 0 for zero font size, got %d", got)
	}
}

func TestEstimateMaxChars_KnownFont(t *testing.T) {
	font := FontInfo{SizePt: 12.0, Family: "Arial"}
	chars := EstimateMaxChars(200, 50, font)
	if chars <= 0 {
		t.Errorf("expected positive char count, got %d", chars)
	}
	if chars < 50 || chars > 150 {
		t.Errorf("char estimate %d seems unreasonable for 200x50pt Arial 12pt", chars)
	}
}

func TestEstimateMaxChars_BoldWider(t *testing.T) {
	font := FontInfo{SizePt: 12.0, Family: "Arial"}
	fontBold := FontInfo{SizePt: 12.0, Family: "Arial", Bold: true}
	regular := EstimateMaxChars(200, 50, font)
	bold := EstimateMaxChars(200, 50, fontBold)
	if bold >= regular {
		t.Errorf("bold (%d) should produce fewer chars than regular (%d)", bold, regular)
	}
}

func TestEstimateMaxChars_UnknownFont(t *testing.T) {
	font := FontInfo{SizePt: 10.0, Family: "UnknownFont123"}
	chars := EstimateMaxChars(100, 100, font)
	if chars <= 0 {
		t.Errorf("expected positive char count for unknown font, got %d", chars)
	}
}

func TestExtractPredominantFont_NilElement(t *testing.T) {
	info := ExtractPredominantFont(nil)
	if info.SizePt != defaultFontSizePt {
		t.Errorf("expected default font size %f, got %f", defaultFontSizePt, info.SizePt)
	}
}

func TestExtractPredominantFont_NoText(t *testing.T) {
	el := &model.PageElement{Shape: &model.Shape{}}
	info := ExtractPredominantFont(el)
	if info.SizePt != defaultFontSizePt {
		t.Errorf("expected default font size %f, got %f", defaultFontSizePt, info.SizePt)
	}
}

func TestExtractPredominantFont_WithRuns(t *testing.T) {
	fontSize := model.Magnitude{Magnitude: 24.0}
	el := &model.PageElement{
		Shape: &model.Shape{
			Text: &model.TextContent{
				TextElements: []model.TextElement{
					{TextRun: &model.TextRun{
						Content: "Hello",
						Style: &model.TextRunStyle{
							FontSize:   &fontSize,
							FontFamily: "Outfit",
							Bold:       true,
						},
					}},
					{TextRun: &model.TextRun{
						Content: "World",
						Style: &model.TextRunStyle{
							FontSize:   &fontSize,
							FontFamily: "Outfit",
							Bold:       true,
						},
					}},
				},
			},
		},
	}
	info := ExtractPredominantFont(el)
	if info.SizePt != 24.0 {
		t.Errorf("expected font size 24.0, got %f", info.SizePt)
	}
	if info.Family != "Outfit" {
		t.Errorf("expected font family Outfit, got %q", info.Family)
	}
	if !info.Bold {
		t.Error("expected bold to be true")
	}
}

func TestEstimateMaxChars_WrapEfficiency(t *testing.T) {
	font := FontInfo{SizePt: 14, Family: "Arial"}
	// Multi-line box: discount applies.
	cpl, lines := EstimateLineGeometry(280, 91, font)
	if lines < 2 {
		t.Fatalf("expected multi-line geometry, got %d lines", lines)
	}
	raw := cpl * lines
	got := EstimateMaxChars(280, 91, font)
	if got >= raw {
		t.Errorf("EstimateMaxChars = %d, want < raw area estimate %d (wrap discount)", got, raw)
	}
	// Single-line box: no discount.
	cpl1, lines1 := EstimateLineGeometry(280, 20, font)
	if lines1 != 1 {
		t.Fatalf("expected single line, got %d", lines1)
	}
	if got := EstimateMaxChars(280, 20, font); got != cpl1 {
		t.Errorf("single-line EstimateMaxChars = %d, want %d", got, cpl1)
	}
}
