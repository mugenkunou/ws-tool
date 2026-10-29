package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/secret"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// stringSliceFlag implements flag.Value for repeatable string flags.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

const secretUsage = "usage: ws secret <ls|show|allow|mute|unmute|skip|unskip> …"

// runSecret dispatches `ws secret <verb>` (spec §5 secret).
func runSecret(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, secretUsage)
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return secretLs(rest, globals, stdout, stderr)
	case "show":
		return secretShow(rest, globals, stdout, stderr)
	case "allow":
		return secretAllow(rest, globals, stdin, stdout, stderr)
	case "mute":
		return secretMute(rest, globals, stdin, stdout, stderr)
	case "unmute":
		return secretUnmute(rest, globals, stdin, stdout, stderr)
	case "skip":
		return secretSkip(rest, globals, stdin, stdout, stderr)
	case "unskip":
		return secretUnskip(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: secret %s\n", verb)
	fmt.Fprintln(stderr, secretUsage)
	return 1
}

// secretEnv is the workspace a secret command runs in.
type secretEnv struct {
	workspace  string
	configPath string
	cfg        wsconfig.WsConfig
}

// loadSecretEnv resolves the workspace and loads ws.json. ok is false after an
// error has been printed.
func loadSecretEnv(globals globalFlags, stderr io.Writer) (secretEnv, bool) {
	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return secretEnv{}, false
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return secretEnv{}, false
	}
	return secretEnv{workspace: workspacePath, configPath: configPath, cfg: cfg}, true
}

// scanOptions are the scan's inputs: the ignore rules, the skipped directories
// (plus extra ones for one run), the muted anchors and, when withAllow is set,
// the allowlist.
func (e secretEnv) scanOptions(extraSkip []string, withAllow bool) (secret.ScanOptions, error) {
	rules, err := wsconfig.LoadIgnoreRules(e.configPath)
	if err != nil {
		return secret.ScanOptions{}, err
	}
	opts := secret.ScanOptions{
		WorkspacePath: e.workspace,
		Engine:        ignore.BuildEngine(rules),
		SkipDirs:      secret.MergeSkipDirs(e.workspace, e.cfg.Secret.SkipDirs, extraSkip),
		Muted:         wsconfig.MutedMap(e.cfg),
	}
	if withAllow {
		opts.Allowlist = wsconfig.AllowlistMap(e.cfg)
	}
	return opts, nil
}

// findings are the current findings: what `secret ls` lists.
func (e secretEnv) findings(extraSkip []string) ([]secret.Violation, error) {
	opts, err := e.scanOptions(extraSkip, true)
	if err != nil {
		return nil, err
	}
	return secret.Scan(opts)
}

// anchorFindings resolves <file:line> targets to current findings, in order,
// without repeats. Every target is checked before anything is planned: the
// first that is not a finding is reported and ok is false.
func (e secretEnv) anchorFindings(targets []string, stderr io.Writer) (picked []secret.Violation, ok bool) {
	current, err := e.findings(nil)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	seen := map[string]bool{}
	for _, t := range targets {
		rel, line, err := secret.ParseAnchor(e.workspace, t)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return nil, false
		}
		v, found := secret.FindingAt(current, rel, line)
		if !found {
			fmt.Fprintf(stderr, "no finding at %s:%d\n", filepath.Join(e.workspace, filepath.FromSlash(rel)), line)
			return nil, false
		}
		if key := secret.Anchor(v); !seen[key] {
			seen[key] = true
			picked = append(picked, v)
		}
	}
	return picked, true
}

// ── ls ─────────────────────────────────────────────────────────────

func secretLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var skipDirFlags stringSliceFlag
	fs.Var(&skipDirFlags, "skip-dir", "")
	muted := fs.Bool("muted", false, "")
	lf := registerListFlags(fs, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "usage: ws secret ls [--muted] [--skip-dir <dir>]… [--filter <expr>] [--sort <fields>] [--wide]")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	schema := secret.Schema(resource.CLIView(), env.workspace)
	if err := schema.Validate(lf.query()); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	extra, err := skipDirFlagsToRel(env.workspace, skipDirFlags)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	all, err := env.findings(extra)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	// A muted finding stays in sight (unlike an allowed one, which the scan
	// never returns at all) but, by default, out of the main list: --muted
	// shows only those, like `ignore ls --muted`.
	var findings []secret.Violation
	for _, v := range all {
		if v.Muted == *muted {
			findings = append(findings, v)
		}
	}
	if globals.verbose {
		if dirs := secret.MergeSkipDirs(env.workspace, env.cfg.Secret.SkipDirs, extra); len(dirs) > 0 {
			fmt.Fprintf(stderr, "[verbose] secret: skipping directories: %s\n", strings.Join(absPaths(env.workspace, dirs), ", "))
		}
	}
	none := "No secrets found."
	if *muted {
		none = "No muted findings."
	}
	code := renderList(globals, "secret.ls", schema, findings, lf, none, stdout, stderr)
	if code == 0 && !*muted && !globals.quiet {
		var mutedCount int
		for _, v := range all {
			if v.Muted {
				mutedCount++
			}
		}
		if mutedCount > 0 {
			fmt.Fprintf(stderr, "%s: ws secret ls --muted\n", plural(mutedCount, "muted finding"))
		}
	}
	return code
}

