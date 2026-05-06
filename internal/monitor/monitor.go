package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/owulveryck/agentigslide/internal/agent"
)

type Monitor struct {
	broker    *Broker
	config    agent.Config
	requestCh chan []byte

	started   bool
	startedMu sync.Mutex
}

func New(cfg agent.Config) *Monitor {
	return &Monitor{
		broker:    NewBroker(),
		config:    cfg,
		requestCh: make(chan []byte, 1),
	}
}

func (m *Monitor) WaitForRequest(ctx context.Context) ([]byte, error) {
	select {
	case data := <-m.requestCh:
		return data, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("cancelled while waiting for upload: %w", ctx.Err())
	}
}

func (m *Monitor) MarkStarted() {
	m.startedMu.Lock()
	m.started = true
	m.startedMu.Unlock()
}

func (m *Monitor) isStarted() bool {
	m.startedMu.Lock()
	defer m.startedMu.Unlock()
	return m.started
}

func (m *Monitor) Broker() *Broker {
	return m.broker
}

func (m *Monitor) Handler(inner slog.Handler) slog.Handler {
	return NewHandler(inner, m.broker)
}

func (m *Monitor) SendURL(url string) {
	m.broker.Broadcast(Event{
		Type:      EventPresentationURL,
		Agent:     "pipeline",
		Timestamp: time.Now(),
		Message:   url,
	})
}

func (m *Monitor) SendError(message string) {
	m.broker.Broadcast(Event{
		Type:      EventPipelineError,
		Agent:     "pipeline",
		Timestamp: time.Now(),
		Message:   message,
		Level:     "error",
	})
}

func (m *Monitor) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", m.serveDashboard)
	mux.HandleFunc("GET /events", m.serveSSE)
	mux.HandleFunc("GET /config", m.serveConfig)
	mux.HandleFunc("POST /upload", m.serveUpload)
	return http.ListenAndServe(addr, mux)
}
