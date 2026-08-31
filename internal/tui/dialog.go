package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// listPointer is the glyph aegisListDelegate marks the selected row with
// (P74.5) and the one findGlyphPos looks for to place the real terminal
// cursor on that row (P74.7) — kept as a constant so the two stay in sync.
const listPointer = "❯"

// findGlyphPos returns the (column, row) of the first occurrence of glyph in
// content, both measured in visible cells/lines rather than bytes — content
// is stripped of ANSI escapes per line before searching so styling doesn't
// throw off the column count. ok is false if glyph never appears.
func findGlyphPos(content, glyph string) (x, y int, ok bool) {
	for i, line := range strings.Split(content, "\n") {
		stripped := ansi.Strip(line)
		idx := strings.Index(stripped, glyph)
		if idx < 0 {
			continue
		}
		return utf8.RuneCountInString(stripped[:idx]), i, true
	}
	return 0, 0, false
}

// overlayOrigin returns the (x, y) renderOverlay places fg at when centering
// it over a width×height frame — the same formula renderOverlay itself uses,
// factored out so callers that need to translate a position local to fg (e.g.
// a dialog's cursorPos) into screen coordinates don't duplicate the math.
func overlayOrigin(fg string, width, height int) (x, y int) {
	fw, fh := lipgloss.Width(fg), lipgloss.Height(fg)
	return max(0, (width-fw)/2), max(0, (height-fh)/2)
}

// Shared chrome for the overlay pickers (command palette, persona/session
// pickers, …) and the two genuinely modal dialogs (approval, quit-confirm).
// Early versions mirrored Crush's dialog styling on every overlay alike — a
// rounded primary frame, a brand title chip on a solid fill, and a left
// accent bar for the selected row — but P74.5 reversed that for pickers:
// they already composite over a dimmed transcript (renderOverlay, P16.6), so
// a frame plus a filled chip plus a bordered, bold selection was three or
// four "you are here" signals stacked on top of the dim, and read as an
// application dialog box rather than a list. Pickers now render as plain
// text straight over the terminal's own background — a bold title over a
// hairline rule, and a single "❯" pointer plus a colour shift as the only
// selection cue. `dialogFrame` stays for the dialogs that are actually
// modal and gain from reading as a box: approval (approval.go) and
// quit-confirm (quitconfirm.go), neither of which is a listDialog.

// aegisListDelegate returns the shared list delegate styling for overlay
// pickers: the selected row is marked with a "❯" pointer and a primary
// colour shift — no border bar, no bold — normal rows use the base / muted
// foreground tiers.
func aegisListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	pointer := lipgloss.Border{Left: listPointer}
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(pointer, false, false, false, true).
		BorderForeground(colPrimary).
		Foreground(colPrimary).
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

// configureDialogList applies the chrome common to overlay pickers: a plain
// bold title over a hairline rule, hidden status/help bars, and filtering.
// Pass pagination=true for lists long enough to page.
func configureDialogList(l *list.Model, title string, pagination bool) {
	l.Title = title
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(colPrimary).
		Bold(true)
	l.Styles.TitleBar = lipgloss.NewStyle().
		Width(l.Width()).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(colSeparator).
		Padding(0, 0, 1, 0)
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

// dialogFrame wraps overlay content in the shared rounded primary border. As
// of P74.5 this backs only the genuinely modal dialogs (approval,
// quit-confirm) — pickers (listDialog) render frameless, see the comment
// above aegisListDelegate.
func dialogFrame(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colPrimary).
		Background(colSurface).
		Padding(0, 1).
		Render(content)
}

// fixedPanelFrame wraps a fixed-width form overlay in the shared rounded
// accent-bordered panel. It backs the two hand-built form overlays that are
// NOT listDialogs — the setup wizard (wizard.go) and the security-scanner
// config (securityconfig.go), both huh forms rather than pickers, so they can't
// literally reuse the list widget. P40.7 folds their two byte-identical
// hand-rolled frames into this one helper (living beside dialogFrame /
// renderOverlay) so the panel chrome — border style, accent color, padding —
// is defined once instead of per form; width stays a caller argument since the
// wizard and scanner form size differently. The dimming/centering half of the
// shared chrome the forms already get for free from renderOverlay (see View).
func fixedPanelFrame(content string, width int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Width(width).
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

// View renders the picker frameless: no border, no fill — the terminal's own
// background is the surface, with only the hairline rule under the title and
// the aegisListDelegate pointer marking selection (P74.5). A rounded frame
// stays for dialogFrame's other callers (approval, quit-confirm), which are
// actually modal rather than a transient list composited over the dimmed
// transcript renderOverlay already provides.
func (d listDialog) View() string {
	v := d.list.View()
	if d.fixedW {
		v = lipgloss.NewStyle().Width(d.list.Width()).Render(v)
	}
	v += "\n" + d.footer()
	return lipgloss.NewStyle().Padding(0, 1).Render(v)
}

// cursorPos locates the selected row's pointer glyph within d.View(), so
// render() can hand the real terminal cursor a position on it (P74.7) instead
// of leaving hardware focus wherever the composer last left it.
func (d listDialog) cursorPos() (x, y int, ok bool) {
	return findGlyphPos(d.View(), listPointer)
}

// footer is the one dim hint line naming the picker's only genuinely
// undiscoverable interaction — that typing filters it, with no visible query
// or match count otherwise (P74.6). Once filtering is active it right-aligns
// a "n/m" match count in the same row, budgeted by dialogListH's own "+6"
// alongside the title/rule.
func (d listDialog) footer() string {
	style := lipgloss.NewStyle().Foreground(colTextMuted)
	if d.list.FilterState() == list.Unfiltered {
		return style.Render("type to filter · ↑↓ move · enter select · esc close")
	}
	// Once filtering is active "type to filter" no longer needs saying — drop
	// it to make room for the match count, the thing the user needs mid-filter.
	hint := style.Render("↑↓ move · enter select · esc close")
	count := style.Render(fmt.Sprintf("%d/%d", len(d.list.VisibleItems()), len(d.list.Items())))
	gap := d.list.Width() - lipgloss.Width(hint) - lipgloss.Width(count)
	if gap < 1 {
		return hint
	}
	return hint + strings.Repeat(" ", gap) + count
}

// renderOverlay composites fg centered over bg and fades bg to faint
// (terminal "dim" SGI attribute) outside fg's bounds, so an open dialog reads
// as foreground against a visually receded chat instead of replacing it
// outright (P16.6). width/height are the full terminal frame dimensions.
func renderOverlay(bg, fg string, width, height int) string {
	if width <= 0 || height <= 0 {
		return fg
	}
	x, y := overlayOrigin(fg, width, height)

	root := lipgloss.NewLayer(bg, lipgloss.NewLayer(fg).X(x).Y(y).Z(1))
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(root))
	dimOutside(canvas, x, y, lipgloss.Width(fg), lipgloss.Height(fg))
	return canvas.Render()
}

