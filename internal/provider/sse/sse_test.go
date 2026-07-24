package sse

import (
	"net/http"
	"testing"
	"time"
)

// TestNewStreamingClient_DefaultTimeout is the P35.5/P38.1 regression: leaving
// the header timeout unset (<= 0) must substitute the default, which P38.1
// raised to 30m so a slow-prefill local threat-model turn is not killed
// mid-build (5m was too tight for a content-rich repo on modest hardware).
func TestNewStreamingClient_DefaultTimeout(t *testing.T) {
	c := NewStreamingClient(0)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
	}
	if DefaultResponseHeaderTimeout != 30*time.Minute {
		t.Errorf("DefaultResponseHeaderTimeout = %v, want 30m", DefaultResponseHeaderTimeout)
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
