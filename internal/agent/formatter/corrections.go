package formatter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/owulveryck/agentigslide/internal/revision"
	"google.golang.org/api/slides/v1"
)

// ValidateCorrections filters a correction plan to keep only corrections that
// reference known element IDs, have a valid type, and include at least one
// style change. Invalid corrections are logged and discarded.
func ValidateCorrections(plan *CorrectionPlan, structure []SlideInfo) []Correction {
	objectIDs := make(map[string]bool)
	for _, slide := range structure {
		for _, elem := range slide.Elements {
			objectIDs[elem.ObjectID] = true
		}
	}

	var valid []Correction
	for _, c := range plan.Corrections {
		if !objectIDs[c.ObjectID] {
			slog.Warn("skipping correction for unknown objectId", "objectID", c.ObjectID)
			continue
		}
		if c.Type != "textStyle" && c.Type != "paragraphStyle" && c.Type != "shapeProperties" {
			slog.Warn("skipping correction with unknown type", "type", c.Type, "objectID", c.ObjectID)
			continue
		}
		if c.Type == "textStyle" && c.FontSizePt == nil && c.FontFamily == nil && c.ForegroundColor == nil && c.Bold == nil && c.Italic == nil && c.Underline == nil && c.Strikethrough == nil {
			slog.Warn("skipping textStyle correction with no changes", "objectID", c.ObjectID)
			continue
		}
		if c.Type == "paragraphStyle" && c.LineSpacing == nil && c.SpaceAbovePt == nil && c.SpaceBelowPt == nil && c.Alignment == nil {
			slog.Warn("skipping paragraphStyle correction with no changes", "objectID", c.ObjectID)
			continue
		}
		if c.Type == "shapeProperties" && c.BackgroundColor == nil && c.ContentAlignmentVal == nil && c.OutlineColor == nil && c.OutlineWeightPt == nil {
			slog.Warn("skipping shapeProperties correction with no changes", "objectID", c.ObjectID)
			continue
		}
		valid = append(valid, c)
	}

	return valid
}

// BuildCorrections converts validated corrections into Google Slides API
// BatchUpdate requests (UpdateTextStyle, UpdateParagraphStyle, or
// UpdateShapeProperties).
func BuildCorrections(corrections []Correction) []*slides.Request {
	var requests []*slides.Request
	for _, c := range corrections {
		switch c.Type {
		case "textStyle":
			requests = append(requests, buildTextStyleRequest(c))
		case "paragraphStyle":
			requests = append(requests, buildParagraphStyleRequest(c))
		case "shapeProperties":
			requests = append(requests, buildShapePropertiesRequest(c))
		}
	}
	return requests
}

func buildTextStyleRequest(c Correction) *slides.Request {
	style := &slides.TextStyle{}
	var fields []string

	if c.FontSizePt != nil {
		style.FontSize = &slides.Dimension{
			Magnitude: *c.FontSizePt,
			Unit:      "PT",
		}
		if *c.FontSizePt == 0 {
			style.FontSize.ForceSendFields = []string{"Magnitude"}
		}
		fields = append(fields, "fontSize")
	}
	if c.FontFamily != nil {
		style.FontFamily = *c.FontFamily
		fields = append(fields, "fontFamily")
	}
	if c.ForegroundColor != nil {
		style.ForegroundColor = &slides.OptionalColor{
			OpaqueColor: &slides.OpaqueColor{
				RgbColor: &slides.RgbColor{
					Red:   c.ForegroundColor.Red,
					Green: c.ForegroundColor.Green,
					Blue:  c.ForegroundColor.Blue,
				},
			},
		}
		fields = append(fields, "foregroundColor")
	}
	if c.Bold != nil {
		style.Bold = *c.Bold
		fields = append(fields, "bold")
	}
	if c.Italic != nil {
		style.Italic = *c.Italic
		fields = append(fields, "italic")
	}
	if c.Underline != nil {
		style.Underline = *c.Underline
		fields = append(fields, "underline")
	}
	if c.Strikethrough != nil {
		style.Strikethrough = *c.Strikethrough
		fields = append(fields, "strikethrough")
	}

	req := &slides.UpdateTextStyleRequest{
		ObjectId: c.ObjectID,
		Style:    style,
		Fields:   strings.Join(fields, ","),
	}

	req.TextRange = buildTextRange(c.StartIndex, c.EndIndex)

	if c.CellLocation != nil {
		req.CellLocation = &slides.TableCellLocation{
			RowIndex:    int64(c.CellLocation.RowIndex),
			ColumnIndex: int64(c.CellLocation.ColumnIndex),
		}
	}

	return &slides.Request{UpdateTextStyle: req}
}

