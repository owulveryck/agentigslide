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
	maxCount := 0
	for family, c := range familyCounts {
		if c > maxCount {
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

// EstimateMaxChars estimates the maximum number of characters that fit in a
// rectangular area of the given dimensions (in points) with the specified font.
// Returns 0 if any input dimension is non-positive.
func EstimateMaxChars(widthPt, heightPt float64, font FontInfo) int {
	if widthPt <= 0 || heightPt <= 0 || font.SizePt <= 0 {
		return 0
	}
	charWidthRatio := fontCharWidthRatio(font.Family, font.Bold)
	charsPerLine := widthPt / (font.SizePt * charWidthRatio)
	lines := heightPt / (font.SizePt * lineHeightMultiplier)
	maxChars := int(charsPerLine * lines)
	if maxChars < 0 {
		return 0
	}
	return maxChars
}
