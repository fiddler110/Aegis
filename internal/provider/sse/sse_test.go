package sse

import (
	"net/http"
	"testing"
	"time"
)

// TestNewStreamingClient_DefaultTimeout is the P35.5 regression: leaving the
// header timeout unset (<= 0) must keep the previously-hardcoded 5-minute
// value so behavior is unchanged for callers that don't opt in to a
// configured provider.response_header_timeout.
func TestNewStreamingClient_DefaultTimeout(t *testing.T) {
	c := NewStreamingClient(0)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
	}
	if DefaultResponseHeaderTimeout != 5*time.Minute {
		t.Errorf("DefaultResponseHeaderTimeout = %v, want 5m", DefaultResponseHeaderTimeout)
	}
}

// TestNewStreamingClient_CustomTimeout verifies an explicit positive timeout
// is honored instead of the default (P35.5 — lets a slow-prefill local box
// raise the ceiling via provider.response_header_timeout).
func TestNewStreamingClient_CustomTimeout(t *testing.T) {
	c := NewStreamingClient(15 * time.Minute)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != 15*time.Minute {
		t.Errorf("ResponseHeaderTimeout = %v, want 15m", tr.ResponseHeaderTimeout)
	}
}

// TestNewStreamingClient_NegativeTimeoutFallsBackToDefault mirrors the
// zero-value case: a negative value (e.g. a bad config) also substitutes the
// default rather than producing an effectively-instant timeout.
func TestNewStreamingClient_NegativeTimeoutFallsBackToDefault(t *testing.T) {
	c := NewStreamingClient(-1 * time.Second)
	tr := c.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want default %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
	}
}
