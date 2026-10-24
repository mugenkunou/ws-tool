package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mugenkunou/ws-tool/internal/age"
	"github.com/mugenkunou/ws-tool/internal/resource"
	wssession "github.com/mugenkunou/ws-tool/internal/session"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// runSession dispatches `ws session <verb>` (spec §5 session).
func runSession(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws session <ls|show|record|stop|grep|rm|prune> …")
		return 1
	}
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stateDir, err := wsconfig.StateDir()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// Sessions live under <workspace>/data/sessions/, and sync with the workspace.
	s := sessionEnv{
		logDir:   filepath.Join(workspacePath, "data", "sessions"),
		stateDir: stateDir,
		cfg:      cfg,
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return s.ls(rest, globals, stdout, stderr)
	case "show":
		return s.show(rest, globals, stdout, stderr)
	case "record":
		return s.record(rest, globals, stdin, stdout, stderr)
	case "stop":
		return s.stop(rest, globals, stdout, stderr)
	case "grep":
		return s.grep(rest, globals, stdout, stderr)
	case "rm":
		return s.rm(rest, globals, stdin, stdout, stderr)
	case "prune":
		return s.prune(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: session %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws session <ls|show|record|stop|grep|rm|prune> …")
	return 1
}

type sessionEnv struct {
	logDir, stateDir string
	cfg              wsconfig.WsConfig
}

// ── ls, show, grep ─────────────────────────────────────────────────

func (e sessionEnv) ls(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lf := registerListFlags(fs, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	sessions, err := wssession.List(e.logDir, e.stateDir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return renderList(globals, "session.ls", wssession.Schema(resource.CLIView()), sessions, lf, "No recorded sessions.", stdout, stderr)
}

func (e sessionEnv) show(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	commands := fs.Bool("commands", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws session show <tag> [--commands]")
		return 1
	}
	res, err := wssession.Show(e.logDir, fs.Args()[0], *commands)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "session.show", map[string]any{
			"tag": res.Tag, "path": res.Path, "commands": res.Commands, "text": res.Text,
		})
	}
	fmt.Fprint(textOut(globals, stdout), res.Text)
	return 0
}

func (e sessionEnv) grep(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-grep", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	max := fs.Int("max", 0, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// Several arguments are one text, joined by single spaces.
	text := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(stderr, "usage: ws session grep <text> [--max <n>]")
		return 1
	}
	matches, err := wssession.Grep(e.logDir, e.stateDir, text, *max)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "session.grep", matches)
	}
	out := textOut(globals, stdout)
	if len(matches) == 0 {
		fmt.Fprintln(out, "No matches.")
		return 0
	}
	for _, m := range matches {
		fmt.Fprintf(out, "%s:%d: %s\n", m.Path, m.Line, m.Text)
	}
	return 0
}

// ── record, stop ───────────────────────────────────────────────────

// scriptMinVersion is the util-linux release that has the script(1) options
// ws needs (-O, --flush, -e).
var scriptVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

