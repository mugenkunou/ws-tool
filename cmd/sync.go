package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/mirror"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

const syncUsage = "usage: ws sync <push|status|restore> …"

// runSync is the `sync` noun: a one-way mirror of the workspace into a folder
// a sync client watches (spec Factor XII, §5 ws sync).
func runSync(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, syncUsage)
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "push":
		return syncPush(rest, globals, stdin, stdout, stderr)
	case "status":
		return syncStatus(rest, globals, stdout, stderr)
	case "restore":
		return syncRestore(rest, globals, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: sync %s\n", verb)
	fmt.Fprintln(stderr, syncUsage)
	return 1
}

func parseSyncFlags(name string, args []string, globals *globalFlags, stderr io.Writer) ([]string, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	return fs.Args(), true
}

// syncOptions loads ws.json and turns its sync block into mirror options.
func syncOptions(globals globalFlags, stderr io.Writer) (mirror.Options, error) {
	ws, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		return mirror.Options{}, err
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		return mirror.Options{}, err
	}
	return optionsFrom(ws, cfg)
}

func optionsFrom(ws string, cfg wsconfig.WsConfig) (mirror.Options, error) {
	state, err := wsconfig.StateDir()
	if err != nil {
		return mirror.Options{}, err
	}
	o := mirror.Options{
		Workspace:    ws,
		Rules:        cfg.IgnoreRules,
		Exclude:      cfg.Sync.Exclude,
		MaxDeletePct: cfg.Sync.MaxDeletePct,
		StateDir:     state,
	}
	if t := strings.TrimSpace(cfg.Sync.Mirror.Target); t != "" {
		o.Target = t // relative: Check rejects it
		if strings.HasPrefix(t, "~") || strings.HasPrefix(t, "/") {
			if o.Target, err = wsconfig.ExpandUserPath(t); err != nil {
				return mirror.Options{}, err
			}
		}
	}
	return o, nil
}

// askOrRefuse asks a safety question. Under --quiet or --json (cron) nobody can
// answer, so it refuses: ws sync never deletes unattended past a guard.
func askOrRefuse(globals globalFlags, stdin io.Reader, stdout io.Writer, msg string) bool {
	if globals.quiet || globals.json || globals.autoAccept {
		return false
	}
	return promptChoice(stdin, stdout, globals, msg, "[y/N]", "yn", "n") == "y"
}

func syncPush(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if _, ok := parseSyncFlags("sync-push", args, &globals, stderr); !ok {
		return 1
	}
	nc := globals.noColor
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	o, err := syncOptions(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := mirror.Check(o); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	unlock, busy, err := mirror.Lock(o)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if busy {
		fmt.Fprintln(out, "Another ws sync push is running; skipped.")
		if globals.json {
			return writeJSON(stdout, stderr, "sync.push", map[string]any{"skipped": "busy"})
		}
		return 0
	}
	defer unlock()

	p, err := mirror.Prepare(o)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer p.Close()

	// Symlink and wiped-mirror warnings go to stderr so cron logs keep them.
	if n := len(p.Filter.Symlinks); n > 0 {
		fmt.Fprintln(stderr, style.ResultWarning(nc, "%s skipped: symlinks never sync (ws ignore lint lists them)", pluralize(n, "symlink")))
	}
	switch p.State {
	case mirror.StampWiped:
		fmt.Fprintln(stderr, style.ResultWarning(nc, "The mirror was emptied since the last push (%s). Re-uploading %s; older copies are in your sync client's trash.",
			p.Last.Time.Local().Format("2006-01-02 15:04"), pluralize(p.Filter.Files, "file")))
	case mirror.StampForeign:
		msg := fmt.Sprintf("%s was last written by another push, not this machine's. Sync is one-way: push from one machine only. Push anyway?", o.Target)
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultWarning(nc, "%s", strings.TrimSuffix(msg, " Push anyway?")))
		} else if !askOrRefuse(globals, stdin, stdout, msg) {
			fmt.Fprintln(stderr, "Refused: the mirror belongs to another push. Run ws sync push by hand to confirm.")
			return 1
		}
	}
	if p.GuardTripped() {
		msg := fmt.Sprintf("This push deletes %d of %d files in the mirror (over sync.max_delete_pct). Continue?", len(p.Deleted), p.MirrorFiles)
		if globals.dryRun {
			fmt.Fprintln(out, style.ResultWarning(nc, "%s", strings.TrimSuffix(msg, " Continue?")))
		} else if !askOrRefuse(globals, stdin, stdout, msg) {
			fmt.Fprintln(stderr, "Refused: too many deletions. Check the workspace, then run ws sync push by hand to confirm.")
			return 1
		}
	}

	if globals.dryRun {
		for _, f := range p.Changed {
			fmt.Fprintf(out, "  %s %s\n", style.Mutedf(nc, "copy  "), f)
		}
		for _, f := range p.Deleted {
			fmt.Fprintf(out, "  %s %s\n", style.Mutedf(nc, "delete"), f)
		}
		fmt.Fprintf(out, "Would push to %s: %d to copy, %d to delete (%s, %s in all).\n",
			o.Target, len(p.Changed), len(p.Deleted), pluralize(p.Filter.Files, "file"), style.HumanBytes(p.Filter.Bytes))
		if globals.json {
			return writeJSONDryRun(stdout, stderr, "sync.push", true, map[string]any{
				"target": o.Target, "changed": p.Changed, "deleted": p.Deleted,
				"files": p.Filter.Files, "bytes": p.Filter.Bytes, "stamp": p.State,
				"skipped_symlinks": nonNil(p.Filter.Symlinks),
			})
		}
		return 0
	}

	res, err := p.Run()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	res.Symlinks = nonNil(res.Symlinks)
	if globals.json {
		return writeJSON(stdout, stderr, "sync.push", res)
	}
	fmt.Fprintln(out, style.ResultSuccess(nc, "Pushed to %s: %d copied, %d deleted (%s, %s).",
		o.Target, res.Changed, res.Deleted, pluralize(res.Files, "file"), style.HumanBytes(res.Bytes)))
	return 0
}

