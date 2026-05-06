package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

var agentPrefixRe = regexp.MustCompile(`^\[agent:(\w+)\]\s*(.*)`)
var pipelineStepRe = regexp.MustCompile(`step (\d)/5:\s*(\w+)`)

type MonitorHandler struct {
	inner  slog.Handler
	broker *Broker
}

func NewHandler(inner slog.Handler, broker *Broker) *MonitorHandler {
	return &MonitorHandler{inner: inner, broker: broker}
}

func (h *MonitorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *MonitorHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.inner.Handle(ctx, r)

	event := h.classify(r)
	if event != nil {
		h.broker.Broadcast(*event)
	}

	return err
}

func (h *MonitorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &MonitorHandler{inner: h.inner.WithAttrs(attrs), broker: h.broker}
}

func (h *MonitorHandler) WithGroup(name string) slog.Handler {
	return &MonitorHandler{inner: h.inner.WithGroup(name), broker: h.broker}
}

func (h *MonitorHandler) classify(r slog.Record) *Event {
	msg := r.Message
	data := extractAttrs(r)
	level := levelString(r.Level)

	if m := agentPrefixRe.FindStringSubmatch(msg); m != nil {
		agent := m[1]
		rest := m[2]
		return h.classifyAgent(agent, rest, msg, data, level, r.Time)
	}

	if strings.HasPrefix(msg, "[pipeline]") {
		rest := strings.TrimPrefix(msg, "[pipeline] ")
		return h.classifyPipeline(rest, msg, data, level, r.Time)
	}

	if strings.HasPrefix(msg, "assembler:") {
		return &Event{
			Type:      EventAgentDone,
			Agent:     "assembler",
			Timestamp: r.Time,
			Message:   msg,
			Data:      data,
			Level:     level,
		}
	}

	if strings.HasPrefix(msg, "[enforceMaxChars]") || strings.HasPrefix(msg, "[validate]") {
		return &Event{
			Type:      EventLog,
			Agent:     "pipeline",
			Timestamp: r.Time,
			Message:   msg,
			Data:      data,
			Level:     level,
		}
	}

	if r.Level >= slog.LevelError {
		return &Event{
			Type:      EventPipelineError,
			Agent:     "pipeline",
			Timestamp: r.Time,
			Message:   msg,
			Data:      data,
			Level:     level,
		}
	}

	return nil
}

func (h *MonitorHandler) classifyAgent(agent, rest, fullMsg string, data map[string]any, level string, ts time.Time) *Event {
	e := &Event{
		Agent:     agent,
		Timestamp: ts,
		Message:   fullMsg,
		Data:      data,
		Level:     level,
	}

	switch {
	case strings.HasPrefix(rest, "starting"):
		e.Type = EventAgentStart
	case strings.HasPrefix(rest, "API usage"):
		e.Type = EventAgentUsage
	case strings.HasPrefix(rest, "done"):
		e.Type = EventAgentDone
	case strings.HasPrefix(rest, "failed"):
		e.Type = EventAgentError
	case strings.HasPrefix(rest, "plan approved"):
		e.Type = EventReviewResult
		e.Data["approved"] = true
	case strings.HasPrefix(rest, "issues found"):
		e.Type = EventReviewResult
		e.Data["approved"] = false
	case strings.HasPrefix(rest, "retrying"):
		e.Type = EventRetry
	case strings.HasPrefix(rest, "mapping"):
		e.Type = EventAgentStart
	case strings.HasPrefix(rest, "validating"):
		e.Type = EventAgentStart
	default:
		e.Type = EventLog
	}

	return e
}

func (h *MonitorHandler) classifyPipeline(rest, fullMsg string, data map[string]any, level string, ts time.Time) *Event {
	e := &Event{
		Agent:     "pipeline",
		Timestamp: ts,
		Message:   fullMsg,
		Data:      data,
		Level:     level,
	}

	switch {
	case strings.HasPrefix(rest, "starting"):
		e.Type = EventPipelineStart
	case strings.HasPrefix(rest, "step"):
		e.Type = EventPipelineStep
		if m := pipelineStepRe.FindStringSubmatch(rest); m != nil {
			e.Data["step"] = m[1]
			e.Data["agent"] = m[2]
		}
	case strings.HasPrefix(rest, "generation complete"):
		e.Type = EventPipelineDone
	case strings.Contains(rest, "validation failed") || strings.Contains(rest, "retrying"):
		e.Type = EventRetry
		if strings.Contains(rest, "selector") {
			e.Agent = "selector"
		}
	case strings.Contains(rest, "review iteration"):
		e.Type = EventRetry
		e.Agent = "reviewer"
	case strings.Contains(rest, "reviewer failed") || strings.Contains(rest, "reviewer did not approve"):
		e.Type = EventLog
		e.Agent = "reviewer"
	default:
		e.Type = EventLog
	}

	return e
}

func extractAttrs(r slog.Record) map[string]any {
	data := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		data[a.Key] = resolveAttr(a.Value)
		return true
	})
	return data
}

func resolveAttr(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v.Any())
	}
}

func levelString(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	default:
		return "info"
	}
}
