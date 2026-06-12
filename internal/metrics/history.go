package metrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"
)

// RunRecord captures the metrics of a single pipeline run for historical tracking.
type RunRecord struct {
	Timestamp       time.Time      `json:"timestamp"`
	Mode            string         `json:"mode"`
	SlidesGenerated int            `json:"slidesGenerated"`
	TotalCalls      int            `json:"totalCalls"`
	TotalCostUSD    float64        `json:"totalCostUSD"`
	CostPerSlide    float64        `json:"costPerSlide"`
	DurationSecs    float64        `json:"durationSecs"`
	CacheHitRate    float64        `json:"cacheHitRate"`
	CacheSavingsUSD float64        `json:"cacheSavingsUSD"`
	AgentRows       []AgentRowJSON `json:"agentRows"`
	// PhaseDurations attributes wall-clock seconds per pipeline phase so
	// run-over-run regressions in non-LLM phases (execution, visual review,
	// formatter) are visible in the history.
	PhaseDurations map[string]float64 `json:"phaseDurations,omitempty"`
}

// AgentRowJSON is the JSON-serializable form of AgentRow.
type AgentRowJSON struct {
	Agent                    string  `json:"agent"`
	Model                    string  `json:"model"`
	Calls                    int     `json:"calls"`
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	Cost                     float64 `json:"cost"`
	DurationMs               int64   `json:"durationMs"`
}

func defaultHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".slidegen", "metrics_history.jsonl"), nil
}

// AppendHistory writes a RunRecord as a single JSON line to the history file.
func AppendHistory(s *Summary, mode string) error {
	path, err := defaultHistoryPath()
	if err != nil {
		return err
	}
	return AppendHistoryTo(path, s, mode)
}

// AppendHistoryTo writes a RunRecord to the given file path.
func AppendHistoryTo(path string, s *Summary, mode string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating history directory: %w", err)
	}

	rows := make([]AgentRowJSON, len(s.AgentRows))
	for i, r := range s.AgentRows {
		rows[i] = AgentRowJSON{
			Agent:                    r.Agent,
			Model:                    r.Model,
			Calls:                    r.Calls,
			InputTokens:              r.InputTokens,
			OutputTokens:             r.OutputTokens,
			CacheReadInputTokens:     r.CacheReadInputTokens,
			CacheCreationInputTokens: r.CacheCreationInputTokens,
			Cost:                     r.Cost,
			DurationMs:               r.Duration.Milliseconds(),
		}
	}

	record := RunRecord{
		Timestamp:       time.Now(),
		Mode:            mode,
		SlidesGenerated: s.SlidesGenerated,
		TotalCalls:      s.GrandTotal.Calls,
		TotalCostUSD:    s.GrandTotal.Cost,
		CostPerSlide:    s.CostPerSlide,
		DurationSecs:    s.PipelineDuration.Seconds(),
		CacheHitRate:    s.CacheHitRate,
		CacheSavingsUSD: s.CacheSavingsUSD,
		AgentRows:       rows,
	}
	if len(s.PhaseDurations) > 0 {
		record.PhaseDurations = make(map[string]float64, len(s.PhaseDurations))
		for k, v := range s.PhaseDurations {
			record.PhaseDurations[k] = v.Seconds()
		}
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshaling record: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening history file: %w", err)
	}
	defer f.Close()

	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// LoadHistory reads all RunRecords from the default history file.
func LoadHistory() ([]RunRecord, error) {
	path, err := defaultHistoryPath()
	if err != nil {
		return nil, err
	}
	return LoadHistoryFrom(path)
}

// LoadHistoryFrom reads all RunRecords from the given file path.
func LoadHistoryFrom(path string) ([]RunRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []RunRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

// PrintHistory loads and renders the last n runs from the history as an ASCII table.
func PrintHistory(w io.Writer, n int) error {
	records, err := LoadHistory()
	if err != nil {
		return err
	}
	printHistoryRecords(w, records, n)
	return nil
}

func printHistoryRecords(w io.Writer, records []RunRecord, n int) {
	if len(records) == 0 {
		fmt.Fprintln(w, "No history records found.")
		return
	}

	if n > len(records) {
		n = len(records)
	}
	records = records[len(records)-n:]

	fmt.Fprintln(w)
	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w, "                              COST HISTORY")
	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "  DATE\tMODE\tSLIDES\tCALLS\tCOST\tΔ COST\t$/SLIDE\tCACHE%%\tDURATION\t\n")

	for i, r := range records {
		delta := ""
		if i > 0 {
			d := r.TotalCostUSD - records[i-1].TotalCostUSD
			if d >= 0 {
				delta = fmt.Sprintf("+$%.2f", d)
			} else {
				delta = fmt.Sprintf("-$%.2f", -d)
			}
		}

		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t$%.2f\t%s\t$%.3f\t%.0f%%\t%.0fs\t\n",
			r.Timestamp.Format("2006-01-02 15:04"),
			r.Mode,
			r.SlidesGenerated,
			r.TotalCalls,
			r.TotalCostUSD,
			delta,
			r.CostPerSlide,
			r.CacheHitRate*100,
			r.DurationSecs,
		)
	}
	_ = tw.Flush()

	fmt.Fprintln(w, ruler)
	fmt.Fprintln(w)
}
