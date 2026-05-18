package diagram

import (
	"fmt"

	"github.com/owulveryck/agentigslide/internal/model"
)

const (
	slideWidthEMU  int64 = 9144000
	slideHeightEMU int64 = 6858000

	marginTop    int64 = 914400
	marginBottom int64 = 457200
	marginLeft   int64 = 685800
	marginRight  int64 = 685800

	DefaultNodeWidth  int64 = 1371600
	DefaultNodeHeight int64 = 548640
	nodeGapX          int64 = 457200
	nodeGapY          int64 = 457200

	groupPadding int64 = 228600
)

// PositionedDiagram is the layout result with concrete EMU coordinates.
type PositionedDiagram struct {
	Title  string
	PageID string
	Nodes  []PositionedNode
	Edges  []PositionedEdge
	Groups []PositionedGroup
}

// PositionedNode is a node with computed position and size in EMU.
type PositionedNode struct {
	ID     string
	Label  string
	Shape  string
	Style  string
	X, Y   int64
	Width  int64
	Height int64
}

// PositionedEdge is an edge with computed start/end points in EMU.
type PositionedEdge struct {
	From, To       string
	Label          string
	LineStyle      string
	StartX, StartY int64
	EndX, EndY     int64
}

// PositionedGroup is a group background rectangle with computed bounds.
type PositionedGroup struct {
	ID     string
	Label  string
	Style  string
	X, Y   int64
	Width  int64
	Height int64
}

// Layout computes concrete positions for all diagram elements.
func Layout(spec *model.DiagramSpec, pageID string) (*PositionedDiagram, error) {
	if len(spec.Nodes) == 0 {
		return nil, fmt.Errorf("diagram has no nodes")
	}

	nodeByID := make(map[string]*model.DiagramNode, len(spec.Nodes))
	for i := range spec.Nodes {
		nodeByID[spec.Nodes[i].ID] = &spec.Nodes[i]
	}

	adj := make(map[string][]string)
	inDegree := make(map[string]int)
	for _, n := range spec.Nodes {
		adj[n.ID] = nil
		inDegree[n.ID] = 0
	}
	for _, e := range spec.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		inDegree[e.To]++
	}

	layers := assignLayers(spec.Nodes, adj, inDegree)

	horizontal := spec.LayoutHint == "left-to-right"

	usableW := slideWidthEMU - marginLeft - marginRight
	usableH := slideHeightEMU - marginTop - marginBottom

	numLayers := len(layers)
	maxPerLayer := 0
	for _, layer := range layers {
		if len(layer) > maxPerLayer {
			maxPerLayer = len(layer)
		}
	}

	nodeW := DefaultNodeWidth
	nodeH := DefaultNodeHeight

	if horizontal {
		nodeW, nodeH = fitNodes(numLayers, maxPerLayer, usableW, usableH, nodeW, nodeH)
	} else {
		nodeW, nodeH = fitNodes(maxPerLayer, numLayers, usableW, usableH, nodeW, nodeH)
	}

	positions := make(map[string]PositionedNode)
	var posNodes []PositionedNode

	for layerIdx, layer := range layers {
		for posIdx, nodeID := range layer {
			n := nodeByID[nodeID]
			shape := n.Shape
			if shape == "" {
				shape = "rectangle"
			}
			style := n.Style
			if style == "" {
				style = "neutral"
			}

			var x, y int64
			if horizontal {
				x = marginLeft + int64(layerIdx)*(nodeW+nodeGapX)
				totalH := int64(len(layer))*nodeH + int64(len(layer)-1)*nodeGapY
				startY := marginTop + (usableH-totalH)/2
				y = startY + int64(posIdx)*(nodeH+nodeGapY)
			} else {
				totalW := int64(len(layer))*nodeW + int64(len(layer)-1)*nodeGapX
				startX := marginLeft + (usableW-totalW)/2
				x = startX + int64(posIdx)*(nodeW+nodeGapX)
				y = marginTop + int64(layerIdx)*(nodeH+nodeGapY)
			}

			pn := PositionedNode{
				ID: nodeID, Label: n.Label,
				Shape: shape, Style: style,
				X: x, Y: y, Width: nodeW, Height: nodeH,
			}
			positions[nodeID] = pn
			posNodes = append(posNodes, pn)
		}
	}

	var posEdges []PositionedEdge
	for _, e := range spec.Edges {
		from, okF := positions[e.From]
		to, okT := positions[e.To]
		if !okF || !okT {
			continue
		}
		ls := e.LineStyle
		if ls == "" {
			ls = "arrow"
		}
		sx, sy, ex, ey := computeEdgeEndpoints(from, to, horizontal)
		posEdges = append(posEdges, PositionedEdge{
			From: e.From, To: e.To, Label: e.Label,
			LineStyle: ls,
			StartX:    sx, StartY: sy, EndX: ex, EndY: ey,
		})
	}

	var posGroups []PositionedGroup
	for _, g := range spec.Groups {
		pg := computeGroupBounds(g, positions)
		posGroups = append(posGroups, pg)
	}

	return &PositionedDiagram{
		Title: spec.Title, PageID: pageID,
		Nodes: posNodes, Edges: posEdges, Groups: posGroups,
	}, nil
}

