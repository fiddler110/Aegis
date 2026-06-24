package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// APIError is a structured provider error. Adapters return it from Stream so the
// retry layer (see retry.go) can classify failures without parsing error
// strings. StatusCode == 0 denotes a transport/network failure (Err is set).
type APIError struct {
	Provider   string        // adapter name, e.g. "anthropic"
	StatusCode int           // HTTP status; 0 for transport-level errors
	Message    string        // response body or error detail
	RetryAfter time.Duration // parsed from a Retry-After header; 0 if absent
	Err        error         // underlying transport error, if any
}

// Error implements error.
func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		if e.Err != nil {
			return fmt.Sprintf("%s: request failed: %v", e.Provider, e.Err)
		}
		return fmt.Sprintf("%s: request failed", e.Provider)
	}
	return fmt.Sprintf("%s: status %d: %s", e.Provider, e.StatusCode, e.Message)
}

// Unwrap exposes the underlying transport error to errors.Is/As.
func (e *APIError) Unwrap() error { return e.Err }

// Retryable reports whether the request may succeed if retried. Rate limits
// (429), request timeouts (408), conflicts (409) and 5xx server errors are
// transient; transport errors are retryable unless they stem from context
// cancellation. 4xx client errors (other than the above) are permanent.
func (e *APIError) Retryable() bool {
	if errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded) {
		return false
	}
	if e.StatusCode == 0 {
		return e.Err != nil // a transport error with no status
	}
	switch e.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode <= 599
}

// parseRetryAfter interprets a Retry-After header value, which is either a
// number of seconds or an HTTP date. It returns 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// NewHTTPError builds an APIError from a non-2xx HTTP response.
func NewHTTPError(providerName string, status int, retryAfter, body string) *APIError {
	return &APIError{
		Provider:   providerName,
		StatusCode: status,
		Message:    body,
		RetryAfter: parseRetryAfter(retryAfter),
	}
}

// NewTransportError builds an APIError from a transport-level failure (no HTTP
// status, e.g. connection refused or DNS error).
func NewTransportError(providerName string, err error) *APIError {
	return &APIError{Provider: providerName, Err: err}
}
