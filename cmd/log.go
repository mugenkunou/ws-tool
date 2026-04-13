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
	"time"

	"github.com/mugenkunou/ws-tool/internal/config"
	wslog "github.com/mugenkunou/ws-tool/internal/log"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var logHelp = cmdHelp{Usage: "ws log <start|stop|ls|prune|rm>"}

func runLog(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, logHelp)
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

	// Log sessions live under <scratch_root>/.ws-log/ (dot-prefix avoids
	// collision with user scratch directories).
	scratchRoot, err := config.ResolvePath("", cfg.Scratch.RootDir)
	if err != nil {
		fmt.Fprintf(stderr, "Error resolving scratch root: %s\n", err)
		return 1
	}
	logDir := filepath.Join(scratchRoot, ".ws-log")

	if len(args) == 0 {
		return printUsageError(stderr, logHelp)
	}

	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "start":
		fs := flag.NewFlagSet("log-start", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		tag := fs.String("tag", "", "session tag")
		quietStart := fs.Bool("quiet-start", false, "quiet start")
		noPrompt := fs.Bool("no-prompt", false, "disable prompt marker")
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}

		// --quiet-start auto-accepts the plan (spec exemption).
		if *quietStart {
			globals.quiet = true
		}

		var res wslog.StartResult
		plan := Plan{Command: "log.start"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "log-start",
			Description: "Start recording session",
			Execute: func() error {
				var err error
				res, err = wslog.Start(wslog.StartOptions{LogDir: logDir, Tag: *tag, QuietStart: *quietStart, NoPrompt: *noPrompt})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "log.start", map[string]any{"result": res, "actions": planResult.Actions})
		}
		if !planResult.WasExecuted("log-start") {
			return planResult.ExitCode()
		}

		nc := globals.noColor

		// Check if stdout is a real TTY for PTY recording.
		// Non-TTY (tests, piped) falls back to metadata-only mode.
		if !isTerminalWriter(stdout) {
			if !*quietStart {
				out := textOut(globals, stdout)
				style.KV(out, "Tag", res.Tag, nc)
				style.KV(out, "Stdin", style.Mutedf(nc, "%s", res.StdinPath), nc)
				style.KV(out, "Stdout", style.Mutedf(nc, "%s", res.StdoutPath), nc)
			}
			return planResult.ExitCode()
		}

		// Find script(1) from util-linux for PTY recording.
		scriptPath, err := exec.LookPath("script")
		if err != nil {
			fmt.Fprintln(stderr, "Error: script(1) not found. Install util-linux: sudo apt install util-linux")
			wslog.Stop(wslog.StopOptions{LogDir: logDir})
			return 1
		}

		// Print session banner (unless --quiet-start).
		if !*quietStart {
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, style.ResultSuccess(nc, "Recording (PTY mode)"))
			style.KV(stdout, "Session", res.Tag, nc)
			style.KV(stdout, "Stdin", style.Mutedf(nc, "%s", res.StdinPath), nc)
			style.KV(stdout, "Stdout", style.Mutedf(nc, "%s", res.StdoutPath), nc)
			style.KV(stdout, "Exit", style.Mutedf(nc, "type 'exit' or Ctrl-D to end"), nc)
			fmt.Fprintln(stdout)
		}

		// Build script command with shell init for prompt indicator.
		shellCmd, extraEnv := logBuildShellInit(res.SessionDir, nc, *noPrompt)
		// script -I captures raw PTY stdin (includes arrow keys, tab completions,
		// escape sequences — but also captures SSH session keystrokes).
		// script -O captures raw PTY stdout.
		scriptArgs := []string{
			"-I", res.StdinPath,
			"-O", res.StdoutPath,
			"-q", "--flush", "-e",
		}
		if shellCmd != "" {
			scriptArgs = append(scriptArgs, "-c", shellCmd)
		}

		scriptCmd := exec.Command(scriptPath, scriptArgs...)
		scriptCmd.Env = append(os.Environ(), extraEnv...)
		scriptCmd.Stdin = os.Stdin
		scriptCmd.Stdout = os.Stdout
		scriptCmd.Stderr = os.Stderr

		if err := scriptCmd.Start(); err != nil {
			fmt.Fprintf(stderr, "Error starting recording: %s\n", err)
			wslog.Stop(wslog.StopOptions{LogDir: logDir})
			return 1
		}

		// Record PID so ws log stop can signal the process.
		wslog.SetActivePID(logDir, scriptCmd.Process.Pid)

		// Block until the recording session ends (exit, Ctrl-D, or SIGTERM).
		scriptCmd.Wait()

		// Finalize the session (idempotent — skips if ws log stop already finalized).
		stopRes, _ := wslog.Stop(wslog.StopOptions{LogDir: logDir})
		if stopRes.Stopped {
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, style.ResultSuccess(nc, "Session ended"))
			style.KV(stdout, "Tag", res.Tag, nc)
			style.KV(stdout, "Duration", logFormatDuration(stopRes.DurationSec), nc)
			style.KV(stdout, "Stdin", fmt.Sprintf("%s  (%s)", res.StdinPath, style.HumanBytes(logFileSize(res.StdinPath))), nc)
			style.KV(stdout, "Stdout", fmt.Sprintf("%s  (%s)", res.StdoutPath, style.HumanBytes(logFileSize(res.StdoutPath))), nc)
		}

		return 0
	case "stop":
		// ws log stop is exempted from confirmation prompts (spec).
		globals.autoAccept = true

		var stopRes wslog.StopResult
		plan := Plan{Command: "log.stop"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "log-stop",
			Description: "Stop recording session",
			Execute: func() error {
				// Signal the recording process if running.
				if pid := wslog.GetActivePID(logDir); pid > 0 {
					if proc, err := os.FindProcess(pid); err == nil {
						_ = proc.Signal(syscall.SIGTERM)
						// Wait for process to exit (up to 3s).
						for i := 0; i < 30; i++ {
							if err := proc.Signal(syscall.Signal(0)); err != nil {
								break
							}
							time.Sleep(100 * time.Millisecond)
						}
					}
				}
				var err error
				stopRes, err = wslog.Stop(wslog.StopOptions{LogDir: logDir})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSON(stdout, stderr, "log.stop", map[string]any{"result": stopRes, "actions": planResult.Actions})
		}
		if planResult.WasExecuted("log-stop") {
			nc := globals.noColor
			out := textOut(globals, stdout)
			if !stopRes.Stopped {
				fmt.Fprintln(out, style.ResultInfo(nc, "No active recording session."))
			} else {
				fmt.Fprintln(out, style.ResultSuccess(nc, "Session ended"))
				style.KV(out, "Tag", stopRes.Tag, nc)
				style.KV(out, "Duration", logFormatDuration(stopRes.DurationSec), nc)
				stdinPath := filepath.Join(logDir, stopRes.Tag, "stdin.log")
				stdoutPath := filepath.Join(logDir, stopRes.Tag, "stdout.log")
				style.KV(out, "Stdin", fmt.Sprintf("%s  (%s)", stdinPath, style.HumanBytes(logFileSize(stdinPath))), nc)
				style.KV(out, "Stdout", fmt.Sprintf("%s  (%s)", stdoutPath, style.HumanBytes(logFileSize(stdoutPath))), nc)
			}
		}
		return planResult.ExitCode()
	case "ls":
		sessions, err := wslog.List(logDir)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "log.ls", sessions)
		}
		out := textOut(globals, stdout)
		if len(sessions) == 0 {
			fmt.Fprintln(out, "No recorded sessions.")
			return 0
		}
		for _, s := range sessions {
			nc := globals.noColor
			mark := style.Mutedf(nc, " ")
			if s.Active {
				mark = style.Paint(style.FgRed, "●", nc)
			}
			fmt.Fprintf(out, "%s %s  %s  %s\n",
				mark,
				style.Boldf(nc, "%-20s", s.Tag),
				style.Mutedf(nc, "%d cmds", s.Commands),
				style.Mutedf(nc, "%s", style.HumanBytes(s.SizeBytes)))
		}
		return 0
	case "prune":
		fs := flag.NewFlagSet("log-prune", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		olderThan := fs.String("older-than", "", "duration (e.g. 30d, 720h)")
		all := fs.Bool("all", false, "prune all sessions")
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		duration := time.Duration(0)
		if strings.TrimSpace(*olderThan) != "" {
			d, err := parseDurationWithDays(*olderThan)
			if err != nil {
				fmt.Fprintln(stderr, err.Error())
				return 1
			}
			duration = d
		}

		// Discover sessions that would be pruned, then build per-session actions.
		sessions, err := wslog.List(logDir)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		now := time.Now().UTC()
		plan := Plan{Command: "log.prune"}
		for _, s := range sessions {
			if s.Active {
				continue
			}
			remove := *all
			if !remove && duration > 0 {
				remove = now.Sub(s.StartedAt) >= duration
			}
			if !remove {
				continue
			}
			tag := s.Tag
			plan.Actions = append(plan.Actions, Action{
				ID:          "prune-" + tag,
				Description: fmt.Sprintf("Remove session %s (%s)", tag, style.HumanBytes(s.SizeBytes)),
				Execute: func() error {
					_, err := wslog.Remove(wslog.RemoveOptions{LogDir: logDir, Tag: tag, DryRun: false})
					return err
				},
			})
		}

		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "log.prune", globals.dryRun, map[string]any{"actions": planResult.Actions})
		}
		out := textOut(globals, stdout)
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultInfo(globals.noColor, "Dry run: %d session(s) would be removed", len(plan.Actions)))
		} else {
			fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Pruned %d session(s)", planResult.ExecutedCount()))
		}
		return planResult.ExitCode()
	case "rm":
		fs := flag.NewFlagSet("log-rm", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		registerGlobalFlags(fs, &globals)
		if err := fs.Parse(subArgs); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if len(fs.Args()) == 0 {
			fmt.Fprintln(stderr, "usage: ws log rm <tag>")
			return 1
		}
		tag := fs.Args()[0]

		plan := Plan{Command: "log.rm"}
		plan.Actions = append(plan.Actions, Action{
			ID:          "log-rm-" + tag,
			Description: fmt.Sprintf("Remove log session %q", tag),
			Execute: func() error {
				_, err := wslog.Remove(wslog.RemoveOptions{LogDir: logDir, Tag: tag, DryRun: false})
				return err
			},
		})
		planResult := RunPlan(plan, stdin, stdout, globals)
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "log.rm", globals.dryRun, map[string]any{"actions": planResult.Actions})
		}
		return planResult.ExitCode()
	default:
		fmt.Fprintf(stderr, "unknown log subcommand: %s\n", sub)
		return 1
	}
}

