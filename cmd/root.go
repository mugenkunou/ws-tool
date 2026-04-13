package cmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

type globalFlags struct {
	workspace  string
	config     string
	manifest   string
	quiet      bool
	verbose    bool
	json       bool
	dryRun     bool
	noColor    bool
	autoAccept bool // auto-accept plan actions without suppressing output (for exempted commands)
}

func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if shouldShowHelp(args) {
		// Quick parse --no-color for help display
		nc := style.ShouldDisableColor()
		for _, a := range args {
			if a == "--no-color" {
				nc = true
			}
		}
		printHelpStyled(stdout, nc)
		return 0
	}

	globals, rest, err := parseGlobalFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(rest) == 0 {
		printHelp(stdout)
		return 0
	}

	command := rest[0]
	commandArgs := rest[1:]

	verboseLog(globals, stderr, "command=%s args=%v", command, commandArgs)

	switch command {
	case "help", "--help", "-h":
		printHelpStyled(stdout, globals.noColor)
		return 0
	case "version":
		return runVersion(commandArgs, globals, stdout, stderr)
	case "init":
		return runInit(commandArgs, globals, stdin, stdout, stderr)
	case "reset":
		return runReset(commandArgs, globals, stdin, stdout, stderr)
	case "restore":
		return runRestore(commandArgs, globals, stdin, stdout, stderr)
	case "config":
		return runConfig(commandArgs, globals, stdout, stderr)
	case "dotfile":
		return runDotfile(commandArgs, globals, stdin, stdout, stderr)
	case "ignore":
		return runIgnore(commandArgs, globals, stdin, stdout, stderr)
	case "secret":
		return runSecret(commandArgs, globals, stdin, stdout, stderr)
	case "notify":
		return runNotify(commandArgs, globals, stdin, stdout, stderr)
	case "tui":
		return runTUI(commandArgs, globals, stdout, stderr)
	case "log":
		return runLog(commandArgs, globals, stdin, stdout, stderr)
	case "scratch":
		return runScratch(commandArgs, globals, stdin, stdout, stderr)
	case "repo":
		return runRepo(commandArgs, globals, stdin, stdout, stderr)
	case "context":
		return runContext(commandArgs, globals, stdin, stdout, stderr)
	case "trash":
		return runTrash(commandArgs, globals, stdin, stdout, stderr)
	case "credential", "git-credential-helper":
		return runGitCredentialHelper(commandArgs, globals, stdin, stdout, stderr)
	case "completions":
		return runCompletions(commandArgs, globals, stdin, stdout, stderr)
	case "__complete":
		return runComplete(commandArgs, globals, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", command)
		fmt.Fprintln(stderr, "Run `ws help` for available commands.")
		return 1
	}
}

func parseGlobalFlags(args []string) (globalFlags, []string, error) {
	globals := globalFlags{}

	type flagDef struct {
		str *string // non-nil for string flags
		boo *bool   // non-nil for bool flags
	}
	registry := map[string]flagDef{
		"workspace": {str: &globals.workspace},
		"w":         {str: &globals.workspace},
		"config":    {str: &globals.config},
		"c":         {str: &globals.config},
		"manifest":  {str: &globals.manifest},
		"json":      {boo: &globals.json},
		"dry-run":   {boo: &globals.dryRun},
		"no-color":  {boo: &globals.noColor},
		"quiet":     {boo: &globals.quiet},
		"q":         {boo: &globals.quiet},
		"verbose":   {boo: &globals.verbose},
	}

	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			continue
		}

		// Strip leading dashes and split on '=' for --flag=value form.
		raw := strings.TrimLeft(a, "-")
		name, value, hasEq := raw, "", false
		if idx := strings.Index(raw, "="); idx >= 0 {
			name, value, hasEq = raw[:idx], raw[idx+1:], true
		}

		def, known := registry[name]
		if !known {
			rest = append(rest, a)
			continue
		}

		if def.boo != nil {
			if hasEq {
				*def.boo = (value == "true" || value == "1")
			} else {
				*def.boo = true
			}
		} else {
			if hasEq {
				*def.str = value
			} else if i+1 < len(args) {
				i++
				*def.str = args[i]
			} else {
				return globalFlags{}, nil, fmt.Errorf("--%s requires a value", name)
			}
		}
	}

	// Respect NO_COLOR convention (https://no-color.org)
	if !globals.noColor && style.ShouldDisableColor() {
		globals.noColor = true
	}

	return globals, rest, nil
}