// renderAnchoredOverlay composites fg over bg at a caller-chosen (x,y)
// instead of centering, and does not dim the rest of the frame (P33.18). It
// backs non-modal overlays anchored to a fixed point on screen — the
// completion popup sits just above the composer while the user keeps typing
// behind it, so unlike renderOverlay's centered/dimmed modal treatment,
// nothing here should visually recede.
func renderAnchoredOverlay(bg, fg string, x, y, width, height int) string {
	if width <= 0 || height <= 0 {
		return fg
	}
	y = max(0, y)

	root := lipgloss.NewLayer(bg, lipgloss.NewLayer(fg).X(x).Y(y).Z(1))
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(root))
	return canvas.Render()
}

// transientPanel is a dismissable, scrollable text overlay for purely
// informational slash-command output (/status, /help, /memory, … — P33.11).
// Unlike the listDialog family it renders free-form text rather than a
// filterable list, and it never enters the transcript, so housekeeping
// commands don't leave stale blocks behind. Tall output (/help) scrolls a
// fixed-height window with the arrow/page keys; esc, enter, or q closes it.
// It composites over the live chat via renderOverlay, the same treatment the
// pickers and approval dialog use.
type transientPanel struct {
	title string
	body  string // the raw, unwrapped output text
	isErr bool   // tint the title chip when the command reported an error

	lines  []string // body wrapped to width, one entry per rendered row
	offset int      // index of the first visible line
	width  int      // content width the lines were wrapped to
	height int      // number of body rows visible at once (the scroll window)
}

// newTransientPanel builds a panel sized to the terminal. termW/termH are the
// full frame dimensions; the panel takes a bounded fraction so it reads as an
// overlay rather than a full-screen takeover.
func newTransientPanel(title, body string, isErr bool, termW, termH int) transientPanel {
	p := transientPanel{title: title, body: body, isErr: isErr}
	p.resize(termW, termH)
	return p
}

// resize re-wraps the body for the current terminal size and clamps the scroll
// offset so a shrink doesn't leave it scrolled past the end.
func (p *transientPanel) resize(termW, termH int) {
	// Leave room for the rounded frame, its padding, and the surrounding dim
	// margin; floor/ceil so a tiny or huge terminal still renders sanely.
	p.width = min(max(termW-8, 24), 100)
	p.height = max(termH-10, 3)
	p.lines = strings.Split(wrap(p.body, p.width), "\n")
	p.offset = min(p.offset, p.maxOffset())
}

// maxOffset is the largest first-visible-line index that still fills the
// window; 0 when the whole body fits.
func (p *transientPanel) maxOffset() int { return max(0, len(p.lines)-p.height) }

// scroll moves the viewport by delta lines, bounded to the content.
func (p *transientPanel) scroll(delta int) {
	p.offset = max(0, min(p.offset+delta, p.maxOffset()))
}

// scrollable reports whether the body is taller than the window, i.e. whether
// the scroll keys and position indicator are meaningful.
func (p *transientPanel) scrollable() bool { return len(p.lines) > p.height }

func (p transientPanel) View() string {
	titleBg := colBrandBg
	if p.isErr {
		titleBg = colError
	}
	titleChip := lipgloss.NewStyle().
		Background(titleBg).
		Foreground(colBrandFg).
		Bold(true).
		Padding(0, 1).
		Render(p.title)

	end := min(p.offset+p.height, len(p.lines))
	visible := append([]string(nil), p.lines[p.offset:end]...)
	// Hold the box at a fixed height while scrolling so the frame doesn't jump
	// as a short final page scrolls into view; a body that fits is left at its
	// natural height (no padding) so /memory doesn't render as a tall box of
	// blank lines.
	if p.scrollable() {
		for len(visible) < p.height {
			visible = append(visible, "")
		}
	}
	bodyStyle := lipgloss.NewStyle().Foreground(colFgBase)
	if p.isErr {
		bodyStyle = bodyStyle.Foreground(colError)
	}
	body := bodyStyle.Width(p.width).Render(strings.Join(visible, "\n"))

	hint := "esc to close"
	if p.scrollable() {
		hint = fmt.Sprintf("%d–%d of %d · j/k ↑/↓ · g/G top/bottom · esc close", p.offset+1, end, len(p.lines))
	}
	footer := lipgloss.NewStyle().Foreground(colTextMuted).Render(hint)

	return dialogFrame(titleChip + "\n\n" + body + "\n\n" + footer)
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
