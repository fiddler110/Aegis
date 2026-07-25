package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
)

// convSeedText returns the text of a fresh conversation's single seed message,
// so a test can assert which prompt freshPhaseConv chose.
func convSeedText(t *testing.T, conv *engine.Conversation) string {
	t.Helper()
	if conv.System == "" {
		t.Error("freshPhaseConv must carry the system prompt onto the reset conversation")
	}
	if len(conv.Messages) != 1 {
		t.Fatalf("freshPhaseConv seed = %d messages, want exactly 1", len(conv.Messages))
	}
	var b strings.Builder
	for _, blk := range conv.Messages[0].Content {
		if tb, ok := blk.(provider.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String()
}

// TestFreshPhaseConv_ReseedChoice is the P47.2 guard for the overflow-reset
// reseed: after a context overflow the phase retries with a brand-new
// conversation, and which prompt seeds it depends on whether the phase has
// scaffolded its files yet. A content phase (run dir on disk) resumes from disk
// with the in-phase continuation prompt; the setup phase overflowing before the
// run dir exists (runDir == "") has nothing to resume from, so it restarts from
// the phase's own full seed prompt.
func TestFreshPhaseConv_ReseedChoice(t *testing.T) {
	st := &phasedDriveState{
		eng:        nil,
		system:     "SYSTEM PROMPT",
		errOut:     io.Discard,
		cwd:        "/ws",
		skillDir:   "/ws/.aegis/builtin-skills/threat-modeling",
		taskPrompt: "threat model this repo",
		maxTurns:   50,
	}

	// Setup phase, nothing scaffolded yet: restart from the full architecture
	// seed prompt (recon → scaffold → fill), not the "fill the next marker"
	// continuation, because no files exist to resume from.
	setup := threatModelPhases[0]
	conv := st.freshPhaseConv(setup, "", setup.pending(""))
	seed := convSeedText(t, conv)
	if !strings.Contains(seed, "recon.py") || !strings.Contains(seed, "ARCHITECTURE phase") {
		t.Errorf("setup-phase reset with no run dir must reseed from the full phase prompt; got:\n%s", seed)
	}
	if strings.Contains(seed, "still contain") {
		t.Error("setup-phase reset with no run dir must NOT use the in-phase continuation prompt")
	}

	// A content phase mid-build (run dir exists): resume from disk with the
	// continuation prompt naming the still-PENDING files.
	analysis := threatModelPhases[2]
	pending := []string{"2-stride-analysis.md"}
	conv = st.freshPhaseConv(analysis, "/ws/.aegis/security/threat-model/run", pending)
	seed = convSeedText(t, conv)
	if !strings.Contains(seed, "still contain") || !strings.Contains(seed, "2-stride-analysis.md") {
		t.Errorf("content-phase reset must resume from disk via the continuation prompt naming pending files; got:\n%s", seed)
	}
	if strings.Contains(seed, "recon.py") {
		t.Error("content-phase reset must not restart from the setup seed prompt")
	}
}
