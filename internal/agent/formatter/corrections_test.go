package formatter

import (
	"slices"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeCorrection(objectID, typ string, opts ...func(*Correction)) Correction {
	c := Correction{ObjectID: objectID, Type: typ, Reason: "test reason"}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func withFontSize(pt float64) func(*Correction)    { return func(c *Correction) { c.FontSizePt = &pt } }
func withFontFamily(ff string) func(*Correction)    { return func(c *Correction) { c.FontFamily = &ff } }
func withLineSpacing(ls float64) func(*Correction)  { return func(c *Correction) { c.LineSpacing = &ls } }
func withSpaceAbove(pt float64) func(*Correction)   { return func(c *Correction) { c.SpaceAbovePt = &pt } }
func withSpaceBelow(pt float64) func(*Correction)   { return func(c *Correction) { c.SpaceBelowPt = &pt } }
func withRange(start, end int) func(*Correction) {
	return func(c *Correction) { c.StartIndex = &start; c.EndIndex = &end }
}
func withCellLoc(row, col int) func(*Correction) {
	return func(c *Correction) { c.CellLocation = &CellRef{RowIndex: row, ColumnIndex: col} }
}
func withForegroundColor(r, g, b float64) func(*Correction) {
	return func(c *Correction) { c.ForegroundColor = &RGBColor{Red: r, Green: g, Blue: b} }
}
func withAlignment(a string) func(*Correction)  { return func(c *Correction) { c.Alignment = &a } }
func withBold(b bool) func(*Correction)          { return func(c *Correction) { c.Bold = &b } }
func withBackgroundColor(r, g, b float64) func(*Correction) {
	return func(c *Correction) { c.BackgroundColor = &RGBColor{Red: r, Green: g, Blue: b} }
}
func withContentAlignment(a string) func(*Correction) {
	return func(c *Correction) { c.ContentAlignmentVal = &a }
}
func withOutlineColor(r, g, b float64) func(*Correction) {
	return func(c *Correction) { c.OutlineColor = &RGBColor{Red: r, Green: g, Blue: b} }
}
func withOutlineWeight(w float64) func(*Correction) {
	return func(c *Correction) { c.OutlineWeightPt = &w }
}

func sampleStructure() []SlideInfo {
	return []SlideInfo{{SlideIndex: 0, PageID: "p0", Elements: []ElementInfo{
		{ObjectID: "obj1"}, {ObjectID: "obj2"}, {ObjectID: "tableObj"},
	}}}
}

// ---------------------------------------------------------------------------
// ValidateCorrections tests (migrated from fixfonts)
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
// NEW ValidateCorrections tests
// ---------------------------------------------------------------------------

func TestValidateCorrections_ShapePropertiesType(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "shapeProperties", withBackgroundColor(0.1, 0.2, 0.3)),
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

func TestValidateCorrections_ShapePropertiesNoChanges(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "shapeProperties"), // no BackgroundColor, ContentAlignmentVal, OutlineColor, or OutlineWeightPt
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 0 {
		t.Errorf("expected 0 (shapeProperties with no changes filtered), got %d", len(result))
	}
}

func TestValidateCorrections_TextStyleWithForegroundColor(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle", withForegroundColor(1.0, 0.0, 0.0)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1 (foregroundColor alone is sufficient), got %d", len(result))
	}
}

