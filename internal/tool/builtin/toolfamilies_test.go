package builtin

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/cron"
	"github.com/fiddler110/aegis/internal/longmem"
	"github.com/fiddler110/aegis/internal/swarm"
	"github.com/fiddler110/aegis/internal/tool"
)

// familyOptions supplies the three stores whose presence gates the optional
// families. Zero-value pointers are deliberate: Register only stores them on the
// tool structs, so a test about *which tools get registered* has no business
// opening a database to find out.
func familyOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Root:        t.TempDir(),
		Cron:        &cron.Scheduler{},
		TeamTasks:   &swarm.TaskList{},
		MailboxRoot: t.TempDir(),
		LongMem:     &longmem.Store{},
	}
}

func registeredNames(t *testing.T, opts Options) map[string]bool {
	t.Helper()
	reg := tool.NewRegistry()
	if err := Register(reg, opts); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range reg.Deferred() {
		names[d.Name] = true
	}
	for _, s := range reg.Schemas() {
		names[s.Name] = true
	}
	return names
}

// familyMembers is one representative per family, chosen as the member whose
// absence would be least likely to be noticed by another test.
var familyMembers = map[string][]string{
	familyTeam:   {"team_send", "team_task_add"},
	familyCron:   {"cron_create", "cron_history"},
	familyEntity: {"entity_remember", "entity_recall"},
}

// TestDefaultProfileRegistersEveryFamily pins the half of P62.6's family gate
// that is a promise not to change anything: the default profile is not the one
// paying for a 26-tool advertisement, and tools.families is ignored there.
func TestDefaultProfileRegistersEveryFamily(t *testing.T) {
	names := registeredNames(t, familyOptions(t))
	for family, members := range familyMembers {
		for _, m := range members {
			if !names[m] {
				t.Errorf("default profile: %q (family %q) not registered", m, family)
			}
		}
	}
}

// TestLocalProfileOmitsCoordinationFamilies is the saving: thirteen of the local
// profile's twenty-six deferred tools were advertised on every turn for
// capabilities a small-model file-scoped task does not reach for.
func TestLocalProfileOmitsCoordinationFamilies(t *testing.T) {
	opts := familyOptions(t)
	opts.LocalProfile = true
	names := registeredNames(t, opts)
	for family, members := range familyMembers {
		for _, m := range members {
			if names[m] {
				t.Errorf("local profile: %q (family %q) still registered; the profile should omit it", m, family)
			}
		}
	}
	// The gate must be narrow. security/latex/diagram are the families a local
	// run genuinely reaches for, and P34.3's persona preload depends on the
	// security ones being registered-and-deferred rather than absent.
	for _, keep := range []string{"security_scan", "recon_scan", "dast_scan", "latex_build", "render_diagram", "scope"} {
		if !names[keep] {
			t.Errorf("local profile: %q was dropped, but no family gate covers it", keep)
		}
	}
}

// TestToolFamiliesRestoresOmittedFamily: the omission is a profile default, not
// a deletion — a local model driving a swarm is a real configuration. Restoring
// one family must not restore the others, or the knob is a boolean wearing a
// list's clothes.
func TestToolFamiliesRestoresOmittedFamily(t *testing.T) {
	opts := familyOptions(t)
	opts.LocalProfile = true
	opts.ToolFamilies = []string{"  Team  "} // whitespace and case are the operator's, not ours
	names := registeredNames(t, opts)
	for _, m := range familyMembers[familyTeam] {
		if !names[m] {
			t.Errorf("tools.families=[team]: %q not restored", m)
		}
	}
	for _, family := range []string{familyCron, familyEntity} {
		for _, m := range familyMembers[family] {
			if names[m] {
				t.Errorf("tools.families=[team]: %q (family %q) was restored too", m, family)
			}
		}
	}
}

// TestUnknownToolFamilyIsIgnored: an unknown name is a no-op rather than a
// startup failure, because a family can be retired and a stale config line must
// not stop the daemon.
func TestUnknownToolFamilyIsIgnored(t *testing.T) {
	opts := familyOptions(t)
	opts.LocalProfile = true
	opts.ToolFamilies = []string{"nonesuch"}
	names := registeredNames(t, opts)
	for family, members := range familyMembers {
		for _, m := range members {
			if names[m] {
				t.Errorf("unknown family name restored %q (family %q)", m, family)
			}
		}
	}
}

// TestToolFamiliesNamesAreTheGatedOnes keeps the exported list, the config
// documentation and the gate from drifting apart: ToolFamilies is what an
// operator reads to learn what tools.families accepts, and a name that is not
// actually gated would be advice that does nothing.
func TestToolFamiliesNamesAreTheGatedOnes(t *testing.T) {
	if len(ToolFamilies) != len(localOmittedFamilies) {
		t.Fatalf("ToolFamilies=%v but localOmittedFamilies=%v", ToolFamilies, localOmittedFamilies)
	}
	for _, name := range ToolFamilies {
		if _, ok := familyMembers[name]; !ok {
			t.Errorf("ToolFamilies advertises %q, which this test knows no members for — add them or drop the name", name)
		}
		if name != strings.ToLower(name) {
			t.Errorf("family name %q is not lowercase; config matching lowercases the operator's value", name)
		}
		var gated bool
		for _, o := range localOmittedFamilies {
			if o == name {
				gated = true
			}
		}
		if !gated {
			t.Errorf("ToolFamilies advertises %q but the local profile never omits it, so naming it in config does nothing", name)
		}
	}
}
