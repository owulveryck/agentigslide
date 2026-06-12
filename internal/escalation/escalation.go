// Package escalation centralizes the human-in-the-loop policy of the
// pipeline. The doctrine (ADR 026): the system runs autonomously and only
// solicits a human on explicitly listed litigious events — a sanitized
// selection, stale review issues, visual defects shipped after the final
// pass, deletions in agent memory. Every prompt has a timeout and a default
// so an unattended run never blocks.
package escalation

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// DefaultTimeout bounds how long a prompt waits for the human before
// applying the default decision.
const DefaultTimeout = 60 * time.Second

// Request describes one litigious event submitted to the human.
type Request struct {
	// Reason is the short policy label (e.g. "selection sanitisée").
	Reason string
	// Details is the one-screen summary shown before the question.
	Details string
	// Question is the yes/no question asked to the human.
	Question string
	// Default is the decision applied when stdin is not interactive, on
	// timeout, or on read error.
	Default bool
	// Timeout overrides DefaultTimeout when > 0.
	Timeout time.Duration
}

// Ask submits the request to the human on stderr/stdin. It returns the
// human decision, or req.Default when no human is available (non-TTY),
// the timeout elapses, or reading fails. The event is always logged, which
// also mirrors it to the SSE dashboard via the monitor slog handler.
func Ask(req Request) bool {
	slog.Warn("[escalation] human validation requested",
		"reason", req.Reason,
		"default", req.Default,
	)

	if req.Details != "" {
		fmt.Fprintf(os.Stderr, "\n⚠ ESCALADE — %s\n%s\n", req.Reason, req.Details)
	} else {
		fmt.Fprintf(os.Stderr, "\n⚠ ESCALADE — %s\n", req.Reason)
	}

	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		slog.Info("[escalation] no interactive terminal, applying default",
			"reason", req.Reason, "decision", req.Default)
		return req.Default
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	defLabel := "O/n"
	if !req.Default {
		defLabel = "o/N"
	}
	fmt.Fprintf(os.Stderr, "%s [%s] (défaut dans %s) ", req.Question, defLabel, timeout)

	answerCh := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			answerCh <- req.Default
			return
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "o", "oui", "y", "yes":
			answerCh <- true
		case "n", "non", "no":
			answerCh <- false
		default:
			answerCh <- req.Default
		}
	}()

	select {
	case decision := <-answerCh:
		slog.Info("[escalation] human decision", "reason", req.Reason, "decision", decision)
		return decision
	case <-time.After(timeout):
		fmt.Fprintln(os.Stderr)
		slog.Info("[escalation] timeout, applying default",
			"reason", req.Reason, "decision", req.Default)
		return req.Default
	}
}
