package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"429 rate limit", &googleapi.Error{Code: 429}, true},
		{"500 internal", &googleapi.Error{Code: 500}, true},
		{"503 unavailable", &googleapi.Error{Code: 503}, true},
		{"400 bad request", &googleapi.Error{Code: 400}, false},
		{"403 forbidden", &googleapi.Error{Code: 403}, false},
		{"404 not found", &googleapi.Error{Code: 404}, false},
		{"non-API error", errors.New("network error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDoWithResult_Success(t *testing.T) {
	calls := 0
	result, err := DoWithResult(context.Background(), "test", func() (string, error) {
		calls++
		return "ok", nil
	}, WithMaxRetries(3), WithBaseDelay(time.Millisecond))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("got %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Fatalf("called %d times, want 1", calls)
	}
}

func TestDoWithResult_RetryThenSuccess(t *testing.T) {
	calls := 0
	result, err := DoWithResult(context.Background(), "test", func() (string, error) {
		calls++
		if calls < 3 {
			return "", &googleapi.Error{Code: 429, Message: "rate limited"}
		}
		return "ok", nil
	}, WithMaxRetries(5), WithBaseDelay(time.Millisecond))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("got %q, want %q", result, "ok")
	}
	if calls != 3 {
		t.Fatalf("called %d times, want 3", calls)
	}
}

func TestDoWithResult_MaxRetriesExceeded(t *testing.T) {
	calls := 0
	_, err := DoWithResult(context.Background(), "test-op", func() (string, error) {
		calls++
		return "", &googleapi.Error{Code: 429, Message: "rate limited"}
	}, WithMaxRetries(3), WithBaseDelay(time.Millisecond))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("called %d times, want 3", calls)
	}

	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected wrapped googleapi.Error, got %T", err)
	}
}

func TestDoWithResult_NonRetryableError(t *testing.T) {
	calls := 0
	_, err := DoWithResult(context.Background(), "test", func() (string, error) {
		calls++
		return "", &googleapi.Error{Code: 400, Message: "bad request"}
	}, WithMaxRetries(5), WithBaseDelay(time.Millisecond))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("called %d times, want 1 (should not retry)", calls)
	}
}

func TestDoWithResult_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := DoWithResult(ctx, "test", func() (string, error) {
		calls++
		if calls == 1 {
			cancel()
		}
		return "", &googleapi.Error{Code: 429, Message: "rate limited"}
	}, WithMaxRetries(5), WithBaseDelay(time.Millisecond))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDo(t *testing.T) {
	calls := 0
	err := Do(context.Background(), "test", func() error {
		calls++
		if calls < 2 {
			return &googleapi.Error{Code: 500, Message: "internal"}
		}
		return nil
	}, WithMaxRetries(3), WithBaseDelay(time.Millisecond))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("called %d times, want 2", calls)
	}
}
