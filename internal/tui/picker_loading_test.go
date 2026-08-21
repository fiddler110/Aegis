package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fiddler110/aegis/internal/api"
)

// openSessionPicker presses ctrl+y on an idle model and returns the resulting
// model — the P33.7 path where the dialog opens on the keypress rather than on
// the daemon's answer.
func openSessionPicker(t *testing.T, m model) model {
	t.Helper()
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if m.dialog == nil {
		t.Fatal("expected ctrl+y to open the session picker immediately")
	}
	return m
}

func idleModel(t *testing.T) model {
	t.Helper()
	m := newModel(Config{SessionID: "s", Mode: "build", Model: "test-model", WorkDir: t.TempDir()})
	return driveUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
}

// TestSessionPickerOpensBeforeDataLands is the P33.7 regression: ctrl+y used to
// produce no visible reaction at all until ListSessions returned — on a busy
// daemon, a menu press into nothing. The dialog must be on screen with a
// loading row the moment the key is pressed, and fill in when the data lands.
func TestSessionPickerOpensBeforeDataLands(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))

	if !m.dialog.loading {
		t.Error("expected the picker to open in its loading state")
	}
	if m.dialog.kind != dialogSessionPicker {
		t.Errorf("expected a session picker, got kind %v", m.dialog.kind)
	}
	view := plainView(m)
	if !strings.Contains(view, "loading…") {
		t.Errorf("expected a loading row on screen, got:\n%s", view)
	}
	if !strings.Contains(view, "Switch Session") {
		t.Errorf("expected the picker's title on screen, got:\n%s", view)
	}

	// The fetch lands: the spinner row is replaced by the real rows in place.
	m = driveUpdate(t, m, sessionsLoadedMsg{items: []api.SessionMeta{
		{ID: "aaaaaaaa1", Title: "first", Mode: "build", UpdatedAt: time.Now()},
		{ID: "bbbbbbbb2", Title: "second", Mode: "plan", UpdatedAt: time.Now()},
	}})
	if m.dialog == nil {
		t.Fatal("expected the picker to still be open once its data landed")
	}
	if m.dialog.loading {
		t.Error("expected the loading state to clear once the data landed")
	}
	if n := len(m.dialog.list.Items()); n != 2 {
		t.Fatalf("expected 2 session rows, got %d", n)
	}
	if _, ok := m.dialog.list.Items()[0].(sessionItem); !ok {
		t.Errorf("expected sessionItem rows, got %#v", m.dialog.list.Items()[0])
	}
	view = plainView(m)
	if strings.Contains(view, "loading…") {
		t.Errorf("expected the loading row gone, got:\n%s", view)
	}
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Errorf("expected the fetched sessions listed, got:\n%s", view)
	}
}

// TestSessionPickerFrameWidthSurvivesPopulation guards the flicker opening
// early could otherwise introduce. A dialog frame shrink-wraps its rows, so a
// picker that opened on a narrow "loading…" row would snap to a different
// width the instant real rows arrived — a fast daemon would make that a
// flash rather than a transition. Dialogs opened ahead of their data hold the
// list's own width across both states (listDialog.fixedW).
func TestSessionPickerFrameWidthSurvivesPopulation(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))
	loadingW := lipgloss.Width(m.dialog.View())

	items := []api.SessionMeta{
		{ID: "aaaaaaaa1", Title: "s", Mode: "build", UpdatedAt: time.Now()},
		{ID: "bbbbbbbb2", Title: "a much, much longer session title than the first", Mode: "plan", UpdatedAt: time.Now()},
	}
	m = driveUpdate(t, m, sessionsLoadedMsg{items: items})

	if got := lipgloss.Width(m.dialog.View()); got != loadingW {
		t.Errorf("picker frame width jumped when the data landed: %d -> %d", loadingW, got)
	}
	if got, want := m.dialog.list.Height(), sessionPickerH(40, len(items)); got != want {
		t.Errorf("populated picker height = %d, want the item-count height %d", got, want)
	}

	// A notice is the same frame too — an error must not resize the box either.
	m2 := openSessionPicker(t, idleModel(t))
	m2 = driveUpdate(t, m2, sessionsLoadedMsg{err: errors.New("x")})
	if got := lipgloss.Width(m2.dialog.View()); got != loadingW {
		t.Errorf("picker frame width jumped on the error notice: %d -> %d", loadingW, got)
	}
}

