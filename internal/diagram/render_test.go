package diagram

import (
	"strings"
	"testing"
)

func TestRender_BasicNodes(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "testpage",
		Nodes: []PositionedNode{
			{ID: "a", Label: "Node A", Shape: "rectangle", Style: "primary", X: 100, Y: 200, Width: 1000, Height: 500},
			{ID: "b", Label: "Node B", Shape: "ellipse", Style: "accent", X: 2000, Y: 200, Width: 1000, Height: 500},
		},
	}

	reqs := Render(d)

	var createShapes, insertTexts, updateStyles, updateParas int
	for _, r := range reqs {
		switch {
		case r.CreateShape != nil:
			createShapes++
		case r.InsertText != nil:
			insertTexts++
		case r.UpdateTextStyle != nil:
			updateStyles++
		case r.UpdateParagraphStyle != nil:
			updateParas++
		}
	}

	if createShapes != 2 {
		t.Errorf("expected 2 CreateShape, got %d", createShapes)
	}
	if insertTexts != 2 {
		t.Errorf("expected 2 InsertText, got %d", insertTexts)
	}
}

func TestRender_EdgesWithLabels(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "testpage",
		Nodes: []PositionedNode{
			{ID: "a", Label: "A", X: 0, Y: 0, Width: 100, Height: 100},
			{ID: "b", Label: "B", X: 500, Y: 0, Width: 100, Height: 100},
		},
		Edges: []PositionedEdge{
			{From: "a", To: "b", Label: "connect", LineStyle: "arrow", StartX: 100, StartY: 50, EndX: 500, EndY: 50},
		},
	}

	reqs := Render(d)

	var createLines, edgeLabels int
	for _, r := range reqs {
		if r.CreateLine != nil {
			createLines++
		}
		if r.CreateShape != nil && strings.Contains(r.CreateShape.ObjectId, "elabel") {
			edgeLabels++
		}
	}

	if createLines != 1 {
		t.Errorf("expected 1 CreateLine, got %d", createLines)
	}
	if edgeLabels != 1 {
		t.Errorf("expected 1 edge label, got %d", edgeLabels)
	}

	for _, r := range reqs {
		if r.UpdateLineProperties == nil {
			continue
		}
		lp := r.UpdateLineProperties.LineProperties
		if lp.StartConnection == nil {
			t.Fatal("expected StartConnection to be set")
		}
		if lp.StartConnection.ConnectedObjectId != "diag_testpage_node_0" {
			t.Errorf("start connected to %q, want diag_testpage_node_0", lp.StartConnection.ConnectedObjectId)
		}
		// a is left of b -> horizontal dominant -> start=right(3 for rectangle)
		if lp.StartConnection.ConnectionSiteIndex != 3 {
			t.Errorf("start site = %d, want 3 (right)", lp.StartConnection.ConnectionSiteIndex)
		}
		if lp.EndConnection == nil {
			t.Fatal("expected EndConnection to be set")
		}
		if lp.EndConnection.ConnectedObjectId != "diag_testpage_node_1" {
			t.Errorf("end connected to %q, want diag_testpage_node_1", lp.EndConnection.ConnectedObjectId)
		}
		// end=left(1 for rectangle)
		if lp.EndConnection.ConnectionSiteIndex != 1 {
			t.Errorf("end site = %d, want 1 (left)", lp.EndConnection.ConnectionSiteIndex)
		}
	}
}

func TestRender_Groups(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "testpage",
		Groups: []PositionedGroup{
			{ID: "g1", Label: "Group 1", X: 0, Y: 0, Width: 1000, Height: 1000},
		},
	}

	reqs := Render(d)

	var groupShapes int
	for _, r := range reqs {
		if r.CreateShape != nil && strings.Contains(r.CreateShape.ObjectId, "group") {
			groupShapes++
		}
	}

	if groupShapes != 1 {
		t.Errorf("expected 1 group shape, got %d", groupShapes)
	}
}

