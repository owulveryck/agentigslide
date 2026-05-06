package monitor

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/owulveryck/slideAppScripter/internal/agent"
)

type Monitor struct {
	broker *Broker
	config agent.Config
}

func New(cfg agent.Config) *Monitor {
	return &Monitor{
		broker: NewBroker(),
		config: cfg,
	}
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

func (m *Monitor) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", m.serveDashboard)
	mux.HandleFunc("GET /events", m.serveSSE)
	mux.HandleFunc("GET /config", m.serveConfig)
	return http.ListenAndServe(addr, mux)
}