func (e sessionEnv) record(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noPrompt := fs.Bool("no-prompt", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 1 {
		fmt.Fprintln(stderr, "usage: ws session record [tag] [--no-prompt] [--quiet]")
		return 1
	}
	tag := ""
	if len(fs.Args()) == 1 {
		tag = fs.Args()[0]
		if err := wssession.ValidateTag(tag); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}

	if globals.dryRun {
		name := tag
		if name == "" {
			name = time.Now().Format("2006-01-02-1504")
		}
		fmt.Fprintf(textOut(globals, stdout), "Would record into %s\n", filepath.Join(e.logDir, name))
		return 0
	}

	// A recording hands the terminal to a shell, so both ends must be one.
	inFile, inOK := stdin.(*os.File)
	if !inOK || !isTerminalFile(inFile) || !isTerminalWriter(stdout) {
		fmt.Fprintln(stderr, "ws session record needs an interactive terminal")
		return 1
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		fmt.Fprintln(stderr, "script(1) not found. Install util-linux 2.35 or newer (sudo apt install util-linux)")
		return 1
	}
	if out, verr := exec.Command(scriptPath, "--version").Output(); verr == nil {
		if m := scriptVersion.FindStringSubmatch(string(out)); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			if major < 2 || (major == 2 && minor < 35) {
				fmt.Fprintf(stderr, "script(1) from util-linux 2.35 or newer is needed, this is %s.%s (sudo apt install util-linux)\n", m[1], m[2])
				return 1
			}
		}
	}

	res, err := wssession.Start(wssession.StartOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: tag})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	nc := globals.noColor
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}

	if !globals.quiet {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.ResultSuccess(nc, "Recording %s", res.Tag))
		fmt.Fprintf(out, "  %-11s %s\n", "Transcript", res.StdoutPath)
		fmt.Fprintf(out, "  %-11s %s\n", "Commands", res.StdinPath)
		fmt.Fprintf(out, "  %-11s %s\n", "End", "type exit or press Ctrl-D")
		fmt.Fprintln(out)
	}

	shellCmd, extraEnv := sessionShellInit(res.SessionDir, nc, *noPrompt)
	cmd := exec.Command(scriptPath, scriptArgs(res.StdoutPath, shellCmd)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "Error starting recording: %s\n", err)
		_, _ = wssession.Stop(wssession.StopOptions{LogDir: e.logDir, StateDir: e.stateDir})
		return 1
	}
	_ = wssession.SetActivePID(e.stateDir, cmd.Process.Pid)

	// End the recording when it grows past session.max_session_mb.
	maxMB := e.cfg.Session.MaxSessionMB
	if maxMB <= 0 {
		maxMB = 100
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		limit := int64(maxMB) * 1024 * 1024
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if fileSizeOf(res.StdinPath)+fileSizeOf(res.StdoutPath) >= limit {
					_ = cmd.Process.Signal(syscall.SIGTERM)
					return
				}
			}
		}
	}()
	_ = cmd.Wait()
	close(done)

	// Finalize (idempotent: `session stop` may have done it already).
	stopped, _ := wssession.Stop(wssession.StopOptions{LogDir: e.logDir, StateDir: e.stateDir})
	if globals.json {
		return writeJSON(stdout, stderr, "session.record", map[string]any{
			"tag": res.Tag, "session_dir": res.SessionDir, "stdin_path": res.StdinPath,
			"stdout_path": res.StdoutPath, "duration_sec": stopped.DurationSec,
		})
	}
	if !globals.quiet {
		printSessionEnded(out, nc, res.Tag, stopped.DurationSec, res.StdoutPath, res.StdinPath)
	}
	return 0
}

// scriptArgs are script(1)'s arguments. It records everything the terminal
// shows into stdoutPath. Keystrokes are not recorded: script -I would also
// capture passwords typed at prompts that do not echo (sudo, ssh), into a file
// that syncs. Commands come from the shell's history hook instead.
func scriptArgs(stdoutPath, shellCmd string) []string {
	return []string{"-q", "--flush", "-e", "-O", stdoutPath, "-c", shellCmd}
}

// printSessionEnded is the summary after a recording ends.
func printSessionEnded(out io.Writer, nc bool, tag string, durationSec int64, stdoutPath, stdinPath string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.ResultSuccess(nc, "Session ended"))
	fmt.Fprintf(out, "  %-11s %s\n", "Tag", tag)
	fmt.Fprintf(out, "  %-11s %s\n", "Duration", logFormatDuration(durationSec))
	fmt.Fprintf(out, "  %-11s %s  (%s)\n", "Transcript", stdoutPath, style.HumanBytes(fileSizeOf(stdoutPath)))
	fmt.Fprintf(out, "  %-11s %s  (%s)\n", "Commands", stdinPath, plural(countFileLines(stdinPath), "command"))
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func (e sessionEnv) stop(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-stop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	out := textOut(globals, stdout)
	nc := globals.noColor

	// stop runs without a plan and without a prompt (spec §4.7).
	if globals.dryRun {
		if m, ok := wssession.ActiveMarker(e.stateDir); ok {
			fmt.Fprintf(out, "Would stop the recording %s\n", m.Tag)
		} else {
			fmt.Fprintln(out, "No active recording session.")
		}
		return 0
	}
	res, err := wssession.StopActive(wssession.StopOptions{LogDir: e.logDir, StateDir: e.stateDir})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "session.stop", map[string]any{
			"tag": res.Tag, "duration_sec": res.DurationSec, "stopped": res.Stopped,
		})
	}
	if !res.Stopped {
		fmt.Fprintln(out, "No active recording session.")
		return 0
	}
	printSessionEnded(out, nc, res.Tag, res.DurationSec,
		filepath.Join(res.Dir, wssession.TranscriptFile), filepath.Join(res.Dir, wssession.CommandsFile))
	return 0
}

// ── rm, prune ──────────────────────────────────────────────────────