// TestSessionPickerFetchErrorShowsInDialog: an error reaches the user where
// they are looking — inside the dialog they just opened — instead of as a
// toast behind it (P33.7).
func TestSessionPickerFetchErrorShowsInDialog(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))
	m = driveUpdate(t, m, sessionsLoadedMsg{err: errors.New("daemon unreachable")})

	if m.dialog == nil {
		t.Fatal("expected the dialog to stay open to carry the error")
	}
	if m.dialog.loading {
		t.Error("expected the loading state to clear on error")
	}
	view := plainView(m)
	if !strings.Contains(view, "daemon unreachable") {
		t.Errorf("expected the fetch error in the dialog, got:\n%s", view)
	}

	// The error row is not a choice: enter must not emit a selection the
	// caller would type-assert into a sessionItem.
	_, cmd := m.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("expected enter on a notice row to be swallowed, got msg %#v", cmd())
	}
}

// TestSessionPickerEmptyResultShowsInDialog covers the other nothing-to-list
// outcome.
func TestSessionPickerEmptyResultShowsInDialog(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))
	m = driveUpdate(t, m, sessionsLoadedMsg{})

	if m.dialog == nil {
		t.Fatal("expected the dialog to stay open to carry the empty-result notice")
	}
	if view := plainView(m); !strings.Contains(view, "no sessions to switch to") {
		t.Errorf("expected the empty-result notice in the dialog, got:\n%s", view)
	}
}

// TestSessionPickerDismissedBeforeDataArrives is the race P33.7 introduces by
// opening early: esc closes the loading dialog, then the fetch lands. The late
// data must not re-open the picker or hijack whatever the user moved on to.
func TestSessionPickerDismissedBeforeDataArrives(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected esc to emit a cancel cmd")
	}
	m = driveUpdate(t, m, cmd())
	if m.dialog != nil {
		t.Fatal("expected esc to close the loading picker")
	}

	m = driveUpdate(t, m, sessionsLoadedMsg{items: []api.SessionMeta{
		{ID: "aaaaaaaa1", Title: "first", Mode: "build", UpdatedAt: time.Now()},
	}})
	if m.dialog != nil {
		t.Fatal("late session data re-opened a dismissed picker")
	}

	// An error for a dismissed picker still has somewhere to go: the toast it
	// used before the dialog existed.
	m = driveUpdate(t, m, sessionsLoadedMsg{err: errors.New("boom")})
	if m.dialog != nil {
		t.Fatal("late session error re-opened a dismissed picker")
	}
	if m.activeToast == nil || !strings.Contains(m.activeToast.message, "boom") {
		t.Errorf("expected the toast fallback for a dismissed picker, got %#v", m.activeToast)
	}
}

// TestSessionPickerDataDoesNotHijackAnotherDialog: the user dismissed the
// picker and opened something else before the fetch landed. The result belongs
// to a dialog that is no longer there and must be dropped, not poured into
// whatever took its place.
func TestSessionPickerDataDoesNotHijackAnotherDialog(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))
	pal := newPalette(m.width, m.height, allCommandEntries(nil))
	m.dialog = &pal

	m = driveUpdate(t, m, sessionsLoadedMsg{items: []api.SessionMeta{
		{ID: "aaaaaaaa1", Title: "first", Mode: "build", UpdatedAt: time.Now()},
	}})
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatal("expected the palette to still own the screen")
	}
	if view := plainView(m); strings.Contains(view, "first") {
		t.Errorf("session rows leaked into the palette, got:\n%s", view)
	}
}

