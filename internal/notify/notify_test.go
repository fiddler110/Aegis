package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewNilWhenDisabled(t *testing.T) {
	if n := New(false, "", nil); n != nil {
		t.Error("expected nil Notifier when no channel enabled")
	}
	if n := New(true, "", nil); n == nil {
		t.Error("expected non-nil Notifier when desktop enabled")
	}
}

func TestWebhookDelivery(t *testing.T) {
	received := make(chan Event, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev Event
		json.NewDecoder(r.Body).Decode(&ev)
		received <- ev
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(false, srv.URL, nil)
	n.Notify(context.Background(), Event{SessionID: "abc", Status: StatusCompleted, Message: "done"})

	select {
	case ev := <-received:
		if ev.SessionID != "abc" || ev.Status != StatusCompleted {
			t.Errorf("bad event: %+v", ev)
		}
		if ev.Time == "" {
			t.Error("expected Time to be populated")
		}
	default:
		t.Fatal("webhook did not receive event")
	}
}

func TestOSAQuote(t *testing.T) {
	if got := osaQuote(`he said "hi"`); got != `"he said \"hi\""` {
		t.Errorf("osaQuote = %s", got)
	}
}
