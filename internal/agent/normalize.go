package agent

import (
	"fmt"
	"log/slog"
	"strings"
)

// NormalizeOutline adjusts SlideNeeds whose itemCount exceeds the maximum
// text-capable fields available in the catalog. It merges adjacent
// contentItems (joining with "\n") until itemCount fits. Slides of type
// cover, section_divider, conclusion, and diagram are left untouched.
func NormalizeOutline(outline *PresentationOutline, compactCatalog string) {
	maxFields := MaxTextFields(compactCatalog)
	if maxFields == 0 {
		return
	}

	for i := range outline.Sections {
		for j := range outline.Sections[i].SlideNeeds {
			need := &outline.Sections[i].SlideNeeds[j]

			switch need.SlideType {
			case "cover", "section_divider", "conclusion", "diagram":
				continue
			}

			if need.ItemCount <= maxFields {
				continue
			}

			slog.Info("[normalize] merging content items to fit catalog capacity",
				"section", i,
				"slide", j,
				"intent", need.Intent,
				"itemCount", need.ItemCount,
				"maxFields", maxFields,
			)

			need.ContentItems = mergeItems(need.ContentItems, maxFields)
			need.ItemCount = len(need.ContentItems)
			need.MaxItemLength = maxItemLen(need.ContentItems)
		}
	}
}

// mergeItems reduces items to at most target entries by joining adjacent
// pairs with "\n".
func mergeItems(items []string, target int) []string {
	for len(items) > target {
		merged := make([]string, 0, (len(items)+1)/2)
		for k := 0; k < len(items); k += 2 {
			if k+1 < len(items) {
				merged = append(merged, items[k]+"\n"+items[k+1])
			} else {
				merged = append(merged, items[k])
			}
		}
		items = merged
	}
	return items
}

func maxItemLen(items []string) int {
	m := 0
	for _, s := range items {
		if n := len([]rune(s)); n > m {
			m = n
		}
	}
	return m
}

// MaxTextFields returns the maximum number of text-capable fields
// (titre + sous-titre + contenu) across all templates in the catalog.
func MaxTextFields(compactCatalog string) int {
	catalog := ParseCatalog(compactCatalog)
	max := 0
	for _, counts := range catalog.FieldCountsBySlide {
		total := counts.Titles + counts.Subtitles + counts.Contents
		if total > max {
			max = total
		}
	}
	return max
}

// CapacitySummary returns a short text block summarizing the catalog's field
// capacity distribution. Intended to be prepended to the catalog when sent
// to the selector.
func CapacitySummary(compactCatalog string) string {
	catalog := ParseCatalog(compactCatalog)

	maxTotal := 0
	buckets := [5]int{} // 0, 1-2, 3-4, 5-6, 7+

	for _, counts := range catalog.FieldCountsBySlide {
		total := counts.Titles + counts.Subtitles + counts.Contents
		if total > maxTotal {
			maxTotal = total
		}
		switch {
		case total == 0:
			buckets[0]++
		case total <= 2:
			buckets[1]++
		case total <= 4:
			buckets[2]++
		case total <= 6:
			buckets[3]++
		default:
			buckets[4]++
		}
	}

	var b strings.Builder
	b.WriteString("RÉSUMÉ DE CAPACITÉ DU CATALOGUE :\n")
	fmt.Fprintf(&b, "- Max zones texte totales (titre+sous-titre+contenu) disponibles : %d\n", maxTotal)
	fmt.Fprintf(&b, "- Distribution par total zones texte : 0: %d | 1-2: %d | 3-4: %d | 5-6: %d | 7+: %d\n",
		buckets[0], buckets[1], buckets[2], buckets[3], buckets[4])
	b.WriteString("- RAPPEL : le template choisi DOIT avoir un total (titre + sous-titre + contenu) >= itemCount.\n")
	return b.String()
}
