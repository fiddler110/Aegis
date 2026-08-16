package redact

import (
	"strings"
	"testing"
)

// secretCases is the pattern set's table, moved here with the patterns under
// P66.11 (it was internal/mcp's TestScanArgsForSecrets). Each synthetic secret
// class must be flagged with its class name, and plausible clean text —
// including text that merely *talks about* secrets — must not be.
//
// The clean cases are the load-bearing half now that this set also filters an
// exported transcript: a pattern that fires on prose would quietly shred a
// user's artifact, which is a worse failure than the leak it was guarding.
var secretCases = []struct {
	name string
	in   string
	want string // expected class substring; "" = no hit at all
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

func TestClasses(t *testing.T) {
	for _, tc := range secretCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classes(tc.in)
			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("Classes(%s) = %v, want no hits", tc.in, got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "; "), tc.want) {
				t.Errorf("Classes(%s) = %v, want a hit containing %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTextRemovesTheSecretAndCountsIt runs the same table through the replacing
// form: whatever Classes flags, Text must actually remove — and it must say how
// many times. A redaction pass that reports nothing is indistinguishable from one
// that was never wired up, which is the state internal/share was in.
func TestTextRemovesTheSecretAndCountsIt(t *testing.T) {
	for _, tc := range secretCases {
		t.Run(tc.name, func(t *testing.T) {
			out, n := Text(tc.in)
			if tc.want == "" {
				if n != 0 || out != tc.in {
					t.Errorf("Text(%s) redacted %d time(s) and returned %q, want the input unchanged", tc.in, n, out)
				}
				return
			}
			if n == 0 {
				t.Fatalf("Text(%s) redacted nothing though Classes flags it", tc.in)
			}
			if !strings.Contains(out, "[redacted: ") {
				t.Errorf("Text(%s) = %q, want a placeholder naming the class", tc.in, out)
			}
			// The class name may appear (it is the placeholder), but the matched
			// text must not survive anywhere in the output.
			if left := Classes(out); len(left) != 0 {
				t.Errorf("Text(%s) = %q, which still matches %v", tc.in, out, left)
			}
		})
	}
}

// TestTextCountsEveryOccurrence: the count is per match, not per class, because a
// transcript with the same key pasted twenty times is a different situation from
// one with a single hit and the reader is entitled to tell them apart.
func TestTextCountsEveryOccurrence(t *testing.T) {
	one := "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	in := "first " + one + " then " + one + " and " + one
	out, n := Text(in)
	if n != 3 {
		t.Errorf("redaction count = %d, want 3", n)
	}
	if strings.Contains(out, one) {
		t.Errorf("output still contains the token: %q", out)
	}
}

// TestTextIsIdempotent: a placeholder must not itself look like a secret, or a
// second pass over already-redacted text would keep reporting hits and a caller
// that redacts at two layers would report a count that means nothing.
func TestTextIsIdempotent(t *testing.T) {
	first, n1 := Text("key: sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345")
	if n1 == 0 {
		t.Fatal("nothing redacted on the first pass")
	}
	second, n2 := Text(first)
	if n2 != 0 || second != first {
		t.Errorf("second pass redacted %d more time(s): %q -> %q", n2, first, second)
	}
}
