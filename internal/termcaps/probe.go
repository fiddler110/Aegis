package termcaps

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

// ProbeDeadline is the outer safety deadline, not a decision input. The DA1
// ordering rule needs no timeout; this only bounds the damage when the other
// end is not a conforming terminal (a pty with nothing attached, a Windows
// console without VT input, a socket that passed the isatty check). A
// conforming terminal answers in microseconds, so this is never waited out in
// practice — which is why it can afford to be generous.
const ProbeDeadline = 2 * time.Second

// EnvOverride is the escape hatch: it forces the answer instead of asking.
//
//	AEGIS_TERM_CAPS=off | none        — do not probe, report nothing supported
//	AEGIS_TERM_CAPS=auto | ""         — probe (the default)
//	AEGIS_TERM_CAPS=kitty,sync,truecolor — force this exact set, do not probe
//
// Recognized feature words: kitty, sync (synchronized/2026), truecolor
// (24bit/rgb). Anything else in the list is ignored, so a typo degrades to
// "that feature is off", never to a hang.
const EnvOverride = "AEGIS_TERM_CAPS"

// Override reads EnvOverride out of environ. ok is false when the environment
// asks for the normal probe.
func Override(environ []string) (caps Caps, ok bool) {
	raw := ""
	for _, kv := range environ {
		if strings.HasPrefix(kv, EnvOverride+"=") {
			raw = kv[len(EnvOverride)+1:]
		}
	}
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "auto":
		return Caps{}, false
	case "off", "none", "0", "false":
		return Caps{Source: EnvOverride + "=" + v + " (probe disabled)"}, true
	}
	caps = Caps{Source: EnvOverride + "=" + v + " (forced, not probed)"}
	for _, f := range strings.Split(v, ",") {
		switch strings.TrimSpace(f) {
		case "kitty", "graphics":
			caps.KittyGraphics = true
		case "sync", "synchronized", "2026":
			caps.SyncOutput = true
		case "truecolor", "24bit", "rgb":
			caps.TrueColor = true
		}
	}
	return caps, true
}

var (
	once   sync.Once
	cached Caps
)

// Cached probes stdin/stdout the first time it is called and returns the same
// answer forever after. This is the entry point every caller should use: the
// probe is a startup act, never a per-frame one.
//
// It must be reached before bubbletea starts reading stdin — replies share the
// keyboard's file descriptor, and a reply that arrives after bubbletea owns the
// input channel is delivered to the UI as garbage keys. In this codebase that
// ordering is structural rather than hopeful: tui.Run resolves the image
// protocol (which calls here) while building the model, which happens strictly
// before tea.NewProgram(...).Run().
func Cached() Caps {
	once.Do(func() { cached = Probe(os.Stdin, os.Stdout, os.Environ()) })
	return cached
}

// Probe writes the query batch to out and reads the answers from in.
//
// It degrades to "nothing supported, nothing asked" — never to an error and
// never to a hang — when either end is not a terminal (piped output, CI,
// `aegis serve`), when raw mode is unavailable, or when the console cannot be
// put into a mode where the queries would even be interpreted.
func Probe(in, out *os.File, environ []string) Caps {
	if caps, ok := Override(environ); ok {
		return caps
	}
	if in == nil || out == nil {
		return Caps{Source: "not probed: no terminal handles"}
	}
	// Screen with Stat() before Fd(). os.File.Fd() permanently unregisters the
	// descriptor from the Go runtime poller and switches it to blocking mode,
	// so asking term.IsTerminal first would damage every non-terminal caller on
	// the way to telling us it is not a terminal: SetReadDeadline afterwards
	// returns nil and has no effect, and the next Read blocks in a raw syscall
	// forever. A character device is a necessary condition for a terminal, so
	// this rejects pipes, regular files and sockets without touching Fd(), and
	// everything past this point already needs a real terminal.
	if !isCharDevice(in) || !isCharDevice(out) {
		return Caps{Source: "not probed: stdin/stdout is not a terminal"}
	}
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return Caps{Source: "not probed: stdin/stdout is not a terminal"}
	}
	// Windows consoles interpret the query escapes only with VT processing
	// enabled on the output handle; without it the queries would be *printed*
	// rather than answered. If it cannot be turned on, do not write anything.
	restoreOut, err := enableVTOutput(out)
	if err != nil {
		return Caps{Source: "not probed: " + err.Error()}
	}
	defer restoreOut()

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return Caps{Source: "not probed: raw mode unavailable (" + err.Error() + ")"}
	}
	defer func() { _ = term.Restore(in.Fd(), state) }()

	if _, err := io.WriteString(out, QueryBatch); err != nil {
		return Caps{Source: "not probed: query write failed (" + err.Error() + ")"}
	}

	caps, err := readReplies(in, ProbeDeadline)
	switch {
	case err == nil:
		caps.Source = "probed: the terminal answered (DA1-terminated batch)"
	default:
		caps.Source = fmt.Sprintf("partially probed: %v — features answered before the cut-off are still reliable", err)
	}
	return caps
}

// isCharDevice reports whether f is a character device, the cheap precondition
// for "is a terminal" that costs no poller registration. A failed Stat is
// reported as not-a-device: the probe's contract is to degrade to nothing
// supported rather than to guess.
func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// readReplies runs Decide against in under an outer safety deadline.
//
// The preferred mechanism is the file's own read deadline, which unblocks the
// read itself. Consoles that do not support one (Windows, and any fd the
// runtime poller cannot take) fall back to reading on a goroutine and giving up
// on it — that goroutine stays parked on a read until the next byte arrives,
// which only ever happens on a terminal that answers nothing, i.e. one where
// the probe has already concluded.
func readReplies(in *os.File, d time.Duration) (Caps, error) {
	if err := in.SetReadDeadline(time.Now().Add(d)); err == nil {
		defer func() { _ = in.SetReadDeadline(time.Time{}) }()
		caps, err := Decide(in)
		if os.IsTimeout(err) {
			return caps, ErrNoTerminator
		}
		return caps, err
	}

	type result struct {
		caps Caps
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		caps, err := Decide(in)
		ch <- result{caps, err}
	}()
	select {
	case r := <-ch:
		return r.caps, r.err
	case <-time.After(d):
		return Caps{}, ErrNoTerminator
	}
}
