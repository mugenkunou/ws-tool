package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/plan"
)

// WSBinary resolves the running ws binary (symlinks followed) for embedding
// in crontab lines.
func WSBinary() string {
	bin, err := os.Executable()
	if err != nil {
		return "ws"
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		return resolved
	}
	return bin
}

// DefaultDisplay is the X11 DISPLAY embedded for jobs that need one:
// $DISPLAY, else ":1".
func DefaultDisplay() string {
	if d := os.Getenv("DISPLAY"); d != "" {
		return d
	}
	return ":1"
}

// PlanOptions say where a plan reads and writes.
type PlanOptions struct {
	Bin       string // the ws binary, absolute
	Workspace string
	Display   string // X11 display for mega-sync
	// Declared is the declared crontab, <workspace>/configs/crontab.
	Declared string
}

// Normalize fills the defaults.
func (o PlanOptions) normalize() PlanOptions {
	if o.Bin == "" {
		o.Bin = WSBinary()
	}
	if o.Declared == "" && o.Workspace != "" {
		o.Declared = DeclaredPath(o.Workspace)
	}
	return o
}

// readBoth reads the live and the declared crontab.
func readBoth(declaredPath string) (live, declared string, err error) {
	if live, err = ReadCrontab(); err != nil {
		return "", "", err
	}
	if declaredPath != "" {
		if declared, err = ReadDeclared(declaredPath); err != nil {
			return "", "", err
		}
	}
	return live, declared, nil
}

// AddActions builds the `cron add` plan: one action per job that needs a change,
// covering the live crontab and configs/crontab together, so the two cannot drift
// apart. A job that is already `installed` (both files, current form) gets no
// action and is returned in already. An outdated, undeclared or not-applied job is
// written fresh, and a block under its old name is removed from both files. Lines
// outside ws blocks are untouched.
func AddActions(jobs []*BuiltinJob, opts PlanOptions) (actions []plan.Action, already []string, err error) {
	opts = opts.normalize()
	live, declared, err := readBoth(opts.Declared)
	if err != nil {
		return nil, nil, err
	}
	liveBlocks, _ := ParseBlocks(live)
	declBlocks, _ := ParseBlocks(declared)
	find := func(blocks []Block, names []string) (Block, bool) {
		for _, b := range blocks {
			if slices.Contains(names, b.Name) {
				return b, true
			}
		}
		return Block{}, false
	}

	for _, j := range jobs {
		j := j
		names := Names(j.Name)
		lb, inLive := find(liveBlocks, names)
		_, inDecl := find(declBlocks, names)
		if inLive && inDecl && !Outdated(lb) {
			already = append(already, j.Name)
			continue
		}
		desc := fmt.Sprintf("Install %s (%s + @reboot)", j.Name, j.Schedule)
		if old := oldNameIn(liveBlocks, declBlocks, j.Name); old != "" {
			desc += ", replacing " + old + ","
		}
		desc += " in the crontab"
		if opts.Declared != "" {
			desc += " and " + opts.Declared
		}
		block := BlockFor(j, opts.Bin, opts.Workspace, opts.Display)
		actions = append(actions, plan.Action{
			ID:          "cron-install-" + j.Name,
			Description: desc,
			Execute: func() error {
				ct, err := ReadCrontab()
				if err != nil {
					return err
				}
				if err := WriteCrontab(upsertBlock(ct, block, names...)); err != nil {
					return err
				}
				if opts.Declared == "" {
					return nil
				}
				dc, err := ReadDeclared(opts.Declared)
				if err != nil {
					return err
				}
				return WriteDeclared(opts.Declared, upsertBlock(dc, block, names...))
			},
		})
	}
	return actions, already, nil
}

// oldNameIn returns the old job name that has a block in either list, if any.
func oldNameIn(a, b []Block, job string) string {
	for _, blocks := range [][]Block{a, b} {
		for _, bl := range blocks {
			if c, old := Canonical(bl.Name); old && c == job {
				return bl.Name
			}
		}
	}
	return ""
}

// RemoveActions builds the `cron rm` plan: one action per named job that has a
// block in either file, removing it, under its current and old names, from the
// live crontab and from configs/crontab, and deleting the job's legacy wrapper
// script. names may be built-in jobs, old job names or retired ones. A name with
// no block anywhere is returned in notInstalled and gets no action. Run history
// and log lines are kept.
func RemoveActions(names []string, opts PlanOptions) (actions []plan.Action, notInstalled []string, err error) {
	opts = opts.normalize()
	live, declared, err := readBoth(opts.Declared)
	if err != nil {
		return nil, nil, err
	}
	liveBlocks, _ := ParseBlocks(live)
	declBlocks, _ := ParseBlocks(declared)
	has := func(blocks []Block, set []string) bool {
		return slices.ContainsFunc(blocks, func(b Block) bool { return slices.Contains(set, b.Name) })
	}

	seen := map[string]bool{}
	for _, name := range names {
		canon, _ := Canonical(name)
		if seen[canon] {
			continue
		}
		seen[canon] = true
		set := Names(canon)
		if !has(liveBlocks, set) && !has(declBlocks, set) {
			notInstalled = append(notInstalled, canon)
			continue
		}
		desc := fmt.Sprintf("Remove %s from the crontab", canon)
		if opts.Declared != "" {
			desc += " and " + opts.Declared
		}
		actions = append(actions, plan.Action{
			ID:          "cron-rm-" + canon,
			Description: desc,
			Execute: func() error {
				ct, err := ReadCrontab()
				if err != nil {
					return err
				}
				if updated := removeBlocks(ct, set...); updated != ct {
					if err := WriteCrontab(updated); err != nil {
						return err
					}
				}
				if opts.Declared != "" {
					dc, err := ReadDeclared(opts.Declared)
					if err != nil {
						return err
					}
					if updated := removeBlocks(dc, set...); updated != dc {
						if err := WriteDeclared(opts.Declared, updated); err != nil {
							return err
						}
					}
				}
				for _, n := range set {
					if script, err := ScriptPath(n); err == nil {
						_ = os.Remove(script) // best-effort; usually absent
					}
				}
				return nil
			},
		})
	}
	return actions, notInstalled, nil
}

