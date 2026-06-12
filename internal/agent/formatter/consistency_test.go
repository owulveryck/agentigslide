package formatter

import (
	"testing"
)

// ===================== Helpers =====================

func makeSlideInfo(slideIndex int, pageID string, elements ...ElementInfo) SlideInfo {
	return SlideInfo{
		SlideIndex: slideIndex,
		PageID:     pageID,
		Elements:   elements,
	}
}

func makeElement(objectID, placeholderType string, runs []TextRunInfo, paragraphs []ParagraphInfo) ElementInfo {
	return ElementInfo{
		ObjectID:        objectID,
		PlaceholderType: placeholderType,
		TextRuns:        runs,
		Paragraphs:      paragraphs,
	}
}

func makeRun(content, fontFamily string, sizePt float64) TextRunInfo {
	return TextRunInfo{
		Content:    content,
		FontFamily: fontFamily,
		FontSizePt: sizePt,
	}
}

func makeRunBold(content, fontFamily string, sizePt float64) TextRunInfo {
	return TextRunInfo{
		Content:    content,
		FontFamily: fontFamily,
		FontSizePt: sizePt,
		Bold:       true,
	}
}

func makeRunWithColor(content, fontFamily string, sizePt float64, color *RGBColor) TextRunInfo {
	return TextRunInfo{
		Content:         content,
		FontFamily:      fontFamily,
		FontSizePt:      sizePt,
		ForegroundColor: color,
	}
}

func makeParagraph(alignment string, lineSpacing float64) ParagraphInfo {
	return ParagraphInfo{
		Alignment:   alignment,
		LineSpacing: lineSpacing,
	}
}

func rgb(r, g, b float64) *RGBColor {
	return &RGBColor{Red: r, Green: g, Blue: b}
}

// ===================== Tests =====================

func TestCheckConsistency_EmptyStructure(t *testing.T) {
	issues := CheckConsistency(nil)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for empty input, got %d", len(issues))
	}

	issues = CheckConsistency([]SlideInfo{})
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for empty slice, got %d", len(issues))
	}
}

func TestCheckConsistency_ConsistentPresentation(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE",
				[]TextRunInfo{makeRunBold("Title 1", "Roboto", 24)},
				nil,
			),
			makeElement("b0", "BODY",
				[]TextRunInfo{makeRun("Body 1", "Roboto", 14)},
				nil,
			),
		),
		makeSlideInfo(1, "p1",
			makeElement("t1", "TITLE",
				[]TextRunInfo{makeRunBold("Title 2", "Roboto", 24)},
				nil,
			),
			makeElement("b1", "BODY",
				[]TextRunInfo{makeRun("Body 2", "Roboto", 14)},
				nil,
			),
		),
		makeSlideInfo(2, "p2",
			makeElement("t2", "TITLE",
				[]TextRunInfo{makeRunBold("Title 3", "Roboto", 24)},
				nil,
			),
			makeElement("b2", "BODY",
				[]TextRunInfo{makeRun("Body 3", "Roboto", 14)},
				nil,
			),
		),
	}

	issues := CheckConsistency(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for consistent presentation, got %d:", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s slide=%d obj=%s expected=%s actual=%s severity=%s",
				iss.Rule, iss.SlideIndex, iss.ObjectID, iss.Expected, iss.Actual, iss.Severity)
		}
	}
}

func TestCheckFontFamilyByRole(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", []TextRunInfo{makeRun("Title 1", "Roboto", 24)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("t1", "TITLE", []TextRunInfo{makeRun("Title 2", "Roboto", 24)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("t2", "TITLE", []TextRunInfo{makeRun("Title 3", "Arial", 24)}, nil),
		),
	}

	issues := checkFontFamilyByRole(structure)

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	iss := issues[0]
	if iss.Rule != "FontFamilyByRole" {
		t.Errorf("expected Rule=FontFamilyByRole, got %s", iss.Rule)
	}
	if iss.Expected != "Roboto" {
		t.Errorf("expected Expected=Roboto, got %s", iss.Expected)
	}
	if iss.Actual != "Arial" {
		t.Errorf("expected Actual=Arial, got %s", iss.Actual)
	}
	if iss.ObjectID != "t2" {
		t.Errorf("expected ObjectID=t2, got %s", iss.ObjectID)
	}
	if iss.Severity != "warning" {
		t.Errorf("expected Severity=warning, got %s", iss.Severity)
	}
}

