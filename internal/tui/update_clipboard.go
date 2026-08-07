package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// updateClipboardResult reports the outcome of a copy-to-clipboard command.
func (m model) updateClipboardResult(msg clipboardResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("copy: "+msg.err.Error(), toastError)
		m.activeToast = t
		return m, cmd
	}
	t, cmd := newToastCmd("copied to clipboard", toastInfo)
	m.activeToast = t
	return m, cmd
}

// updatePasteImageResult attaches (or reports the absence of) a clipboard image.
func (m model) updatePasteImageResult(msg pasteImageResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		t, cmd := newToastCmd("paste image: "+msg.err.Error(), toastError)
		m.activeToast = t
		return m, cmd
	}
	if !msg.ok {
		t, cmd := newToastCmd("clipboard has no image", toastInfo)
		m.activeToast = t
		return m, cmd
	}
	m.ta.InsertString(attachTokenFor(msg.path))
	t, cmd := newToastCmd("image attached: "+filepath.Base(msg.path), toastInfo)
	m.activeToast = t
	return m, cmd
}

// updateEditorDone folds the external editor's buffer back into the composer.
// A false bool means the message also falls through to the composer.
func (m model) updateEditorDone(msg editorDoneMsg) (model, tea.Cmd, bool) {
	m.ta.Focus()
	if msg.err != nil {
		t, cmd := newToastCmd("editor: "+msg.err.Error(), toastError)
		m.activeToast = t
		return m, cmd, true
	}
	if strings.TrimSpace(msg.content) != "" {
		m.ta.SetValue(msg.content)
	}
	return m, nil, false
}

// updatePaste handles a bracketed paste.
//
// TQ9: a pasted image path becomes an attachment token instead of raw text,
// replacing the typed @image:<path> incantation. Anything else falls through to
// the textarea's own paste handling. The approval dialog owns all input while
// open (P25.4a), same as it does for tea.KeyMsg, so a paste can't land silently
// in the composer.
func (m model) updatePaste(msg tea.PasteMsg) (model, tea.Cmd, bool) {
	if !m.termFocused && m.approval == nil {
		p := strings.TrimSpace(msg.Content)
		// Windows "Copy as path" and shell copies often quote the path.
		if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
			p = p[1 : len(p)-1]
		}
		if looksLikeImagePath(p) {
			m.ta.InsertString(attachTokenFor(p))
			t, cmd := newToastCmd("image attached: "+filepath.Base(p), toastInfo)
			m.activeToast = t
			return m, cmd, true
		}
	}
	return m, nil, false
}