// registerGlobalFlags registers all global flags on a subcommand FlagSet,
// bound to the actual globals struct. This lets users write
// `ws version --json` instead of only `ws --json version`.
// Flags already defined on the FlagSet (like a local --dry-run) are skipped
// so the subcommand's own variable takes priority.
func registerGlobalFlags(fs *flag.FlagSet, globals *globalFlags) {
	if fs.Lookup("workspace") == nil {
		fs.StringVar(&globals.workspace, "workspace", globals.workspace, "")
	}
	if fs.Lookup("w") == nil {
		fs.StringVar(&globals.workspace, "w", globals.workspace, "")
	}
	if fs.Lookup("config") == nil {
		fs.StringVar(&globals.config, "config", globals.config, "")
	}
	if fs.Lookup("c") == nil {
		fs.StringVar(&globals.config, "c", globals.config, "")
	}
	if fs.Lookup("manifest") == nil {
		fs.StringVar(&globals.manifest, "manifest", globals.manifest, "")
	}
	if fs.Lookup("json") == nil {
		fs.BoolVar(&globals.json, "json", globals.json, "")
	}
	if fs.Lookup("dry-run") == nil {
		fs.BoolVar(&globals.dryRun, "dry-run", globals.dryRun, "")
	}
	if fs.Lookup("no-color") == nil {
		fs.BoolVar(&globals.noColor, "no-color", globals.noColor, "")
	}
	if fs.Lookup("quiet") == nil {
		fs.BoolVar(&globals.quiet, "quiet", globals.quiet, "")
	}
	if fs.Lookup("q") == nil {
		fs.BoolVar(&globals.quiet, "q", globals.quiet, "")
	}
	if fs.Lookup("verbose") == nil {
		fs.BoolVar(&globals.verbose, "verbose", globals.verbose, "")
	}
}