func (e sessionEnv) rm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws session rm <tag…>")
		return 1
	}
	sessions, err := wssession.List(e.logDir, e.stateDir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	byTag := map[string]wssession.Session{}
	for _, s := range sessions {
		byTag[s.Tag] = s
	}

	// Tags must match exactly. If one does not exist or is the recording in
	// progress, nothing is deleted.
	var chosen []wssession.Session
	seen := map[string]bool{}
	for _, tag := range fs.Args() {
		if err := wssession.ValidateTag(tag); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		s, ok := byTag[tag]
		switch {
		case !ok:
			fmt.Fprintf(stderr, "session not found: %s\n", tag)
			return 1
		case s.Active:
			fmt.Fprintf(stderr, "cannot remove the active session: %s (stop it first with `ws session stop`)\n", tag)
			return 1
		}
		if !seen[tag] {
			seen[tag] = true
			chosen = append(chosen, s)
		}
	}

	p := Plan{Command: "session.rm"}
	for _, s := range chosen {
		p.Actions = append(p.Actions, e.deleteAction(s, ""))
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "session.rm", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// deleteAction soft-deletes one session. note, when set, follows the size in
// the plan line (prune adds the age).
func (e sessionEnv) deleteAction(s wssession.Session, note string) Action {
	detail := style.HumanBytes(s.SizeBytes)
	if note != "" {
		detail += ", " + note
	}
	return Action{
		ID:          "session-rm-" + s.Tag,
		Description: fmt.Sprintf("Delete %s (%s)", filepath.Join(e.logDir, s.Tag), detail),
		Execute: func() error {
			return wssession.Remove(wssession.RemoveOptions{LogDir: e.logDir, StateDir: e.stateDir, Tag: s.Tag})
		},
	}
}

func (e sessionEnv) prune(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session-prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	olderThan := fs.String("older-than", "", "")
	all := fs.Bool("all", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "usage: ws session prune [--older-than <age> | --all]")
		return 1
	}
	if *all && *olderThan != "" {
		fmt.Fprintln(stderr, "--all and --older-than cannot be used together")
		return 1
	}
	sel := wssession.PruneSelection{CapMB: e.cfg.Session.CapMB, All: *all, Now: time.Now().UTC()}
	if *olderThan != "" {
		d, err := age.Parse(*olderThan)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		sel.OlderThan = d
	}

	sessions, err := wssession.List(e.logDir, e.stateDir)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	total := wssession.TotalBytes(sessions)
	chosen := wssession.SelectPrune(sessions, sel)

	p := Plan{Command: "session.prune"}
	for _, s := range chosen {
		days := int(sel.Now.Sub(s.StartedAt).Hours() / 24)
		p.Actions = append(p.Actions, e.deleteAction(s, fmt.Sprintf("%dd old", days)))
	}
	if len(p.Actions) == 0 && !globals.json {
		out := textOut(globals, stdout)
		switch {
		case *all:
			fmt.Fprintln(out, "Nothing to prune: no finished sessions.")
		case *olderThan != "":
			fmt.Fprintf(out, "Nothing to prune: no session older than %s.\n", *olderThan)
		case e.cfg.Session.CapMB <= 0:
			fmt.Fprintln(out, "Nothing to prune: no size cap is set (session.cap_mb).")
		default:
			fmt.Fprintf(out, "Nothing to prune: %s of the %d MB cap.\n", style.HumanBytes(total), e.cfg.Session.CapMB)
		}
		return 0
	}

	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "session.prune", globals.dryRun, map[string]any{
			"actions": planResult.Actions, "storage_bytes": total, "cap_mb": e.cfg.Session.CapMB,
		})
	}
	return planResult.ExitCode()
}

// ── helpers ────────────────────────────────────────────────────────

// isTerminalWriter reports whether w is a terminal-backed *os.File.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTerminalFile(f)
}

