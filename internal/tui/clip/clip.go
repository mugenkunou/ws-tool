// Package clip copies text to the system clipboard. It always emits OSC 52
// (the terminal sets the clipboard; works over SSH and in tmux with
// set-clipboard on) and, when a native tool is available, also pipes the text
// to wl-copy / xclip / xsel so it works in terminals without OSC 52.
package clip

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// CopiedMsg reports a finished copy. Native is the tool that also received
// the text ("" when only OSC 52 was used); Err is that tool's failure.
type CopiedMsg struct {
	Text   string
	Native string
	Err    error
}

// Copy returns a command that copies text and then emits CopiedMsg.
func Copy(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		name, args := nativeTool()
		if name == "" {
			return CopiedMsg{Text: text}
		}
		c := exec.Command(name, args...)
		c.Stdin = strings.NewReader(text)
		return CopiedMsg{Text: text, Native: name, Err: c.Run()}
	})
}

// nativeTool picks a clipboard writer for the current session.
func nativeTool() (string, []string) {
	candidates := []struct {
		need string // env var that must be set
		name string
		args []string
	}{
		{"WAYLAND_DISPLAY", "wl-copy", nil},
		{"DISPLAY", "xclip", []string{"-selection", "clipboard"}},
		{"DISPLAY", "xsel", []string{"--clipboard", "--input"}},
	}
	for _, c := range candidates {
		if os.Getenv(c.need) == "" {
			continue
		}
		if _, err := exec.LookPath(c.name); err == nil {
			return c.name, c.args
		}
	}
	return "", nil
}
