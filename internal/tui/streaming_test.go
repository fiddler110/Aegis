package tui

import "testing"

func TestFindSafeMarkdownBoundary(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int // expected boundary (byte offset)
	}{
		{
			name: "empty",
			s:    "",
			want: 0,
		},
		{
			name: "no blank line",
			s:    "one line\nno blank",
			want: 0,
		},
		{
			name: "single blank line at end",
			s:    "paragraph\n\n",
			want: 11, // after the second \n
		},
		{
			name: "blank line mid-text",
			s:    "para one\n\npara two in progress",
			want: 10, // after "para one\n\n"
		},
		{
			name: "multiple blank lines – last one wins",
			s:    "a\n\nb\n\nc in progress",
			want: 6, // after "a\n\nb\n\n" (6 chars: a \n \n b \n \n)
		},
		{
			name: "blank line inside code fence ignored",
			s:    "pre\n\n```go\nfunc f() {\n\n}\n```\n\npost",
			// blank line inside the fence (after "func f() {") is ignored.
			// The last safe boundary is after the blank line following "```".
			// "pre\n\n```go\nfunc f() {\n\n}\n```\n\n" = 30 chars.
			want: 30,
		},
		{
			name: "incomplete fence – no boundary inside",
			s:    "pre\n\n```go\ncode\n",
			// blank line after "pre" is safe (before fence opens)
			want: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findSafeMarkdownBoundary(tc.s)
			if got != tc.want {
				t.Errorf("findSafeMarkdownBoundary(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

func TestIsFenceMarker(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"```", true},
		{"~~~", true},
		{"```go", true},
		{"~~~~", true},
		{"``", false},
		{"~~", false},
		{"", false},
		{"text", false},
		{"`a`", false},
	}
	for _, tc := range tests {
		got := isFenceMarker(tc.line)
		if got != tc.want {
			t.Errorf("isFenceMarker(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
