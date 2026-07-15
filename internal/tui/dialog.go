package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// Shared chrome for the overlay dialogs (command palette, persona/session
// pickers) so they read as one cohesive component family — a rounded primary
// frame, a brand title chip, and a left accent bar marking the selection. This
// mirrors Crush's dialog styling (primary frame + accent selection) while
// staying idiomatic to the bubbles list delegate.

// aegisListDelegate returns the shared list delegate styling for overlay
// dialogs: the selected row is marked with a left primary accent bar, normal
// rows use the base / muted foreground tiers.
func aegisListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colPrimary).
		Foreground(colPrimary).
		Bold(true).
		Padding(0, 0, 0, 1)
	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(colSecondary).
		Padding(0, 0, 0, 2)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colFgBase).
		Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(colFgMost).
		Padding(0, 0, 0, 2)
	return d
}

// configureDialogList applies the chrome common to overlay pickers: a brand
// title chip, hidden status/help bars, and filtering. Pass pagination=true for
// lists long enough to page.
func configureDialogList(l *list.Model, title string, pagination bool) {
	l.Title = title
	l.Styles.Title = lipgloss.NewStyle().
		Background(colBrandBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowPagination(pagination)
	l.SetShowHelp(false)
}

// dialogListH sizes a picker's list to n rows, bounded by the terminal height
// and a per-picker floor. Shared so a picker opened empty (P33.7's loading
// row) and the same picker once populated derive their height identically.
func dialogListH(termH, n, minH int) int {
	return min(termH-8, max(n*2+6, minH))
}

// dialogFrame wraps overlay content in the shared rounded primary border.
func dialogFrame(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colPrimary).
		Background(colSurface).
		Padding(0, 1).
		Render(content)
}

// dialogKind tags which logical overlay a listDialog instance backs, so the
// single routing block in the model's Update/View can dispatch the right
// follow-up action on selection without needing four near-identical dialog
// types (P16.6).
type dialogKind int

const (
	dialogPalette dialogKind = iota
	dialogPersonaPicker
	dialogSessionPicker
	dialogTimelinePicker
	dialogModelPicker
	dialogHistoryPicker
	dialogThreatModelPicker
	dialogBacktrackPicker
)

// noticeItem is a non-selectable placeholder row: the spinner shown while a
// picker's fetch is in flight (P33.7), and the error or empty-result line when
// that fetch lands with nothing to list.
type noticeItem struct{ text string }

func (n noticeItem) FilterValue() string { return "" }
func (n noticeItem) Title() string       { return n.text }
func (n noticeItem) Description() string { return "" }

// noticeRow is a dialog's row set while it has nothing real to list.
func noticeRow(text string) []list.Item {
	return []list.Item{noticeItem{text: text}}
}

// dialogSelectedMsg is emitted when the user picks an item from any
// listDialog; kind disambiguates which dialog it came from since only one is
// ever open at a time.
type dialogSelectedMsg struct {
	kind dialogKind
	item list.Item
}

// dialogCancelMsg is emitted when a listDialog is closed without a selection.
type dialogCancelMsg struct{ kind dialogKind }

// listDialog is the shared filterable-list overlay backing the command
// palette, persona/session/timeline pickers, and the model picker. It used to
// be four separate near-identical types (paletteModel, personaPickerModel,
// sessionPickerModel, timelinePickerModel); item-specific behavior (row
// rendering, what a selection does) now lives entirely in each item type
// (FilterValue/Title/Description) and in the caller's handling of
// dialogSelectedMsg, not in the dialog itself.
type listDialog struct {
	kind dialogKind
	list list.Model
	// loading marks a dialog opened ahead of its rows (P33.7), so the spinner
	// row keeps animating until setItems or setNotice retires it.
	loading bool
	// fixedW holds the frame at the list's own width instead of letting it
	// shrink-wrap the rows. Set for dialogs opened ahead of their data (P33.7):
	// a shrink-wrapped frame is sized by its longest row, so the box would jump
	// width under the user the moment the loading row gave way to real ones.
	fixedW bool
}

