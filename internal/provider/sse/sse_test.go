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

// TestApplyResponseHeaderTimeout_PreservesTheRestOfTheClient is the P61.5
// composition guarantee: applying a header timeout must change the header
// timeout and nothing else. Everything an earlier option put on the client —
// notably the deliberately-zero Client.Timeout (P59.2) and any transport tuning
// — has to survive, because the old "build a fresh client and assign it"
// implementation silently discarded all of it.
func TestApplyResponseHeaderTimeout_PreservesTheRestOfTheClient(t *testing.T) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 17
	tr.DisableCompression = true
	base := &http.Client{Timeout: 0, Transport: tr}

	got := ApplyResponseHeaderTimeout(base, 90*time.Second)
	gotTr, ok := got.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", got.Transport)
	}
	if gotTr.ResponseHeaderTimeout != 90*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 90s", gotTr.ResponseHeaderTimeout)
	}
	if gotTr.MaxIdleConnsPerHost != 17 || !gotTr.DisableCompression {
		t.Errorf("transport settings lost: MaxIdleConnsPerHost=%d DisableCompression=%v",
			gotTr.MaxIdleConnsPerHost, gotTr.DisableCompression)
	}
	if got.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0 (P59.2: a streamed turn must not be capped)", got.Timeout)
	}
	// The caller's transport may be shared with the rest of their program, so it
	// is cloned rather than reconfigured in place.
	if tr.ResponseHeaderTimeout != 0 {
		t.Errorf("the source transport was mutated: ResponseHeaderTimeout = %v", tr.ResponseHeaderTimeout)
	}
	if got == base {
		t.Error("ApplyResponseHeaderTimeout returned the caller's own client")
	}
}

// TestApplyResponseHeaderTimeout_KeepsACustomRoundTripper: a client whose
// transport is not an *http.Transport (a test stub, an instrumented wrapper)
// has no ResponseHeaderTimeout field to set. Keeping the RoundTripper and
// skipping the timeout is the lesser evil — dropping it to gain a timeout is
// exactly the wholesale replacement P61.5 removed.
func TestApplyResponseHeaderTimeout_KeepsACustomRoundTripper(t *testing.T) {
	custom := roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	got := ApplyResponseHeaderTimeout(&http.Client{Transport: custom}, time.Minute)
	if _, ok := got.Transport.(roundTripperFunc); !ok {
		t.Errorf("Transport = %T, want the caller's RoundTripper preserved", got.Transport)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestApplyResponseHeaderTimeout_NilClient falls back to a full streaming
// client, so a caller never has to nil-check before composing.
func TestApplyResponseHeaderTimeout_NilClient(t *testing.T) {
	got := ApplyResponseHeaderTimeout(nil, 5*time.Minute)
	if tr := got.Transport.(*http.Transport); tr.ResponseHeaderTimeout != 5*time.Minute {
		t.Errorf("ResponseHeaderTimeout = %v, want 5m", tr.ResponseHeaderTimeout)
	}
	if got.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0", got.Timeout)
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