// ── show ───────────────────────────────────────────────────────────

// secretContextLines is how many lines `secret show` prints on each side of
// the match.
const secretContextLines = 5

func secretShow(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) != 1 {
		fmt.Fprintln(stderr, "usage: ws secret show <file:line>")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	picked, ok := env.anchorFindings(fs.Args(), stderr)
	if !ok {
		return 1
	}
	v := picked[0]
	abs := filepath.Join(env.workspace, filepath.FromSlash(v.Path))
	ctx, err := secret.GetFileContext(abs, v.Line, secretContextLines)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if ctx == nil {
		ctx = []secret.ContextLine{}
	}

	if globals.json {
		rec := secret.Schema(resource.CLIView(), env.workspace).Record(v)
		rec = append(rec, resource.KV{Key: "context", Value: ctx})
		return writeJSON(stdout, stderr, "secret.show", rec)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s:%d  %s  %s\n", abs, v.Line, v.Severity, v.Message)
	width := 3
	for _, cl := range ctx {
		if n := len(fmt.Sprint(cl.Number)); n > width {
			width = n
		}
	}
	for _, cl := range ctx {
		marker := "   "
		line := cl.Content
		if cl.IsMatch {
			marker = " " + style.Warningf(nc, "→") + " "
			line = style.Warningf(nc, "%s", line)
		}
		fmt.Fprintf(out, "%s%*d  %s\n", marker, width, cl.Number, line)
	}
	return 0
}

// ── allow ──────────────────────────────────────────────────────────

func secretAllow(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-allow", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws secret allow <file:line…>  (at least one target is required)")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	picked, ok := env.anchorFindings(fs.Args(), stderr)
	if !ok {
		return 1
	}
	opts, err := env.scanOptions(nil, true)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	stale, err := secret.StaleAnchors(opts, env.cfg.Secret.Allowlist)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p := Plan{Command: "secret.allow", Actions: secret.AllowActions(env.configPath, env.workspace, picked, stale)}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "secret.allow", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// ── mute / unmute (per-finding; stays in secret ls, quietly) ────────

func secretMute(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-mute", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws secret mute <file:line…>  (at least one target is required)")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	picked, ok := env.anchorFindings(fs.Args(), stderr)
	if !ok {
		return 1
	}
	out := quietOut(globals, stdout)
	var toMute []secret.Violation
	for _, v := range picked {
		if v.Muted {
			fmt.Fprintf(out, "Already muted: %s\n", secret.AbsAnchor(env.workspace, v))
			continue
		}
		toMute = append(toMute, v)
	}
	p := Plan{Command: "secret.mute", Actions: secret.MuteActions(env.configPath, env.workspace, toMute)}
	return runSecretPlan(p, "secret.mute", globals, stdin, stdout, stderr)
}

func secretUnmute(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-unmute", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws secret unmute <file:line…>  (at least one target is required)")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	picked, ok := env.anchorFindings(fs.Args(), stderr)
	if !ok {
		return 1
	}
	out := quietOut(globals, stdout)
	var toUnmute []secret.Violation
	for _, v := range picked {
		if !v.Muted {
			fmt.Fprintf(out, "Not muted: %s\n", secret.AbsAnchor(env.workspace, v))
			continue
		}
		toUnmute = append(toUnmute, v)
	}
	p := Plan{Command: "secret.unmute", Actions: secret.UnmuteActions(env.configPath, env.workspace, toUnmute)}
	return runSecretPlan(p, "secret.unmute", globals, stdin, stdout, stderr)
}

// ── skip / unskip (directory-level; the directory is never scanned) ─

