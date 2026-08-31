package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// The credential pattern set's own table test moved to internal/redact with the
// patterns (P66.11). What stays here is the *boundary* behaviour — that the scan
// is opt-in per server, warns without blocking, and names the class rather than
// the match — which is this package's concern and not the pattern set's.

// TestMCPToolOutboundArgumentScan is the FIND-12/P81.5 regression,
// table-driven over the three behaviors that matter: scan off = secret
// passes through with no warning and no refusal; scan on + secret = Warn log
// naming server, tool, and pattern class AND the call is refused before
// reaching the server (P81.5/FIND-05 escalated this from flag-only to a
// gate — forwarding a flagged argument to the untrusted server an operator
// opted into scanning defeated the point of scanning it); scan on + clean
// args = no warning, call proceeds.
func TestMCPToolOutboundArgumentScan(t *testing.T) {
	cases := []struct {
		name       string
		scanArgs   bool
		args       string
		wantWarn   bool
		wantRefuse bool
	}{
		{"disabled: secret in args, no warning, not refused", false, `{"data":"-----BEGIN RSA PRIVATE KEY-----"}`, false, false},
		{"enabled: secret in args warns and is refused", true, `{"data":"-----BEGIN RSA PRIVATE KEY-----"}`, true, true},
		{"enabled: clean args, no warning, not refused", true, `{"query":"weather in Paris"}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			c := newPipeClient(t)
			mt := &mcpTool{client: c, info: ToolInfo{Name: "echo"}, exposedName: "mcp__test__echo", scanArgs: tc.scanArgs, logger: logger}

			res, err := mt.Execute(context.Background(), json.RawMessage(tc.args))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if tc.wantRefuse {
				if !res.IsError || !strings.Contains(res.Content, "refusing to forward") {
					t.Errorf("expected a refusal, got isErr=%v content=%q", res.IsError, res.Content)
				}
				if strings.Contains(res.Content, "BEGIN RSA PRIVATE KEY") {
					t.Errorf("refusal leaked the matched secret text: %q", res.Content)
				}
			} else if !strings.Contains(res.Content, "called echo with") {
				t.Errorf("call did not proceed, content: %q", res.Content)
			}

			logged := buf.String()
			gotWarn := strings.Contains(logged, "flagged possible secret")
			if gotWarn != tc.wantWarn {
				t.Fatalf("warning logged = %v, want %v; log: %q", gotWarn, tc.wantWarn, logged)
			}
			if tc.wantWarn {
				for _, want := range []string{"level=WARN", "server=test", "tool=echo", "PEM private key"} {
					if !strings.Contains(logged, want) {
						t.Errorf("warn log missing %q: %q", want, logged)
					}
				}
				// The matched secret text itself must never be copied into
				// the log — only its pattern class.
				if strings.Contains(logged, "BEGIN RSA PRIVATE KEY") {
					t.Errorf("log leaked the matched secret text: %q", logged)
				}
			}
		})
	}
}

// TestOutboundArgumentScanCoversResourceAndPromptTools checks the other two
// argument-forwarding adapters (resources/read, prompts/get) honor
// scan_arguments the same way tools/call does — both the Warn log and the
// P81.5 refusal — mirroring how scan_output covers all three.
func TestOutboundArgumentScanCoversResourceAndPromptTools(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	c := newPipeClient(t)

	rt := &mcpResourceReadTool{client: c, exposedName: "mcp__test__read_resource", scanArgs: true, logger: logger}
	res, err := rt.Execute(context.Background(), json.RawMessage(`{"uri":"https://x.test/?k=AKIAIOSFODNN7EXAMPLE"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "AWS access key ID") {
		t.Errorf("resource read args not scanned: %q", buf.String())
	}
	if !res.IsError || !strings.Contains(res.Content, "refusing to forward") {
		t.Errorf("resource read with flagged args should be refused, got isErr=%v content=%q", res.IsError, res.Content)
	}

	buf.Reset()
	pt := &mcpPromptGetTool{client: c, exposedName: "mcp__test__get_prompt", scanArgs: true, logger: logger}
	res, err = pt.Execute(context.Background(), json.RawMessage(`{"name":"greet","arguments":{"who":"ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "GitHub token") {
		t.Errorf("prompt get args not scanned: %q", buf.String())
	}
	if !res.IsError || !strings.Contains(res.Content, "refusing to forward") {
		t.Errorf("prompt get with flagged args should be refused, got isErr=%v content=%q", res.IsError, res.Content)
	}
}
