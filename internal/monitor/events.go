package monitor

import "time"

type EventType string

const (
	EventPipelineStart   EventType = "pipeline_start"
	EventPipelineStep    EventType = "pipeline_step"
	EventPipelineDone    EventType = "pipeline_done"
	EventAgentStart      EventType = "agent_start"
	EventAgentUsage      EventType = "agent_usage"
	EventAgentDone       EventType = "agent_done"
	EventAgentError      EventType = "agent_error"
	EventRetry           EventType = "retry"
	EventReviewResult    EventType = "review_result"
	EventLog             EventType = "log"
	EventPresentationURL EventType = "presentation_url"
	EventPipelineError   EventType = "pipeline_error"
)

type Event struct {
	Type      EventType      `json:"type"`
	Agent     string         `json:"agent"`
	Timestamp time.Time      `json:"timestamp"`
	Message   string         `json:"message"`
	Data      map[string]any `json:"data,omitempty"`
	Level     string         `json:"level"`
}