// skipDirs resolves directory arguments to workspace-relative paths. Every
// argument must be inside the workspace and not the workspace itself.
func (e secretEnv) skipDirs(args []string, stderr io.Writer) ([]string, bool) {
	var rels []string
	for _, a := range args {
		rel, err := secret.WorkspaceRelDir(e.workspace, a)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return nil, false
		}
		rels = append(rels, rel)
	}
	return rels, true
}

func (e secretEnv) abs(rel string) string { return filepath.Join(e.workspace, filepath.FromSlash(rel)) }

func secretSkip(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-skip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws secret skip <dir…>")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	rels, ok := env.skipDirs(fs.Args(), stderr)
	if !ok {
		return 1
	}
	for _, rel := range rels {
		info, err := os.Stat(env.abs(rel))
		if err != nil || !info.IsDir() {
			fmt.Fprintf(stderr, "%s is not an existing directory\n", env.abs(rel))
			return 1
		}
	}

	out := quietOut(globals, stdout)
	p := Plan{Command: "secret.skip"}
	var planned []string
	for _, rel := range rels {
		if covered(planned, rel) {
			continue
		}
		if by, exact, skipped := secret.SkippedBy(env.workspace, env.cfg.Secret.SkipDirs, rel); skipped {
			note := "by itself"
			if !exact {
				note = "by " + env.abs(by)
			}
			fmt.Fprintf(out, "%s is already skipped %s\n", env.abs(rel), note)
			continue
		}
		planned = append(planned, rel)
		p.Actions = append(p.Actions, secret.SkipAction(env.configPath, env.workspace, rel))
	}
	return runSecretPlan(p, "secret.skip", globals, stdin, stdout, stderr)
}

// covered reports whether rel is one of dirs or lies under one.
func covered(dirs []string, rel string) bool {
	for _, d := range dirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

func secretUnskip(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("secret-unskip", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "usage: ws secret unskip <dir…>")
		return 1
	}
	env, ok := loadSecretEnv(globals, stderr)
	if !ok {
		return 1
	}
	rels, ok := env.skipDirs(fs.Args(), stderr)
	if !ok {
		return 1
	}

	// A directory skipped by a parent cannot be unskipped on its own: name the
	// parent and change nothing.
	for _, rel := range rels {
		if by, exact, skipped := secret.SkippedBy(env.workspace, env.cfg.Secret.SkipDirs, rel); skipped && !exact {
			fmt.Fprintf(stderr, "%s is skipped by %s; unskip that directory instead\n", env.abs(rel), env.abs(by))
			return 1
		}
	}

	out := quietOut(globals, stdout)
	p := Plan{Command: "secret.unskip"}
	seen := map[string]bool{}
	for _, rel := range rels {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if _, _, skipped := secret.SkippedBy(env.workspace, env.cfg.Secret.SkipDirs, rel); !skipped {
			fmt.Fprintf(out, "%s is not skipped\n", env.abs(rel))
			continue
		}
		p.Actions = append(p.Actions, secret.UnskipAction(env.configPath, env.workspace, rel))
	}
	return runSecretPlan(p, "secret.unskip", globals, stdin, stdout, stderr)
}

// runSecretPlan runs a plan and reports it: the JSON envelope with the actions,
// or, with no action to run, a note and exit 0.
func runSecretPlan(p Plan, command string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, command, globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

// ── helpers ────────────────────────────────────────────────────────

// promptLine reads a full line of input with a default value.
// Returns defaultValue on EOF, --quiet, or --json.
func promptLine(stdin io.Reader, stdout io.Writer, globals globalFlags, prompt, defaultValue string) string {
	if globals.json || globals.quiet {
		return defaultValue
	}
	nc := globals.noColor
	if defaultValue != "" {
		fmt.Fprintf(stdout, "%s %s: ", prompt, style.Mutedf(nc, "[%s]", defaultValue))
	} else {
		fmt.Fprintf(stdout, "%s: ", prompt)
	}
	line, err := readLine(stdin)
	if err != nil || strings.TrimSpace(line) == "" {
		return defaultValue
	}
	return strings.TrimSpace(line)
}

// skipDirFlagsToRel resolves --skip-dir values per spec "Path Rules" and
// converts them to the workspace-relative form used for matching.
func skipDirFlagsToRel(workspacePath string, flags []string) ([]string, error) {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		if strings.TrimSpace(f) == "" {
			continue
		}
		rel, err := secret.WorkspaceRelDir(workspacePath, f)
		if err != nil {
			return nil, fmt.Errorf("--skip-dir: %w", err)
		}
		out = append(out, rel)
	}
	return out, nil
}
