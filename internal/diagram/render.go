package diagram

import (
	"fmt"

	"google.golang.org/api/slides/v1"
)

// Render converts a PositionedDiagram into Google Slides API requests.
// The returned requests should be executed in a single BatchUpdate call.
func Render(d *PositionedDiagram) []*slides.Request {
	var reqs []*slides.Request

	for _, g := range d.Groups {
		reqs = append(reqs, renderGroup(d.PageID, g)...)
	}

	nodeObjIDs := make(map[string]string, len(d.Nodes))
	nodeByID := make(map[string]PositionedNode, len(d.Nodes))
	for i, n := range d.Nodes {
		reqs = append(reqs, renderNode(d.PageID, n, i)...)
		nodeObjIDs[n.ID] = fmt.Sprintf("diag_%s_node_%d", d.PageID, i)
		nodeByID[n.ID] = n
	}

	for i, e := range d.Edges {
		reqs = append(reqs, renderEdge(d.PageID, e, d, i, nodeObjIDs, nodeByID)...)
	}

	return reqs
}

func renderNode(pageID string, n PositionedNode, idx int) []*slides.Request {
	objID := fmt.Sprintf("diag_%s_node_%d", pageID, idx)
	shapeType := goShapeType(n.Shape)
	s := LookupStyle(n.Style)

	fontSize := s.FontSize
	if hScale := float64(n.Height) / float64(DefaultNodeHeight); hScale < 1 {
		fontSize = s.FontSize * hScale
	}
	// Scale down further if the label is too wide for the node.
	// Approximate: each character at 11pt ≈ 100,000 EMU width.
	// With internal padding (~10%), usable width is 80% of node width.
	if len(n.Label) > 0 {
		charWidthEMU := fontSize / 11.0 * 100000
		textWidthEMU := charWidthEMU * float64(len(n.Label))
		usableWidth := float64(n.Width) * 0.8
		if textWidthEMU > usableWidth {
			wScale := usableWidth / textWidthEMU
			fontSize = fontSize * wScale
		}
	}
	if fontSize < 7 {
		fontSize = 7
	}

	reqs := []*slides.Request{
		{
			CreateShape: &slides.CreateShapeRequest{
				ObjectId:  objID,
				ShapeType: shapeType,
				ElementProperties: &slides.PageElementProperties{
					PageObjectId: pageID,
					Size: &slides.Size{
						Width:  &slides.Dimension{Magnitude: float64(n.Width), Unit: "EMU"},
						Height: &slides.Dimension{Magnitude: float64(n.Height), Unit: "EMU"},
					},
					Transform: &slides.AffineTransform{
						ScaleX:     1,
						ScaleY:     1,
						TranslateX: float64(n.X),
						TranslateY: float64(n.Y),
						Unit:       "EMU",
					},
				},
			},
		},
		{
			InsertText: &slides.InsertTextRequest{
				ObjectId:       objID,
				Text:           n.Label,
				InsertionIndex: 0,
			},
		},
	}

	// Text style and paragraph must come BEFORE ShapeProperties so that
	// TEXT_AUTOFIT is not reset by the text mutations.
	reqs = append(reqs, &slides.Request{
		UpdateTextStyle: &slides.UpdateTextStyleRequest{
			ObjectId: objID,
			Style: &slides.TextStyle{
				FontFamily: s.FontFamily,
				FontSize:   &slides.Dimension{Magnitude: fontSize, Unit: "PT"},
				ForegroundColor: &slides.OptionalColor{
					OpaqueColor: &slides.OpaqueColor{
						RgbColor: &slides.RgbColor{
							Red: s.TextR, Green: s.TextG, Blue: s.TextB,
						},
					},
				},
				Bold: false,
			},
			TextRange: &slides.Range{Type: "ALL"},
			Fields:    "fontFamily,fontSize,foregroundColor,bold",
		},
	})

	reqs = append(reqs, &slides.Request{
		UpdateParagraphStyle: &slides.UpdateParagraphStyleRequest{
			ObjectId: objID,
			Style: &slides.ParagraphStyle{
				Alignment: "CENTER",
			},
			TextRange: &slides.Range{Type: "ALL"},
			Fields:    "alignment",
		},
	})

	shapeProps := &slides.ShapeProperties{
		ContentAlignment: "MIDDLE",
		Outline: &slides.Outline{
			Weight: &slides.Dimension{Magnitude: 1.5, Unit: "PT"},
			OutlineFill: &slides.OutlineFill{
				SolidFill: &slides.SolidFill{
					Color: &slides.OpaqueColor{
						RgbColor: &slides.RgbColor{
							Red: s.OutlineR, Green: s.OutlineG, Blue: s.OutlineB,
						},
					},
				},
			},
		},
	}
	fields := "outline,contentAlignment"

	if s.HasFill {
		shapeProps.ShapeBackgroundFill = &slides.ShapeBackgroundFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red: s.FillR, Green: s.FillG, Blue: s.FillB,
					},
				},
				Alpha: s.FillAlpha,
			},
		}
		fields += ",shapeBackgroundFill"
	}

	reqs = append(reqs, &slides.Request{
		UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
			ObjectId:        objID,
			ShapeProperties: shapeProps,
			Fields:          fields,
		},
	})

	return reqs
}

