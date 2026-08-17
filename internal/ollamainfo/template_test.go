package ollamainfo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTemplateDropsToolCalls pins the detector against templates captured from
// a real Ollama server on 2026-08-17, because the defect it looks for is a
// property of vendor-shipped templates rather than of anything in this repo:
// a synthetic fixture would keep passing after the real templates changed shape.
//
// The measurement behind the "drops" verdict, on qwen3:14b-32k at temperature 0
// with a history of prose + read_file{path:"srv/etc/config.txt"} + result, then
// asked which path it had read: 0/3 correct as captured, 3/3 with the prose
// withheld, and 3/3 with the corrected template below and the prose intact.
func TestTemplateDropsToolCalls(t *testing.T) {
	cases := []struct {
		file string
		want bool
		why  string
	}{
		{
			file: "qwen3-14b-go.tmpl",
			want: true,
			why:  "stock Qwen3 Go template renders .Content and .ToolCalls as if/else-if branches",
		},
		{
			file: "qwen35-9b-jinja.tmpl",
			want: false,
			why:  "Qwen3.5 ships a Jinja template that renders prose and then the tool call",
		},
		{
			file: "qwen3-14b-go-fixed.tmpl",
			want: false,
			why:  "same template with the else-if split into two independent ifs",
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read testdata: %v", err)
			}
			if got := templateDropsToolCalls(string(b)); got != tc.want {
				t.Errorf("templateDropsToolCalls(%s) = %v, want %v — %s", tc.file, got, tc.want, tc.why)
			}
		})
	}
}

// TestTemplateDropsToolCallsEdgeCases covers the shapes the regex must not
// confuse: a template that renders both unconditionally is fine, and one that
// merely mentions .ToolCalls inside a plain if is fine too. Only reaching
// .ToolCalls through an else branch means content can preempt it.
func TestTemplateDropsToolCallsEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		want bool
	}{
		{"empty", "", false},
		{"content then calls", `{{ if .Content }}{{ .Content }}{{ end }}{{ if .ToolCalls }}x{{ end }}`, false},
		{"calls only", `{{ if .ToolCalls }}x{{ end }}`, false},
		{"else if", `{{ if .Content }}{{ .Content }}{{ else if .ToolCalls }}x{{ end }}`, true},
		{"else if, trimmed", `{{ if .Content }}a{{- else if .ToolCalls }}x{{ end }}`, true},
		{"else with", `{{ if .Content }}a{{ else with .ToolCalls }}x{{ end }}`, true},
		{"else, no calls", `{{ if .Content }}a{{ else if .Thinking }}x{{ end }}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := templateDropsToolCalls(tc.tmpl); got != tc.want {
				t.Errorf("templateDropsToolCalls(%q) = %v, want %v", tc.tmpl, got, tc.want)
			}
		})
	}
}
