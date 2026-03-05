package netretry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/avast/retry-go"
)

const (
	defaultAttempts = 3
	defaultDelay    = 400 * time.Millisecond
	defaultMaxDelay = 3 * time.Second
)

// HTTPStatusError represents an HTTP response error and carries enough context
// for logging/debugging and retry classification.
type HTTPStatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("API error: status %d, %s %s", e.StatusCode, e.Method, e.URL)
	}
	return fmt.Sprintf("API error: status %d, %s %s, body: %s", e.StatusCode, e.Method, e.URL, e.Body)
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

// Permanent marks an error as non-retryable.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// Do runs operation with retry-go and a shared policy for transient network failures.
func Do(ctx context.Context, operation func() error) error {
	return retry.Do(
		operation,
		retry.Context(ctx),
		retry.Attempts(defaultAttempts),
		retry.Delay(defaultDelay),
		retry.MaxDelay(defaultMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.RetryIf(isRetryable),
	)
}

// IsRetryableStatusCode classifies HTTP statuses that are generally transient.
func IsRetryableStatusCode(code int) bool {
	switch code {
	case 408, 425, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	var perr *permanentError
	if errors.As(err, &perr) {
		return false
	}

	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return IsRetryableStatusCode(httpErr.StatusCode)
	}

	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Timeout() {
		return true
	}

	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}

	// Catch common transient transport errors wrapped in text.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "server closed idle connection") ||
		strings.Contains(msg, "no such host")
}