func buildParagraphStyleRequest(c Correction) *slides.Request {
	style := &slides.ParagraphStyle{}
	var fields []string

	if c.LineSpacing != nil {
		style.LineSpacing = *c.LineSpacing
		style.ForceSendFields = append(style.ForceSendFields, "LineSpacing")
		fields = append(fields, "lineSpacing")
	}
	if c.SpaceAbovePt != nil {
		dim := &slides.Dimension{
			Magnitude: *c.SpaceAbovePt,
			Unit:      "PT",
		}
		if *c.SpaceAbovePt == 0 {
			dim.ForceSendFields = []string{"Magnitude"}
		}
		style.SpaceAbove = dim
		fields = append(fields, "spaceAbove")
	}
	if c.SpaceBelowPt != nil {
		dim := &slides.Dimension{
			Magnitude: *c.SpaceBelowPt,
			Unit:      "PT",
		}
		if *c.SpaceBelowPt == 0 {
			dim.ForceSendFields = []string{"Magnitude"}
		}
		style.SpaceBelow = dim
		fields = append(fields, "spaceBelow")
	}
	if c.Alignment != nil {
		style.Alignment = *c.Alignment
		fields = append(fields, "alignment")
	}

	req := &slides.UpdateParagraphStyleRequest{
		ObjectId: c.ObjectID,
		Style:    style,
		Fields:   strings.Join(fields, ","),
	}

	req.TextRange = buildTextRange(c.StartIndex, c.EndIndex)

	if c.CellLocation != nil {
		req.CellLocation = &slides.TableCellLocation{
			RowIndex:    int64(c.CellLocation.RowIndex),
			ColumnIndex: int64(c.CellLocation.ColumnIndex),
		}
	}

	return &slides.Request{UpdateParagraphStyle: req}
}

func buildShapePropertiesRequest(c Correction) *slides.Request {
	props := &slides.ShapeProperties{}
	var fields []string

	if c.BackgroundColor != nil {
		props.ShapeBackgroundFill = &slides.ShapeBackgroundFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red:   c.BackgroundColor.Red,
						Green: c.BackgroundColor.Green,
						Blue:  c.BackgroundColor.Blue,
					},
				},
			},
		}
		fields = append(fields, "shapeBackgroundFill")
	}
	if c.ContentAlignmentVal != nil {
		props.ContentAlignment = *c.ContentAlignmentVal
		fields = append(fields, "contentAlignment")
	}
	if c.OutlineColor != nil {
		if props.Outline == nil {
			props.Outline = &slides.Outline{}
		}
		props.Outline.OutlineFill = &slides.OutlineFill{
			SolidFill: &slides.SolidFill{
				Color: &slides.OpaqueColor{
					RgbColor: &slides.RgbColor{
						Red:   c.OutlineColor.Red,
						Green: c.OutlineColor.Green,
						Blue:  c.OutlineColor.Blue,
					},
				},
			},
		}
		fields = append(fields, "outline")
	}
	if c.OutlineWeightPt != nil {
		if props.Outline == nil {
			props.Outline = &slides.Outline{}
		}
		props.Outline.Weight = &slides.Dimension{
			Magnitude: *c.OutlineWeightPt,
			Unit:      "PT",
		}
		if *c.OutlineWeightPt == 0 {
			props.Outline.Weight.ForceSendFields = []string{"Magnitude"}
		}
		// Only add "outline" to fields if not already present.
		hasOutline := false
		for _, f := range fields {
			if f == "outline" {
				hasOutline = true
				break
			}
		}
		if !hasOutline {
			fields = append(fields, "outline")
		}
	}

	return &slides.Request{
		UpdateShapeProperties: &slides.UpdateShapePropertiesRequest{
			ObjectId:        c.ObjectID,
			ShapeProperties: props,
			Fields:          strings.Join(fields, ","),
		},
	}
}

func buildTextRange(startIndex, endIndex *int) *slides.Range {
	if startIndex != nil && endIndex != nil {
		si := int64(*startIndex)
		ei := int64(*endIndex)
		return &slides.Range{
			Type:            "FIXED_RANGE",
			StartIndex:      &si,
			EndIndex:        &ei,
			ForceSendFields: []string{"StartIndex"},
		}
	}
	return &slides.Range{Type: "ALL"}
}

type slidesBatchAdapter struct {
	svc *slides.Service
}

func (a *slidesBatchAdapter) BatchUpdate(id string, req *slides.BatchUpdatePresentationRequest) (*slides.BatchUpdatePresentationResponse, error) {
	return a.svc.Presentations.BatchUpdate(id, req).Do()
}

// ApplyCorrections sends the correction requests to the Google Slides API
// via a BatchUpdate call.
func ApplyCorrections(ctx context.Context, api revision.SlidesBatchUpdater, presentationID string, requests []*slides.Request, revLog *revision.Log) error {
	_, err := revision.BatchUpdate(api, presentationID, &slides.BatchUpdatePresentationRequest{
		Requests: requests,
	}, revLog, "apply_font_corrections")
	if err != nil {
		return fmt.Errorf("batch update failed: %w", err)
	}
	return nil
}
