// Command traceeval replays the deterministic checks of the pipeline against
// one or more debug traces (--trace output of slidegen) and reports the KPIs
// used to measure pipeline improvements: phase attribution, retries, token
// and cost estimates, enforcement (truncation) counts, review and visual
// findings. When several traces are given, every trace is compared to the
// first one (the baseline), so a golden trace checked into the repo acts as
// a regression gate.
//
// Usage:
//
//	go run cmd/traceeval/main.go baseline-trace.json [new-trace.json ...]
//	go run cmd/traceeval/main.go -gate baseline-trace.json new-trace.json
//
// With -gate, the command exits non-zero when the new trace regresses
// against the baseline (cost, unresolved findings, review iterations) or
// violates absolute invariants (uncorrected overruns, validation replays,
// pipeline errors) — usable in CI at zero API cost (ADR 032).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/owulveryck/agentigslide/internal/metrics"
	"github.com/owulveryck/agentigslide/internal/trace"
)

// KPI holds the comparable indicators extracted from one trace file.
type KPI struct {
	Path             string
	DurationMs       int64
	PhaseAttribution float64 // sum(phase durations) / total duration
	PhaseDurations   map[string]int64

	OutlineAttempts    int
	SelectorAttempts   int
	SelectorFailures   int
	SelectionSanitized bool

	WriterCalls       int
	EnforcementCount  int
	OverLimitOutputs  int
	ReviewIterations  int
	ReviewIssues      map[string]int
	ReviewApproved    bool
	VisualPasses      int
	VisualFindings    []int // findings (issues) per pass
	VisualUnresolved  int   // issues still present in the last pass
	FormatterIssues   []int // per pass
	TokensIn          int
	TokensOut         int
	CacheReadTokens   int
	CacheWriteTokens  int
	EstimatedCostUSD  float64
	CostIsComplete    bool // true when computed from the per-call ledger (agentCalls)
	ErrorCount        int
	ValidationReplays []string // deterministic re-checks that disagree with the recorded run
}

var (
	gateFlag      = flag.Bool("gate", false, "Regression gate mode: exit non-zero when the last trace regresses vs the baseline (CI)")
	costTolerance = flag.Float64("cost-tolerance", 0.15, "Allowed relative cost increase vs baseline in gate mode (0.15 = +15%)")
)

func main() {
	flag.Parse()
	if flag.NArg() < 1 || (*gateFlag && flag.NArg() < 2) {
		fmt.Fprintln(os.Stderr, "usage: traceeval [-gate [-cost-tolerance 0.15]] <baseline-trace.json> [trace.json ...]")
		os.Exit(2)
	}

	var kpis []KPI
	for _, path := range flag.Args() {
		kpi, err := evalTrace(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "traceeval: %s: %v\n", path, err)
			os.Exit(1)
		}
		kpis = append(kpis, kpi)
	}

	for i, kpi := range kpis {
		printKPI(kpi)
		if i > 0 {
			printDelta(kpis[0], kpi)
		}
	}

	if *gateFlag {
		base, last := kpis[0], kpis[len(kpis)-1]
		if failures := gate(base, last, *costTolerance); len(failures) > 0 {
			fmt.Fprintf(os.Stderr, "\nGATE: ÉCHEC (%d violation(s)) :\n", len(failures))
			for _, f := range failures {
				fmt.Fprintf(os.Stderr, "  ✗ %s\n", f)
			}
			os.Exit(1)
		}
		fmt.Println("\nGATE: OK — pas de régression vs baseline")
	}
}

// gate compares the new trace to the baseline and returns the list of
// violated invariants. Absolute invariants fail regardless of the baseline;
// relative ones fail only on regression.
func gate(base, last KPI, costTol float64) []string {
	var failures []string

	if last.OverLimitOutputs > 0 {
		failures = append(failures, fmt.Sprintf("%d dépassement(s) de budget texte non corrigé(s) (invariant : 0)", last.OverLimitOutputs))
	}
	if len(last.ValidationReplays) > 0 {
		failures = append(failures, fmt.Sprintf("%d rejeu(x) déterministe(s) en désaccord avec le run (invariant : 0)", len(last.ValidationReplays)))
	}
	if last.ErrorCount > 0 {
		failures = append(failures, fmt.Sprintf("%d erreur(s) pipeline (invariant : 0)", last.ErrorCount))
	}
	if last.SelectionSanitized {
		failures = append(failures, "sélection sanitizée (run dégradé)")
	}
	if base.EstimatedCostUSD > 0 && last.EstimatedCostUSD > base.EstimatedCostUSD*(1+costTol) {
		failures = append(failures, fmt.Sprintf("coût $%.3f > baseline $%.3f +%.0f%%", last.EstimatedCostUSD, base.EstimatedCostUSD, costTol*100))
	}
	if last.VisualUnresolved > base.VisualUnresolved {
		failures = append(failures, fmt.Sprintf("findings visuels non résolus : %d > baseline %d", last.VisualUnresolved, base.VisualUnresolved))
	}
	if last.ReviewIterations > base.ReviewIterations+1 {
		failures = append(failures, fmt.Sprintf("itérations de review : %d > baseline %d+1", last.ReviewIterations, base.ReviewIterations))
	}

	return failures
}

