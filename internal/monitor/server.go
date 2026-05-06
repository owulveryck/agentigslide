package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (m *Monitor) serveUpload(w http.ResponseWriter, r *http.Request) {
	if m.isStarted() {
		http.Error(w, `{"error":"pipeline already started"}`, http.StatusConflict)
		return
	}

	var data []byte
	var err error

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		file, _, fErr := r.FormFile("file")
		if fErr != nil {
			http.Error(w, `{"error":"missing file field"}`, http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		data, err = io.ReadAll(file)
	} else {
		data, err = io.ReadAll(r.Body)
	}
	if err != nil {
		http.Error(w, `{"error":"read failed"}`, http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		http.Error(w, `{"error":"empty content"}`, http.StatusBadRequest)
		return
	}

	select {
	case m.requestCh <- data:
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	default:
		http.Error(w, `{"error":"upload already pending"}`, http.StatusConflict)
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