// TestLoadingSpinnerAnimatesWhileFetching: the spinner tick is claimed by the
// dialog block (which returns ahead of the model's own spinner case) and
// re-queued only while a fetch is outstanding, so the loading row actually
// turns instead of sitting on one frozen frame.
func TestLoadingSpinnerAnimatesWhileFetching(t *testing.T) {
	m := openSessionPicker(t, idleModel(t))
	first := m.dialog.list.Items()[0].(noticeItem).text

	var frames []string
	for i := 0; i < 6; i++ {
		next, cmd := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
		m = next.(model)
		if cmd == nil {
			t.Fatal("expected the spinner tick to be re-queued while loading")
		}
		frames = append(frames, m.dialog.list.Items()[0].(noticeItem).text)
	}
	advanced := false
	for _, f := range frames {
		if f != first {
			advanced = true
		}
	}
	if !advanced {
		t.Errorf("loading row never advanced past %q, got %v", first, frames)
	}

	// Once the rows land the tick stops being re-queued: the loading row is
	// gone, so nothing is animating.
	m = driveUpdate(t, m, sessionsLoadedMsg{items: []api.SessionMeta{
		{ID: "aaaaaaaa1", Title: "first", Mode: "build", UpdatedAt: time.Now()},
	}})
	next, _ := m.Update(spinner.TickMsg{Time: time.Now(), ID: m.sp.ID()})
	m = next.(model)
	if _, ok := m.dialog.list.Items()[0].(noticeItem); ok {
		t.Error("expected a spinner tick after loading to leave the real rows alone")
	}
}

// TestEscEscOpensBacktrackPickerImmediately applies P33.7 to the Esc-Esc
// backtrack path (P22.3/P33.5): the second press opens the picker on the
// keypress, ahead of its two checkpoint/session round-trips.
func TestEscEscOpensBacktrackPickerImmediately(t *testing.T) {
	m := idleModel(t)

	m = driveUpdate(t, m, escKeyMsg())
	if m.dialog != nil {
		t.Fatal("the first (arming) Esc must not open anything")
	}
	next, cmd := m.Update(escKeyMsg())
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected the second Esc to fire the backtrack fetch")
	}
	if m.dialog == nil || m.dialog.kind != dialogBacktrackPicker {
		t.Fatal("expected the second Esc to open the backtrack picker immediately")
	}
	if !m.dialog.loading {
		t.Error("expected the backtrack picker to open in its loading state")
	}
	if view := plainView(m); !strings.Contains(view, "loading…") {
		t.Errorf("expected a loading row on screen, got:\n%s", view)
	}

	items := []backtrackItem{
		{cpID: "cp1", text: "fix the bug", createdAt: time.Now()},
		{cpID: "cp2", text: "add a test", createdAt: time.Now()},
	}
	m = driveUpdate(t, m, backtrackTargetsMsg{items: items})
	if m.dialog == nil || m.dialog.loading {
		t.Fatal("expected the backtrack picker populated in place")
	}
	if n := len(m.dialog.list.Items()); n != 2 {
		t.Fatalf("expected 2 backtrack rows, got %d", n)
	}
	if got, want := m.dialog.list.Height(), backtrackPickerH(40, len(items)); got != want {
		t.Errorf("populated picker height = %d, want %d", got, want)
	}
	if view := ansi.Strip(m.renderContent()); !strings.Contains(view, "fix the bug") {
		t.Errorf("expected the fetched turns listed, got:\n%s", view)
	}
}

// TestBacktrackPickerEmptyResultShowsInDialog: the "nothing to backtrack to"
// case reports inside the dialog the second Esc just opened.
func TestBacktrackPickerEmptyResultShowsInDialog(t *testing.T) {
	m := idleModel(t)
	m = driveUpdate(t, m, escKeyMsg())
	m = driveUpdate(t, m, escKeyMsg())

	m = driveUpdate(t, m, backtrackTargetsMsg{})
	if m.dialog == nil {
		t.Fatal("expected the dialog to stay open to carry the notice")
	}
	if view := plainView(m); !strings.Contains(view, "no checkpoints yet") {
		t.Errorf("expected the empty-result notice in the dialog, got:\n%s", view)
	}
}