func evalTrace(path string) (KPI, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return KPI{}, err
	}
	var tf trace.TraceFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return KPI{}, fmt.Errorf("parsing trace: %w", err)
	}

	kpi := KPI{
		Path:           path,
		DurationMs:     tf.DurationMs,
		PhaseDurations: make(map[string]int64),
		ReviewIssues:   make(map[string]int),
		ErrorCount:     len(tf.Errors),
	}

	var phaseSum int64
	for _, p := range tf.Phases {
		kpi.PhaseDurations[p.Name] += p.DurationMs
		phaseSum += p.DurationMs
	}
	if tf.DurationMs > 0 {
		kpi.PhaseAttribution = float64(phaseSum) / float64(tf.DurationMs)
	}

	if tf.Outline != nil {
		kpi.OutlineAttempts = len(tf.Outline.Attempts)
	}
	if tf.Selection != nil {
		kpi.SelectorAttempts = len(tf.Selection.Attempts)
		for _, a := range tf.Selection.Attempts {
			if a.ValidationError != "" {
				kpi.SelectorFailures++
			}
		}
		// All attempts failing means the run proceeded on a sanitized plan.
		kpi.SelectionSanitized = kpi.SelectorAttempts > 0 && kpi.SelectorFailures == kpi.SelectorAttempts
	}

	kpi.WriterCalls = len(tf.Writers)
	for _, w := range tf.Writers {
		kpi.EnforcementCount += len(w.Enforcement)
		for _, m := range w.Output.Modifications {
			// Deterministic replay of the budget check on the recorded
			// writer outputs: a CharCount above MaxChars that was not
			// enforced is a disagreement worth flagging.
			if m.MaxChars > 0 && m.CharCount > m.MaxChars {
				kpi.OverLimitOutputs++
				kpi.ValidationReplays = append(kpi.ValidationReplays,
					fmt.Sprintf("writer slide %d field %s: %d chars > max %d and no enforcement recorded",
						w.SlideIndex, m.VariableName, m.CharCount, m.MaxChars))
			}
		}
		kpi.TokensIn += w.TokensIn
		kpi.TokensOut += w.TokensOut
		kpi.EstimatedCostUSD += costOf(w.ModelUsed, w.TokensIn, w.TokensOut)
	}

	if tf.Outline != nil {
		for _, a := range tf.Outline.Attempts {
			kpi.TokensIn += a.TokensIn
			kpi.TokensOut += a.TokensOut
			kpi.EstimatedCostUSD += costOf(tf.Config.OutlinerModel, a.TokensIn, a.TokensOut)
		}
	}
	if tf.Selection != nil {
		for _, a := range tf.Selection.Attempts {
			kpi.TokensIn += a.TokensIn
			kpi.TokensOut += a.TokensOut
			kpi.EstimatedCostUSD += costOf(tf.Config.SelectorModel, a.TokensIn, a.TokensOut)
		}
	}
	if tf.Review != nil {
		kpi.ReviewIterations = len(tf.Review.Iterations)
		for _, it := range tf.Review.Iterations {
			kpi.TokensIn += it.TokensIn
			kpi.TokensOut += it.TokensOut
			kpi.EstimatedCostUSD += costOf(tf.Config.ReviewerModel, it.TokensIn, it.TokensOut)
			for _, issue := range it.Issues {
				kpi.ReviewIssues[issue.IssueType]++
			}
			kpi.ReviewApproved = it.Approved
		}
	}

	kpi.VisualPasses = len(tf.VisualReview)
	for i, vr := range tf.VisualReview {
		var issues int
		for _, f := range vr.Findings {
			issues += len(f.Issues)
		}
		kpi.VisualFindings = append(kpi.VisualFindings, issues)
		if i == len(tf.VisualReview)-1 {
			kpi.VisualUnresolved = issues
		}
	}

	for _, f := range tf.Formatter {
		kpi.FormatterIssues = append(kpi.FormatterIssues, f.IssueCount)
	}

	// When the trace carries the per-call LLM ledger (agentCalls), it is the
	// authoritative cost source: it covers visual review, memory synthesis
	// and designer calls absent from the per-phase sections, and it prices
	// cache reads/writes. The per-phase accumulation above then only serves
	// as a fallback for older traces.
	if len(tf.AgentCalls) > 0 {
		kpi.TokensIn, kpi.TokensOut = 0, 0
		kpi.EstimatedCostUSD = 0
		for _, c := range tf.AgentCalls {
			p := metrics.LookupPricing(c.Model)
			kpi.TokensIn += c.InputTokens
			kpi.TokensOut += c.OutputTokens
			kpi.CacheReadTokens += c.CacheReadTokens
			kpi.CacheWriteTokens += c.CacheWriteTokens
			kpi.EstimatedCostUSD += float64(c.InputTokens)/1_000_000*p.InputPerMTok +
				float64(c.OutputTokens)/1_000_000*p.OutputPerMTok +
				float64(c.CacheReadTokens)/1_000_000*p.CacheReadPerMTok +
				float64(c.CacheWriteTokens)/1_000_000*p.CacheWritePerMTok
		}
		kpi.CostIsComplete = true
	}

	return kpi, nil
}

