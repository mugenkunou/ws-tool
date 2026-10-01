package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mugenkunou/ws-tool/internal/tui/clip"
	"github.com/mugenkunou/ws-tool/internal/tui/tuitest"
)

// noNativeClipboard keeps tests from touching the real clipboard.
func noNativeClipboard(t *testing.T) {
	t.Helper()
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
}

func drag(m Model, x0, y0, x1, y1 int) (Model, tea.Cmd) {
	m = send(m, tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	return sendRun(m, tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
}

// copied runs cmd (expanding batches) and returns the CopiedMsg, if any.
func copied(t *testing.T, cmd tea.Cmd) *clip.CopiedMsg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case clip.CopiedMsg:
			return &msg
		}
	}
	return nil
}

// Frame row 2 is "🌅 Good morning  ·  ~/Workspace"; 🌅 fills cells 0-1.
func TestDragCopiesSelectedText(t *testing.T) {
	noNativeClipboard(t)
	m := loaded(t, 100, 30)
	m, cmd := drag(m, 3, 2, 14, 2)
	got := copied(t, cmd)
	if got == nil || got.Text != "Good morning" {
		t.Fatalf("copied %+v, want %q", got, "Good morning")
	}
	if m.selecting {
		t.Fatal("selection still active after release")
	}
}

func TestDragAcrossLines(t *testing.T) {
	noNativeClipboard(t)
	m := loaded(t, 100, 30)
	_, cmd := drag(m, 3, 2, 5, 4) // from "Good" to the start of row 4
	got := copied(t, cmd)
	if got == nil || !strings.HasPrefix(got.Text, "Good morning") || strings.Count(got.Text, "\n") != 2 {
		t.Fatalf("multi-line copy: %+v", got)
	}
}

func TestClickDoesNotCopy(t *testing.T) {
	noNativeClipboard(t)
	m := loaded(t, 100, 30)
	m = send(m, tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	_, cmd := sendRun(m, tea.MouseReleaseMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	if copied(t, cmd) != nil {
		t.Fatal("a click copied text")
	}
}

func TestDragHighlightsWithoutChangingText(t *testing.T) {
	m := loaded(t, 100, 30)
	before := view(m)
	m = send(m, tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}, tea.MouseMotionMsg{X: 14, Y: 5, Button: tea.MouseLeft})
	during := view(m)
	if during == before {
		t.Fatal("drag did not highlight")
	}
	if tuitest.Plain(during) != tuitest.Plain(before) {
		t.Fatal("highlight changed the text")
	}
	if m = send(m, press("down")); m.selecting || view(m) == during {
		t.Fatal("a key press should clear the selection")
	}
}

// TestCopyToastExpires: the toast replaces the footer's last line (no layout
// shift) and disappears when its timer fires.
func TestCopyToastExpires(t *testing.T) {
	m := loaded(t, 100, 30)
	lines := strings.Count(view(m), "\n")
	m, cmd := sendRun(m, clip.CopiedMsg{Text: "hello"})
	v := tuitest.Plain(view(m))
	if !strings.Contains(v, "Copied 5 characters") {
		t.Fatalf("toast missing:\n%s", v)
	}
	if strings.Count(view(m), "\n") != lines {
		t.Fatal("toast changed the layout height")
	}
	if cmd == nil {
		t.Fatal("toast scheduled no expiry")
	}
	// An older toast's timer must not hide a newer toast.
	m, _ = sendRun(m, clip.CopiedMsg{Text: "second"})
	m = send(m, toastExpireMsg{id: 1})
	if !strings.Contains(tuitest.Plain(view(m)), "Copied 6 characters") {
		t.Fatal("stale expiry hid the newer toast")
	}
	m = send(m, toastExpireMsg{id: 2})
	if strings.Contains(tuitest.Plain(view(m)), "Copied") {
		t.Fatal("toast did not expire")
	}
}

func TestWheelScrollsLikeArrows(t *testing.T) {
	a := send(loaded(t, 100, 30), press("down"), press("down"))
	b := send(loaded(t, 100, 30), tea.MouseWheelMsg{Button: tea.MouseWheelDown}, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if view(a) != view(b) {
		t.Fatal("wheel down did not move like ↓")
	}
}

func TestMouseOptOut(t *testing.T) {
	if v := loaded(t, 80, 24).View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse capture not enabled by default")
	}
	t.Setenv("WS_NO_MOUSE", "1")
	m := loaded(t, 80, 24)
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("WS_NO_MOUSE did not disable mouse capture")
	}
	m = send(m, tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}, tea.MouseMotionMsg{X: 14, Y: 2})
	if m.selecting {
		t.Fatal("selection started with the mouse disabled")
	}
}