func TestCheckFontFamilyByRole_NoPlaceholderType(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("e0", "", []TextRunInfo{makeRun("Text", "Roboto", 14)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("e1", "", []TextRunInfo{makeRun("Text", "Arial", 14)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("e2", "", []TextRunInfo{makeRun("Text", "Helvetica", 14)}, nil),
		),
	}

	issues := checkFontFamilyByRole(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for elements without PlaceholderType, got %d", len(issues))
	}
}

func TestCheckFontSizeByRole(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("b0", "BODY", []TextRunInfo{makeRun("Body 1", "Roboto", 14)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("b1", "BODY", []TextRunInfo{makeRun("Body 2", "Roboto", 14)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("b2", "BODY", []TextRunInfo{makeRun("Body 3", "Roboto", 18)}, nil),
		),
	}

	issues := checkFontSizeByRole(structure)

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	iss := issues[0]
	if iss.Rule != "FontSizeByRole" {
		t.Errorf("expected Rule=FontSizeByRole, got %s", iss.Rule)
	}
	if iss.ObjectID != "b2" {
		t.Errorf("expected ObjectID=b2, got %s", iss.ObjectID)
	}
	if iss.Severity != "warning" {
		t.Errorf("expected Severity=warning, got %s", iss.Severity)
	}
}

func TestCheckFontSizeByRole_WithinTolerance(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("b0", "BODY", []TextRunInfo{makeRun("Body 1", "Roboto", 14.0)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("b1", "BODY", []TextRunInfo{makeRun("Body 2", "Roboto", 14.3)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("b2", "BODY", []TextRunInfo{makeRun("Body 3", "Roboto", 14.0)}, nil),
		),
	}

	issues := checkFontSizeByRole(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for sizes within tolerance, got %d", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s obj=%s expected=%s actual=%s", iss.Rule, iss.ObjectID, iss.Expected, iss.Actual)
		}
	}
}

func TestCheckSizeHierarchy_Valid(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", []TextRunInfo{makeRun("Title", "Roboto", 24)}, nil),
			makeElement("s0", "SUBTITLE", []TextRunInfo{makeRun("Subtitle", "Roboto", 18)}, nil),
			makeElement("b0", "BODY", []TextRunInfo{makeRun("Body", "Roboto", 14)}, nil),
		),
	}

	issues := checkSizeHierarchy(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for valid hierarchy, got %d", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s expected=%s actual=%s", iss.Rule, iss.Expected, iss.Actual)
		}
	}
}

func TestCheckSizeHierarchy_Violated(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", []TextRunInfo{makeRun("Title", "Roboto", 14)}, nil),
			makeElement("b0", "BODY", []TextRunInfo{makeRun("Body", "Roboto", 18)}, nil),
		),
	}

	issues := checkSizeHierarchy(structure)

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	iss := issues[0]
	if iss.Rule != "SizeHierarchy" {
		t.Errorf("expected Rule=SizeHierarchy, got %s", iss.Rule)
	}
	if iss.Severity != "error" {
		t.Errorf("expected Severity=error, got %s", iss.Severity)
	}
}

func TestCheckColorPalette_Consistent(t *testing.T) {
	black := rgb(0, 0, 0)
	blue := rgb(0, 0, 1)

	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("e0", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("e1", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
			makeElement("e1b", "BODY", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, blue)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("e2", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, blue)}, nil),
		),
	}

	issues := checkColorPalette(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for consistent colors, got %d", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s slide=%d obj=%s actual=%s", iss.Rule, iss.SlideIndex, iss.ObjectID, iss.Actual)
		}
	}
}