func parseDurationWithDays(input string) (time.Duration, error) {
	v := strings.TrimSpace(strings.ToLower(input))
	if strings.HasSuffix(v, "d") {
		n := strings.TrimSpace(strings.TrimSuffix(v, "d"))
		if n == "" {
			return 0, fmt.Errorf("invalid duration: %s", input)
		}
		nn, err := time.ParseDuration(n + "h")
		if err == nil {
			return nn * 24, nil
		}
		// fallback for integer days
		var dayCount int
		_, err = fmt.Sscanf(n, "%d", &dayCount)
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %s", input)
		}
		return time.Duration(dayCount) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %s", input)
	}
	return d, nil
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// isTerminalWriter reports whether w is a terminal-backed *os.File.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// logBuildShellInit creates a shell init file for prompt indicator injection
// and clean command capture via PROMPT_COMMAND. Returns the -c argument for
// script(1) and any extra env vars. Commands are captured via bash history
// into stdin.log (one command per line), not via script -I (raw PTY garbage).
func logBuildShellInit(sessionDir string, noColor, noPrompt bool) (shellCmd string, env []string) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	shellBase := filepath.Base(shell)

	stdinLog := filepath.Join(sessionDir, "stdin.log")

	if noPrompt {
		// Still capture commands even without prompt modification.
		initContent := logShellInitContent(shellBase, stdinLog, "", noColor)
		initPath := filepath.Join(sessionDir, ".ws-init.sh")
		os.WriteFile(initPath, []byte(initContent), 0o644)
		if shellBase == "zsh" {
			return "zsh", []string{"ZDOTDIR=" + sessionDir, "WS_LOG_RECORDING=1"}
		}
		return fmt.Sprintf("bash --rcfile '%s'", initPath), []string{"WS_LOG_RECORDING=1"}
	}

	initContent := logShellInitContent(shellBase, stdinLog, "indicator", noColor)
	initPath := filepath.Join(sessionDir, ".ws-init.sh")

	switch shellBase {
	case "zsh":
		initPath = filepath.Join(sessionDir, ".zshrc")
		os.WriteFile(initPath, []byte(initContent), 0o644)
		return "zsh", []string{"ZDOTDIR=" + sessionDir, "WS_LOG_RECORDING=1"}
	default:
		os.WriteFile(initPath, []byte(initContent), 0o644)
		return fmt.Sprintf("bash --rcfile '%s'", initPath), []string{"WS_LOG_RECORDING=1"}
	}
}