// TestBacktrackPickerDismissedBeforeDataArrives mirrors the session picker's
// dismiss-before-data race on the Esc-Esc path — where it is likeliest, since
// esc is both the key that opened the dialog and the key that closes it.
func TestBacktrackPickerDismissedBeforeDataArrives(t *testing.T) {
	m := idleModel(t)
	m = driveUpdate(t, m, escKeyMsg())
	m = driveUpdate(t, m, escKeyMsg())
	if m.dialog == nil {
		t.Fatal("expected the backtrack picker open after the second Esc")
	}

	next, cmd := m.Update(escKeyMsg())
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected esc to emit a cancel cmd")
	}
	m = driveUpdate(t, m, cmd())
	if m.dialog != nil {
		t.Fatal("expected esc to close the loading picker")
	}

	m = driveUpdate(t, m, backtrackTargetsMsg{items: []backtrackItem{{cpID: "cp1", text: "fix the bug"}}})
	if m.dialog != nil {
		t.Fatal("late backtrack data re-opened a dismissed picker")
	}
}

// openPersonaPicker types "/persona" and presses Enter on an idle model,
// returning the resulting model — the P33.13 path where the picker opens on
// dispatch, ahead of ListPersonas answering, rather than the fetch-then-open
// P33.7 left behind because opening early needed a pre-dispatch hook the
// generic slash-command path didn't have (see dispatchSlash in tui.go).
func openPersonaPicker(t *testing.T, m model) model {
	t.Helper()
	m.ta.SetValue("/persona")
	m = driveUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil {
		t.Fatal("expected \"/persona\" to open the picker immediately")
	}
	return m
}

// TestPersonaPickerOpensBeforeDataLands is the P33.13 regression: submitting
// "/persona" used to sit with no visible reaction until ListPersonas
// returned. The dialog must be on screen with a loading row the moment the
// command is dispatched, and fill in when the data lands.
func TestPersonaPickerOpensBeforeDataLands(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))

	if !m.dialog.loading {
		t.Error("expected the picker to open in its loading state")
	}
	if m.dialog.kind != dialogPersonaPicker {
		t.Errorf("expected a persona picker, got kind %v", m.dialog.kind)
	}
	view := plainView(m)
	if !strings.Contains(view, "loading…") {
		t.Errorf("expected a loading row on screen, got:\n%s", view)
	}
	if !strings.Contains(view, "Select Persona") {
		t.Errorf("expected the picker's title on screen, got:\n%s", view)
	}

	// The dispatch lands: the spinner row is replaced by the real rows in
	// place, via the same slashResultMsg every other slash command uses.
	m = driveUpdate(t, m, slashResultMsg{Personas: []api.PersonaInfo{
		{Name: "general", Description: "General-purpose"},
		{Name: "security", Description: "Security review"},
	}})
	if m.dialog == nil {
		t.Fatal("expected the picker to still be open once its data landed")
	}
	if m.dialog.loading {
		t.Error("expected the loading state to clear once the data landed")
	}
	if n := len(m.dialog.list.Items()); n != 2 {
		t.Fatalf("expected 2 persona rows, got %d", n)
	}
	if _, ok := m.dialog.list.Items()[0].(personaItem); !ok {
		t.Errorf("expected personaItem rows, got %#v", m.dialog.list.Items()[0])
	}
	view = plainView(m)
	if strings.Contains(view, "loading…") {
		t.Errorf("expected the loading row gone, got:\n%s", view)
	}
	if !strings.Contains(view, "general") || !strings.Contains(view, "security") {
		t.Errorf("expected the fetched personas listed, got:\n%s", view)
	}
}