func TestCheckColorPalette_OrphanColor(t *testing.T) {
	black := rgb(0, 0, 0)
	red := rgb(1, 0, 0)

	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("e0", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("e1", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("e2", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
		),
		makeSlideInfo(3, "p3",
			makeElement("e3", "TITLE", []TextRunInfo{makeRunWithColor("Text", "Roboto", 14, black)}, nil),
		),
		makeSlideInfo(4, "p4",
			makeElement("e4", "TITLE", []TextRunInfo{makeRunWithColor("Orphan", "Roboto", 14, red)}, nil),
		),
	}

	issues := checkColorPalette(structure)

	found := false
	for _, iss := range issues {
		if iss.Rule == "ColorPalette" && iss.SlideIndex == 4 && iss.ObjectID == "e4" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a ColorPalette issue for the orphan color on slide 4, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s slide=%d obj=%s actual=%s", iss.Rule, iss.SlideIndex, iss.ObjectID, iss.Actual)
		}
	}
}

func TestCheckBackgroundConsistency(t *testing.T) {
	white := rgb(1, 1, 1)
	gray := rgb(0.5, 0.5, 0.5)

	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			ElementInfo{ObjectID: "t0", PlaceholderType: "TITLE", BackgroundColor: white},
		),
		makeSlideInfo(1, "p1",
			ElementInfo{ObjectID: "t1", PlaceholderType: "TITLE", BackgroundColor: white},
		),
		makeSlideInfo(2, "p2",
			ElementInfo{ObjectID: "t2", PlaceholderType: "TITLE", BackgroundColor: gray},
		),
	}

	issues := checkBackgroundConsistency(structure)

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	iss := issues[0]
	if iss.Rule != "BackgroundConsistency" {
		t.Errorf("expected Rule=BackgroundConsistency, got %s", iss.Rule)
	}
	if iss.ObjectID != "t2" {
		t.Errorf("expected ObjectID=t2, got %s", iss.ObjectID)
	}
	if iss.Severity != "warning" {
		t.Errorf("expected Severity=warning, got %s", iss.Severity)
	}
}

func TestCheckParagraphSpacing(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("b0", "BODY", nil, []ParagraphInfo{makeParagraph("START", 115)}),
		),
		makeSlideInfo(1, "p1",
			makeElement("b1", "BODY", nil, []ParagraphInfo{makeParagraph("START", 115)}),
		),
		makeSlideInfo(2, "p2",
			makeElement("b2", "BODY", nil, []ParagraphInfo{makeParagraph("START", 150)}),
		),
	}

	issues := checkParagraphSpacing(structure)

	found := false
	for _, iss := range issues {
		if iss.Rule == "ParagraphSpacing" && iss.ObjectID == "b2" {
			found = true
			if iss.Severity != "warning" {
				t.Errorf("expected Severity=warning, got %s", iss.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected a ParagraphSpacing issue for b2, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s obj=%s expected=%s actual=%s", iss.Rule, iss.ObjectID, iss.Expected, iss.Actual)
		}
	}
}

func TestCheckAlignmentByRole(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", nil, []ParagraphInfo{makeParagraph("CENTER", 115)}),
		),
		makeSlideInfo(1, "p1",
			makeElement("t1", "TITLE", nil, []ParagraphInfo{makeParagraph("CENTER", 115)}),
		),
		makeSlideInfo(2, "p2",
			makeElement("t2", "TITLE", nil, []ParagraphInfo{makeParagraph("START", 115)}),
		),
	}

	issues := checkAlignmentByRole(structure)

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	iss := issues[0]
	if iss.Rule != "AlignmentByRole" {
		t.Errorf("expected Rule=AlignmentByRole, got %s", iss.Rule)
	}
	if iss.ObjectID != "t2" {
		t.Errorf("expected ObjectID=t2, got %s", iss.ObjectID)
	}
	if iss.Expected != "CENTER" {
		t.Errorf("expected Expected=CENTER, got %s", iss.Expected)
	}
	if iss.Actual != "START" {
		t.Errorf("expected Actual=START, got %s", iss.Actual)
	}
	if iss.Severity != "warning" {
		t.Errorf("expected Severity=warning, got %s", iss.Severity)
	}
}

