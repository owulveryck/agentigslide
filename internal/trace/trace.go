package trace

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// Tracer accumulates structured trace data across the pipeline and writes it
// to a JSON file. All methods are safe to call on a nil receiver (no-op).
type Tracer struct {
	mu      sync.Mutex
	file    TraceFile
	outPath string
	start   time.Time
}

// New creates a Tracer that will write to outPath. Returns nil if outPath is empty.
func New(outPath string) *Tracer {
	if outPath == "" {
		return nil
	}
	return &Tracer{
		outPath: outPath,
		start:   time.Now(),
		file: TraceFile{
			Version:     "1.0",
			GeneratedAt: time.Now(),
		},
	}
}

func (t *Tracer) RecordConfig(cfg ConfigTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Config = cfg
}

func (t *Tracer) SetUserRequest(request string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	const maxLen = 2000
	if len(request) > maxLen {
		t.file.UserRequest = request[:maxLen] + "... [truncated]"
	} else {
		t.file.UserRequest = request
	}
}

// RecordPhase appends a phase window starting at start and ending now. Call it
// when the phase completes so the full wall-clock can be attributed.
func (t *Tracer) RecordPhase(name string, start time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Phases = append(t.file.Phases, PhaseTrace{
		Name:       name,
		StartedAt:  start,
		DurationMs: time.Since(start).Milliseconds(),
	})
}

func (t *Tracer) RecordOutlineAttempt(a OutlineAttempt) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file.Outline == nil {
		t.file.Outline = &OutlineTrace{}
	}
	t.file.Outline.Attempts = append(t.file.Outline.Attempts, a)
}

func (t *Tracer) SetOutlineResult(inputSummary string, sections []SectionSummary) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file.Outline == nil {
		t.file.Outline = &OutlineTrace{}
	}
	t.file.Outline.InputSummary = inputSummary
	t.file.Outline.FinalSections = sections
}

func (t *Tracer) RecordSelectionAttempt(a SelectionAttempt) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file.Selection == nil {
		t.file.Selection = &SelectionTrace{}
	}
	t.file.Selection.Attempts = append(t.file.Selection.Attempts, a)
}

func (t *Tracer) SetSelectionResult(entries []SelectionEntry) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file.Selection == nil {
		t.file.Selection = &SelectionTrace{}
	}
	t.file.Selection.Final = entries
}

func (t *Tracer) RecordWriter(w WriterTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Writers = append(t.file.Writers, w)
}

func (t *Tracer) RecordReviewIteration(r ReviewIteration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file.Review == nil {
		t.file.Review = &ReviewTrace{}
	}
	t.file.Review.Iterations = append(t.file.Review.Iterations, r)
}

func (t *Tracer) RecordExecution(e ExecutionTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Execution = &e
}

func (t *Tracer) RecordFormatter(f FormatterTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Formatter = append(t.file.Formatter, f)
}

func (t *Tracer) RecordVisualReview(v VisualReviewTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.VisualReview = append(t.file.VisualReview, v)
}

// SetAgentCalls stores the complete per-call LLM ledger (typically dumped
// from the metrics collector at the end of the run). Replaces any previous
// ledger so the final dump wins.
func (t *Tracer) SetAgentCalls(calls []AgentCallTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.AgentCalls = calls
}

func (t *Tracer) RecordError(phase, message string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.file.Errors = append(t.file.Errors, ErrorEntry{Phase: phase, Message: message})
}

// Flush sorts writer traces by slide index and writes the trace file to disk.
func (t *Tracer) Flush() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.file.DurationMs = time.Since(t.start).Milliseconds()

	sort.Slice(t.file.Writers, func(i, j int) bool {
		return t.file.Writers[i].SlideIndex < t.file.Writers[j].SlideIndex
	})

	data, err := json.MarshalIndent(t.file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.outPath, data, 0644)
}