func TestRender_ObjectIDs(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "p1",
		Nodes: []PositionedNode{
			{ID: "n", Label: "N", X: 0, Y: 0, Width: 100, Height: 100},
		},
		Edges: []PositionedEdge{
			{From: "n", To: "n", StartX: 0, StartY: 0, EndX: 100, EndY: 100},
		},
	}

	reqs := Render(d)

	seen := make(map[string]bool)
	for _, r := range reqs {
		var id string
		switch {
		case r.CreateShape != nil:
			id = r.CreateShape.ObjectId
		case r.CreateLine != nil:
			id = r.CreateLine.ObjectId
		default:
			continue
		}
		if seen[id] {
			t.Errorf("duplicate ObjectID: %s", id)
		}
		seen[id] = true
	}
}

func TestRender_DashedLine(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "p2",
		Edges: []PositionedEdge{
			{From: "a", To: "b", LineStyle: "dashed_arrow", StartX: 0, StartY: 0, EndX: 100, EndY: 100},
		},
	}

	reqs := Render(d)

	for _, r := range reqs {
		if r.UpdateLineProperties != nil {
			lp := r.UpdateLineProperties.LineProperties
			if lp.DashStyle != "DASH" {
				t.Error("expected DASH style for dashed_arrow")
			}
			if lp.EndArrow != "OPEN_ARROW" {
				t.Error("expected OPEN_ARROW for dashed_arrow")
			}
			if lp.StartConnection != nil {
				t.Error("expected no StartConnection when nodes are missing")
			}
			if lp.EndConnection != nil {
				t.Error("expected no EndConnection when nodes are missing")
			}
		}
	}
}

func TestComputeConnectionSites(t *testing.T) {
	// Verified site indices against Google Slides API:
	//   Rectangle/RoundRect/Diamond: 0=top, 1=left, 2=bottom, 3=right
	//   Ellipse (8 sites): 0=top, 2=left, 4=bottom, 6=right
	tests := []struct {
		name               string
		from, to           PositionedNode
		wantStart, wantEnd int64
	}{
		{
			name:      "rect_horizontal_left_to_right",
			from:      PositionedNode{Shape: "rectangle", X: 0, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "rectangle", X: 500, Y: 0, Width: 100, Height: 100},
			wantStart: 3, wantEnd: 1,
		},
		{
			name:      "rect_horizontal_right_to_left",
			from:      PositionedNode{Shape: "rectangle", X: 500, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "rectangle", X: 0, Y: 0, Width: 100, Height: 100},
			wantStart: 1, wantEnd: 3,
		},
		{
			name:      "rect_vertical_top_to_bottom",
			from:      PositionedNode{Shape: "rectangle", X: 0, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "rectangle", X: 0, Y: 500, Width: 100, Height: 100},
			wantStart: 2, wantEnd: 0,
		},
		{
			name:      "rect_vertical_bottom_to_top",
			from:      PositionedNode{Shape: "rectangle", X: 0, Y: 500, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "rectangle", X: 0, Y: 0, Width: 100, Height: 100},
			wantStart: 0, wantEnd: 2,
		},
		{
			name:      "rect_self_loop",
			from:      PositionedNode{Shape: "rectangle", X: 100, Y: 100, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "rectangle", X: 100, Y: 100, Width: 100, Height: 100},
			wantStart: 0, wantEnd: 2,
		},
		{
			name:      "ellipse_horizontal",
			from:      PositionedNode{Shape: "ellipse", X: 0, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "ellipse", X: 500, Y: 0, Width: 100, Height: 100},
			wantStart: 6, wantEnd: 2,
		},
		{
			name:      "ellipse_vertical",
			from:      PositionedNode{Shape: "ellipse", X: 0, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "ellipse", X: 0, Y: 500, Width: 100, Height: 100},
			wantStart: 4, wantEnd: 0,
		},
		{
			name:      "mixed_rect_to_ellipse_horizontal",
			from:      PositionedNode{Shape: "rectangle", X: 0, Y: 0, Width: 100, Height: 100},
			to:        PositionedNode{Shape: "ellipse", X: 500, Y: 0, Width: 100, Height: 100},
			wantStart: 3, wantEnd: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startSite, endSite := computeConnectionSites(tt.from, tt.to)
			if startSite != tt.wantStart {
				t.Errorf("startSite = %d, want %d", startSite, tt.wantStart)
			}
			if endSite != tt.wantEnd {
				t.Errorf("endSite = %d, want %d", endSite, tt.wantEnd)
			}
		})
	}
}