// parseInterspersed parses a FlagSet that may have flags and positional
// arguments mixed together (e.g. `ws search report --type pdf`).
// The standard flag package stops at the first non-flag arg; this helper
// reorders the slice so all recognised flags come before positional args,
// then calls fs.Parse.
func parseInterspersed(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		if strings.Contains(a, "=") {
			flags = append(flags, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		f := fs.Lookup(name)
		flags = append(flags, a)
		if f == nil {
			continue // unknown flag; let fs.Parse report the error
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		// non-bool flag consumes the next arg as its value
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return fs.Parse(append(flags, positional...))
}

// textOut returns stdout when informational text should be shown.
// In quiet mode (without --json), it returns io.Discard so text is
// silently suppressed while exit codes still communicate results.
// JSON output must always go to stdout directly — never through this.
func textOut(globals globalFlags, stdout io.Writer) io.Writer {
	if globals.quiet && !globals.json {
		return io.Discard
	}
	return stdout
}

// verboseLog prints a diagnostic message to stderr when --verbose is set.
func verboseLog(globals globalFlags, stderr io.Writer, format string, args ...any) {
	if globals.verbose {
		fmt.Fprintf(stderr, "[verbose] "+format+"\n", args...)
	}
}

// writeJSON writes a standard JSON envelope to stdout.
// Every JSON output from ws goes through this to ensure a consistent schema:
//
//	{"ws_version": "...", "schema": 1, "command": "...", "data": ...}
func writeJSON(stdout, stderr io.Writer, command string, data any) int {
	payload := map[string]any{
		"ws_version": appVersion,
		"schema":     1,
		"command":    command,
		"data":       data,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, string(encoded))
	return 0
}

// writeJSONDryRun is like writeJSON but includes "dry_run": true when applicable.
func writeJSONDryRun(stdout, stderr io.Writer, command string, dryRun bool, data any) int {
	payload := map[string]any{
		"ws_version": appVersion,
		"schema":     1,
		"command":    command,
		"data":       data,
	}
	if dryRun {
		payload["dry_run"] = true
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, string(encoded))
	return 0
}

func requireWorkspaceInitialized(globals globalFlags, stderr ...io.Writer) (string, string, string, error) {
	workspacePath := globals.workspace
	if workspacePath == "" {
		workspacePath = os.Getenv("WS_WORKSPACE")
	}
	if workspacePath == "" {
		workspacePath = "~/Workspace"
	}

	resolvedWorkspace, err := config.ExpandUserPath(workspacePath)
	if err != nil {
		return "", "", "", err
	}

	configPath := globals.config
	if configPath == "" {
		configPath = resolvedWorkspace + "/ws/config.json"
	}

	manifestPath := globals.manifest
	if manifestPath == "" {
		manifestPath = resolvedWorkspace + "/ws/manifest.json"
	}

	// Verbose: log resolved paths if stderr was provided.
	if len(stderr) > 0 && stderr[0] != nil {
		verboseLog(globals, stderr[0], "workspace=%s config=%s manifest=%s", resolvedWorkspace, configPath, manifestPath)
	}

	if !workspace.ConfigExists(configPath) {
		return "", "", "", errors.New("Error: workspace not initialized.\nRun `ws init` to set up this directory as a ws workspace.")
	}

	return resolvedWorkspace, configPath, manifestPath, nil
}

var configHelp = cmdHelp{
	Usage: "ws config <view|defaults>",
	Subcommands: []string{
		"  view       Print resolved config",
		"  defaults   Print built-in default config",
	},
}

func runConfig(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, configHelp)
	}

	if len(args) == 0 {
		return printUsageError(stderr, configHelp)
	}

	subcommand := strings.TrimSpace(args[0])

	if subcommand == "defaults" || subcommand == "--defaults" {
		fs := flag.NewFlagSet("config-defaults", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		defaults := config.Default()
		if globals.json {
			return writeJSON(stdout, stderr, "config.defaults", defaults)
		}
		encoded, err := json.MarshalIndent(defaults, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(textOut(globals, stdout), string(encoded))
		return 0
	}

	if subcommand != "view" {
		fmt.Fprintf(stderr, "unknown config subcommand: %s\n", subcommand)
		return printUsageError(stderr, configHelp)
	}

	fs := flag.NewFlagSet("config-view", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	_, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if globals.json {
		return writeJSON(stdout, stderr, "config.view", cfg)
	}

	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	fmt.Fprintln(textOut(globals, stdout), string(encoded))
	return 0
}

func printHelp(stdout io.Writer) {
	printHelpStyled(stdout, false)
}

func printHelpStyled(stdout io.Writer, noColor bool) {
	fmt.Fprintln(stdout, style.Boldf(noColor, "ws")+style.Mutedf(noColor, " — Workspace manager"))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Usage:"))
	fmt.Fprintf(stdout, "  %s <command> [flags]\n", style.Accentf(noColor, "ws"))

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Commands:"))

	cmds := []struct{ name, desc string }{
		{"version", "Print version information"},
		{"init", "Initialize a ws workspace"},
		{"reset", "Reverse ws init — undo all provisions"},
		{"restore", "Guided restore workflow"},
		{"dotfile", "Manage workspace-backed dotfiles"},
		{"repo", "Repository fleet operations"},
		{"context", "Task context sidecar management"},
		{"trash", "Soft-delete setup and status"},
		{"notify", "Notification daemon commands"},
		{"tui", "Interactive dashboard summary"},
		{"log", "Session recording commands"},
		{"scratch", "Scratch directory commands"},
		{"completions", "Generate shell completions"},
		{"ignore", "Ignore rule scan/check"},
		{"secret", "Secret scanning and pass store management"},
		{"git-credential-helper", "Git credential helper (pass-backed)"},
		{"config", "Configuration of ws"},
	}
	for _, c := range cmds {
		fmt.Fprintf(stdout, "  %-18s %s\n", style.Accentf(noColor, c.name), c.desc)
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Global Flags:"))
	flags := []string{
		"  -w, --workspace string   Path to workspace root (default: ~/Workspace)",
		"  -c, --config string      Path to config file (default: <workspace>/ws/config.json)",
		"      --manifest string    Path to manifest file (default: <workspace>/ws/manifest.json)",
		"  -q, --quiet              Errors only (default: false)",
		"      --verbose            Show internal decisions (default: false)",
		"      --json               Machine-readable output (default: false)",
		"      --dry-run            Preview actions, no changes (default: false)",
		"      --no-color           Disable colors/unicode output (default: false)",
		"  -h, --help               Show help",
	}
	for _, f := range flags {
		fmt.Fprintln(stdout, f)
	}
}

func hasHelpArg(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}

func shouldShowHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return true
	}
	return false
}

func maybePageOutput(stdout io.Writer, text string) bool {
	if text == "" {
		return false
	}
	f, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return false
	}
	if os.Getenv("PAGER") == "cat" {
		return false
	}

	// Only page when the output exceeds the terminal height.
	lineCount := strings.Count(text, "\n")
	if lineCount <= terminalHeight(f) {
		return false
	}

	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		pager = "less"
	}
	cmd := exec.Command(pager, "-R")
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return false
	}
	_, _ = io.WriteString(stdin, text)
	_ = stdin.Close()
	_ = cmd.Wait()
	return true
}

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// terminalHeight returns the number of rows of the given terminal file,
// falling back to 24 when the size cannot be determined.
func terminalHeight(f *os.File) int {
	var ws winsize
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno == 0 && ws.Row > 0 {
		return int(ws.Row)
	}
	return 24
}

