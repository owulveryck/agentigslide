package metrics

import (
	"sync"
	"time"
)

// AgentCall records the token usage from a single API call.
type AgentCall struct {
	Agent                    string
	Model                    string
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// Collector accumulates API usage metrics across the pipeline. It is
// goroutine-safe for concurrent writer calls.
type Collector struct {
	mu               sync.Mutex
	calls            []AgentCall
	selectorRetries  int
	reviewerRetries  int
	slidesGenerated  int
	pipelineDuration time.Duration
}

// NewCollector creates an empty Collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Record adds a single API call's usage to the collector.
func (c *Collector) Record(call AgentCall) {
	c.mu.Lock()
	c.calls = append(c.calls, call)
	c.mu.Unlock()
}

// SetSelectorRetries records the number of selector validation retries.
func (c *Collector) SetSelectorRetries(n int) {
	c.mu.Lock()
	c.selectorRetries = n
	c.mu.Unlock()
}

// SetReviewerRetries records the number of reviewer retry iterations.
func (c *Collector) SetReviewerRetries(n int) {
	c.mu.Lock()
	c.reviewerRetries = n
	c.mu.Unlock()
}

// SetSlidesGenerated records the number of slides in the final plan.
func (c *Collector) SetSlidesGenerated(n int) {
	c.mu.Lock()
	c.slidesGenerated = n
	c.mu.Unlock()
}

// SetPipelineDuration records the total pipeline execution time.
func (c *Collector) SetPipelineDuration(d time.Duration) {
	c.mu.Lock()
	c.pipelineDuration = d
	c.mu.Unlock()
}

// AgentRow is a per-agent, per-model aggregation of API calls.
type AgentRow struct {
	Agent                    string
	Model                    string
	Calls                    int
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	Cost                     float64
}

// Summary holds the fully aggregated pipeline metrics.
type Summary struct {
	AgentRows        []AgentRow
	GrandTotal       AgentRow
	SelectorRetries  int
	ReviewerRetries  int
	SlidesGenerated  int
	PipelineDuration time.Duration
}

// Summary computes the aggregated metrics from all recorded calls.
func (c *Collector) Summary() *Summary {
	c.mu.Lock()
	calls := make([]AgentCall, len(c.calls))
	copy(calls, c.calls)
	s := &Summary{
		SelectorRetries:  c.selectorRetries,
		ReviewerRetries:  c.reviewerRetries,
		SlidesGenerated:  c.slidesGenerated,
		PipelineDuration: c.pipelineDuration,
	}
	c.mu.Unlock()

	type key struct{ agent, model string }
	order := []key{}
	agg := map[key]*AgentRow{}

	for _, call := range calls {
		k := key{call.Agent, call.Model}
		row, ok := agg[k]
		if !ok {
			row = &AgentRow{Agent: call.Agent, Model: call.Model}
			agg[k] = row
			order = append(order, k)
		}
		row.Calls++
		row.InputTokens += call.InputTokens
		row.OutputTokens += call.OutputTokens
		row.CacheReadInputTokens += call.CacheReadInputTokens
		row.CacheCreationInputTokens += call.CacheCreationInputTokens
	}

	for _, k := range order {
		row := agg[k]
		row.Cost = EstimateRowCost(row)
		s.AgentRows = append(s.AgentRows, *row)
	}

	for _, row := range s.AgentRows {
		s.GrandTotal.Calls += row.Calls
		s.GrandTotal.InputTokens += row.InputTokens
		s.GrandTotal.OutputTokens += row.OutputTokens
		s.GrandTotal.CacheReadInputTokens += row.CacheReadInputTokens
		s.GrandTotal.CacheCreationInputTokens += row.CacheCreationInputTokens
		s.GrandTotal.Cost += row.Cost
	}

	return s
}
