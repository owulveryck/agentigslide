package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (m *Monitor) serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (m *Monitor) serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Flush headers immediately so the browser's EventSource fires onopen.
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ch := m.broker.Subscribe()
	defer m.broker.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (m *Monitor) serveConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cfg := map[string]any{
		"agents": map[string]any{
			"outliner": map[string]any{
				"model":     m.config.OutlinerModel,
				"maxTokens": m.config.OutlinerMaxTokens,
				"role":      "Analyse structurelle de la demande utilisateur, decompose en sections et slides",
			},
			"selector": map[string]any{
				"model":      m.config.SelectorModel,
				"maxRetries": m.config.MaxSelectorRetries,
				"role":       "Associe chaque besoin de slide a un template du catalogue",
			},
			"writer": map[string]any{
				"model":       m.config.WriterModel,
				"simpleModel": m.config.WriterSimpleModel,
				"maxParallel": m.config.MaxParallel,
				"role":        "Genere le contenu textuel pour chaque slide",
			},
			"assembler": map[string]any{
				"role": "Combine les sorties des Writers en plan de generation unifie",
			},
			"reviewer": map[string]any{
				"model":          m.config.ReviewerModel,
				"thinkingBudget": m.config.ReviewerThinkingBudget,
				"maxRetries":     m.config.MaxReviewRetries,
				"role":           "Valide la qualite du plan assemble (debordement, doublons, coherence)",
			},
		},
	}

	_ = json.NewEncoder(w).Encode(cfg)
}
