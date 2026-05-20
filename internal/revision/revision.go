package revision

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/slides/v1"
)

type Entry struct {
	Step         string
	RevisionID   string
	Timestamp    time.Time
	RequestCount int
}

type Log struct {
	PresentationID string
	Entries        []Entry
	mu             sync.Mutex
}

func New(presentationID string) *Log {
	return &Log{PresentationID: presentationID}
}

func (l *Log) Record(step string, resp *slides.BatchUpdatePresentationResponse, requestCount int) {
	var revID string
	if resp != nil && resp.WriteControl != nil {
		revID = resp.WriteControl.RequiredRevisionId
	}

	entry := Entry{
		Step:         step,
		RevisionID:   revID,
		Timestamp:    time.Now(),
		RequestCount: requestCount,
	}

	l.mu.Lock()
	l.Entries = append(l.Entries, entry)
	l.mu.Unlock()

	slog.Info("revision recorded", "step", step, "revisionID", revID, "requestCount", requestCount)
}

func (l *Log) LatestRevisionID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.Entries) == 0 {
		return ""
	}
	return l.Entries[len(l.Entries)-1].RevisionID
}

func (l *Log) Summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.Entries) == 0 {
		return "no revisions recorded"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "revision log for %s (%d entries):\n", l.PresentationID, len(l.Entries))
	for i, e := range l.Entries {
		fmt.Fprintf(&b, "  %d. [%s] %s — %d request(s), revision=%s\n",
			i+1, e.Timestamp.Format("15:04:05"), e.Step, e.RequestCount, e.RevisionID)
	}
	return b.String()
}

// BatchUpdate calls the Slides API BatchUpdate and records the resulting revision.
// If log is nil, revision tracking is skipped.
func BatchUpdate(slidesSrv *slides.Service, presentationID string, req *slides.BatchUpdatePresentationRequest, log *Log, step string) (*slides.BatchUpdatePresentationResponse, error) {
	resp, err := slidesSrv.Presentations.BatchUpdate(presentationID, req).Do()
	if err != nil {
		return nil, err
	}
	if log != nil {
		log.Record(step, resp, len(req.Requests))
	}
	return resp, nil
}