func costOf(model string, tokensIn, tokensOut int) float64 {
	p := metrics.LookupPricing(model)
	return float64(tokensIn)/1_000_000*p.InputPerMTok + float64(tokensOut)/1_000_000*p.OutputPerMTok
}

func printKPI(k KPI) {
	fmt.Printf("\n=== %s ===\n", k.Path)
	tw := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Durée totale\t%.1fs\n", float64(k.DurationMs)/1000)
	fmt.Fprintf(tw, "  Attribution par phases\t%.0f%%\n", k.PhaseAttribution*100)
	if len(k.PhaseDurations) > 0 {
		names := make([]string, 0, len(k.PhaseDurations))
		for n := range k.PhaseDurations {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool { return k.PhaseDurations[names[i]] > k.PhaseDurations[names[j]] })
		for _, n := range names {
			fmt.Fprintf(tw, "    phase %s\t%.1fs\n", n, float64(k.PhaseDurations[n])/1000)
		}
	}
	fmt.Fprintf(tw, "  Outliner / Selector tentatives\t%d / %d (%d échecs", k.OutlineAttempts, k.SelectorAttempts, k.SelectorFailures)
	if k.SelectionSanitized {
		fmt.Fprintf(tw, ", SANITIZED")
	}
	fmt.Fprintf(tw, ")\n")
	fmt.Fprintf(tw, "  Writers\t%d appels, %d troncatures, %d dépassements non corrigés\n", k.WriterCalls, k.EnforcementCount, k.OverLimitOutputs)
	fmt.Fprintf(tw, "  Review\t%d itérations, approuvé=%v, issues=%v\n", k.ReviewIterations, k.ReviewApproved, k.ReviewIssues)
	fmt.Fprintf(tw, "  Visual review\t%d passes, findings par passe=%v, non résolus=%d\n", k.VisualPasses, k.VisualFindings, k.VisualUnresolved)
	fmt.Fprintf(tw, "  Formatter\tissues par passe=%v\n", k.FormatterIssues)
	if k.CostIsComplete {
		fmt.Fprintf(tw, "  Tokens (in/out)\t%d / %d (cache read/write : %d / %d)\n", k.TokensIn, k.TokensOut, k.CacheReadTokens, k.CacheWriteTokens)
		if total := k.CacheReadTokens + k.TokensIn; total > 0 {
			fmt.Fprintf(tw, "  Cache hit ratio\t%.0f%%\n", float64(k.CacheReadTokens)/float64(total)*100)
		}
		fmt.Fprintf(tw, "  Coût LLM réel (ledger)\t$%.3f\n", k.EstimatedCostUSD)
	} else {
		fmt.Fprintf(tw, "  Tokens (in/out)\t%d / %d\n", k.TokensIn, k.TokensOut)
		fmt.Fprintf(tw, "  Coût LLM estimé (partiel : pas de ledger)\t$%.3f\n", k.EstimatedCostUSD)
	}
	fmt.Fprintf(tw, "  Erreurs pipeline\t%d\n", k.ErrorCount)
	_ = tw.Flush()
	if len(k.ValidationReplays) > 0 {
		fmt.Println("  Rejeux déterministes en désaccord avec le run :")
		for _, v := range k.ValidationReplays {
			fmt.Printf("    ! %s\n", v)
		}
	}
}

func printDelta(base, k KPI) {
	fmt.Printf("  --- delta vs %s ---\n", base.Path)
	tw := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Durée\t%+.1fs\n", float64(k.DurationMs-base.DurationMs)/1000)
	fmt.Fprintf(tw, "  Coût estimé\t%+.3f$\n", k.EstimatedCostUSD-base.EstimatedCostUSD)
	fmt.Fprintf(tw, "  Échecs selector\t%+d\n", k.SelectorFailures-base.SelectorFailures)
	fmt.Fprintf(tw, "  Troncatures\t%+d\n", k.EnforcementCount-base.EnforcementCount)
	fmt.Fprintf(tw, "  Findings visuels non résolus\t%+d\n", k.VisualUnresolved-base.VisualUnresolved)
	_ = tw.Flush()
}
