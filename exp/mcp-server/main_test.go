package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStructuredError(t *testing.T) {
	t.Run("validation error", func(t *testing.T) {
		r := structuredError(errValidation, false, "Empty content")
		if !r.IsError {
			t.Fatal("expected IsError=true")
		}
		text := extractText(t, r)
		if !strings.Contains(text, "[validation]") {
			t.Errorf("missing [validation] prefix in %q", text)
		}
		if !strings.Contains(text, "Retryable: false") {
			t.Errorf("missing Retryable: false in %q", text)
		}
		if !strings.Contains(text, "Empty content") {
			t.Errorf("missing message in %q", text)
		}
	})

	t.Run("transient error", func(t *testing.T) {
		r := structuredError(errTransient, true, "timeout occurred")
		if !r.IsError {
			t.Fatal("expected IsError=true")
		}
		text := extractText(t, r)
		if !strings.Contains(text, "[transient]") {
			t.Errorf("missing [transient] prefix in %q", text)
		}
		if !strings.Contains(text, "Retryable: true") {
			t.Errorf("missing Retryable: true in %q", text)
		}
	})

	t.Run("business error", func(t *testing.T) {
		r := structuredError(errBusiness, false, "no matching template")
		if !r.IsError {
			t.Fatal("expected IsError=true")
		}
		text := extractText(t, r)
		if !strings.Contains(text, "[business]") {
			t.Errorf("missing [business] prefix in %q", text)
		}
		if !strings.Contains(text, "Retryable: false") {
			t.Errorf("missing Retryable: false in %q", text)
		}
	})
}

func TestIsTransientPipelineError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"timeout", errors.New("request timeout after 30s"), true},
		{"429_rate_limit", errors.New("API returned 429: rate limit exceeded"), true},
		{"529_overloaded", errors.New("got 529 overloaded"), true},
		{"context_deadline", errors.New("context deadline exceeded"), true},
		{"temporarily_unavailable", errors.New("service temporarily unavailable"), true},
		{"generic_error", errors.New("invalid template configuration"), false},
		{"empty_error", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientPipelineError(tt.err)
			if got != tt.want {
				t.Errorf("isTransientPipelineError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func extractText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	return tc.Text
}
