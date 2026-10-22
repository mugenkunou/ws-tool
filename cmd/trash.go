package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/age"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// runTrash dispatches `ws trash <verb>` (spec §5 trash). None of these commands
// needs an initialized workspace: the trash belongs to the machine.
func runTrash(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws trash <ls|restore|rm|empty|prune|enable|disable|status> …")
		return 1
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return trashLs(rest, globals, stdout, stderr)
	case "restore":
		return trashRestore(rest, globals, stdin, stdout, stderr)
	case "rm":
		return trashRm(rest, globals, stdin, stdout, stderr)
	case "empty":
		return trashEmpty(rest, globals, stdin, stdout, stderr)
	case "prune":
		return trashPrune(rest, globals, stdin, stdout, stderr)
	case "enable":
		return trashEnable(rest, globals, stdin, stdout, stderr)
	case "disable":
		return trashDisable(rest, globals, stdin, stdout, stderr)
	case "status":
		return trashStatus(rest, globals, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: trash %s\n", verb)
	fmt.Fprintln(stderr, "usage: ws trash <ls|restore|rm|empty|prune|enable|disable|status> …")
	return 1
}

// quietOut is where informational text goes: nowhere with --json (stdout is the
// envelope) or --quiet.
func quietOut(globals globalFlags, stdout io.Writer) io.Writer {
	if globals.json {
		return io.Discard
	}
	return textOut(globals, stdout)
}

// itemLabel is an item's name as ls shows it: a directory ends in /.
func itemLabel(it trash.Item) string {
	if it.Dir {
		return it.Name + "/"
	}
	return it.Name
}

func trashLs(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-ls", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	lf := registerListFlags(fs, "")
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	items, err := trash.Items()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return renderList(globals, "trash.ls", trash.Schema(resource.CLIView()), items, lf, "Trash is empty.", stdout, stderr)
}

// resolveItems resolves the item arguments of restore and rm. At least one is
// required, and an argument that matches nothing stops the command.
func resolveItems(verb string, args []string, stderr io.Writer) ([]trash.Item, bool) {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: ws trash %s <item…>\n", verb)
		return nil, false
	}
	items, err := trash.Resolve(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	return items, true
}

func trashRestore(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	items, ok := resolveItems("restore", fs.Args(), stderr)
	if !ok {
		return 1
	}
	actions, skipped := trash.RestorePlan(items)

	out := quietOut(globals, stdout)
	for _, s := range skipped {
		fmt.Fprintf(out, "  ! %s: %s; skipped\n", s.Name, s.Reason)
	}

	p := Plan{Command: "trash.restore", Actions: actions}
	planResult := RunPlan(p, stdin, stdout, globals)

	// What was restored: the executed actions, named by their item.
	type restored struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	done := []restored{}
	for _, a := range planResult.Actions {
		if a.Status != "executed" {
			continue
		}
		for _, it := range items {
			if a.ID == "trash-restore-"+it.Name {
				done = append(done, restored{Name: it.Name, Path: it.OriginalPath})
			}
		}
	}
	if skipped == nil {
		skipped = []trash.Skip{}
	}

	// 0 when every target was restored; 3 when some were and others were skipped
	// or failed; 1 when none was.
	code := planResult.ExitCode()
	if bad := len(skipped) + planResult.FailedCount(); bad > 0 && code == 0 {
		code = 1
		if planResult.ExecutedCount() > 0 {
			code = 3
		}
	}
	if globals.json {
		writeJSONDryRun(stdout, stderr, "trash.restore", globals.dryRun, map[string]any{
			"restored": done, "skipped": skipped, "actions": planResult.Actions,
		})
	}
	return code
}

func trashRm(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	items, ok := resolveItems("rm", fs.Args(), stderr)
	if !ok {
		return 1
	}
	p := Plan{Command: "trash.rm"}
	removed := []string{}
	for _, it := range items {
		it := it
		desc := fmt.Sprintf("Permanently delete %s (%s)", itemLabel(it), style.HumanBytes(it.SizeBytes))
		if it.OriginalPath != "" {
			desc += ", deleted from " + it.OriginalPath
		}
		p.Actions = append(p.Actions, Action{
			ID:          "trash-rm-" + it.Name,
			Description: desc,
			Execute: func() error {
				if err := trash.Delete(it); err != nil {
					return err
				}
				removed = append(removed, it.Name)
				return nil
			},
		})
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "trash.rm", globals.dryRun, map[string]any{"removed": removed, "actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

func trashEmpty(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-empty", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "ws trash empty takes no targets; use ws trash rm <item…> to delete some")
		return 1
	}
	sum, err := trash.Summarize()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p := Plan{Command: "trash.empty"}
	if sum.Items > 0 {
		p.Actions = append(p.Actions, Action{
			ID:          "trash-empty",
			Description: fmt.Sprintf("Permanently delete %s (%s) in %s", pluralize(sum.Items, "item"), style.HumanBytes(sum.SizeBytes), sum.Root),
			Execute:     trash.Empty,
		})
	}
	if len(p.Actions) == 0 && !globals.json {
		fmt.Fprintln(textOut(globals, stdout), "Trash is empty.")
		return 0
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "trash.empty", globals.dryRun, map[string]any{
			"items": sum.Items, "files": sum.Files, "size_bytes": sum.SizeBytes, "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

func trashPrune(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	olderThan := fs.String("older-than", "", "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if strings.TrimSpace(*olderThan) == "" {
		fmt.Fprintln(stderr, "usage: ws trash prune --older-than <age>  (the trash has no size cap, so an age is required)")
		return 1
	}
	d, err := age.Parse(*olderThan)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	res, matched, err := trash.SelectOlderThan(d, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	now := time.Now()
	p := Plan{Command: "trash.prune"}
	for _, it := range matched {
		it := it
		p.Actions = append(p.Actions, Action{
			ID: "trash-prune-" + it.Name,
			Description: fmt.Sprintf("Permanently delete %s (%s, deleted %s ago)",
				itemLabel(it), style.HumanBytes(it.SizeBytes), style.Age(now.Sub(it.DeletedAt))),
			Execute: func() error { return trash.Delete(it) },
		})
	}
	out := quietOut(globals, stdout)
	if res.Skipped > 0 {
		fmt.Fprintf(out, "  %d item(s) have no deletion date and are never pruned\n", res.Skipped)
	}
	if len(p.Actions) == 0 && !globals.json {
		fmt.Fprintf(out, "Nothing to prune: no item was deleted more than %s ago.\n", *olderThan)
		return 0
	}
	planResult := RunPlan(p, stdin, stdout, globals)
	if globals.json {
		if res.Matched == nil {
			res.Matched = []trash.Entry{}
		}
		return writeJSONDryRun(stdout, stderr, "trash.prune", globals.dryRun, map[string]any{
			"matched": res.Matched, "kept": res.Kept, "skipped": res.Skipped, "size_bytes": res.SizeBytes, "actions": planResult.Actions,
		})
	}
	return planResult.ExitCode()
}

func trashEnable(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-enable", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noShell := fs.Bool("no-shell-rm", false, "")
	noVSCode := fs.Bool("no-vscode", false, "")
	noExplorer := fs.Bool("no-file-explorer", false, "")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	actions, err := trash.EnableActions(trash.EnableOptions{NoShellRM: *noShell, NoVSCode: *noVSCode, NoFileExplorer: *noExplorer})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if len(actions) == 0 && !globals.json {
		fmt.Fprintln(textOut(globals, stdout), "Soft-delete is already on.")
		return 0
	}
	planResult := RunPlan(Plan{Command: "trash.enable", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		enabled := []string{}
		for _, a := range planResult.Actions {
			if a.Status == "executed" {
				enabled = append(enabled, strings.TrimPrefix(a.ID, "trash-enable-"))
			}
		}
		return writeJSONDryRun(stdout, stderr, "trash.enable", globals.dryRun, map[string]any{"enabled": enabled, "actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

func trashDisable(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-disable", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	actions := trash.DisableActions()
	if len(actions) == 0 && !globals.json {
		fmt.Fprintln(textOut(globals, stdout), "The shell rm integration is not enabled.")
		return 0
	}
	planResult := RunPlan(Plan{Command: "trash.disable", Actions: actions}, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "trash.disable", globals.dryRun, map[string]any{"actions": planResult.Actions})
	}
	return planResult.ExitCode()
}

func trashStatus(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trash-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	st, err := trash.GetStatus()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if globals.json {
		return writeJSON(stdout, stderr, "trash.status", st)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "  %-11s %s\n\n", "Trash root", st.Root)
	for _, in := range st.Integrations {
		icon := style.IconCross(nc)
		if in.Enabled {
			icon = style.IconCheck(nc)
		}
		fmt.Fprintf(out, "  %s %-14s %s\n", icon, in.Name, in.Detail)
	}
	fmt.Fprintf(out, "\n  Items %d · Files %d · Size %s · Warns at %s\n", st.Items, st.Files,
		style.HumanBytes(st.SizeBytes), style.HumanBytes(st.WarnSizeBytes))
	if st.OverLimit {
		fmt.Fprintln(out, style.ResultWarning(nc, "The trash is over the limit; ws trash prune --older-than <age> shrinks it."))
	}
	return 0
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
