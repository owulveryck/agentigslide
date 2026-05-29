package diagram

import (
	"fmt"
	"math"
	"sort"

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

	EdgeLabelWidth  int64 = 685800
	EdgeLabelHeight int64 = 274320
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
	Size   string
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
	LabelX, LabelY int64
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
// When there are 4 or more groups, it switches to poster mode where each
// group gets its own region of the slide arranged in a grid.
func Layout(spec *model.DiagramSpec, pageID string) (*PositionedDiagram, error) {
	if len(spec.Nodes) == 0 {
		return nil, fmt.Errorf("diagram has no nodes")
	}

	if len(spec.Groups) >= 4 {
		return layoutPoster(spec, pageID)
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

	reverseAdj := make(map[string][]string)
	for _, e := range spec.Edges {
		reverseAdj[e.To] = append(reverseAdj[e.To], e.From)
	}

	layers := assignLayers(spec.Nodes, adj, inDegree)
	layers = minimizeCrossings(layers, adj, reverseAdj)

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

			nw, nh := applySize(nodeW, nodeH, n.Size)
			pn := PositionedNode{
				ID: nodeID, Label: n.Label,
				Shape: shape, Style: style, Size: n.Size,
				X: x, Y: y, Width: nw, Height: nh,
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

	adjustEdgeLabels(posEdges, posNodes)

	var posGroups []PositionedGroup
	for _, g := range spec.Groups {
		pg := computeGroupBounds(g, positions)
		posGroups = append(posGroups, pg)
	}

	pd := &PositionedDiagram{
		Title: spec.Title, PageID: pageID,
		Nodes: posNodes, Edges: posEdges, Groups: posGroups,
	}
	validateLayout(pd)
	return pd, nil
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

func minimizeCrossings(layers [][]string, adj, reverseAdj map[string][]string) [][]string {
	if len(layers) < 2 {
		return layers
	}

	posOf := make(map[string]int)
	rebuildPos := func() {
		for _, layer := range layers {
			for i, id := range layer {
				posOf[id] = i
			}
		}
	}
	rebuildPos()

	for iter := 0; iter < 4; iter++ {
		// Forward sweep: reorder each layer based on predecessor positions
		for li := 1; li < len(layers); li++ {
			bary := make(map[string]float64)
			for _, id := range layers[li] {
				preds := reverseAdj[id]
				if len(preds) == 0 {
					bary[id] = float64(posOf[id])
					continue
				}
				sum := 0.0
				for _, p := range preds {
					sum += float64(posOf[p])
				}
				bary[id] = sum / float64(len(preds))
			}
			sort.SliceStable(layers[li], func(i, j int) bool {
				return bary[layers[li][i]] < bary[layers[li][j]]
			})
			rebuildPos()
		}

		// Backward sweep: reorder each layer based on successor positions
		for li := len(layers) - 2; li >= 0; li-- {
			bary := make(map[string]float64)
			for _, id := range layers[li] {
				succs := adj[id]
				if len(succs) == 0 {
					bary[id] = float64(posOf[id])
					continue
				}
				sum := 0.0
				for _, s := range succs {
					sum += float64(posOf[s])
				}
				bary[id] = sum / float64(len(succs))
			}
			sort.SliceStable(layers[li], func(i, j int) bool {
				return bary[layers[li][i]] < bary[layers[li][j]]
			})
			rebuildPos()
		}
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
	if nodeW < 365760 {
		nodeW = 365760
	}
	if nodeH < 274320 {
		nodeH = 274320
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

func validateLayout(pd *PositionedDiagram) {
	clampToBounds(pd)
	resolveGroupOverlaps(pd)
}

func clampToBounds(pd *PositionedDiagram) {
	maxX := slideWidthEMU - marginRight
	maxY := slideHeightEMU - marginBottom

	for i := range pd.Nodes {
		n := &pd.Nodes[i]
		if n.X < marginLeft {
			n.X = marginLeft
		}
		if n.Y < marginTop {
			n.Y = marginTop
		}
		if n.X+n.Width > maxX {
			n.X = maxX - n.Width
		}
		if n.Y+n.Height > maxY {
			n.Y = maxY - n.Height
		}
	}

	for i := range pd.Groups {
		g := &pd.Groups[i]
		if g.X < 0 {
			g.X = 0
		}
		if g.Y < 0 {
			g.Y = 0
		}
		if g.X+g.Width > slideWidthEMU {
			g.X = slideWidthEMU - g.Width
		}
		if g.Y+g.Height > slideHeightEMU {
			g.Y = slideHeightEMU - g.Height
		}
	}
}

func resolveGroupOverlaps(pd *PositionedDiagram) {
	for i := 0; i < len(pd.Groups); i++ {
		for j := i + 1; j < len(pd.Groups); j++ {
			gi := rect{pd.Groups[i].X, pd.Groups[i].Y, pd.Groups[i].Width, pd.Groups[i].Height}
			gj := rect{pd.Groups[j].X, pd.Groups[j].Y, pd.Groups[j].Width, pd.Groups[j].Height}
			if !rectsOverlap(gi, gj) {
				continue
			}
			overlapX := (gi.X + gi.W) - gj.X
			overlapY := (gi.Y + gi.H) - gj.Y
			if overlapX > 0 && overlapX < overlapY {
				pd.Groups[j].X += overlapX + groupPadding
			} else if overlapY > 0 {
				pd.Groups[j].Y += overlapY + groupPadding
			}
		}
	}
}

func applySize(baseW, baseH int64, size string) (int64, int64) {
	switch size {
	case "small":
		return baseW * 6 / 10, baseH * 6 / 10
	case "large":
		return baseW * 14 / 10, baseH * 14 / 10
	case "wide":
		return baseW * 2, baseH * 6 / 10
	default:
		return baseW, baseH
	}
}

type rect struct{ X, Y, W, H int64 }

func rectsOverlap(a, b rect) bool {
	if a.X >= b.X+b.W || b.X >= a.X+a.W {
		return false
	}
	if a.Y >= b.Y+b.H || b.Y >= a.Y+a.H {
		return false
	}
	return true
}

func adjustEdgeLabels(edges []PositionedEdge, nodes []PositionedNode) {
	perpOffset := int64(137160) // ~0.15 inch perpendicular offset
	hw := EdgeLabelWidth / 2
	hh := EdgeLabelHeight / 2

	for i := range edges {
		e := &edges[i]
		if e.Label == "" {
			continue
		}

		midX := (e.StartX + e.EndX) / 2
		midY := (e.StartY + e.EndY) / 2

		dx := float64(e.EndX - e.StartX)
		dy := float64(e.EndY - e.StartY)
		length := math.Sqrt(dx*dx + dy*dy)
		if length > 0 {
			midX += int64(-dy / length * float64(perpOffset))
			midY += int64(dx / length * float64(perpOffset))
		}

		labelRect := rect{midX - hw, midY - hh, EdgeLabelWidth, EdgeLabelHeight}

		collides := false
		for _, n := range nodes {
			if rectsOverlap(labelRect, rect{n.X, n.Y, n.Width, n.Height}) {
				collides = true
				break
			}
		}

		if collides {
			midX = e.StartX + int64(dx/3)
			midY = e.StartY + int64(dy/3)
			if length > 0 {
				midX += int64(-dy / length * float64(perpOffset))
				midY += int64(dx / length * float64(perpOffset))
			}
			labelRect = rect{midX - hw, midY - hh, EdgeLabelWidth, EdgeLabelHeight}

			stillCollides := false
			for _, n := range nodes {
				if rectsOverlap(labelRect, rect{n.X, n.Y, n.Width, n.Height}) {
					stillCollides = true
					break
				}
			}
			if stillCollides {
				midX = e.StartX + int64(2*dx/3)
				midY = e.StartY + int64(2*dy/3)
				if length > 0 {
					midX += int64(-dy / length * float64(perpOffset))
					midY += int64(dx / length * float64(perpOffset))
				}
			}
		}

		e.LabelX = midX
		e.LabelY = midY
	}
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

// layoutPoster arranges groups in a grid and places nodes within each
// group's region. Designed for poster/infographic layouts with 4+ groups.
func layoutPoster(spec *model.DiagramSpec, pageID string) (*PositionedDiagram, error) {
	nodeByID := make(map[string]*model.DiagramNode, len(spec.Nodes))
	for i := range spec.Nodes {
		nodeByID[spec.Nodes[i].ID] = &spec.Nodes[i]
	}

	// Build group membership: node ID → group index
	nodeGroup := make(map[string]int)
	for gi, g := range spec.Groups {
		for _, nid := range g.Nodes {
			nodeGroup[nid] = gi
		}
	}

	// Compute grid dimensions for groups
	nGroups := len(spec.Groups)
	cols := int(math.Ceil(math.Sqrt(float64(nGroups))))
	rows := int(math.Ceil(float64(nGroups) / float64(cols)))

	slideMargin := int64(114300) // ~0.125 inch margin around the entire poster
	titleReserve := int64(0)
	if spec.Title != "" {
		titleReserve = 457200 // ~0.5 inch for the title bar
	}
	availW := slideWidthEMU - 2*slideMargin
	availH := slideHeightEMU - 2*slideMargin - titleReserve
	cellW := availW / int64(cols)
	cellH := availH / int64(rows)
	cellPad := int64(57150) // ~0.0625 inch padding inside each cell

	// Assign each group to a grid cell
	type gridCell struct {
		x, y, w, h int64
	}
	groupCells := make([]gridCell, nGroups)
	for gi := range spec.Groups {
		col := gi % cols
		row := gi / cols
		groupCells[gi] = gridCell{
			x: slideMargin + int64(col)*cellW + cellPad,
			y: slideMargin + titleReserve + int64(row)*cellH + cellPad,
			w: cellW - 2*cellPad,
			h: cellH - 2*cellPad,
		}
	}

	positions := make(map[string]PositionedNode)
	var posNodes []PositionedNode

	// Layout nodes within each group's cell
	for gi, g := range spec.Groups {
		cell := groupCells[gi]

		// Collect group nodes preserving spec order
		var groupNodes []model.DiagramNode
		groupNodeSet := make(map[string]bool)
		for _, nid := range g.Nodes {
			if n, ok := nodeByID[nid]; ok {
				groupNodes = append(groupNodes, *n)
				groupNodeSet[nid] = true
			}
		}
		if len(groupNodes) == 0 {
			continue
		}

		// Reserve space for group label at top
		labelReserve := int64(274320) // ~0.3 inch
		usableX := cell.x
		usableY := cell.y + labelReserve
		usableW := cell.w
		usableH := cell.h - labelReserve

		// Separate wide nodes (bars) from regular nodes.
		// Wide nodes with no incoming edges → top bars.
		// Wide nodes with outgoing edges only → top bars.
		// Wide nodes with no outgoing edges → bottom bars.
		groupAdj := make(map[string][]string)
		groupInDegree := make(map[string]int)
		groupOutDegree := make(map[string]int)
		for _, n := range groupNodes {
			groupAdj[n.ID] = nil
			groupInDegree[n.ID] = 0
			groupOutDegree[n.ID] = 0
		}
		for _, e := range spec.Edges {
			if groupNodeSet[e.From] && groupNodeSet[e.To] {
				groupAdj[e.From] = append(groupAdj[e.From], e.To)
				groupInDegree[e.To]++
				groupOutDegree[e.From]++
			}
		}

		var topBars, bottomBars, regularNodes []model.DiagramNode
		wideBarH := int64(274320) // ~0.3 inch
		wideBarGap := int64(45720)

		for _, n := range groupNodes {
			if n.Size == "wide" {
				if groupOutDegree[n.ID] == 0 {
					bottomBars = append(bottomBars, n)
				} else {
					topBars = append(topBars, n)
				}
			} else {
				regularNodes = append(regularNodes, n)
			}
		}

		// Place top bars
		barY := usableY
		for _, n := range topBars {
			shape := n.Shape
			if shape == "" {
				shape = "rectangle"
			}
			style := n.Style
			if style == "" {
				style = "neutral"
			}
			pn := PositionedNode{
				ID: n.ID, Label: n.Label,
				Shape: shape, Style: style, Size: n.Size,
				X: usableX, Y: barY,
				Width: usableW, Height: wideBarH,
			}
			positions[n.ID] = pn
			posNodes = append(posNodes, pn)
			barY += wideBarH + wideBarGap
		}
		topUsed := barY - usableY

		// Reserve space for bottom bars
		bottomUsed := int64(0)
		if len(bottomBars) > 0 {
			bottomUsed = int64(len(bottomBars))*(wideBarH+wideBarGap) - wideBarGap
		}

		// Layout regular nodes in remaining space
		regY := usableY + topUsed
		regH := usableH - topUsed - bottomUsed
		if len(bottomBars) > 0 {
			regH -= wideBarGap
		}

		if len(regularNodes) > 0 && regH > 0 {
			// Build adj/inDegree for regular nodes only
			regAdj := make(map[string][]string)
			regInDegree := make(map[string]int)
			regSet := make(map[string]bool)
			for _, n := range regularNodes {
				regAdj[n.ID] = nil
				regInDegree[n.ID] = 0
				regSet[n.ID] = true
			}
			for _, e := range spec.Edges {
				if regSet[e.From] && regSet[e.To] {
					regAdj[e.From] = append(regAdj[e.From], e.To)
					regInDegree[e.To]++
				}
			}
			regReverseAdj := make(map[string][]string)
			for _, e := range spec.Edges {
				if regSet[e.From] && regSet[e.To] {
					regReverseAdj[e.To] = append(regReverseAdj[e.To], e.From)
				}
			}

			layers := assignLayers(regularNodes, regAdj, regInDegree)
			layers = minimizeCrossings(layers, regAdj, regReverseAdj)

			numLayers := len(layers)
			maxPerLayer := 0
			for _, layer := range layers {
				if len(layer) > maxPerLayer {
					maxPerLayer = len(layer)
				}
			}

			gapX := int64(91440)
			gapY := int64(57150)

			horizontal := g.LayoutHint == "left-to-right"
			if g.LayoutHint == "" && spec.LayoutHint == "left-to-right" {
				horizontal = true
			}

			var nodeW, nodeH int64
			if horizontal {
				nodeW = (usableW - int64(numLayers-1)*gapX) / int64(numLayers)
				nodeH = (regH - int64(maxPerLayer-1)*gapY) / int64(maxPerLayer)
			} else {
				nodeW = (usableW - int64(maxPerLayer-1)*gapX) / int64(maxPerLayer)
				nodeH = (regH - int64(numLayers-1)*gapY) / int64(numLayers)
			}

			if nodeW > DefaultNodeWidth {
				nodeW = DefaultNodeWidth
			}
			if nodeH > DefaultNodeHeight {
				nodeH = DefaultNodeHeight
			}
			if nodeW < 274320 {
				nodeW = 274320
			}
			if nodeH < 228600 {
				nodeH = 228600
			}

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
						x = usableX + int64(layerIdx)*(nodeW+gapX)
						totalH := int64(len(layer))*nodeH + int64(len(layer)-1)*gapY
						startY := regY + (regH-totalH)/2
						y = startY + int64(posIdx)*(nodeH+gapY)
					} else {
						totalW := int64(len(layer))*nodeW + int64(len(layer)-1)*gapX
						startX := usableX + (usableW-totalW)/2
						x = startX + int64(posIdx)*(nodeW+gapX)
						y = regY + int64(layerIdx)*(nodeH+gapY)
					}

					nw, nh := applySize(nodeW, nodeH, n.Size)
					if shape == "text" && n.Size == "" {
						nh = nh * 6 / 10
					}
					if nw != nodeW {
						x += (nodeW - nw) / 2
					}

					if x+nw > cell.x+cell.w {
						x = cell.x + cell.w - nw
					}
					if y+nh > cell.y+cell.h {
						y = cell.y + cell.h - nh
					}

					pn := PositionedNode{
						ID: nodeID, Label: n.Label,
						Shape: shape, Style: style, Size: n.Size,
						X: x, Y: y, Width: nw, Height: nh,
					}
					positions[nodeID] = pn
					posNodes = append(posNodes, pn)
				}
			}
		}

		// Place bottom bars
		bottomBarY := usableY + usableH - bottomUsed
		for _, n := range bottomBars {
			shape := n.Shape
			if shape == "" {
				shape = "rectangle"
			}
			style := n.Style
			if style == "" {
				style = "neutral"
			}
			pn := PositionedNode{
				ID: n.ID, Label: n.Label,
				Shape: shape, Style: style, Size: n.Size,
				X: usableX, Y: bottomBarY,
				Width: usableW, Height: wideBarH,
			}
			positions[n.ID] = pn
			posNodes = append(posNodes, pn)
			bottomBarY += wideBarH + wideBarGap
		}
	}

	// Place ungrouped nodes at the bottom
	var ungrouped []model.DiagramNode
	for _, n := range spec.Nodes {
		if _, inGroup := nodeGroup[n.ID]; !inGroup {
			ungrouped = append(ungrouped, n)
		}
	}
	if len(ungrouped) > 0 {
		nodeW := DefaultNodeWidth
		nodeH := DefaultNodeHeight / 2
		perRow := int(slideWidthEMU / (nodeW + nodeGapX/2))
		if perRow < 1 {
			perRow = 1
		}
		for i, n := range ungrouped {
			col := i % perRow
			row := i / perRow
			shape := n.Shape
			if shape == "" {
				shape = "rectangle"
			}
			style := n.Style
			if style == "" {
				style = "neutral"
			}
			x := marginLeft + int64(col)*(nodeW+nodeGapX/2)
			y := slideHeightEMU - marginBottom - int64(row+1)*(nodeH+nodeGapY/4)
			pn := PositionedNode{
				ID: n.ID, Label: n.Label,
				Shape: shape, Style: style,
				X: x, Y: y, Width: nodeW, Height: nodeH,
			}
			positions[n.ID] = pn
			posNodes = append(posNodes, pn)
		}
	}

	// Compute edges — only keep intra-group edges in grid mode
	var posEdges []PositionedEdge
	for _, e := range spec.Edges {
		gFrom, okFrom := nodeGroup[e.From]
		gTo, okTo := nodeGroup[e.To]
		if !okFrom || !okTo || gFrom != gTo {
			continue
		}
		from, okF := positions[e.From]
		to, okT := positions[e.To]
		if !okF || !okT {
			continue
		}
		ls := e.LineStyle
		if ls == "" {
			ls = "arrow"
		}
		edgeHoriz := spec.Groups[gFrom].LayoutHint == "left-to-right"
		if spec.Groups[gFrom].LayoutHint == "" && spec.LayoutHint == "left-to-right" {
			edgeHoriz = true
		}
		sx, sy, ex, ey := computeEdgeEndpoints(from, to, edgeHoriz)
		posEdges = append(posEdges, PositionedEdge{
			From: e.From, To: e.To, Label: e.Label,
			LineStyle: ls,
			StartX:    sx, StartY: sy, EndX: ex, EndY: ey,
		})
	}
	adjustEdgeLabels(posEdges, posNodes)

	// Build groups as positioned rectangles matching their grid cells
	var posGroups []PositionedGroup
	for gi, g := range spec.Groups {
		cell := groupCells[gi]
		style := g.Style
		posGroups = append(posGroups, PositionedGroup{
			ID:     g.ID,
			Label:  g.Label,
			Style:  style,
			X:      cell.x,
			Y:      cell.y,
			Width:  cell.w,
			Height: cell.h,
		})
	}

	return &PositionedDiagram{
		Title: spec.Title, PageID: pageID,
		Nodes: posNodes, Edges: posEdges, Groups: posGroups,
	}, nil
}
