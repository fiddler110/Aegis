package cli

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/skills"
)

// buildChatSystem must advertise enabled built-in skills via the
// <skills_available> index, or the model never learns the skills exist and
// never calls the (registered) `skill` tool to load one. This regression
// guards the CLI/daemon parity gap that let a live `aegis chat` threat-model
// run make zero skill calls: chat.go wired the skill tool into the registry
// but omitted the system-prompt index the daemon builds via skills.BuildIndex.
func TestBuildChatSystemAdvertisesEnabledBuiltinSkill(t *testing.T) {
	dataDir := t.TempDir()
	if err := skills.MaterializeBuiltins(dataDir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}
	cfg := &config.Config{DataDir: dataDir}
	cfg.Skills.BuiltinEnabled = []string{"threat-modeling"}

	sys := buildChatSystem(cfg, t.TempDir(), cfg.Skills.BuiltinEnabled, "", "")
	if !strings.Contains(sys, "<skills_available>") {
		t.Fatalf("system prompt missing <skills_available> block:\n%s", sys)
	}
	if !strings.Contains(sys, "threat-modeling") {
		t.Errorf("system prompt does not advertise the enabled built-in skill:\n%s", sys)
	}
}

// With no built-ins enabled the index must be absent entirely — advertisement
// is driven by config, not hardcoded, and an empty enabled-list emits no block.
func TestBuildChatSystemOmitsSkillsIndexWhenNoneEnabled(t *testing.T) {
	dataDir := t.TempDir()
	if err := skills.MaterializeBuiltins(dataDir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}
	cfg := &config.Config{DataDir: dataDir} // Skills.BuiltinEnabled left empty

	sys := buildChatSystem(cfg, t.TempDir(), cfg.Skills.BuiltinEnabled, "", "")
	if strings.Contains(sys, "<skills_available>") {
		t.Errorf("system prompt unexpectedly advertises skills with none enabled:\n%s", sys)
	}
}