func assignLayers(nodes []model.DiagramNode, adj map[string][]string, inDegree map[string]int) [][]string {
	degree := make(map[string]int, len(inDegree))
	for k, v := range inDegree {
		degree[k] = v
	}

	var layers [][]string
	placed := make(map[string]bool)

	for len(placed) < len(nodes) {
		var layer []string
		for _, n := range nodes {
			if placed[n.ID] {
				continue
			}
			if degree[n.ID] == 0 {
				layer = append(layer, n.ID)
			}
		}
		if len(layer) == 0 {
			for _, n := range nodes {
				if !placed[n.ID] {
					layer = append(layer, n.ID)
					break
				}
			}
		}
		for _, id := range layer {
			placed[id] = true
			for _, next := range adj[id] {
				degree[next]--
			}
		}
		layers = append(layers, layer)
	}
	return layers
}

func fitNodes(cols, rows int, usableW, usableH, nodeW, nodeH int64) (int64, int64) {
	maxW := (usableW - int64(cols-1)*nodeGapX) / int64(cols)
	if maxW < nodeW {
		nodeW = maxW
	}
	maxH := (usableH - int64(rows-1)*nodeGapY) / int64(rows)
	if maxH < nodeH {
		nodeH = maxH
	}
	if nodeW < 457200 {
		nodeW = 457200
	}
	if nodeH < 365760 {
		nodeH = 365760
	}
	return nodeW, nodeH
}

func computeEdgeEndpoints(from, to PositionedNode, horizontal bool) (sx, sy, ex, ey int64) {
	if horizontal {
		sx = from.X + from.Width
		sy = from.Y + from.Height/2
		ex = to.X
		ey = to.Y + to.Height/2
	} else {
		sx = from.X + from.Width/2
		sy = from.Y + from.Height
		ex = to.X + to.Width/2
		ey = to.Y
	}
	return
}

func computeGroupBounds(g model.DiagramGroup, positions map[string]PositionedNode) PositionedGroup {
	var minX, minY, maxX, maxY int64
	first := true
	for _, nid := range g.Nodes {
		pn, ok := positions[nid]
		if !ok {
			continue
		}
		if first {
			minX, minY = pn.X, pn.Y
			maxX, maxY = pn.X+pn.Width, pn.Y+pn.Height
			first = false
		} else {
			if pn.X < minX {
				minX = pn.X
			}
			if pn.Y < minY {
				minY = pn.Y
			}
			if pn.X+pn.Width > maxX {
				maxX = pn.X + pn.Width
			}
			if pn.Y+pn.Height > maxY {
				maxY = pn.Y + pn.Height
			}
		}
	}
	style := g.Style
	if style == "" {
		style = ""
	}
	return PositionedGroup{
		ID:     g.ID,
		Label:  g.Label,
		Style:  style,
		X:      minX - groupPadding,
		Y:      minY - groupPadding,
		Width:  (maxX - minX) + 2*groupPadding,
		Height: (maxY - minY) + 2*groupPadding,
	}
}
