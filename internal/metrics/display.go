package metrics

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

const ruler = "════════════════════════════════════════════════════════════════════════════════════"
const separator = "────────────────────────────────────────────────────────────────────────────────────"

// PrintTable renders the pipeline metrics summary as an ASCII table.
func PrintTable(w io.Writer, s *Summary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w, "                          PIPELINE EXECUTION SUMMARY")
	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', tabwriter.AlignRight)

	fmt.Fprintf(tw, "  AGENT\tMODEL\tCALLS\tINPUT\tOUTPUT\tCACHE-R\tCACHE-W\tCOST\tDURATION\t\n")

	for _, row := range s.AgentRows {
		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%d\t%d\t$%.2f\t%s\t\n",
			row.Agent, row.Model, row.Calls,
			row.InputTokens, row.OutputTokens,
			row.CacheReadInputTokens, row.CacheCreationInputTokens,
			row.Cost,
			row.Duration.Round(time.Millisecond),
		)
	}
	_ = tw.Flush()

	fmt.Fprintln(w, separator)

	tw = tabwriter.NewWriter(w, 2, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "  TOTAL\t\t%d\t%d\t%d\t%d\t%d\t$%.2f\t%s\t\n",
		s.GrandTotal.Calls,
		s.GrandTotal.InputTokens, s.GrandTotal.OutputTokens,
		s.GrandTotal.CacheReadInputTokens, s.GrandTotal.CacheCreationInputTokens,
		s.GrandTotal.Cost,
		s.GrandTotal.Duration.Round(time.Millisecond),
	)
	_ = tw.Flush()

	fmt.Fprintln(w)

	maxLabel := len("Reviewer iterations:")
	printKV(w, maxLabel, "Pipeline duration:", s.PipelineDuration.Round(1).String())
	printKV(w, maxLabel, "Slides generated:", fmt.Sprintf("%d", s.SlidesGenerated))
	printKV(w, maxLabel, "Selector retries:", fmt.Sprintf("%d", s.SelectorRetries))
	printKV(w, maxLabel, "Reviewer iterations:", fmt.Sprintf("%d", s.ReviewerRetries))
	printKV(w, maxLabel, "Cache hit rate:", fmt.Sprintf("%.1f%%", s.CacheHitRate*100))
	if s.SlidesGenerated > 0 {
		printKV(w, maxLabel, "Cost per slide:", fmt.Sprintf("$%.3f", s.CostPerSlide))
	}
	if s.CacheSavingsUSD > 0 {
		printKV(w, maxLabel, "Cache savings:", fmt.Sprintf("$%.3f", s.CacheSavingsUSD))
	}

	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w)
}

func printKV(w io.Writer, width int, label, value string) {
	padding := width - len(label)
	padding = max(padding, 0)
	fmt.Fprintf(w, "  %s%s  %s\n", label, strings.Repeat(" ", padding), value)
}