// ApplyResult is what `cron apply` reports.
type ApplyResult struct {
	Installed  []string // declared jobs installed or replaced on this machine
	Removed    []string // live ws blocks that are not declared
	Unchanged  []string // blocks that already match
	Skipped    []string // declared blocks of retired or unknown jobs
	LinesAdded int      // declared lines outside ws blocks that the live crontab lacked
}

// ApplyActions builds the `cron apply` plan: it makes the live crontab match
// configs/crontab. It regenerates the block of every declared built-in job (an
// old name counts) for this machine, instead of copying its text, so a workspace
// synced from another machine gets correct paths. It removes live ws blocks that
// are not declared, and appends declared lines outside ws blocks (such as
// CRON_TZ=UTC) that the live crontab lacks, each once. Live lines outside ws
// blocks are kept. Declared blocks of retired or unknown jobs are skipped.
// configs/crontab itself is not changed. A missing configs/crontab is an error.
func ApplyActions(opts PlanOptions) ([]plan.Action, ApplyResult, error) {
	opts = opts.normalize()
	var res ApplyResult
	declaredText, err := os.ReadFile(opts.Declared)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, res, fmt.Errorf("%s not found; ws cron add <job> creates it", opts.Declared)
		}
		return nil, res, err
	}
	live, err := ReadCrontab()
	if err != nil {
		return nil, res, err
	}
	declBlocks, declOther := ParseBlocks(string(declaredText))
	liveBlocks, liveOther := ParseBlocks(live)

	var actions []plan.Action
	declaredJobs := map[string]bool{}
	for _, b := range declBlocks {
		canon, _ := Canonical(b.Name)
		job, ok := Builtins[canon]
		if !ok {
			res.Skipped = append(res.Skipped, b.Name)
			continue
		}
		if declaredJobs[canon] {
			continue // one block per job
		}
		declaredJobs[canon] = true
		names := Names(canon)
		block := BlockFor(job, opts.Bin, opts.Workspace, opts.Display)

		var current *Block
		for i := range liveBlocks {
			if slices.Contains(names, liveBlocks[i].Name) {
				current = &liveBlocks[i]
				break
			}
		}
		if current != nil && current.Name == canon && current.Text == block {
			res.Unchanged = append(res.Unchanged, canon)
			continue
		}
		res.Installed = append(res.Installed, canon)
		verb := "Install"
		if current != nil {
			verb = "Replace"
		}
		actions = append(actions, plan.Action{
			ID:          "cron-apply-" + canon,
			Description: fmt.Sprintf("%s %s (%s + @reboot) in the crontab", verb, canon, job.Schedule),
			Execute: func() error {
				ct, err := ReadCrontab()
				if err != nil {
					return err
				}
				return WriteCrontab(upsertBlock(ct, block, names...))
			},
		})
	}

	// Live ws blocks that are not declared go.
	for _, b := range liveBlocks {
		canon, _ := Canonical(b.Name)
		if declaredJobs[canon] {
			continue
		}
		b := b
		res.Removed = append(res.Removed, b.Name)
		actions = append(actions, plan.Action{
			ID:          "cron-apply-remove-" + b.Name,
			Description: fmt.Sprintf("Remove %s from the crontab (it is not in %s)", b.Name, opts.Declared),
			Execute: func() error {
				ct, err := ReadCrontab()
				if err != nil {
					return err
				}
				return WriteCrontab(removeBlocks(ct, b.Name))
			},
		})
	}

	// Declared lines outside ws blocks that the live crontab lacks, each once.
	var missing []string
	for _, line := range declOther {
		if !slices.Contains(liveOther, line) && !slices.Contains(missing, line) {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		res.LinesAdded = len(missing)
		actions = append(actions, plan.Action{
			ID:          "cron-apply-lines",
			Description: fmt.Sprintf("Add %d line(s) from %s to the crontab: %s", len(missing), opts.Declared, strings.Join(missing, "; ")),
			Execute: func() error {
				ct, err := ReadCrontab()
				if err != nil {
					return err
				}
				_, other := ParseBlocks(ct)
				var add []string
				for _, l := range missing {
					if !slices.Contains(other, l) {
						add = append(add, l)
					}
				}
				if len(add) == 0 {
					return nil
				}
				if ct != "" && !strings.HasSuffix(ct, "\n") {
					ct += "\n"
				}
				return WriteCrontab(ct + strings.Join(add, "\n") + "\n")
			},
		})
	}
	return actions, res, nil
}
