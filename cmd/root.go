package cmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/editor"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

type globalFlags struct {
	workspace  string
	quiet      bool
	verbose    bool
	json       bool
	dryRun     bool
	noColor    bool
	autoAccept bool // auto-accept plan actions without suppressing output (for exempted commands)
}

func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	globals, rest, err := parseGlobalFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	rest, code, removed := resolveAlias(rest, &globals, stderr)
	if removed {
		return code
	}

	if code, handled := handleHelp(rest, globals.noColor, stdout, stderr); handled {
		return code
	}

	command := rest[0]
	commandArgs := rest[1:]

	verboseLog(globals, stderr, "command=%s args=%v", command, commandArgs)

	switch command {
	case "version":
		return runVersion(commandArgs, globals, stdout, stderr)
	case "init":
		return runInit(commandArgs, globals, stdin, stdout, stderr)
	case "config":
		return runConfig(commandArgs, globals, stdin, stdout, stderr)
	case "symlink":
		return runSymlink(commandArgs, globals, stdin, stdout, stderr)
	case "skill":
		return runSkill(commandArgs, globals, stdin, stdout, stderr)
	case "ignore":
		return runIgnore(commandArgs, globals, stdin, stdout, stderr)
	case "secret":
		return runSecret(commandArgs, globals, stdin, stdout, stderr)
	case "session":
		return runSession(commandArgs, globals, stdin, stdout, stderr)
	case "scratch":
		return runScratch(commandArgs, globals, stdin, stdout, stderr)
	case "repo":
		return runRepo(commandArgs, globals, stdin, stdout, stderr)
	case "trash":
		return runTrash(commandArgs, globals, stdin, stdout, stderr)
	case "cron":
		return runCron(commandArgs, globals, stdin, stdout, stderr)
	case "tui":
		return runTUI(commandArgs, globals, stdin, stdout, stderr)
	case "pass":
		return runPass(commandArgs, globals, stdin, stdout, stderr)
	case "credential":
		return runCredential(commandArgs, globals, stdin, stdout, stderr)
	case "git-credential-helper":
		return runGitCredentialHelper(commandArgs, globals, stdin, stdout, stderr)
	case "completion":
		return runCompletion(commandArgs, globals, stdin, stdout, stderr)
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

// editorFor is the editor argv for a command: its --editor flag, else ws.json
// "editor", else $VISUAL, $EDITOR, vim (spec §4.10).
func editorFor(flagValue string, cfg wsconfig.WsConfig) []string {
	if strings.TrimSpace(flagValue) != "" {
		return editor.Command(flagValue)
	}
	return editor.Command(cfg.Editor)
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

// jsonSchema is the --json envelope schema version (spec §4.4). 2: every path
// in data is absolute. 3: command names are <noun>.<verb>, and repo ls, ignore
// lint and secret ls return the records the old scan commands returned.
const jsonSchema = 3

// writeJSON writes a standard JSON envelope to stdout and returns the exit
// code: 0, or for a write command whose data.actions hold failures, the plan's
// code (1 when every action failed, 3 when some did). A command that reports
// failed actions must never exit 0 just because the JSON was written.
// Every JSON output from ws goes through this to ensure a consistent schema:
//
//	{"ws_version": "...", "schema": 3, "command": "...", "data": ...}
func writeJSON(stdout, stderr io.Writer, command string, data any) int {
	return writeJSONEnvelope(stdout, stderr, command, false, data)
}

// writeJSONDryRun is like writeJSON but includes "dry_run": true when applicable.
func writeJSONDryRun(stdout, stderr io.Writer, command string, dryRun bool, data any) int {
	return writeJSONEnvelope(stdout, stderr, command, dryRun, data)
}

func writeJSONEnvelope(stdout, stderr io.Writer, command string, dryRun bool, data any) int {
	payload := map[string]any{
		"ws_version": appVersion,
		"schema":     jsonSchema,
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
	return jsonExit(data)
}

// jsonExit is the exit code a write command's JSON data implies: the exit code
// of its plan when data.actions is the list of action statuses.
func jsonExit(data any) int {
	if m, ok := data.(map[string]any); ok {
		if actions, ok := m["actions"].([]ActionStatus); ok {
			return PlanResult{Actions: actions}.ExitCode()
		}
	}
	return 0
}

// resolveGlobalPaths resolves the path-valued global flag (--workspace) in
// place, per spec "Path Rules": absolute as is, ~
// workspace) in place, per spec "Path Rules": absolute as is, ~
// expanded, anything else relative to the current directory. Empty values stay
// empty so callers can still apply their defaults.
func resolveGlobalPaths(globals *globalFlags) error {
	for _, f := range []struct {
		name string
		v    *string
	}{
		{"workspace", &globals.workspace},
	} {
		if *f.v == "" {
			continue
		}
		abs, err := wsconfig.ExpandUserPath(*f.v)
		if err != nil {
			return fmt.Errorf("invalid --%s: %w", f.name, err)
		}
		*f.v = abs
	}
	return nil
}

// resolveWorkspacePaths resolves the workspace and config paths without
// side effects: flag → env → default, all absolute.
// Returns the workspace path and the ws.json path.
func resolveWorkspacePaths(globals globalFlags) (workspacePath, configPath string, err error) {
	if err := resolveGlobalPaths(&globals); err != nil {
		return "", "", err
	}
	p, err := workspace.ResolvePaths(workspace.PathOverrides{
		Workspace: globals.workspace,
	})
	if err != nil {
		return "", "", err
	}
	return p.Workspace, p.Config, nil
}

func requireWorkspaceInitialized(globals globalFlags, stderr ...io.Writer) (string, string, error) {
	resolvedWorkspace, configPath, err := resolveWorkspacePaths(globals)
	if err != nil {
		return "", "", err
	}

	// Verbose: log resolved paths if stderr was provided.
	if len(stderr) > 0 && stderr[0] != nil {
		verboseLog(globals, stderr[0], "workspace=%s config=%s", resolvedWorkspace, configPath)
	}

	if info, statErr := os.Stat(configPath); statErr == nil && info.IsDir() {
		return "", "", fmt.Errorf("Error: %s is a directory, not a ws.json file.", configPath)
	}
	if !workspace.ConfigExists(configPath) {
		return "", "", errors.New("Error: workspace not initialized.\nRun `ws init` to set up this directory as a ws workspace.")
	}

	return resolvedWorkspace, configPath, nil
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
	fmt.Fprintf(stdout, "%s %s : ", msg, style.Mutedf(nc, "%s", choices))
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
