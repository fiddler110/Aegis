package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestScanArgsForSecrets exercises the credential-shaped pattern set
// directly: each synthetic secret class must be flagged with its class name,
// and plausible clean arguments (including ones that merely *talk about*
// secrets) must not be.
func TestScanArgsForSecrets(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string // expected pattern class substring; "" = no hit at all
	}{
		{"pem private key", `{"data":"-----BEGIN RSA PRIVATE KEY-----\nMIIE..."}`, "PEM private key"},
		{"openssh pem header", `{"data":"-----BEGIN OPENSSH PRIVATE KEY-----"}`, "PEM private key"},
		{"aws access key id", `{"query":"AKIAIOSFODNN7EXAMPLE"}`, "AWS access key ID"},
		{"sk-style api key", `{"key":"sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"}`, "sk- API key"},
		{"github token", `{"token":"ghp_abcdefghijklmnopqrstuvwxyz0123456789"}`, "GitHub token"},
		{"slack token", `{"auth":"xoxb-1234567890-abcdefghij"}`, "Slack token"},
		{"jwt", `{"session":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.SflKxwRJSMeKKF2QT4fwpM"}`, "JWT"},
		{"bearer token", `{"header":"Bearer abcdefghijklmnopqrstuvwxyz123456"}`, "bearer token"},
		{"api key assignment", `{"config":"api_key=0123456789abcdef0123"}`, "api_key/secret/password assignment"},
		{"clean query", `{"query":"weather in Paris tomorrow"}`, ""},
		{"clean file path", `{"path":"/home/user/project/main.go","line":42}`, ""},
		{"mentions secrets without leaking one", `{"text":"the password policy requires quarterly rotation of every api key"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanArgsForSecrets(json.RawMessage(tc.args))
			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("scanArgsForSecrets(%s) = %v, want no hits", tc.args, got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "; "), tc.want) {
				t.Errorf("scanArgsForSecrets(%s) = %v, want a hit containing %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestMCPToolOutboundArgumentScan is the FIND-12 regression, table-driven
// over the three behaviors that matter: scan off = secret passes with no
// warning; scan on + secret = Warn log naming server, tool, and pattern
// class while the call still proceeds; scan on + clean args = no warning.
func TestMCPToolOutboundArgumentScan(t *testing.T) {
	cases := []struct {
		name     string
		scanArgs bool
		args     string
		wantWarn bool
	}{
		{"disabled: secret in args, no warning", false, `{"data":"-----BEGIN RSA PRIVATE KEY-----"}`, false},
		{"enabled: secret in args warns", true, `{"data":"-----BEGIN RSA PRIVATE KEY-----"}`, true},
		{"enabled: clean args, no warning", true, `{"query":"weather in Paris"}`, false},
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
			// Flag-only: the call must always still be forwarded and its
			// result returned, warning or not.
			if !strings.Contains(res.Content, "called echo with") {
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
// scan_arguments the same way tools/call does, mirroring how scan_output
// covers all three.
func TestOutboundArgumentScanCoversResourceAndPromptTools(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	c := newPipeClient(t)

	rt := &mcpResourceReadTool{client: c, exposedName: "mcp__test__read_resource", scanArgs: true, logger: logger}
	if _, err := rt.Execute(context.Background(), json.RawMessage(`{"uri":"https://x.test/?k=AKIAIOSFODNN7EXAMPLE"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "AWS access key ID") {
		t.Errorf("resource read args not scanned: %q", buf.String())
	}

	buf.Reset()
	pt := &mcpPromptGetTool{client: c, exposedName: "mcp__test__get_prompt", scanArgs: true, logger: logger}
	if _, err := pt.Execute(context.Background(), json.RawMessage(`{"name":"greet","arguments":{"who":"ghp_abcdefghijklmnopqrstuvwxyz0123456789"}}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "GitHub token") {
		t.Errorf("prompt get args not scanned: %q", buf.String())
	}
}
