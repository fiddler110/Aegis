package diagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKrokiRender(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte("<svg>ok</svg>"))
	}))
	defer srv.Close()

	k := NewKroki(srv.URL)
	data, ct, err := k.Render(context.Background(), "mermaid", "graph TD; A-->B", "svg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<svg>ok</svg>" || ct != "image/svg+xml" {
		t.Errorf("unexpected render result: %q %q", data, ct)
	}
	if gotPath != "/mermaid/svg" {
		t.Errorf("kroki path = %q, want /mermaid/svg", gotPath)
	}
	if !strings.Contains(gotBody, "A-->B") {
		t.Errorf("source not posted: %q", gotBody)
	}
}

func TestKrokiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("syntax error"))
	}))
	defer srv.Close()
	_, _, err := NewKroki(srv.URL).Render(context.Background(), "mermaid", "bad", "svg")
	if err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("expected kroki error surfaced, got %v", err)
	}
}

func TestRenderDrawIOWrapsSVG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte("<svg id=\"x\"/>"))
	}))
	defer srv.Close()

	data, ct, err := Render(context.Background(), NewKroki(srv.URL), "mermaid", "graph TD; A-->B", FormatDrawIO)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if ct != "application/xml" {
		t.Errorf("content type = %q, want application/xml", ct)
	}
	if !strings.Contains(out, "<mxfile") || !strings.Contains(out, "shape=image") {
		t.Errorf("drawio wrapper malformed: %q", out)
	}
	if !strings.Contains(out, "data:image/svg+xml,") {
		t.Errorf("drawio wrapper missing embedded svg data uri")
	}
}

func TestRenderValidation(t *testing.T) {
	k := NewKroki("http://unused")
	if _, _, err := Render(context.Background(), k, "", "src", "svg"); err == nil {
		t.Error("expected error for empty type")
	}
	if _, _, err := Render(context.Background(), k, "mermaid", "  ", "svg"); err == nil {
		t.Error("expected error for empty source")
	}
}

func TestChainFallsBack(t *testing.T) {
	// First renderer always fails; second succeeds.
	failing := failRenderer{}
	ok := okRenderer{}
	chain := NewChain(failing, ok)
	data, _, err := chain.Render(context.Background(), "mermaid", "x", "svg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Errorf("chain did not fall back, got %q", data)
	}
}

type failRenderer struct{}

func (failRenderer) Name() string { return "fail" }
func (failRenderer) Render(context.Context, string, string, string) ([]byte, string, error) {
	return nil, "", context.DeadlineExceeded
}

type okRenderer struct{}

func (okRenderer) Name() string { return "ok" }
func (okRenderer) Render(context.Context, string, string, string) ([]byte, string, error) {
	return []byte("ok"), "image/svg+xml", nil
}
