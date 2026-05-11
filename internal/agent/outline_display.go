package agent

import (
	"fmt"
	"strings"
)

// FormatOutline renders a PresentationOutline as a human-readable string
// suitable for terminal display.
func FormatOutline(outline *PresentationOutline) string {
	var b strings.Builder

	fmt.Fprintf(&b, "\n# %s\n", outline.PresentationTitle)

	slideNum := 0
	for i, sec := range outline.Sections {
		fmt.Fprintf(&b, "\n## Section %d: %s (%s)\n\n", i+1, sec.Title, sec.Purpose)
		for _, need := range sec.SlideNeeds {
			slideNum++
			if len(need.ContentItems) > 0 {
				fmt.Fprintf(&b, "%d. **%s** - %s (%d items)\n", slideNum, need.SlideType, need.Intent, len(need.ContentItems))
				for _, item := range need.ContentItems {
					display := item
					if len(display) > 80 {
						display = display[:77] + "..."
					}
					fmt.Fprintf(&b, "   - %q\n", display)
				}
			} else {
				fmt.Fprintf(&b, "%d. **%s** - %s\n", slideNum, need.SlideType, need.Intent)
			}
		}
	}

	fmt.Fprintf(&b, "\n---\n*Total: %d slides, %d sections*\n", slideNum, len(outline.Sections))
	return b.String()
}
