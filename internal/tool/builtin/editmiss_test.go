package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const missSample = "package calc\n\nfunc Div(a, b int) int {\n\treturn a / b\n}\n"

func TestEditMissHintWhitespace(t *testing.T) {
	// The model sent four spaces where the file has a tab.
	_, _, msg := applyReplacement(missSample, "func Div(a, b int) int {\n    return a / b", "x", false, "calc.go")
	if !strings.HasPrefix(msg, "old_string not found in calc.go") {
		t.Fatalf("msg = %q", msg)
	}
	if !strings.Contains(msg, "line 3 if whitespace is ignored") || !strings.Contains(msg, "4| \treturn a / b") {
		t.Errorf("want a whitespace hint quoting lines 3-4, got:\n%s", msg)
	}
}

func TestEditMissHintClosestLine(t *testing.T) {
	_, _, msg := applyReplacement(missSample, "return a / b // divide", "x", false, "calc.go")
	if !strings.Contains(msg, "closest line in the file is 4") {
		t.Errorf("want a closest-line hint, got:\n%s", msg)
	}
}

func TestEditMissHintLiteralEscapes(t *testing.T) {
	_, _, msg := applyReplacement(missSample, `func Div(a, b int) int {\n\treturn a / b`, "x", false, "calc.go")
	if !strings.Contains(msg, `contains the two characters \n`) {
		t.Errorf("want an escaped-newline hint, got:\n%s", msg)
	}
}

func TestEditMissHintSaysNothingWhenNothingIsClose(t *testing.T) {
	_, _, msg := applyReplacement(missSample, "completely unrelated text here", "x", false, "calc.go")
	if msg != "old_string not found in calc.go" {
		t.Errorf("want the bare miss, got:\n%s", msg)
	}
}

func TestEditMissHintIsBoundedAndDeterministic(t *testing.T) {
	long := "x := " + strings.Repeat("abcdefgh ", 400) + "\n"
	content := strings.Repeat(long, 20)
	_, _, a := applyReplacement(content, strings.Replace(long, "x :=", "x  :=", 1)+"y", "z", false, "f.go")
	_, _, b := applyReplacement(content, strings.Replace(long, "x :=", "x  :=", 1)+"y", "z", false, "f.go")
	if a != b {
		t.Error("two identical misses must produce identical errors (the repeated-failure breaker compares them)")
	}
	if len(a) > len("old_string not found in f.go")+1+editMissHintMax+len("…") {
		t.Errorf("hint not bounded: %d bytes", len(a))
	}
}

func TestDoubleEscapedContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		suspect bool
	}{
		{"double-escaped go", `package main\n\nimport "testing"\n\nfunc TestDiv(t *testing.T) {\n\tif Div(4, 2) != 2 {\n\t\tt.Fatal("bad")\n\t}\n}\n`, true},
		{"normal go printing newlines", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Print(\"a\\n\")\n\tfmt.Print(\"b\\n\")\n\tfmt.Print(\"c\\n\")\n}\n", false},
		{"one-line json with escaped strings", `{"text":"line1\nline2\nline3\nline4","more":"a\tb\tc"}`, false},
		{"plain prose", "hello world", false},
	}
	for _, c := range cases {
		if _, _, got := doubleEscapedContent(c.content); got != c.suspect {
			t.Errorf("%s: suspect = %v; want %v", c.name, got, c.suspect)
		}
	}
}

func TestWriteFileRefusesDoubleEscapedContent(t *testing.T) {
	root := t.TempDir()
	w := &writeTool{root: root}
	res, err := w.Execute(context.Background(), mustJSON(t, map[string]any{
		"path":    "main_test.go",
		"content": `package main\n\nfunc TestX(t *testing.T) {\n\tt.Log("x")\n}\n`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "double-escaped") {
		t.Fatalf("want a double-escape refusal, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "main_test.go")); !os.IsNotExist(err) {
		t.Error("the refused file must not be written")
	}
}
