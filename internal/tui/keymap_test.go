package tui

import "testing"

func TestApplyKeybindingsOverridesKeysAndHelp(t *testing.T) {
	km := defaultKeyMap()
	if err := km.applyKeybindings(map[string][]string{
		"terminal": {"alt+t"},
		"Diagnose": {"ctrl+g", "f4"}, // name matching is case-insensitive
	}); err != nil {
		t.Fatalf("applyKeybindings: %v", err)
	}

	if got := km.Terminal.Keys(); len(got) != 1 || got[0] != "alt+t" {
		t.Errorf("Terminal.Keys() = %v, want [alt+t]", got)
	}
	if got := km.Terminal.Help().Key; got != "alt+t" {
		t.Errorf("Terminal help key = %q, want alt+t", got)
	}
	if got := km.Terminal.Help().Desc; got != "toggle terminal pane" {
		t.Errorf("Terminal help desc changed unexpectedly: %q", got)
	}

	if got := km.Diagnose.Keys(); len(got) != 2 || got[0] != "ctrl+g" || got[1] != "f4" {
		t.Errorf("Diagnose.Keys() = %v, want [ctrl+g f4]", got)
	}
}

func TestApplyKeybindingsRejectsUnknownAction(t *testing.T) {
	km := defaultKeyMap()
	err := km.applyKeybindings(map[string][]string{"nonexistent_action": {"ctrl+z"}})
	if err == nil {
		t.Fatal("expected an error for an unknown action name")
	}
}

func TestApplyKeybindingsIgnoresEmptyKeyList(t *testing.T) {
	km := defaultKeyMap()
	orig := km.Terminal.Keys()
	if err := km.applyKeybindings(map[string][]string{"terminal": {}}); err != nil {
		t.Fatalf("applyKeybindings: %v", err)
	}
	if got := km.Terminal.Keys(); len(got) != len(orig) || got[0] != orig[0] {
		t.Errorf("empty override should not change binding, got %v", got)
	}
}

func TestBuildKeyMapNoOverrides(t *testing.T) {
	km, err := buildKeyMap(nil)
	if err != nil {
		t.Fatalf("buildKeyMap(nil): %v", err)
	}
	if got := km.Send.Keys(); len(got) != 1 || got[0] != "enter" {
		t.Errorf("Send.Keys() = %v, want [enter]", got)
	}
}

func TestMustKeyMapFallsBackOnError(t *testing.T) {
	km := mustKeyMap(map[string][]string{"nonexistent_action": {"ctrl+z"}})
	if got := km.Send.Keys(); len(got) != 1 || got[0] != "enter" {
		t.Errorf("mustKeyMap should fall back to defaults, got Send.Keys() = %v", got)
	}
}
