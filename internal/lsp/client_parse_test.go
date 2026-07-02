package lsp

import (
	"encoding/json"
	"testing"
)

func TestParseLocationsArray(t *testing.T) {
	locs := parseLocations(json.RawMessage(`[{"uri":"file:///a.go","range":{"start":{"line":4,"character":2},"end":{"line":4,"character":8}}}]`))
	if len(locs) != 1 || locs[0].URI != "file:///a.go" || locs[0].StartLine != 5 || locs[0].StartCol != 3 {
		t.Fatalf("got %+v", locs)
	}
}

func TestParseLocationsLink(t *testing.T) {
	// LocationLink form uses targetUri/targetRange.
	locs := parseLocations(json.RawMessage(`[{"targetUri":"file:///b.go","targetRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]`))
	if len(locs) != 1 || locs[0].URI != "file:///b.go" || locs[0].StartLine != 1 {
		t.Fatalf("got %+v", locs)
	}
}

func TestParseLocationsSingleAndNull(t *testing.T) {
	one := parseLocations(json.RawMessage(`{"uri":"file:///c.go","range":{"start":{"line":9,"character":0},"end":{"line":9,"character":4}}}`))
	if len(one) != 1 || one[0].StartLine != 10 {
		t.Fatalf("single: got %+v", one)
	}
	if parseLocations(json.RawMessage(`null`)) != nil {
		t.Error("expected nil for null")
	}
}

func TestParseMarkup(t *testing.T) {
	cases := map[string]string{
		`{"kind":"markdown","value":"func F()"}`: "func F()",
		`"plain hover"`:                          "plain hover",
		`[{"language":"go","value":"a"},"b"]`:    "a\nb",
		`null`:                                   "",
	}
	for in, want := range cases {
		if got := parseMarkup(json.RawMessage(in)); got != want {
			t.Errorf("parseMarkup(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSymbolInformation(t *testing.T) {
	syms := parseSymbolInformation(json.RawMessage(`[{"name":"Foo","kind":12,"location":{"uri":"file:///d.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}}}}]`))
	if len(syms) != 1 || syms[0].Name != "Foo" || syms[0].Kind != "function" {
		t.Fatalf("got %+v", syms)
	}
}

func TestSymbolKindName(t *testing.T) {
	if symbolKindName(5) != "class" || symbolKindName(23) != "struct" || symbolKindName(999) != "symbol" {
		t.Error("unexpected kind mapping")
	}
}