func TestRender_ConnectionForceSendFields(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "p3",
		Nodes: []PositionedNode{
			{ID: "x", Label: "X", X: 0, Y: 0, Width: 100, Height: 100},
			{ID: "y", Label: "Y", X: 0, Y: 500, Width: 100, Height: 100},
		},
		Edges: []PositionedEdge{
			{From: "x", To: "y", LineStyle: "arrow", StartX: 50, StartY: 100, EndX: 50, EndY: 500},
		},
	}

	reqs := Render(d)

	for _, r := range reqs {
		if r.UpdateLineProperties == nil {
			continue
		}
		lp := r.UpdateLineProperties.LineProperties
		if lp.StartConnection == nil {
			t.Fatal("expected StartConnection")
		}
		if len(lp.StartConnection.ForceSendFields) == 0 {
			t.Error("StartConnection must have ForceSendFields for ConnectionSiteIndex")
		}
		if lp.EndConnection == nil {
			t.Fatal("expected EndConnection")
		}
		// endSite is 0 (top) which is the Go zero value — ForceSendFields ensures it's sent
		if lp.EndConnection.ConnectionSiteIndex != 0 {
			t.Errorf("endSite = %d, want 0 (top)", lp.EndConnection.ConnectionSiteIndex)
		}
		if len(lp.EndConnection.ForceSendFields) == 0 {
			t.Error("EndConnection must have ForceSendFields for zero-value ConnectionSiteIndex")
		}
	}
}

func TestRender_VerticalCentering(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "p5",
		Nodes: []PositionedNode{
			{ID: "a", Label: "Node", Shape: "rectangle", Style: "primary",
				X: 0, Y: 0, Width: DefaultNodeWidth, Height: DefaultNodeHeight},
		},
	}

	reqs := Render(d)

	var foundMiddle bool
	for _, r := range reqs {
		if r.UpdateShapeProperties != nil {
			if r.UpdateShapeProperties.ShapeProperties.ContentAlignment == "MIDDLE" {
				foundMiddle = true
			}
		}
	}

	if !foundMiddle {
		t.Error("expected ContentAlignment=MIDDLE")
	}
}

func TestRender_FontScaling(t *testing.T) {
	halfHeight := DefaultNodeHeight / 2
	d := &PositionedDiagram{
		PageID: "p6",
		Nodes: []PositionedNode{
			{ID: "full", Label: "Full", Shape: "rectangle", Style: "primary",
				X: 0, Y: 0, Width: DefaultNodeWidth, Height: DefaultNodeHeight},
			{ID: "half", Label: "Half", Shape: "rectangle", Style: "primary",
				X: 0, Y: 1000, Width: DefaultNodeWidth, Height: halfHeight},
			{ID: "tiny", Label: "Tiny", Shape: "rectangle", Style: "primary",
				X: 0, Y: 2000, Width: DefaultNodeWidth, Height: 100},
		},
	}

	reqs := Render(d)

	fontSizes := make(map[string]float64)
	for _, r := range reqs {
		if r.UpdateTextStyle == nil {
			continue
		}
		objID := r.UpdateTextStyle.ObjectId
		fs := r.UpdateTextStyle.Style.FontSize.Magnitude
		fontSizes[objID] = fs
	}

	fullFS := fontSizes["diag_p6_node_0"]
	halfFS := fontSizes["diag_p6_node_1"]
	tinyFS := fontSizes["diag_p6_node_2"]

	if fullFS != 11 {
		t.Errorf("full-size node font = %.1f, want 11", fullFS)
	}
	if halfFS >= fullFS {
		t.Errorf("half-height node font (%.1f) should be smaller than full (%.1f)", halfFS, fullFS)
	}
	if tinyFS < 7 {
		t.Errorf("tiny node font = %.1f, should not go below 7", tinyFS)
	}
	if tinyFS != 7 {
		t.Errorf("tiny node font = %.1f, expected floor of 7", tinyFS)
	}
}