// newListDialog builds a listDialog. w/h are the already-computed list
// dimensions (callers size these differently: the palette uses a fixed cap,
// pickers size to their item count).
func newListDialog(kind dialogKind, w, h int, title string, pagination bool, items []list.Item) listDialog {
	l := list.New(items, aegisListDelegate(), w, h)
	configureDialogList(&l, title, pagination)
	return listDialog{kind: kind, list: l}
}

// newLoadingDialog opens a picker before its rows exist: the dialog appears on
// the keypress with a single spinner row, and setItems fills it in when the
// fetch lands (P33.7). frame is the current m.sp frame; setLoadingFrame
// advances it.
func newLoadingDialog(kind dialogKind, w, h int, title, frame string) listDialog {
	d := newListDialog(kind, w, h, title, true, noticeRow(frame+" loading…"))
	d.loading, d.fixedW = true, true
	return d
}

// setLoadingFrame redraws the spinner row for the current animation frame; a
// no-op once the rows (or a notice) have replaced it.
func (d *listDialog) setLoadingFrame(frame string) {
	if d.loading {
		d.list.SetItems(noticeRow(frame + " loading…"))
	}
}

// setItems swaps a loading dialog's spinner row for the fetched rows, resizing
// to their count so the frame settles into the shape it would have had if it
// had opened with the data in hand.
func (d *listDialog) setItems(items []list.Item, h int) tea.Cmd {
	d.loading = false
	d.list.SetHeight(h)
	return d.list.SetItems(items)
}

// setNotice replaces the rows with one non-selectable line — a fetch error or
// an empty result, reported inside the dialog the user is already looking at
// rather than as a toast behind it (P33.7).
func (d *listDialog) setNotice(text string) tea.Cmd {
	d.loading = false
	return d.list.SetItems(noticeRow(text))
}

func (d listDialog) Update(msg tea.Msg) (listDialog, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			kind := d.kind
			return d, func() tea.Msg { return dialogCancelMsg{kind: kind} }
		case "enter":
			kind := d.kind
			// A placeholder row isn't a choice: swallow enter rather than emit a
			// selection every caller would then have to type-assert around.
			if _, ok := d.list.SelectedItem().(noticeItem); ok {
				return d, nil
			}
			if item := d.list.SelectedItem(); item != nil {
				return d, func() tea.Msg { return dialogSelectedMsg{kind: kind, item: item} }
			}
			return d, func() tea.Msg { return dialogCancelMsg{kind: kind} }
		}
	}
	var cmd tea.Cmd
	d.list, cmd = d.list.Update(msg)
	return d, cmd
}

func (d listDialog) View() string {
	v := d.list.View()
	if d.fixedW {
		v = lipgloss.NewStyle().Width(d.list.Width()).Render(v)
	}
	return dialogFrame(v)
}

// renderOverlay composites fg centered over bg and fades bg to faint
// (terminal "dim" SGI attribute) outside fg's bounds, so an open dialog reads
// as foreground against a visually receded chat instead of replacing it
// outright (P16.6). width/height are the full terminal frame dimensions.
func renderOverlay(bg, fg string, width, height int) string {
	if width <= 0 || height <= 0 {
		return fg
	}
	fw, fh := lipgloss.Width(fg), lipgloss.Height(fg)
	x := max(0, (width-fw)/2)
	y := max(0, (height-fh)/2)

	root := lipgloss.NewLayer(bg, lipgloss.NewLayer(fg).X(x).Y(y).Z(1))
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(root))
	dimOutside(canvas, x, y, fw, fh)
	return canvas.Render()
}

// dimOutside marks every cell of canvas outside the (x,y,w,h) rectangle as
// faint, in place.
func dimOutside(canvas *lipgloss.Canvas, x, y, w, h int) {
	b := canvas.Bounds()
	for cy := b.Min.Y; cy < b.Max.Y; cy++ {
		inRow := cy >= y && cy < y+h
		for cx := b.Min.X; cx < b.Max.X; cx++ {
			if inRow && cx >= x && cx < x+w {
				continue
			}
			c := canvas.CellAt(cx, cy)
			if c == nil {
				continue
			}
			c.Style.Attrs |= uv.AttrFaint
		}
	}
}
