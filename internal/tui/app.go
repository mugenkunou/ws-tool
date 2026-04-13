package tui

import (
	"io"
	"os"

	"github.com/mugenkunou/ws-tool/internal/config"
)

// App is the interactive TUI application.
type App struct {
	WorkspacePath string
	ConfigPath    string
	ManifestPath  string
	Cfg           config.Config
	NoColor       bool
	Stdin         *os.File
	Stdout        *os.File

	data DashboardData
}

// Run launches the interactive TUI. It enters raw mode, renders the dashboard,
// and processes key events until the user quits. Returns exit code.
func (a *App) Run() int {
	size, err := GetTermSize(a.Stdout.Fd())
	if err != nil {
		// Can't get terminal size — fall back.
		return -1
	}

	old, err := setRaw(a.Stdin.Fd())
	if err != nil {
		return -1
	}
	defer func() {
		_ = restoreTermios(a.Stdin.Fd(), old)
		ShowCursor(a.Stdout)
		MainScreen(a.Stdout)
	}()

	AltScreen(a.Stdout)
	HideCursor(a.Stdout)

	// Initial load: try health.json fast-path first.
	a.data = LoadDashboardFromHealth(a.WorkspacePath, a.ConfigPath, a.ManifestPath, a.Cfg)
	a.render(size)

	buf := make([]byte, 16)
	for {
		n, err := a.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}

		// Re-check terminal size on each keypress (handles resize).
		if newSize, err := GetTermSize(a.Stdout.Fd()); err == nil {
			size = newSize
		}

		b := buf[:n]
		if n == 1 {
			switch b[0] {
			case 'q', 3: // q or Ctrl+C
				return 0
			case 'r':
				a.data = LoadDashboard(a.WorkspacePath, a.ConfigPath, a.ManifestPath, a.Cfg)
				a.render(size)
			case 's':
				a.data = LoadDashboard(a.WorkspacePath, a.ConfigPath, a.ManifestPath, a.Cfg)
				a.render(size)
			case '?':
				a.renderHelp(size)
				// Wait for any key to dismiss.
				a.Stdin.Read(buf)
				a.render(size)
			}
		}
	}
	return 0
}

func (a *App) render(size TermSize) {
	ClearScreen(a.Stdout)
	MoveTo(a.Stdout, 1, 1)
	RenderDashboard(a.Stdout, a.data, size, a.NoColor)
}

func (a *App) renderHelp(size TermSize) {
	ClearScreen(a.Stdout)
	MoveTo(a.Stdout, 1, 1)
	w := io.Writer(a.Stdout)
	nc := a.NoColor

	lines := []string{
		"",
		"  ws tui — keyboard shortcuts",
		"",
		"  q        Quit",
		"  r        Refresh all panels (re-runs scan)",
		"  s        Run scan, update violations panel",
		"  ?        Show this help",
		"  Ctrl+C   Quit",
		"",
		"  Press any key to return to dashboard.",
	}
	for _, line := range lines {
		if nc {
			io.WriteString(w, line+"\n")
		} else {
			io.WriteString(w, line+"\n")
		}
	}
}
