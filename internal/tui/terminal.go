package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/scottymacleod/aegis/internal/sandbox"
)

const (
	// termPaneInnerW is the lipgloss Width() value (content, excluding border).
	termPaneInnerW = 44
	// termPaneVpW is the viewport width inside the pane (inner - paddingLeft 1).
	termPaneVpW = 43
	// termPaneTotalW is the total rendered width (inner + left border).
	termPaneTotalW = 45
)

// termOutputMsg delivers incremental shell output to the TUI.
type termOutputMsg struct{ text string }

// termDoneMsg signals that the currently running command has finished.
type termDoneMsg struct{ err error }

// termRun tracks an in-flight shell command.
type termRun struct {
	ch     <-chan string
	done   <-chan error
	cancel context.CancelFunc
}

// waitForTermOutput returns a tea.Cmd that blocks on the next output chunk
// from run. When the channel closes it reads the final error and returns
// termDoneMsg.
func waitForTermOutput(run *termRun) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-run.ch
		if !ok {
			err := <-run.done
			return termDoneMsg{err: err}
		}
		return termOutputMsg{text: text}
	}
}

// execTermCmd starts command in workDir and returns a termRun plus the initial
// wait cmd. Output is streamed line by line via the channel; context
// cancellation kills the underlying process.
func execTermCmd(workDir, command string) (*termRun, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 256)
	done := make(chan error, 1)

	go func() {
		sb := sandbox.NewLocalBackend()
		err := sb.ExecStreaming(ctx, command, sandbox.ExecOpts{Dir: workDir}, func(s string) {
			select {
			case ch <- s:
			case <-ctx.Done():
			}
		})
		close(ch)
		done <- err
		close(done)
	}()

	run := &termRun{ch: ch, done: done, cancel: cancel}
	return run, waitForTermOutput(run)
}

// termPane is a scrollable interactive shell pane rendered as a right-side
// panel in the main TUI layout when termOpen is true.
type termPane struct {
	vp      viewport.Model
	buf     strings.Builder // raw accumulated output
	input   string          // current command line being typed
	history []string        // command history, oldest-first
	histIdx int             // -1 when not navigating history
	draft   string          // saved draft before history navigation began
	workDir string          // cwd for shell commands (updated by cd)
	running bool
	height  int // full pane height passed to lipgloss Height()
}

func newTermPane(workDir string, h int) termPane {
	vpH := max(h-3, 1) // header(1) + sep(1) + input(1) = 3 fixed rows
	tp := termPane{
		workDir: workDir,
		histIdx: -1,
		height:  h,
	}
	tp.vp = viewport.New(viewport.WithWidth(termPaneVpW), viewport.WithHeight(vpH))
	return tp
}

// resize adjusts the pane height after a terminal resize event.
func (tp *termPane) resize(h int) {
	tp.height = h
	tp.vp.SetHeight(max(h-3, 1))
	tp.vp.SetWidth(termPaneVpW)
}

// appendText appends text to the output buffer and refreshes the viewport.
func (tp *termPane) appendText(text string) {
	tp.buf.WriteString(text)
	tp.refreshVP()
}

// refreshVP re-renders the output buffer into the scrolling viewport.
func (tp *termPane) refreshVP() {
	raw := tp.buf.String()
	if strings.IndexByte(raw, 0x1b) >= 0 {
		raw = remapANSI16(raw, ansiPalette)
	}
	tp.vp.SetContent(wrap(raw, termPaneVpW))
	tp.vp.GotoBottom()
}

// historyPrev moves to the previous history entry.
func (tp *termPane) historyPrev() {
	if len(tp.history) == 0 {
		return
	}
	if tp.histIdx == -1 {
		tp.draft = tp.input
		tp.histIdx = len(tp.history) - 1
	} else if tp.histIdx > 0 {
		tp.histIdx--
	}
	tp.input = tp.history[tp.histIdx]
}

// historyNext moves to the next history entry or back to the draft.
func (tp *termPane) historyNext() {
	if tp.histIdx == -1 {
		return
	}
	tp.histIdx++
	if tp.histIdx >= len(tp.history) {
		tp.histIdx = -1
		tp.input = tp.draft
	} else {
		tp.input = tp.history[tp.histIdx]
	}
}

// handleCD intercepts "cd" commands and updates workDir in-process instead of
// running them via the shell (which would not affect the pane's working dir).
// Returns true if the command was consumed (whether or not it succeeded).
func (tp *termPane) handleCD(cmd string) bool {
	if cmd != "cd" && !strings.HasPrefix(cmd, "cd ") && !strings.HasPrefix(cmd, "cd\t") {
		return false
	}
	arg := strings.TrimSpace(cmd[2:])
	var target string
	switch arg {
	case "", "~":
		home, err := os.UserHomeDir()
		if err != nil {
			tp.appendText("cd: cannot determine home directory\n")
			return true
		}
		target = home
	default:
		if strings.HasPrefix(arg, "~/") || strings.HasPrefix(arg, `~\`) {
			if home, err := os.UserHomeDir(); err == nil {
				arg = filepath.Join(home, arg[2:])
			}
		}
		if filepath.IsAbs(arg) {
			target = filepath.Clean(arg)
		} else {
			target = filepath.Join(tp.workDir, arg)
		}
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		tp.appendText("cd: " + err.Error() + "\n")
		return true
	}
	if _, err := os.Stat(abs); err != nil {
		tp.appendText("cd: " + strings.TrimSpace(cmd[2:]) + ": no such file or directory\n")
		return true
	}
	tp.workDir = abs
	tp.appendText("→ " + abs + "\n")
	return true
}

// handleOutput appends streaming command output.
func (tp *termPane) handleOutput(text string) { tp.appendText(text) }

// handleDone marks the command finished and shows any error.
func (tp *termPane) handleDone(err error) {
	tp.running = false
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		tp.appendText("[interrupted]\n")
		return
	}
	tp.appendText("[exit: " + err.Error() + "]\n")
}

// view renders the terminal pane at its configured width and height.
func (tp termPane) view(th theme, focused bool) string {
	// Header row: focus indicator + directory / running status.
	var indicator string
	if focused {
		indicator = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("⬤ TERMINAL")
	} else {
		indicator = lipgloss.NewStyle().Foreground(colTextMuted).Render("○ TERMINAL")
	}
	var statusRight string
	if tp.running {
		statusRight = lipgloss.NewStyle().Foreground(colAccent).Render("running…")
	} else {
		statusRight = lipgloss.NewStyle().Foreground(colTextMuted).Render(
			truncate(shortenPath(tp.workDir), termPaneVpW-12))
	}
	header := indicator + "  " + statusRight

	sep := th.turnSep.Render(strings.Repeat("─", termPaneVpW))

	// Input row: prompt + current input + optional blinking cursor.
	var inputLine string
	if tp.running {
		inputLine = lipgloss.NewStyle().Foreground(colTextMuted).Render("  (running…)")
	} else {
		cursor := ""
		if focused {
			cursor = "▋"
		}
		prompt := lipgloss.NewStyle().Foreground(colAccent).Render("❯ ")
		inputLine = prompt + truncate(tp.input, termPaneVpW-4) + cursor
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, tp.vp.View(), sep, inputLine)

	borderFg := colBorder
	if focused {
		borderFg = colAccent
	}

	return lipgloss.NewStyle().
		Width(termPaneInnerW).
		Height(tp.height).
		MaxHeight(tp.height).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(borderFg).
		PaddingLeft(1).
		Render(content)
}