func TestCheckEmphasisCoherence_Bold(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", []TextRunInfo{makeRunBold("Title 1", "Roboto", 24)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("t1", "TITLE", []TextRunInfo{makeRunBold("Title 2", "Roboto", 24)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("t2", "TITLE", []TextRunInfo{makeRun("Title 3", "Roboto", 24)}, nil),
		),
	}

	issues := checkEmphasisCoherence(structure)

	found := false
	for _, iss := range issues {
		if iss.Rule == "EmphasisCoherence" && iss.ObjectID == "t2" && iss.Expected == "bold=true" {
			found = true
			if iss.Actual != "bold=false" {
				t.Errorf("expected Actual=bold=false, got %s", iss.Actual)
			}
			if iss.Severity != "warning" {
				t.Errorf("expected Severity=warning, got %s", iss.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected an EmphasisCoherence issue for t2 with bold=true expected, got %d issues", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s obj=%s expected=%s actual=%s", iss.Rule, iss.ObjectID, iss.Expected, iss.Actual)
		}
	}
}

func TestCheckOutlineConsistency(t *testing.T) {
	black := rgb(0, 0, 0)
	white := rgb(1, 1, 1)

	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			ElementInfo{ObjectID: "r0", ShapeType: "RECTANGLE", OutlineColor: black, OutlineWeightPt: 1.0},
		),
		makeSlideInfo(1, "p1",
			ElementInfo{ObjectID: "r1", ShapeType: "RECTANGLE", OutlineColor: black, OutlineWeightPt: 1.0},
		),
		makeSlideInfo(2, "p2",
			ElementInfo{ObjectID: "r2", ShapeType: "RECTANGLE", OutlineColor: white, OutlineWeightPt: 2.0},
		),
	}

	issues := checkOutlineConsistency(structure)

	if len(issues) == 0 {
		t.Fatal("expected at least 1 issue for deviant outline, got 0")
	}

	foundColor := false
	foundWeight := false
	for _, iss := range issues {
		if iss.Rule != "OutlineConsistency" {
			t.Errorf("unexpected rule %s", iss.Rule)
			continue
		}
		if iss.ObjectID == "r2" && iss.Severity == "warning" {
			// Check if it is color or weight issue.
			if iss.Expected == "rgb(0.00,0.00,0.00)" {
				foundColor = true
			}
			if iss.Expected == "1.0pt" {
				foundWeight = true
			}
		}
	}
	if !foundColor {
		t.Error("expected an OutlineConsistency issue for deviant outline color on r2")
	}
	if !foundWeight {
		t.Error("expected an OutlineConsistency issue for deviant outline weight on r2")
	}
}

func TestCheckConsistency_WhitespaceRunsSkipped(t *testing.T) {
	structure := []SlideInfo{
		makeSlideInfo(0, "p0",
			makeElement("t0", "TITLE", []TextRunInfo{makeRun("\n", "Roboto", 24)}, nil),
		),
		makeSlideInfo(1, "p1",
			makeElement("t1", "TITLE", []TextRunInfo{makeRun("  ", "Arial", 18)}, nil),
		),
		makeSlideInfo(2, "p2",
			makeElement("t2", "TITLE", []TextRunInfo{makeRun("\t\n", "Helvetica", 30)}, nil),
		),
	}

	issues := CheckConsistency(structure)
	if len(issues) != 0 {
		t.Errorf("expected 0 issues when all runs are whitespace-only, got %d", len(issues))
		for _, iss := range issues {
			t.Logf("  rule=%s slide=%d obj=%s expected=%s actual=%s",
				iss.Rule, iss.SlideIndex, iss.ObjectID, iss.Expected, iss.Actual)
		}
	}
}
