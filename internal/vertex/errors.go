package vertex

import "fmt"

// TransientError is returned when the API fails with a retryable status code
// (429, 529, 5xx) and all retry attempts are exhausted.
type TransientError struct {
	StatusCode int
	Body       string
}

func (e *TransientError) Error() string {
	return fmt.Sprintf("transient API error (status %d): %s", e.StatusCode, e.Body)
}

// PermanentError is returned when the API fails with a non-retryable status
// code (4xx, except 429).
type PermanentError struct {
	StatusCode int
	Body       string
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent API error (status %d): %s", e.StatusCode, e.Body)
}
