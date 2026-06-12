package templateindex

import (
	"math"

	"github.com/owulveryck/agentigslide/internal/model"
)

// emuPerPoint is the number of English Metric Units (EMU) per typographic
// point. Google Slides uses EMU internally for all positioning and sizing.
// 1 point = 1/72 inch = 12,700 EMU.
const emuPerPoint = 12700.0

// FontInfo holds the predominant typographic properties of a text element,
// used to estimate how many characters fit within a given area.
type FontInfo struct {
	SizePt float64
	Family string
	Bold   bool
}

// defaultFontSizePt is assumed when a text element has no explicit font size.
const defaultFontSizePt = 14.0

// ComputeElementSize returns the width and height of a page element in
// typographic points, accounting for its transform scale factors.
func ComputeElementSize(el *model.PageElement) (widthPt, heightPt float64) {
	if el == nil || el.Size == nil {
		return 0, 0
	}
	scaleX := 1.0
	scaleY := 1.0
	if el.Transform != nil {
		if el.Transform.ScaleX != 0 {
			scaleX = el.Transform.ScaleX
		}
		if el.Transform.ScaleY != 0 {
			scaleY = el.Transform.ScaleY
		}
	}
	widthPt = math.Abs(el.Size.Width.Magnitude*scaleX) / emuPerPoint
	heightPt = math.Abs(el.Size.Height.Magnitude*scaleY) / emuPerPoint
	return widthPt, heightPt
}

// ExtractPredominantFont analyzes the text runs of a shape element and returns
// the average font size, most frequent font family, and whether bold styling
// predominates. Returns a default of [defaultFontSizePt] for elements without
// text styling.
func ExtractPredominantFont(el *model.PageElement) FontInfo {
	if el == nil || el.Shape == nil || el.Shape.Text == nil {
		return FontInfo{SizePt: defaultFontSizePt}
	}
	var totalSize float64
	var count int
	familyCounts := make(map[string]int)
	boldCount := 0
	for _, te := range el.Shape.Text.TextElements {
		if te.TextRun != nil && te.TextRun.Style != nil {
			if te.TextRun.Style.FontSize != nil {
				totalSize += te.TextRun.Style.FontSize.Magnitude
				count++
			}
			if te.TextRun.Style.FontFamily != "" {
				familyCounts[te.TextRun.Style.FontFamily]++
			}
			if te.TextRun.Style.Bold {
				boldCount++
			}
		}
	}
	info := FontInfo{SizePt: defaultFontSizePt}
	if count > 0 {
		info.SizePt = totalSize / float64(count)
		info.Bold = boldCount > count/2
	}
	// Tie-break on the family name so the predominant font — and therefore
	// every capacity derived from it — is deterministic across rebuilds
	// (map iteration order would otherwise make `buildindex -check` flap).
	maxCount := 0
	for family, c := range familyCounts {
		if c > maxCount || (c == maxCount && family < info.Family) {
			maxCount = c
			info.Family = family
		}
	}
	return info
}

// fontWidthRatios maps font families to their average character width as a
// fraction of the font size. These empirical values approximate how many
// characters fit per line for common presentation fonts.
var fontWidthRatios = map[string]float64{
	"Outfit":                    0.52,
	"Arial":                     0.60,
	"Helvetica Neue":            0.58,
	"Century Gothic":            0.58,
	"Merriweather Sans":         0.58,
	"Roboto":                    0.56,
	"Fira Sans Extra Condensed": 0.42,
	"Courier New":               0.60,
	"Noto Sans Symbols":         0.60,
	"Comic Sans MS":             0.60,
}

// defaultWidthRatio is used when the font family is not in fontWidthRatios.
const defaultWidthRatio = 0.55

// boldWidthMultiplier scales the character width ratio for bold text.
const boldWidthMultiplier = 1.08

// lineHeightMultiplier approximates line spacing as a factor of font size.
const lineHeightMultiplier = 1.3

// wrapEfficiency discounts the raw area estimate for the capacity lost to
// word-wrap: ragged line endings waste 20-30% of a multi-line box, which is
// why purely area-based budgets overflow in practice. Calibrated against the
// visual-review findings of the edito.md golden trace (ADR 021).
const wrapEfficiency = 0.78

// fontCharWidthRatio returns the character width ratio for a given font,
// applying the bold multiplier when applicable.
func fontCharWidthRatio(fontFamily string, bold bool) float64 {
	ratio, ok := fontWidthRatios[fontFamily]
	if !ok {
		ratio = defaultWidthRatio
	}
	if bold {
		ratio *= boldWidthMultiplier
	}
	return ratio
}

// EstimateLineGeometry returns the estimated characters per line and number
// of lines that fit in a rectangular area of the given dimensions (in points)
// with the specified font. Returns (0, 0) if any input is non-positive.
func EstimateLineGeometry(widthPt, heightPt float64, font FontInfo) (charsPerLine, lines int) {
	if widthPt <= 0 || heightPt <= 0 || font.SizePt <= 0 {
		return 0, 0
	}
	charWidthRatio := fontCharWidthRatio(font.Family, font.Bold)
	charsPerLine = int(widthPt / (font.SizePt * charWidthRatio))
	lines = int(heightPt / (font.SizePt * lineHeightMultiplier))
	if charsPerLine < 0 {
		charsPerLine = 0
	}
	if lines < 0 {
		lines = 0
	}
	return charsPerLine, lines
}

// EstimateMaxChars estimates the maximum number of characters that fit in a
// rectangular area of the given dimensions (in points) with the specified
// font, discounted by wrapEfficiency to account for word-wrap waste.
// Returns 0 if any input dimension is non-positive.
func EstimateMaxChars(widthPt, heightPt float64, font FontInfo) int {
	charsPerLine, lines := EstimateLineGeometry(widthPt, heightPt, font)
	return DerivedMaxChars(charsPerLine, lines)
}

// DerivedMaxChars returns the character capacity derived from a line
// geometry (charsPerLine × lines, discounted by wrapEfficiency for
// multi-line boxes). This is THE single source of truth for text budgets:
// every consumer of a field capacity (writer budgets, selector catalog,
// reviewer catalog, overflow gates) must derive it from the geometry through
// this function, never from an independently estimated value (ADR 027).
// Returns 0 when the geometry is unknown.
func DerivedMaxChars(charsPerLine, lines int) int {
	if charsPerLine <= 0 || lines <= 0 {
		return 0
	}
	if lines == 1 {
		// Single-line fields lose nothing to wrap.
		return charsPerLine
	}
	return int(float64(charsPerLine*lines) * wrapEfficiency)
}