func isTerminalFile(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// sessionShellInit writes the init files of the recorded shell into the session
// directory and returns the -c argument for script(1) and extra environment.
// The shell first loads the user's own rc file, then two hooks that run before
// each prompt: command capture (the last history entry goes to stdin.log) and
// the prompt indicator. Nothing is written to the user's rc files.
func sessionShellInit(sessionDir string, noColor, noPrompt bool) (shellCmd string, env []string) {
	shell := os.Getenv("SHELL")
	shellBase := filepath.Base(shell)
	if shellBase != "zsh" {
		shellBase, shell = "bash", "bash"
	}
	stdinLog := filepath.Join(sessionDir, wssession.CommandsFile)
	content := sessionShellInitContent(shellBase, stdinLog, !noPrompt, noColor)
	env = []string{"WS_LOG_RECORDING=1"}

	if shellBase == "zsh" {
		// zsh reads $ZDOTDIR/.zshrc, so the generated file is that, in both
		// modes. ZDOTDIR also hides the user's ~/.zshenv, so source it too.
		_ = os.WriteFile(filepath.Join(sessionDir, ".zshrc"), []byte(content), 0o644)
		_ = os.WriteFile(filepath.Join(sessionDir, ".zshenv"), []byte(`[ -f "$HOME/.zshenv" ] && source "$HOME/.zshenv"`+"\n"), 0o644)
		return shell, append(env, "ZDOTDIR="+sessionDir)
	}
	initPath := filepath.Join(sessionDir, ".ws-init.sh")
	_ = os.WriteFile(initPath, []byte(content), 0o644)
	return fmt.Sprintf("bash --rcfile '%s'", initPath), env
}

// sessionShellInitContent is the init script of the recorded shell.
func sessionShellInitContent(shellBase, stdinLog string, indicator, noColor bool) string {
	var sb strings.Builder

	switch shellBase {
	case "zsh":
		sb.WriteString(`[ -f "$HOME/.zshrc" ] && ZDOTDIR="$HOME" source "$HOME/.zshrc"` + "\n\n")
		sb.WriteString(fmt.Sprintf("_ws_rec_file='%s'\n", stdinLog))
		sb.WriteString("_ws_rec_last_hist=0\n")
		sb.WriteString(`_ws_rec_cmd() {
  local cur=$(builtin fc -l -1 2>/dev/null)
  local num="${cur%%[[:space:]]*}"
  if [ -n "$num" ] && [ "$num" != "$_ws_rec_last_hist" ]; then
    _ws_rec_last_hist="$num"
    local cmd="${cur#*[[:space:]]}"
    printf '%s\n' "$cmd" >> "$_ws_rec_file"
  fi
}
`)
		if indicator {
			// The indicator ends with the shell. The hook re-adds it when the
			// user's prompt code rewrites PROMPT.
			if noColor {
				sb.WriteString(`_ws_rec_prompt() { [[ "$PROMPT" == *"ws:rec"* ]] || PROMPT="(*) ws:rec $PROMPT"; }` + "\n")
			} else {
				sb.WriteString(`_ws_rec_prompt() { [[ "$PROMPT" == *"ws:rec"* ]] || PROMPT="%F{red}●%f ws:rec $PROMPT"; }` + "\n")
			}
			sb.WriteString("precmd_functions=(_ws_rec_cmd _ws_rec_prompt ${precmd_functions[@]})\n")
		} else {
			sb.WriteString("precmd_functions=(_ws_rec_cmd ${precmd_functions[@]})\n")
		}
	default: // bash
		sb.WriteString("[ -f ~/.bashrc ] && . ~/.bashrc\n\n")
		sb.WriteString(fmt.Sprintf("_ws_rec_file='%s'\n", stdinLog))
		sb.WriteString("_ws_rec_last_hist=0\n")
		sb.WriteString(`_ws_rec_cmd() {
  local cur
  cur=$(HISTTIMEFORMAT= history 1 2>/dev/null) || return
  local num="${cur%%[^0-9]*}"
  if [ -n "$num" ] && [ "$num" != "$_ws_rec_last_hist" ]; then
    _ws_rec_last_hist="$num"
    local cmd="${cur#*[0-9] }"
    printf '%s\n' "$cmd" >> "$_ws_rec_file"
  fi
}
`)
		sb.WriteString(`_ws_rec_orig_pc="${PROMPT_COMMAND:-}"` + "\n")
		if indicator {
			if noColor {
				sb.WriteString(`_ws_rec_prompt() { case "$PS1" in *"ws:rec"*) ;; *) PS1="(*) ws:rec $PS1" ;; esac; }` + "\n")
			} else {
				sb.WriteString(`_ws_rec_prompt() { case "$PS1" in *"ws:rec"*) ;; *) PS1='\[\033[31m\]●\[\033[0m\] ws:rec '"$PS1" ;; esac; }` + "\n")
			}
			sb.WriteString(`PROMPT_COMMAND='_ws_rec_cmd; _ws_rec_prompt; eval "$_ws_rec_orig_pc"'` + "\n")
		} else {
			sb.WriteString(`PROMPT_COMMAND='_ws_rec_cmd; eval "$_ws_rec_orig_pc"'` + "\n")
		}
	}
	return sb.String()
}

// logFormatDuration returns a human-readable duration string from seconds.
func logFormatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	m := seconds / 60
	s := seconds % 60
	if m < 60 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh %dm %ds", h, m, s)
}

// fileSizeOf returns the size of a file in bytes, or 0 on error.
func fileSizeOf(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// countFileLines counts the lines of a file, 0 on error.
func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	n := strings.Count(string(data), "\n")
	if !strings.HasSuffix(string(data), "\n") {
		n++
	}
	return n
}