type syncStatusReport struct {
	Target         string            `json:"target"`
	Stamp          mirror.StampState `json:"stamp"`
	LastPush       *mirror.LastPush  `json:"last_push"`
	PendingChanges int               `json:"pending_changes"`
	PendingDeletes int               `json:"pending_deletes"`
	Files          int               `json:"files"`
	Bytes          int64             `json:"bytes"`
	Symlinks       []string          `json:"skipped_symlinks"`
}

func syncStatus(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if _, ok := parseSyncFlags("sync-status", args, &globals, stderr); !ok {
		return 1
	}
	o, err := syncOptions(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p, err := mirror.Prepare(o)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer p.Close()
	r := syncStatusReport{Target: o.Target, Stamp: p.State, LastPush: p.Last,
		PendingChanges: len(p.Changed), PendingDeletes: len(p.Deleted),
		Files: p.Filter.Files, Bytes: p.Filter.Bytes, Symlinks: nonNil(p.Filter.Symlinks)}
	if globals.json {
		return writeJSON(stdout, stderr, "sync.status", r)
	}
	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "  %-12s %s\n", "Mirror", r.Target)
	if r.LastPush != nil {
		fmt.Fprintf(out, "  %-12s %s · %s · %s · %d copied · %d deleted\n", "Last push",
			r.LastPush.Time.Local().Format("2006-01-02 15:04"), pluralize(r.LastPush.Files, "file"),
			style.HumanBytes(r.LastPush.Bytes), r.LastPush.Changed, r.LastPush.Deleted)
	} else {
		fmt.Fprintf(out, "  %-12s %s\n", "Last push", "never (from this machine)")
	}
	fmt.Fprintf(out, "  %-12s %s\n", "Stamp", stampLabel(r.Stamp))
	fmt.Fprintf(out, "  %-12s %d to copy · %d to delete (%s, %s in all)\n", "Pending",
		r.PendingChanges, r.PendingDeletes, pluralize(r.Files, "file"), style.HumanBytes(r.Bytes))
	if n := len(r.Symlinks); n > 0 {
		fmt.Fprintln(out, style.ResultWarning(nc, "%s skipped: symlinks never sync (ws ignore lint lists them)", pluralize(n, "symlink")))
	}
	return 0
}

func stampLabel(s mirror.StampState) string {
	switch s {
	case mirror.StampOK:
		return "ok (written by this machine's last push)"
	case mirror.StampWiped:
		return "missing: the mirror was emptied since the last push; the next push re-uploads"
	case mirror.StampForeign:
		return "foreign: another push wrote the mirror; the next push asks first"
	}
	return "none yet (first push)"
}

func syncRestore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	pos, ok := parseSyncFlags("sync-restore", args, &globals, stderr)
	if !ok {
		return 1
	}
	if len(pos) > 1 {
		fmt.Fprintln(stderr, "usage: ws sync restore [<mirror-dir>]")
		return 1
	}
	nc := globals.noColor
	out := textOut(globals, stdout)
	if globals.json {
		out = io.Discard
	}
	ws, configPath, err := resolveWorkspacePaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	o := mirror.Options{Workspace: ws}
	hasConfig := workspace.ConfigExists(configPath)
	if hasConfig {
		cfg, err := wsconfig.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if o, err = optionsFrom(ws, cfg); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	} else if o.StateDir, err = wsconfig.StateDir(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	src := o.Target
	if len(pos) == 1 {
		if src, err = wsconfig.ExpandUserPath(pos[0]); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}
	if src == "" {
		fmt.Fprintln(stderr, errors.New("no mirror folder: pass it (ws sync restore <mirror-dir>) or set sync.mirror.target in ws.json"))
		return 1
	}

	if hasConfig && !globals.dryRun {
		msg := fmt.Sprintf("%s already has a ws.json. Copy %s over it (nothing is deleted, newer mirror files win)?", ws, src)
		if !askOrRefuse(globals, stdin, stdout, msg) {
			fmt.Fprintln(stderr, "Refused: the workspace is not empty. Run ws sync restore by hand to confirm.")
			return 1
		}
	}
	res, err := mirror.Restore(o, src, globals.dryRun)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "sync.restore", globals.dryRun, res)
	}
	if globals.dryRun {
		fmt.Fprintf(out, "Would restore %s from %s into %s.\n", pluralize(res.Copied, "file"), src, ws)
		return 0
	}
	fmt.Fprintln(out, style.ResultSuccess(nc, "Restored %s from %s into %s.", pluralize(res.Copied, "file"), src, ws))
	fmt.Fprintln(out, style.Mutedf(nc, "  Executable bits are not restored: chmod +x your scripts by hand."))
	fmt.Fprintln(out, style.Mutedf(nc, "  Next: ws symlink link"))
	return 0
}