// ── Interactive prompt helpers ──
//
// All RW commands must confirm before making changes.
// These helpers read from the provided stdin (never os.Stdin directly)
// so commands remain testable.
//
// When --json is set, prompts are skipped (JSON mode is non-interactive).
// When --quiet is set, prompts auto-accept (for scripted use).
// When --dry-run is set, the caller should show the plan and skip prompting.
// EOF on stdin is treated as "n" / abort.

// confirm prints a Y/n prompt and returns true on "y" or Enter.
// It returns false on "n", EOF, or any error.
// In --json mode or --quiet mode, it auto-accepts without prompting.
func confirm(stdin io.Reader, stdout io.Writer, globals globalFlags, msg string) bool {
	if globals.json || globals.quiet {
		return true
	}
	nc := globals.noColor
	fmt.Fprintf(stdout, "%s %s ", msg, style.Mutedf(nc, "[Y/n]"))
	line, err := readLine(stdin)
	if err != nil {
		return false // EOF or error = abort
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

// promptChoice shows a multi-option prompt and returns the chosen single-char key.
// choices is a display string like "[m]ove [i]gnore [a]dd [s]kip".
// validKeys are the accepted single characters, e.g. "mias".
// defaultKey is returned on Enter (empty input) and on EOF/--quiet/--json.
func promptChoice(stdin io.Reader, stdout io.Writer, globals globalFlags, msg, choices, validKeys, defaultKey string) string {
	if globals.json || globals.quiet {
		return defaultKey
	}
	nc := globals.noColor
	fmt.Fprintf(stdout, "%s %s : ", msg, style.Mutedf(nc, choices))
	line, err := readLine(stdin)
	if err != nil {
		return defaultKey // EOF = default
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultKey
	}
	if len(line) == 1 && strings.Contains(validKeys, line) {
		return line
	}
	return defaultKey
}

// readLine reads a single line from the reader.
func readLine(r io.Reader) (string, error) {
	var buf [1]byte
	var line []byte
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if len(line) > 0 {
				return string(line), nil
			}
			return "", err
		}
	}
}