func TestRender_FontScalingByWidth(t *testing.T) {
	shortLabel := &PositionedDiagram{
		PageID: "p_wscale",
		Nodes: []PositionedNode{
			{ID: "short", Label: "OK", Shape: "rectangle", Style: "primary",
				X: 0, Y: 0, Width: DefaultNodeWidth, Height: DefaultNodeHeight},
			{ID: "long", Label: "This Is A Very Long Label Text", Shape: "rectangle", Style: "primary",
				X: 0, Y: 1000, Width: DefaultNodeWidth / 2, Height: DefaultNodeHeight},
		},
	}

	reqs := Render(shortLabel)

	fontSizes := make(map[string]float64)
	for _, r := range reqs {
		if r.UpdateTextStyle == nil {
			continue
		}
		fontSizes[r.UpdateTextStyle.ObjectId] = r.UpdateTextStyle.Style.FontSize.Magnitude
	}

	shortFS := fontSizes["diag_p_wscale_node_0"]
	longFS := fontSizes["diag_p_wscale_node_1"]

	if longFS >= shortFS {
		t.Errorf("long label in narrow node (%.1fpt) should have smaller font than short label (%.1fpt)", longFS, shortFS)
	}
}

func TestRender_CategoryField(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "p4",
		Nodes: []PositionedNode{
			{ID: "a", Label: "A", X: 0, Y: 0, Width: 100, Height: 100},
			{ID: "b", Label: "B", X: 0, Y: 500, Width: 100, Height: 100},
		},
		Edges: []PositionedEdge{
			{From: "a", To: "b", LineStyle: "arrow", StartX: 50, StartY: 100, EndX: 50, EndY: 500},
		},
	}

	reqs := Render(d)

	for _, r := range reqs {
		if r.CreateLine == nil {
			continue
		}
		if r.CreateLine.Category != "STRAIGHT" {
			t.Errorf("Category = %q, want STRAIGHT", r.CreateLine.Category)
		}
		if r.CreateLine.LineCategory != "" {
			t.Error("deprecated LineCategory should not be set")
		}
	}
}

func TestRender_BentConnectorForBackwardEdge(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "bent_test",
		Nodes: []PositionedNode{
			{ID: "top", Label: "Top", Shape: "rectangle", X: 100, Y: 100, Width: 200, Height: 100},
			{ID: "bottom", Label: "Bottom", Shape: "rectangle", X: 100, Y: 500, Width: 200, Height: 100},
		},
		Edges: []PositionedEdge{
			// Backward edge: bottom -> top (target Y < source Y at same X)
			{From: "bottom", To: "top", LineStyle: "arrow", StartX: 200, StartY: 500, EndX: 200, EndY: 200},
		},
	}

	reqs := Render(d)

	var found bool
	for _, r := range reqs {
		if r.CreateLine == nil {
			continue
		}
		found = true
		if r.CreateLine.Category != "BENT" {
			t.Errorf("backward edge Category = %q, want BENT", r.CreateLine.Category)
		}
	}
	if !found {
		t.Error("no CreateLine request found")
	}
}

func TestRender_StraightConnectorForForwardEdge(t *testing.T) {
	d := &PositionedDiagram{
		PageID: "straight_test",
		Nodes: []PositionedNode{
			{ID: "a", Label: "A", Shape: "rectangle", X: 100, Y: 100, Width: 200, Height: 100},
			{ID: "b", Label: "B", Shape: "rectangle", X: 100, Y: 500, Width: 200, Height: 100},
		},
		Edges: []PositionedEdge{
			// Forward edge: top -> bottom
			{From: "a", To: "b", LineStyle: "arrow", StartX: 200, StartY: 200, EndX: 200, EndY: 500},
		},
	}

	reqs := Render(d)

	for _, r := range reqs {
		if r.CreateLine == nil {
			continue
		}
		if r.CreateLine.Category != "STRAIGHT" {
			t.Errorf("forward edge Category = %q, want STRAIGHT", r.CreateLine.Category)
		}
	}
}
