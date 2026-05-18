package diagram

import (
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestLayout_SimpleChain(t *testing.T) {
	spec := &model.DiagramSpec{
		Title:      "Test",
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "Start"},
			{ID: "b", Label: "Process"},
			{ID: "c", Label: "End"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
		},
	}

	result, err := Layout(spec, "page1")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result.Nodes))
	}
	if len(result.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(result.Edges))
	}

	nodeY := make(map[string]int64)
	for _, n := range result.Nodes {
		nodeY[n.ID] = n.Y
	}
	if nodeY["a"] >= nodeY["b"] {
		t.Errorf("node a (Y=%d) should be above node b (Y=%d)", nodeY["a"], nodeY["b"])
	}
	if nodeY["b"] >= nodeY["c"] {
		t.Errorf("node b (Y=%d) should be above node c (Y=%d)", nodeY["b"], nodeY["c"])
	}
}

func TestLayout_LeftToRight(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "left-to-right",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "Input"},
			{ID: "b", Label: "Output"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "b"},
		},
	}

	result, err := Layout(spec, "page2")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	nodeX := make(map[string]int64)
	for _, n := range result.Nodes {
		nodeX[n.ID] = n.X
	}
	if nodeX["a"] >= nodeX["b"] {
		t.Errorf("node a (X=%d) should be left of node b (X=%d)", nodeX["a"], nodeX["b"])
	}
}

func TestLayout_NoEdges(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
		},
	}

	result, err := Layout(spec, "page3")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result.Nodes))
	}
}

func TestLayout_EmptyNodes(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes:      nil,
	}

	_, err := Layout(spec, "page4")
	if err == nil {
		t.Fatal("expected error for empty nodes")
	}
}

func TestLayout_WithGroups(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "left-to-right",
		Nodes: []model.DiagramNode{
			{ID: "fe1", Label: "React App"},
			{ID: "fe2", Label: "Mobile App"},
			{ID: "api", Label: "API Gateway"},
			{ID: "db", Label: "Database"},
		},
		Edges: []model.DiagramEdge{
			{From: "fe1", To: "api"},
			{From: "fe2", To: "api"},
			{From: "api", To: "db"},
		},
		Groups: []model.DiagramGroup{
			{ID: "frontend", Label: "Frontend", Nodes: []string{"fe1", "fe2"}},
		},
	}

	result, err := Layout(spec, "page5")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}

	g := result.Groups[0]
	if g.Width <= 0 || g.Height <= 0 {
		t.Errorf("group bounds invalid: W=%d H=%d", g.Width, g.Height)
	}

	for _, n := range result.Nodes {
		if n.ID == "fe1" || n.ID == "fe2" {
			if n.X < g.X || n.X+n.Width > g.X+g.Width {
				t.Errorf("node %s (X=%d, W=%d) outside group X bounds (%d, %d)", n.ID, n.X, n.Width, g.X, g.X+g.Width)
			}
			if n.Y < g.Y || n.Y+n.Height > g.Y+g.Height {
				t.Errorf("node %s (Y=%d, H=%d) outside group Y bounds (%d, %d)", n.ID, n.Y, n.Height, g.Y, g.Y+g.Height)
			}
		}
	}
}

func TestLayout_BranchingDAG(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "start", Label: "Start"},
			{ID: "check", Label: "Check"},
			{ID: "yes", Label: "Yes Path"},
			{ID: "no", Label: "No Path"},
			{ID: "end", Label: "End"},
		},
		Edges: []model.DiagramEdge{
			{From: "start", To: "check"},
			{From: "check", To: "yes"},
			{From: "check", To: "no"},
			{From: "yes", To: "end"},
			{From: "no", To: "end"},
		},
	}

	result, err := Layout(spec, "page6")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	nodeY := make(map[string]int64)
	for _, n := range result.Nodes {
		nodeY[n.ID] = n.Y
	}

	if nodeY["start"] >= nodeY["check"] {
		t.Error("start should be above check")
	}
	if nodeY["check"] >= nodeY["yes"] || nodeY["check"] >= nodeY["no"] {
		t.Error("check should be above yes and no")
	}
	if nodeY["yes"] >= nodeY["end"] || nodeY["no"] >= nodeY["end"] {
		t.Error("yes and no should be above end")
	}
}

func TestLayout_NoOverlap(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "A"}, {ID: "b", Label: "B"},
			{ID: "c", Label: "C"}, {ID: "d", Label: "D"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "c"}, {From: "b", To: "d"},
		},
	}

	result, err := Layout(spec, "page7")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	for i, n1 := range result.Nodes {
		for j, n2 := range result.Nodes {
			if i >= j {
				continue
			}
			if overlaps(n1, n2) {
				t.Errorf("nodes %s and %s overlap: (%d,%d,%d,%d) vs (%d,%d,%d,%d)",
					n1.ID, n2.ID,
					n1.X, n1.Y, n1.X+n1.Width, n1.Y+n1.Height,
					n2.X, n2.Y, n2.X+n2.Width, n2.Y+n2.Height)
			}
		}
	}
}

func overlaps(a, b PositionedNode) bool {
	if a.X >= b.X+b.Width || b.X >= a.X+a.Width {
		return false
	}
	if a.Y >= b.Y+b.Height || b.Y >= a.Y+a.Height {
		return false
	}
	return true
}