func renderEdge(pageID string, e PositionedEdge, d *PositionedDiagram, idx int, nodeObjIDs map[string]string, nodeByID map[string]PositionedNode) []*slides.Request {
	objID := fmt.Sprintf("diag_%s_edge_%d", pageID, idx)

	s := LookupStyle("primary")

	category := "STRAIGHT"
	fromNode, fromExists := nodeByID[e.From]
	toNode, toExists := nodeByID[e.To]
	if fromExists && toExists {
		backward := (toNode.Y < fromNode.Y && fromNode.X == toNode.X) ||
			(toNode.X < fromNode.X && fromNode.Y == toNode.Y)
		if backward {
			category = "BENT"
		}
	}

	req := &slides.Request{
		CreateLine: &slides.CreateLineRequest{
			ObjectId: objID,
			Category: category,
			ElementProperties: &slides.PageElementProperties{
				PageObjectId: pageID,
				Size: &slides.Size{
					Width:  &slides.Dimension{Magnitude: float64(max64(abs64(e.EndX-e.StartX), 1)), Unit: "EMU"},
					Height: &slides.Dimension{Magnitude: float64(max64(abs64(e.EndY-e.StartY), 1)), Unit: "EMU"},
				},
				Transform: &slides.AffineTransform{
					ScaleX:     scaleForLine(e.StartX, e.EndX),
					ScaleY:     scaleForLine(e.StartY, e.EndY),
					TranslateX: float64(min64(e.StartX, e.EndX)),
					TranslateY: float64(min64(e.StartY, e.EndY)),
					Unit:       "EMU",
				},
			},
		},
	}

	var reqs []*slides.Request
	reqs = append(reqs, req)

	lineProps := &slides.LineProperties{
		LineFill: &slides.LineFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red: s.OutlineR, Green: s.OutlineG, Blue: s.OutlineB,
					},
				},
			},
		},
		Weight: &slides.Dimension{Magnitude: 1.5, Unit: "PT"},
	}
	fields := "lineFill,weight"

	if e.LineStyle == "arrow" || e.LineStyle == "dashed_arrow" {
		lineProps.EndArrow = "OPEN_ARROW"
		fields += ",endArrow"
	}
	if e.LineStyle == "dashed_arrow" || e.LineStyle == "dashed_line" {
		lineProps.DashStyle = "DASH"
		fields += ",dashStyle"
	}

	fromObjID, fromOK := nodeObjIDs[e.From]
	toObjID, toOK := nodeObjIDs[e.To]
	if fromOK && toOK && fromExists && toExists {
		startSite, endSite := computeConnectionSites(fromNode, toNode)

		lineProps.StartConnection = &slides.LineConnection{
			ConnectedObjectId:   fromObjID,
			ConnectionSiteIndex: startSite,
			ForceSendFields:     []string{"ConnectionSiteIndex"},
		}
		lineProps.EndConnection = &slides.LineConnection{
			ConnectedObjectId:   toObjID,
			ConnectionSiteIndex: endSite,
			ForceSendFields:     []string{"ConnectionSiteIndex"},
		}
		fields += ",startConnection,endConnection"
	}

	reqs = append(reqs, &slides.Request{
		UpdateLineProperties: &slides.UpdateLinePropertiesRequest{
			ObjectId:       objID,
			LineProperties: lineProps,
			Fields:         fields,
		},
	})

	if e.Label != "" {
		reqs = append(reqs, renderEdgeLabel(pageID, e, idx)...)
	}

	return reqs
}

