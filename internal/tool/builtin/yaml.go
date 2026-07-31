package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/fiddler110/aegis/internal/tool"
)

const (
	// maxYAMLBytes bounds what yaml_validate will parse. YAML deliverables in
	// this codebase (inventory.yaml, slide decks) are kilobytes; a 5 MiB ceiling
	// stops a mis-aimed call on a huge data file from parsing a whole tree into
	// memory for a structural probe.
	maxYAMLBytes = 5 << 20
	// maxYAMLOutlineKeys caps the success outline so a wide mapping can't flood
	// a turn's context — the point is a cheap structural probe, not a dump.
	maxYAMLOutlineKeys = 60
	// yamlExcerptContext is how many lines on each side of a parse error the
	// excerpt shows.
	yamlExcerptContext = 2
)

// yamlErrLine pulls the line number out of a go.yaml.in/yaml error string,
// which is formatted as "yaml: line N: <problem>" (decode.go's parser.fail).
// The library never exposes the problem mark's column for syntax errors, so
// the line plus a source excerpt is the most precise location available — see
// formatYAMLError.
var yamlErrLine = regexp.MustCompile(`(?:^|\n)\s*yaml: line (\d+): `)

// yamlValidateTool parses a workspace YAML file and reports either the parse
// failure (with line and a source excerpt) or a compact outline of its
// top-level keys (P52.9).
//
// YAML is a first-class deliverable in two shipped flows — the threat-model
// suite's inventory.yaml and the documentation-as-code skill's slide decks —
// yet the model edits both as opaque text with edit_file. A broken indent is
// otherwise invisible until a downstream consumer fails with an error far from
// the cause, which on a slow local model costs several turns to localize.
//
// The success path deliberately returns the key outline rather than a bare
// "ok": that makes the tool a cheap structural probe usable *before* an edit
// (what keys exist, what line is each on, is `slides` a list) and not only a
// post-hoc check. Capability is CapRead: it opens one file and writes nothing.
type yamlValidateTool struct{ root string }

func (t *yamlValidateTool) Name() string                { return "yaml_validate" }
func (t *yamlValidateTool) Capability() tool.Capability { return tool.CapRead }

func (t *yamlValidateTool) Description() string {
	return "Check that a workspace YAML file parses, and see its shape. On failure it reports the parse error with the line it occurred on plus a source excerpt; on success it returns a compact outline of the top-level keys with their value kinds and line numbers. Use it before editing a .yaml file to learn its structure without reading the whole thing, and after editing to confirm the indentation still parses — otherwise a broken file only surfaces later, as a downstream consumer's error far from the actual cause."
}

func (t *yamlValidateTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative path to the YAML file"}},"required":["path"]}`)
}

func (t *yamlValidateTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"result":{"type":"string","description":"parse error with line and source excerpt, or a top-level key outline on success"}},"required":["result"]}`)
}

func (t *yamlValidateTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return tool.Result{Content: "path is required", IsError: true}, nil
	}
	// Same workspace confinement (and symlink resolution) as every other
	// file-touching builtin.
	abs, err := resolvePath(effectiveRoot(ctx, t.root), args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}
	if info.IsDir() {
		return tool.Result{Content: fmt.Sprintf("%s is a directory, not a YAML file", args.Path), IsError: true}, nil
	}
	if info.Size() > maxYAMLBytes {
		return tool.Result{Content: fmt.Sprintf("%s is too large to validate (%d bytes, max %d)", args.Path, info.Size(), maxYAMLBytes), IsError: true}, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return tool.Result{Content: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}, nil
	}

	// Decode document-by-document: yaml.Unmarshal silently ignores everything
	// after the first `---`, so a validator that used it would call a file with
	// a broken second document valid.
	var docs []*yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return tool.Result{Content: formatYAMLError(args.Path, data, err), IsError: true}, nil
		}
		docs = append(docs, &doc)
	}
	return tool.Result{Content: yamlOutline(args.Path, docs)}, nil
}

// formatYAMLError renders a parse failure with the line the parser reported and
// a short source excerpt around it. go.yaml.in/yaml/v3 only ever puts the line
// in its error string (the problem mark's column is not exported), so the
// excerpt — not a fabricated column — is what pins the failure down.
func formatYAMLError(path string, data []byte, err error) string {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	var b strings.Builder
	fmt.Fprintf(&b, "INVALID YAML: %s\n", path)
	if m := yamlErrLine.FindStringSubmatch(err.Error()); m != nil {
		line, _ := strconv.Atoi(m[1])
		detail := msg
		if marker := fmt.Sprintf("line %d: ", line); strings.Contains(msg, marker) {
			detail = msg[strings.Index(msg, marker)+len(marker):]
		}
		fmt.Fprintf(&b, "parse error at line %d (the parser does not report a column): %s\n",
			line, strings.TrimSpace(detail))
		b.WriteString(yamlExcerpt(data, line))
	} else {
		// Some failures (unknown anchor, multi-error unmarshal) carry no line.
		fmt.Fprintf(&b, "parse error (no line reported): %s\n", msg)
	}
	return strings.TrimRight(b.String(), "\n")
}

// yamlExcerpt renders the failing line and its neighbours, marking the failing
// one with ">".
func yamlExcerpt(data []byte, line int) string {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	lo, hi := line-yamlExcerptContext, line+yamlExcerptContext
	if lo < 1 {
		lo = 1
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	var b strings.Builder
	for i := lo; i <= hi; i++ {
		marker := " "
		if i == line {
			marker = ">"
		}
		fmt.Fprintf(&b, "%s %4d | %s\n", marker, i, lines[i-1])
	}
	return b.String()
}

// yamlOutline renders the success report: per document, the top-level keys with
// their value kinds and line numbers.
func yamlOutline(path string, docs []*yaml.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "valid YAML: %s (%d document(s))\n", path, len(docs))
	if len(docs) == 0 {
		b.WriteString("file is empty — no documents\n")
		return strings.TrimRight(b.String(), "\n")
	}
	for i, doc := range docs {
		root := doc
		if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
			root = root.Content[0]
		}
		if len(docs) > 1 {
			fmt.Fprintf(&b, "document %d:\n", i+1)
		}
		switch {
		case root.Kind == yaml.DocumentNode || root.Kind == 0:
			b.WriteString("  (empty document)\n")
		case root.Kind == yaml.MappingNode:
			n := len(root.Content) / 2
			fmt.Fprintf(&b, "  %d top-level key(s):\n", n)
			for j := 0; j+1 < len(root.Content); j += 2 {
				if j/2 >= maxYAMLOutlineKeys {
					fmt.Fprintf(&b, "  ... and %d more\n", n-maxYAMLOutlineKeys)
					break
				}
				k, v := root.Content[j], root.Content[j+1]
				fmt.Fprintf(&b, "  %s: %s (line %d)\n", k.Value, yamlKind(v), k.Line)
			}
		default:
			fmt.Fprintf(&b, "  root is a %s (line %d)\n", yamlKind(root), root.Line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// yamlKind describes a value compactly: containers report their size, scalars
// their type only (values are the model's business, not the validator's).
func yamlKind(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return fmt.Sprintf("map{%d}", len(n.Content)/2)
	case yaml.SequenceNode:
		return fmt.Sprintf("list[%d]", len(n.Content))
	case yaml.AliasNode:
		return "alias(*" + n.Value + ")"
	case yaml.ScalarNode:
		if n.Tag == "!!null" || n.Value == "" {
			return "empty"
		}
		return "scalar"
	default:
		return "unknown"
	}
}
