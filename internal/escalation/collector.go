package escalation

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Collector accumulates advisory escalations (constats whose default is to
// proceed) so the human is solicited ONCE, at the end of the run, instead of
// being interrupted at each event (ADR 032, refining ADR 026). Blocking
// decisions (e.g. sanitized selection, litigious memory writes) keep using
// Ask directly: their outcome changes the pipeline behavior.
type Collector struct {
	mu    sync.Mutex
	items []Request
}

// NewCollector returns an empty Collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Add records an advisory escalation for the consolidated end-of-run
// acknowledgement. Safe on a nil receiver (no-op) so call sites work without
// a collector wired.
func (c *Collector) Add(req Request) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = append(c.items, req)
	c.mu.Unlock()
	slog.Warn("[escalation] advisory event collected for end-of-run acknowledgement",
		"reason", req.Reason,
	)
}

// Pending returns the number of collected events.
func (c *Collector) Pending() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Flush presents every collected event on a single screen and asks one
// acknowledgement question. It returns the human decision (or true — the
// acknowledgement default — without a TTY or on timeout) and resets the
// collector. No-op returning true when nothing was collected.
func (c *Collector) Flush() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	items := c.items
	c.items = nil
	c.mu.Unlock()

	if len(items) == 0 {
		return true
	}

	var b strings.Builder
	for i, req := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, req.Reason)
		if req.Details != "" {
			for _, line := range strings.Split(strings.TrimRight(req.Details, "\n"), "\n") {
				fmt.Fprintf(&b, "   %s\n", line)
			}
		}
	}

	return Ask(Request{
		Reason:   fmt.Sprintf("%d point(s) à acquitter avant la fin du run", len(items)),
		Details:  b.String(),
		Question: "Acquitter ces constats (le deck est déjà livré) ?",
		Default:  true,
	})
}