func TestValidateCorrections_TextStyleWithBold(t *testing.T) {
	plan := &CorrectionPlan{
		Corrections: []Correction{
			makeCorrection("obj1", "textStyle", withBold(true)),
		},
	}
	result := ValidateCorrections(plan, sampleStructure())
	if len(result) != 1 {
		t.Fatalf("expected 1 (bold alone is sufficient), got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// BuildCorrections tests (migrated from fixfonts)
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

// ---------------------------------------------------------------------------
// NEW BuildCorrections tests
// ---------------------------------------------------------------------------

func TestBuildCorrections_TextStyleForegroundColor(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withForegroundColor(0.8, 0.2, 0.5)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	uts := reqs[0].UpdateTextStyle
	if uts == nil {
		t.Fatal("expected UpdateTextStyle request")
	}
	if uts.Style.ForegroundColor == nil {
		t.Fatal("expected ForegroundColor to be set")
	}
	oc := uts.Style.ForegroundColor.OpaqueColor
	if oc == nil || oc.RgbColor == nil {
		t.Fatal("expected OpaqueColor.RgbColor to be set")
	}
	if oc.RgbColor.Red != 0.8 {
		t.Errorf("Red: want 0.8, got %f", oc.RgbColor.Red)
	}
	if oc.RgbColor.Green != 0.2 {
		t.Errorf("Green: want 0.2, got %f", oc.RgbColor.Green)
	}
	if oc.RgbColor.Blue != 0.5 {
		t.Errorf("Blue: want 0.5, got %f", oc.RgbColor.Blue)
	}
	if uts.Fields != "foregroundColor" {
		t.Errorf("Fields: want foregroundColor, got %s", uts.Fields)
	}
}

func TestBuildCorrections_TextStyleBold(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "textStyle", withBold(true)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	uts := reqs[0].UpdateTextStyle
	if uts == nil {
		t.Fatal("expected UpdateTextStyle request")
	}
	if !uts.Style.Bold {
		t.Error("expected Bold to be true")
	}
	if uts.Fields != "bold" {
		t.Errorf("Fields: want bold, got %s", uts.Fields)
	}
}

func TestBuildCorrections_ParagraphStyleAlignment(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "paragraphStyle", withAlignment("CENTER")),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	ups := reqs[0].UpdateParagraphStyle
	if ups == nil {
		t.Fatal("expected UpdateParagraphStyle request")
	}
	if ups.Style.Alignment != "CENTER" {
		t.Errorf("Alignment: want CENTER, got %s", ups.Style.Alignment)
	}
	if ups.Fields != "alignment" {
		t.Errorf("Fields: want alignment, got %s", ups.Fields)
	}
}

func TestBuildCorrections_ShapePropertiesBackground(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "shapeProperties", withBackgroundColor(0.1, 0.2, 0.3)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.UpdateShapeProperties == nil {
		t.Fatal("expected UpdateShapeProperties request")
	}
	usp := r.UpdateShapeProperties
	if usp.ObjectId != "obj1" {
		t.Errorf("ObjectId: want obj1, got %s", usp.ObjectId)
	}
	props := usp.ShapeProperties
	if props.ShapeBackgroundFill == nil || props.ShapeBackgroundFill.SolidFill == nil {
		t.Fatal("expected ShapeBackgroundFill.SolidFill to be set")
	}
	rgb := props.ShapeBackgroundFill.SolidFill.Color.RgbColor
	if rgb.Red != 0.1 {
		t.Errorf("Red: want 0.1, got %f", rgb.Red)
	}
	if rgb.Green != 0.2 {
		t.Errorf("Green: want 0.2, got %f", rgb.Green)
	}
	if rgb.Blue != 0.3 {
		t.Errorf("Blue: want 0.3, got %f", rgb.Blue)
	}
	if usp.Fields != "shapeBackgroundFill" {
		t.Errorf("Fields: want shapeBackgroundFill, got %s", usp.Fields)
	}
}

func TestBuildCorrections_ShapePropertiesContentAlignment(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "shapeProperties", withContentAlignment("MIDDLE")),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	usp := reqs[0].UpdateShapeProperties
	if usp == nil {
		t.Fatal("expected UpdateShapeProperties request")
	}
	if usp.ShapeProperties.ContentAlignment != "MIDDLE" {
		t.Errorf("ContentAlignment: want MIDDLE, got %s", usp.ShapeProperties.ContentAlignment)
	}
	if usp.Fields != "contentAlignment" {
		t.Errorf("Fields: want contentAlignment, got %s", usp.Fields)
	}
}

func TestBuildCorrections_ShapePropertiesOutline(t *testing.T) {
	corrections := []Correction{
		makeCorrection("obj1", "shapeProperties", withOutlineColor(0.5, 0.6, 0.7), withOutlineWeight(2.0)),
	}
	reqs := BuildCorrections(corrections)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	usp := reqs[0].UpdateShapeProperties
	if usp == nil {
		t.Fatal("expected UpdateShapeProperties request")
	}
	props := usp.ShapeProperties
	if props.Outline == nil {
		t.Fatal("expected Outline to be set")
	}
	// Check outline fill color
	if props.Outline.OutlineFill == nil || props.Outline.OutlineFill.SolidFill == nil {
		t.Fatal("expected OutlineFill.SolidFill to be set")
	}
	rgb := props.Outline.OutlineFill.SolidFill.Color.RgbColor
	if rgb.Red != 0.5 {
		t.Errorf("OutlineColor Red: want 0.5, got %f", rgb.Red)
	}
	if rgb.Green != 0.6 {
		t.Errorf("OutlineColor Green: want 0.6, got %f", rgb.Green)
	}
	if rgb.Blue != 0.7 {
		t.Errorf("OutlineColor Blue: want 0.7, got %f", rgb.Blue)
	}
	// Check outline weight
	if props.Outline.Weight == nil {
		t.Fatal("expected Outline.Weight to be set")
	}
	if props.Outline.Weight.Magnitude != 2.0 {
		t.Errorf("OutlineWeight: want 2.0, got %f", props.Outline.Weight.Magnitude)
	}
	if props.Outline.Weight.Unit != "PT" {
		t.Errorf("OutlineWeight Unit: want PT, got %s", props.Outline.Weight.Unit)
	}
	// Fields should contain "outline" only once
	if usp.Fields != "outline" {
		t.Errorf("Fields: want outline, got %s", usp.Fields)
	}
}