func renderEdgeLabel(pageID string, e PositionedEdge, idx int) []*slides.Request {
	objID := fmt.Sprintf("diag_%s_elabel_%d", pageID, idx)

	labelX := e.LabelX
	labelY := e.LabelY
	if labelX == 0 && labelY == 0 {
		labelX = (e.StartX + e.EndX) / 2
		labelY = (e.StartY + e.EndY) / 2
	}

	w := EdgeLabelWidth
	h := EdgeLabelHeight

	return []*slides.Request{
		{
			CreateShape: &slides.CreateShapeRequest{
				ObjectId:  objID,
				ShapeType: "TEXT_BOX",
				ElementProperties: &slides.PageElementProperties{
					PageObjectId: pageID,
					Size: &slides.Size{
						Width:  &slides.Dimension{Magnitude: float64(w), Unit: "EMU"},
						Height: &slides.Dimension{Magnitude: float64(h), Unit: "EMU"},
					},
					Transform: &slides.AffineTransform{
						ScaleX:     1,
						ScaleY:     1,
						TranslateX: float64(labelX - w/2),
						TranslateY: float64(labelY - h/2),
						Unit:       "EMU",
					},
				},
			},
		},
		{
			InsertText: &slides.InsertTextRequest{
				ObjectId: objID, Text: e.Label, InsertionIndex: 0,
			},
		},
		{
			UpdateTextStyle: &slides.UpdateTextStyleRequest{
				ObjectId: objID,
				Style: &slides.TextStyle{
					FontFamily: "Roboto",
					FontSize:   &slides.Dimension{Magnitude: 8, Unit: "PT"},
					ForegroundColor: &slides.OptionalColor{
						OpaqueColor: &slides.OpaqueColor{
							RgbColor: &slides.RgbColor{Red: 0.4, Green: 0.4, Blue: 0.4},
						},
					},
				},
				TextRange: &slides.Range{Type: "ALL"},
				Fields:    "fontFamily,fontSize,foregroundColor",
			},
		},
		{
			UpdateParagraphStyle: &slides.UpdateParagraphStyleRequest{
				ObjectId:  objID,
				Style:     &slides.ParagraphStyle{Alignment: "CENTER"},
				TextRange: &slides.Range{Type: "ALL"},
				Fields:    "alignment",
			},
		},
	}
}

