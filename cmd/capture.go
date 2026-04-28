package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/mugenkunou/ws-tool/internal/capture"
	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var captureHelp = cmdHelp{
	Usage: "ws capture [location] [-a] [-e] [--dry-run]",
	Subcommands: []string{
		"  (default)    Pin clipboard or stdin pipe to captures",
		"  ls           List configured capture locations",
	},
	Flags: []string{
		"  -a, --amend  Append to the last entry instead of creating a new one",
		"  -e, --edit   Open captures file in editor",
		"  --dry-run    Preview without writing",
	},
}

func runCapture(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, captureHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	wsDir := workspacePath + "/ws"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Check for "ls" subcommand before flag parsing
	if len(args) > 0 && args[0] == "ls" {
		return runCaptureLs(args[1:], globals, cfg, wsDir, stdout, stderr)
	}

	// Parse flags — location is positional, extracted before flag parsing
	locName, flagArgs, err := extractLocation(args, cfg)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	amend := fs.Bool("amend", false, "append to last entry")
	amendShort := fs.Bool("a", false, "append to last entry (short)")
	edit := fs.Bool("edit", false, "open in editor")
	editShort := fs.Bool("e", false, "open in editor (short)")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(flagArgs); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}
	isAmend := *amend || *amendShort
	isEdit := *edit || *editShort

	capturesFile, assetsDir, err := capture.ResolveLocation(wsDir, cfg.Capture.Locations, locName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Edit mode: open captures file in editor
	if isEdit {
		return runCaptureEdit(capturesFile, globals, cfg, stdin, stdout, stderr)
	}

	opts := capture.PinOptions{
		CapturesFile: capturesFile,
		AssetsDir:    assetsDir,
		DryRun:       globals.dryRun,
		Amend:        isAmend,
	}

	// Try reading stdin (piped input)
	piped, isPiped := readPipedStdin(stdin)
	if isPiped && len(piped) > 0 {
		return capturePiped(string(piped), opts, globals, stdout, stderr)
	}

	// Clipboard — prompt for topic on TTY (skip when amending)
	if !isAmend {
		opts.Topic = promptTopic(stdin, stdout, globals)
	}
	return captureClipboard(opts, globals, stdout, stderr)
}

// extractLocation separates a positional location name from the args list.
// Returns the location name (empty string for default) and the remaining args.
// Returns an error if the first positional argument is not a known location.
func extractLocation(args []string, cfg config.Config) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, nil
	}
	first := args[0]
	// If it starts with "-", it's a flag, not a location
	if strings.HasPrefix(first, "-") {
		return "", args, nil
	}
	// Check if it matches a configured location name
	if _, ok := cfg.Capture.Locations[first]; ok {
		return first, args[1:], nil
	}
	// Unknown positional argument — reject it
	return "", nil, fmt.Errorf("unknown capture location %q — use 'ws capture ls' to see configured locations", first)
}

func runCaptureEdit(capturesFile string, globals globalFlags, cfg config.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	// Ensure captures file exists before opening
	dir := filepath.Dir(capturesFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if _, err := os.Stat(capturesFile); os.IsNotExist(err) {
		if err := os.WriteFile(capturesFile, []byte("# Captures\n\n"), 0o644); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	editor := resolveEditor(cfg)

	if globals.json {
		return writeJSON(stdout, stderr, "capture.edit", map[string]any{
			"editor": editor,
			"file":   capturesFile,
		})
	}

	cmd := exec.Command(editor, capturesFile)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		out := textOut(globals, stdout)
		nc := globals.noColor
		fmt.Fprintln(out, style.ResultWarning(nc, "Editor launch skipped: %v", err))
		fmt.Fprintln(out, capturesFile)
		return 0
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s\n", style.ResultSuccess(nc, "Opened    %s → %s", editor, capturesFile))
	return 0
}

func runCaptureLs(args []string, globals globalFlags, cfg config.Config, wsDir string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("capture-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Warn about reserved "default" key in config
	if _, ok := cfg.Capture.Locations["default"]; ok {
		fmt.Fprintln(stderr, "warning: \"default\" is a reserved location name and is ignored in config")
	}

	locs := capture.Locations(wsDir, cfg.Capture.Locations)

	if globals.json {
		return writeJSON(stdout, stderr, "capture.ls", map[string]any{"locations": locs})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	for _, loc := range locs {
		existsStr := style.Mutedf(nc, "(not created)")
		if loc.Exists {
			existsStr = style.Mutedf(nc, "(exists)")
		}
		fmt.Fprintf(out, "  %-14s %s  %s\n",
			style.Boldf(nc, "%s", loc.Name),
			style.Mutedf(nc, "%s", loc.Path),
			existsStr)
	}
	return 0
}

func capturePiped(content string, opts capture.PinOptions, globals globalFlags, stdout, stderr io.Writer) int {
	content = strings.TrimSpace(content)
	if content == "" {
		fmt.Fprintln(stderr, "empty input")
		return 1
	}
	res, err := capture.PinText(content, opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return printPinResult(res, globals, stdout, stderr)
}

func captureClipboard(opts capture.PinOptions, globals globalFlags, stdout, stderr io.Writer) int {
	res, err := capture.PinClipboard(opts)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return printPinResult(res, globals, stdout, stderr)
}

func printPinResult(res capture.PinResult, globals globalFlags, stdout, stderr io.Writer) int {
	if globals.json {
		return writeJSON(stdout, stderr, "capture.pin", res)
	}
	out := textOut(globals, stdout)
	nc := globals.noColor
	var msg string
	if res.Amended {
		msg = fmt.Sprintf("Amended   %s → %s", res.Source, filepath.Base(res.File))
	} else {
		msg = fmt.Sprintf("Pinned    %s → %s", res.Source, filepath.Base(res.File))
	}
	if globals.dryRun {
		fmt.Fprintf(out, "%s\n", style.ResultWarning(nc, "[dry-run] %s", msg))
	} else {
		fmt.Fprintf(out, "%s\n", style.ResultSuccess(nc, "%s", msg))
	}
	return 0
}

// promptTopic asks the user for an entry topic.
// Returns empty string in quiet/json mode or on EOF (auto-derive will be used).
func promptTopic(stdin io.Reader, stdout io.Writer, globals globalFlags) string {
	if globals.quiet || globals.json {
		return ""
	}
	nc := globals.noColor
	fmt.Fprintf(stdout, "%s ", style.Mutedf(nc, "Topic:"))
	line, err := readLine(stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

func resolveEditor(cfg config.Config) string {
	if cfg.Scratch.EditorCmd != "" {
		return cfg.Scratch.EditorCmd
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	return "vi"
}

// readPipedStdin reads all available bytes from stdin.
// Returns the content and true if there was actual piped content.
// When stdin is strings.NewReader (tests) or an actual pipe, Read returns data then EOF.
// When stdin is a TTY, we don't block — but in tests stdin is always a Reader.
func readPipedStdin(r io.Reader) ([]byte, bool) {
	// Check if the reader is a *os.File and if so, check if it's a terminal.
	if f, ok := r.(*os.File); ok {
		if isTerminal(f.Fd()) {
			return nil, false
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, false
	}
	if len(data) == 0 {
		return nil, false
	}
	return data, true
}

func isTerminal(fd uintptr) bool {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	return errno == 0
}