// logShellInitContent generates the shell init script that:
// 1. Sources the user's normal rc file
// 2. Injects prompt indicator via PROMPT_COMMAND (survives rc PROMPT_COMMAND overrides)
// 3. Captures clean commands via history into stdin.log
func logShellInitContent(shellBase, stdinLog, mode string, noColor bool) string {
	var sb strings.Builder

	switch shellBase {
	case "zsh":
		// Source user's zshrc first.
		sb.WriteString(`[ -f "$HOME/.zshrc" ] && ZDOTDIR="$HOME" source "$HOME/.zshrc"` + "\n\n")

		// Command capture: use precmd hook to log last command.
		sb.WriteString(fmt.Sprintf(`_ws_log_file='%s'`+"\n", stdinLog))
		sb.WriteString(`_ws_log_last_hist=0` + "\n")
		sb.WriteString(`_ws_log_cmd() {
  local cur=$(builtin fc -l -1 2>/dev/null)
  local num="${cur%%[[:space:]]*}"
  if [ -n "$num" ] && [ "$num" != "$_ws_log_last_hist" ]; then
    _ws_log_last_hist="$num"
    local cmd="${cur#*[[:space:]]}"
    printf '%s\n' "$cmd" >> "$_ws_log_file"
  fi
}
`)

		if mode == "indicator" {
			if noColor {
				sb.WriteString(`_ws_log_prompt() { [[ "$PS1" == *"(rec)"* ]] || PS1="(rec) $PS1"; }` + "\n")
			} else {
				sb.WriteString(`_ws_log_prompt() { [[ "$PS1" == *"ws:log"* ]] || PS1="%F{red}●%f ws:log $PS1"; }` + "\n")
			}
			sb.WriteString("precmd_functions=(_ws_log_cmd _ws_log_prompt ${precmd_functions[@]})\n")
		} else {
			sb.WriteString("precmd_functions=(_ws_log_cmd ${precmd_functions[@]})\n")
		}
	default: // bash
		// Source user's bashrc first.
		sb.WriteString("[ -f ~/.bashrc ] && . ~/.bashrc\n\n")

		// Command capture: use PROMPT_COMMAND to log last command from history.
		sb.WriteString(fmt.Sprintf("_ws_log_file='%s'\n", stdinLog))
		sb.WriteString("_ws_log_last_hist=0\n")
		sb.WriteString(`_ws_log_cmd() {
  local cur
  cur=$(HISTTIMEFORMAT= history 1 2>/dev/null) || return
  local num="${cur%%[^0-9]*}"
  if [ -n "$num" ] && [ "$num" != "$_ws_log_last_hist" ]; then
    _ws_log_last_hist="$num"
    local cmd="${cur#*[0-9] }"
    printf '%s\n' "$cmd" >> "$_ws_log_file"
  fi
}
`)

		if mode == "indicator" {
			if noColor {
				sb.WriteString(`_ws_log_prompt() { case "$PS1" in *"(rec)"*) ;; *) PS1="(rec) $PS1" ;; esac; }` + "\n")
			} else {
				sb.WriteString(`_ws_log_prompt() { case "$PS1" in *"ws:log"*) ;; *) PS1='\[\033[31m\]●\[\033[0m\] ws:log '"$PS1" ;; esac; }` + "\n")
			}
			sb.WriteString(`_ws_log_orig_pc="${PROMPT_COMMAND:-}"` + "\n")
			sb.WriteString(`PROMPT_COMMAND='_ws_log_cmd; _ws_log_prompt; eval "$_ws_log_orig_pc"'` + "\n")
		} else {
			sb.WriteString(`_ws_log_orig_pc="${PROMPT_COMMAND:-}"` + "\n")
			sb.WriteString(`PROMPT_COMMAND='_ws_log_cmd; eval "$_ws_log_orig_pc"'` + "\n")
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

// logFileSize returns the size of a file in bytes, or 0 on error.
func logFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