func renderGroup(pageID string, g PositionedGroup) []*slides.Request {
	objID := fmt.Sprintf("diag_%s_group_%s", pageID, g.ID)
	s := LookupGroupStyle(g.Style)

	reqs := []*slides.Request{
		{
			CreateShape: &slides.CreateShapeRequest{
				ObjectId:  objID,
				ShapeType: "ROUND_RECTANGLE",
				ElementProperties: &slides.PageElementProperties{
					PageObjectId: pageID,
					Size: &slides.Size{
						Width:  &slides.Dimension{Magnitude: float64(g.Width), Unit: "EMU"},
						Height: &slides.Dimension{Magnitude: float64(g.Height), Unit: "EMU"},
					},
					Transform: &slides.AffineTransform{
						ScaleX: 1, ScaleY: 1,
						TranslateX: float64(g.X), TranslateY: float64(g.Y),
						Unit: "EMU",
					},
				},
			},
		},
	}

	shapeProps := &slides.ShapeProperties{
		Outline: &slides.Outline{
			Weight:    &slides.Dimension{Magnitude: 1, Unit: "PT"},
			DashStyle: "DASH",
			OutlineFill: &slides.OutlineFill{
				SolidFill: &slides.SolidFill{
					Color: &slides.OpaqueColor{
						RgbColor: &slides.RgbColor{
							Red: s.OutlineR, Green: s.OutlineG, Blue: s.OutlineB,
						},
					},
				},
			},
		},
	}
	fields := "outline"

	if s.HasFill {
		shapeProps.ShapeBackgroundFill = &slides.ShapeBackgroundFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red: s.FillR, Green: s.FillG, Blue: s.FillB,
					},
				},
				Alpha: s.FillAlpha,
			},
		}
		fields += ",shapeBackgroundFill"
	}

	reqs = append(reqs, &slides.Request{
		UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
			ObjectId:        objID,
			ShapeProperties: shapeProps,
			Fields:          fields,
		},
	})

	if g.Label != "" {
		reqs = append(reqs, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId: objID, Text: g.Label, InsertionIndex: 0,
			},
		})
		reqs = append(reqs, &slides.Request{
			UpdateTextStyle: &slides.UpdateTextStyleRequest{
				ObjectId: objID,
				Style: &slides.TextStyle{
					FontFamily: s.FontFamily,
					FontSize:   &slides.Dimension{Magnitude: s.FontSize, Unit: "PT"},
					ForegroundColor: &slides.OptionalColor{
						OpaqueColor: &slides.OpaqueColor{
							RgbColor: &slides.RgbColor{
								Red: s.TextR, Green: s.TextG, Blue: s.TextB,
							},
						},
					},
					Bold: true,
				},
				TextRange: &slides.Range{Type: "ALL"},
				Fields:    "fontFamily,fontSize,foregroundColor,bold",
			},
		})
	}

	return reqs
}

func goShapeType(shape string) string {
	switch shape {
	case "round_rectangle":
		return "ROUND_RECTANGLE"
	case "ellipse":
		return "ELLIPSE"
	case "diamond":
		return "DIAMOND"
	default:
		return "RECTANGLE"
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func scaleForLine(start, end int64) float64 {
	if end >= start {
		return 1
	}
	return -1
}

// Connection site indices verified against Google Slides API:
//
//	Rectangle/RoundRect/Diamond (4 sites): 0=top, 1=left, 2=bottom, 3=right
//	Ellipse (8 sites): 0=top, 2=left, 4=bottom, 6=right
func connectionSite(shape string, side int) int64 {
	// side: 0=top, 1=right, 2=bottom, 3=left
	if shape == "ellipse" {
		return [4]int64{0, 6, 4, 2}[side]
	}
	return [4]int64{0, 3, 2, 1}[side]
}

// computeConnectionSites returns the Google Slides connection site indices
// for the start (source) and end (target) nodes based on their relative
// positions and shape types.
func computeConnectionSites(from, to PositionedNode) (startSite, endSite int64) {
	fromCX := from.X + from.Width/2
	fromCY := from.Y + from.Height/2
	toCX := to.X + to.Width/2
	toCY := to.Y + to.Height/2

	dx := toCX - fromCX
	dy := toCY - fromCY

	var fromSide, toSide int // 0=top, 1=right, 2=bottom, 3=left
	if abs64(dx) > abs64(dy) {
		if dx > 0 {
			fromSide = 1 // right of source
			toSide = 3   // left of target
		} else {
			fromSide = 3
			toSide = 1
		}
	} else {
		if dy > 0 {
			fromSide = 2 // bottom of source
			toSide = 0   // top of target
		} else {
			fromSide = 0
			toSide = 2
		}
	}

	startSite = connectionSite(from.Shape, fromSide)
	endSite = connectionSite(to.Shape, toSide)
	return
}