// TestPersonaPickerFrameWidthSurvivesPopulation guards the same flicker as
// TestSessionPickerFrameWidthSurvivesPopulation: a dialog frame shrink-wraps
// its rows, so a picker that opened on a narrow "loading…" row would snap to
// a different width the instant real rows (or a notice) arrived.
func TestPersonaPickerFrameWidthSurvivesPopulation(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))
	loadingW := lipgloss.Width(m.dialog.View())

	items := []api.PersonaInfo{
		{Name: "general", Description: "s"},
		{Name: "security", Description: "a much, much longer persona description than the first"},
	}
	m = driveUpdate(t, m, slashResultMsg{Personas: items})

	if got := lipgloss.Width(m.dialog.View()); got != loadingW {
		t.Errorf("picker frame width jumped when the data landed: %d -> %d", loadingW, got)
	}
	if got, want := m.dialog.list.Height(), personaPickerH(40, len(items)); got != want {
		t.Errorf("populated picker height = %d, want the item-count height %d", got, want)
	}

	// A notice is the same frame too — an error must not resize the box either.
	m2 := openPersonaPicker(t, idleModel(t))
	m2 = driveUpdate(t, m2, slashResultMsg{Output: "Failed to list personas: x", IsError: true})
	if got := lipgloss.Width(m2.dialog.View()); got != loadingW {
		t.Errorf("picker frame width jumped on the error notice: %d -> %d", loadingW, got)
	}
}

// TestPersonaPickerFetchErrorShowsInDialog: an error reaches the user where
// they are looking — inside the dialog they just opened — instead of as a
// transcript line behind it (P33.13).
func TestPersonaPickerFetchErrorShowsInDialog(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))
	m = driveUpdate(t, m, slashResultMsg{Output: "Failed to list personas: daemon unreachable", IsError: true})

	if m.dialog == nil {
		t.Fatal("expected the dialog to stay open to carry the error")
	}
	if m.dialog.loading {
		t.Error("expected the loading state to clear on error")
	}
	view := plainView(m)
	if !strings.Contains(view, "daemon unreachable") {
		t.Errorf("expected the fetch error in the dialog, got:\n%s", view)
	}

	// The error row is not a choice: enter must not emit a selection the
	// caller would type-assert into a personaItem.
	_, cmd := m.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("expected enter on a notice row to be swallowed, got msg %#v", cmd())
	}
}

// TestPersonaPickerEmptyResultShowsInDialog covers cmdPersona's "no personas
// configured" outcome — reported inside the dialog rather than the
// transcript, same as the fetch-error case above.
func TestPersonaPickerEmptyResultShowsInDialog(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))
	m = driveUpdate(t, m, slashResultMsg{Output: "No personas available."})

	if m.dialog == nil {
		t.Fatal("expected the dialog to stay open to carry the empty-result notice")
	}
	if view := plainView(m); !strings.Contains(view, "No personas available.") {
		t.Errorf("expected the empty-result notice in the dialog, got:\n%s", view)
	}
}

// TestPersonaPickerDismissedBeforeDataArrives is the race opening early
// introduces, same as the session/backtrack pickers: esc closes the loading
// dialog, then the dispatch lands. Late data (success or error) must not
// re-open the picker.
func TestPersonaPickerDismissedBeforeDataArrives(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected esc to emit a cancel cmd")
	}
	m = driveUpdate(t, m, cmd())
	if m.dialog != nil {
		t.Fatal("expected esc to close the loading picker")
	}

	m = driveUpdate(t, m, slashResultMsg{Personas: []api.PersonaInfo{{Name: "general"}}})
	if m.dialog != nil {
		t.Fatal("late persona data re-opened a dismissed picker")
	}

	m = driveUpdate(t, m, slashResultMsg{Output: "Failed to list personas: boom", IsError: true})
	if m.dialog != nil {
		t.Fatal("late persona error re-opened a dismissed picker")
	}
}

// TestPersonaPickerDataDoesNotHijackAnotherDialog: the user dismissed the
// persona picker and opened something else (the palette) before the dispatch
// landed. The result belongs to a dialog that is no longer there and must be
// dropped, not poured into whatever took its place.
func TestPersonaPickerDataDoesNotHijackAnotherDialog(t *testing.T) {
	m := openPersonaPicker(t, idleModel(t))
	pal := newPalette(m.width, m.height, allCommandEntries(nil))
	m.dialog = &pal

	m = driveUpdate(t, m, slashResultMsg{Personas: []api.PersonaInfo{{Name: "general"}}})
	if m.dialog == nil || m.dialog.kind != dialogPalette {
		t.Fatal("expected the palette to still own the screen")
	}
	if view := plainView(m); strings.Contains(view, "general") {
		t.Errorf("persona rows leaked into the palette, got:\n%s", view)
	}
}
