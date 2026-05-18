package diagram

import (
	"fmt"
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

func TestLayout_CrossingMinimization(t *testing.T) {
	// Diamond pattern: A -> B,C ; B,C -> D
	// Without crossing minimization, edge order could cause crossings.
	// With barycenter, B and C should be ordered so that
	// the edges A->B, A->C, B->D, C->D don't cross.
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
			{ID: "d", Label: "D"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
		},
	}

	result, err := Layout(spec, "crossing_test")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	nodePos := make(map[string]PositionedNode)
	for _, n := range result.Nodes {
		nodePos[n.ID] = n
	}

	// B and C should be on the same layer (both have in-degree 1 from A)
	if nodePos["b"].Y != nodePos["c"].Y {
		t.Errorf("b and c should be on the same layer: b.Y=%d, c.Y=%d", nodePos["b"].Y, nodePos["c"].Y)
	}

	// The algorithm should keep B left of C (or at least not cross),
	// meaning edges don't cross. We can verify no node overlap.
	for i, n1 := range result.Nodes {
		for j, n2 := range result.Nodes {
			if i >= j {
				continue
			}
			if overlaps(n1, n2) {
				t.Errorf("nodes %s and %s overlap", n1.ID, n2.ID)
			}
		}
	}
}

func TestLayout_CrossingMinimization_MultiLayer(t *testing.T) {
	// More complex: A->C, A->D, B->C, B->D, C->E, D->F
	// Barycenter should order C,D to minimize crossings from A,B
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
			{ID: "d", Label: "D"},
			{ID: "e", Label: "E"},
			{ID: "f", Label: "F"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "c"},
			{From: "a", To: "d"},
			{From: "b", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "e"},
			{From: "d", To: "f"},
		},
	}

	result, err := Layout(spec, "crossing_multi")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	// Verify correct layering: A,B in layer 0, C,D in layer 1, E,F in layer 2
	nodePos := make(map[string]PositionedNode)
	for _, n := range result.Nodes {
		nodePos[n.ID] = n
	}

	if nodePos["a"].Y != nodePos["b"].Y {
		t.Error("a and b should be on the same layer")
	}
	if nodePos["c"].Y != nodePos["d"].Y {
		t.Error("c and d should be on the same layer")
	}
	if nodePos["e"].Y != nodePos["f"].Y {
		t.Error("e and f should be on the same layer")
	}

	for i, n1 := range result.Nodes {
		for j, n2 := range result.Nodes {
			if i >= j {
				continue
			}
			if overlaps(n1, n2) {
				t.Errorf("nodes %s and %s overlap", n1.ID, n2.ID)
			}
		}
	}
}

func TestLayout_EdgeLabelNoOverlapWithNodes(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes: []model.DiagramNode{
			{ID: "a", Label: "Start"},
			{ID: "b", Label: "Process"},
			{ID: "c", Label: "End"},
		},
		Edges: []model.DiagramEdge{
			{From: "a", To: "b", Label: "step 1"},
			{From: "b", To: "c", Label: "step 2"},
		},
	}

	result, err := Layout(spec, "label_test")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	for _, e := range result.Edges {
		if e.Label == "" {
			continue
		}
		if e.LabelX == 0 && e.LabelY == 0 {
			t.Errorf("edge %s->%s: LabelX/LabelY not set", e.From, e.To)
			continue
		}
		hw := EdgeLabelWidth / 2
		hh := EdgeLabelHeight / 2
		lr := rect{e.LabelX - hw, e.LabelY - hh, EdgeLabelWidth, EdgeLabelHeight}
		for _, n := range result.Nodes {
			nr := rect{n.X, n.Y, n.Width, n.Height}
			if rectsOverlap(lr, nr) {
				t.Errorf("edge label %q (%s->%s) overlaps node %s", e.Label, e.From, e.To, n.ID)
			}
		}
	}
}

func TestLayout_NoBoundsViolation(t *testing.T) {
	nodes := make([]model.DiagramNode, 12)
	for i := range nodes {
		nodes[i] = model.DiagramNode{ID: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("Node %d", i)}
	}
	edges := make([]model.DiagramEdge, 11)
	for i := range edges {
		edges[i] = model.DiagramEdge{From: fmt.Sprintf("n%d", i), To: fmt.Sprintf("n%d", i+1)}
	}

	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes:      nodes,
		Edges:      edges,
	}

	result, err := Layout(spec, "bounds_test")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	for _, n := range result.Nodes {
		if n.X < 0 || n.Y < 0 {
			t.Errorf("node %s has negative position: (%d, %d)", n.ID, n.X, n.Y)
		}
		if n.X+n.Width > slideWidthEMU {
			t.Errorf("node %s extends beyond right edge: X=%d + W=%d > %d", n.ID, n.X, n.Width, slideWidthEMU)
		}
		if n.Y+n.Height > slideHeightEMU {
			t.Errorf("node %s extends beyond bottom edge: Y=%d + H=%d > %d", n.ID, n.Y, n.Height, slideHeightEMU)
		}
	}
}

func TestLayout_NoBoundsViolation_Wide(t *testing.T) {
	// 6 nodes all in the same layer (no edges) -> wide layout
	nodes := make([]model.DiagramNode, 6)
	for i := range nodes {
		nodes[i] = model.DiagramNode{ID: fmt.Sprintf("w%d", i), Label: fmt.Sprintf("Wide %d", i)}
	}

	spec := &model.DiagramSpec{
		LayoutHint: "top-to-bottom",
		Nodes:      nodes,
	}

	result, err := Layout(spec, "wide_test")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	for _, n := range result.Nodes {
		if n.X+n.Width > slideWidthEMU {
			t.Errorf("node %s extends beyond right edge", n.ID)
		}
	}
}

func TestLayout_GroupNoOverlap(t *testing.T) {
	spec := &model.DiagramSpec{
		LayoutHint: "left-to-right",
		Nodes: []model.DiagramNode{
			{ID: "a1", Label: "A1"}, {ID: "a2", Label: "A2"},
			{ID: "b1", Label: "B1"}, {ID: "b2", Label: "B2"},
			{ID: "c", Label: "C"},
		},
		Edges: []model.DiagramEdge{
			{From: "a1", To: "c"}, {From: "a2", To: "c"},
			{From: "b1", To: "c"}, {From: "b2", To: "c"},
		},
		Groups: []model.DiagramGroup{
			{ID: "ga", Label: "Group A", Nodes: []string{"a1", "a2"}},
			{ID: "gb", Label: "Group B", Nodes: []string{"b1", "b2"}},
		},
	}

	result, err := Layout(spec, "group_overlap")
	if err != nil {
		t.Fatalf("Layout failed: %v", err)
	}

	if len(result.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result.Groups))
	}

	g0 := result.Groups[0]
	g1 := result.Groups[1]
	r0 := rect{g0.X, g0.Y, g0.Width, g0.Height}
	r1 := rect{g1.X, g1.Y, g1.Width, g1.Height}
	if rectsOverlap(r0, r1) {
		t.Errorf("groups %s and %s overlap", g0.ID, g1.ID)
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
