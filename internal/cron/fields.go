package cron

import (
	"fmt"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Job states, in the order `cron ls` checks them (spec §5 cron ls).
const (
	StateNA           = "crontab n/a"   // the crontab cannot be read
	StateRetired      = "retired"       // the job no longer exists but its block remains
	StateOutdated     = "outdated"      // the live block uses an old name or lacks --if-due
	StateUndeclared   = "undeclared"    // in the live crontab, not in configs/crontab
	StateNotApplied   = "not applied"   // in configs/crontab, not in the live crontab
	StateInstalled    = "installed"     // in both, current form
	StateNotInstalled = "not installed" // in neither
)

// Job is one built-in job, preset or retired job with its state and last run: the
// record `ws cron ls` prints and the TUI cron screen lists.
type Job struct {
	Name        string
	Description string
	Schedule    string // empty for presets and retired jobs
	Preset      bool
	Retired     bool
	Members     []string // jobs a preset installs
	// State is one of the State constants, or for a preset "installed", "not
	// installed" or "n of m installed".
	State     string
	Installed bool // a block is in the live crontab (false when it cannot be read)
	Declared  bool // a block is in configs/crontab
	CrontabOK bool // the crontab could be read
	LastRun   RunRecord
	Interval  time.Duration // between runs; 0 for presets
}

// NextRun is when the job should next run, or zero if unknown or not installed.
func (j Job) NextRun() time.Time {
	if !j.Installed || j.LastRun.Time.IsZero() || j.Interval == 0 {
		return time.Time{}
	}
	return j.LastRun.Time.Add(j.Interval)
}

// Jobs lists every built-in job, then every preset, then every retired job that
// still has a block in either crontab, each group sorted by name. workspace is
// the workspace root, whose configs/crontab is the declared crontab; empty means
// there is none. crontabErr is the preflight failure, if the crontab cannot be
// read (the install state is then unknown).
func Jobs(workspace string) (jobs []Job, crontabErr error) {
	crontabErr = Preflight()
	statePath, _ := StateFilePath()

	var live, declared []Block
	if crontabErr == nil {
		if ct, err := ReadCrontab(); err == nil {
			live, _ = ParseBlocks(ct)
		}
	}
	if workspace != "" {
		if ct, err := ReadDeclared(DeclaredPath(workspace)); err == nil {
			declared, _ = ParseBlocks(ct)
		}
	}
	has := func(blocks []Block, names ...string) (Block, bool) {
		for _, b := range blocks {
			for _, n := range names {
				if b.Name == n {
					return b, true
				}
			}
		}
		return Block{}, false
	}

	for _, name := range JobNames() {
		b := Builtins[name]
		j := Job{
			Name: name, Description: b.Description, Schedule: b.Schedule,
			Interval: time.Duration(b.IntervalSecs) * time.Second, CrontabOK: crontabErr == nil,
		}
		liveBlock, inLive := has(live, Names(name)...)
		_, inDeclared := has(declared, Names(name)...)
		j.Installed, j.Declared = inLive, inDeclared
		j.State = jobState(crontabErr == nil, liveBlock, inLive, inDeclared)
		j.LastRun, _ = LastRun(statePath, name)
		jobs = append(jobs, j)
	}
	for _, name := range presetNames() {
		members := Presets[name]
		j := Job{Name: name, Preset: true, Members: members, CrontabOK: crontabErr == nil,
			Description: "installs: " + strings.Join(members, ", ")}
		installed := 0
		for _, m := range jobs {
			for _, member := range members {
				if m.Name == member && m.Installed {
					installed++
				}
			}
		}
		j.Installed = crontabErr == nil && installed == len(members)
		j.Declared = true
		for _, member := range members {
			if _, ok := has(declared, Names(member)...); !ok {
				j.Declared = false
			}
		}
		switch {
		case crontabErr != nil:
			j.State = StateNA
		case installed == len(members):
			j.State = StateInstalled
		case installed == 0:
			j.State = StateNotInstalled
		default:
			j.State = fmt.Sprintf("%d of %d installed", installed, len(members))
		}
		jobs = append(jobs, j)
	}

	// Retired jobs appear only while a block remains.
	for _, name := range Retired {
		_, inLive := has(live, name)
		_, inDeclared := has(declared, name)
		if !inLive && !inDeclared {
			continue
		}
		state := StateRetired
		if crontabErr != nil {
			state = StateNA
		}
		jobs = append(jobs, Job{
			Name: name, Retired: true, State: state, Installed: inLive, Declared: inDeclared, CrontabOK: crontabErr == nil,
			Description: "no longer a ws job; ws cron rm " + name,
		})
	}
	return jobs, crontabErr
}

func presetNames() []string {
	var out []string
	for _, n := range AllNames() {
		if _, ok := Presets[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// jobState is the first matching state of spec §5 cron ls for a built-in job.
func jobState(crontabOK bool, live Block, inLive, inDeclared bool) string {
	switch {
	case !crontabOK:
		return StateNA
	case inLive && Outdated(live):
		return StateOutdated
	case inLive && !inDeclared:
		return StateUndeclared
	case !inLive && inDeclared:
		return StateNotApplied
	case inLive && inDeclared:
		return StateInstalled
	}
	return StateNotInstalled
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Schema describes cron jobs for the CLI and the TUI.
func Schema(v resource.View) resource.Schema[Job] {
	ago := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		if d := v.Now().Sub(t); d >= 0 {
			return style.Age(d) + " ago"
		}
		return "in " + style.Age(t.Sub(v.Now()))
	}
	return resource.Schema[Job]{
		Name: "cron job",
		ID:   func(j Job) string { return j.Name },
		Fields: []resource.Field[Job]{
			{Key: "name", Title: "Job", Value: func(j Job) any { return j.Name }},
			{Key: "schedule", Title: "Schedule", Value: func(j Job) any { return j.Schedule },
				Text: func(j Job) string {
					if j.Preset {
						return "preset"
					}
					return j.Schedule
				}},
			{Key: "state", Title: "State", Value: func(j Job) any { return j.State }},
			{Key: "installed", Title: "Installed", Value: func(j Job) any { return j.Installed }},
			{Key: "declared", Title: "Declared", Value: func(j Job) any { return j.Declared }},
			{Key: "last_run", Title: "Last run", Value: func(j Job) any { return rfc3339(j.LastRun.Time) },
				Text: func(j Job) string {
					s := ago(j.LastRun.Time)
					if s != "" && j.LastRun.ExitCode != 0 {
						s += fmt.Sprintf(" (exit %d)", j.LastRun.ExitCode)
					}
					return s
				}},
			{Key: "last_exit", Title: "Last exit", Value: func(j Job) any { return j.LastRun.ExitCode },
				Text: func(j Job) string {
					if j.LastRun.Time.IsZero() {
						return ""
					}
					return fmt.Sprint(j.LastRun.ExitCode)
				}},
			{Key: "next_run", Title: "Next run", Value: func(j Job) any { return rfc3339(j.NextRun()) },
				Text: func(j Job) string {
					next := j.NextRun()
					if next.IsZero() {
						return ""
					}
					if next.Before(v.Now()) {
						return "overdue"
					}
					return ago(next)
				}},
			{Key: "description", Title: "Description", Value: func(j Job) any { return j.Description }},
			{Key: "preset", Title: "Preset", Value: func(j Job) any { return j.Preset }},
			{Key: "members", Title: "Members", Value: func(j Job) any {
				if j.Members == nil {
					return []string{}
				}
				return j.Members
			}, Text: func(j Job) string { return strings.Join(j.Members, ", ") }},
		},
		Columns: []string{"name", "schedule", "state", "last_run", "next_run", "description"},
	}
}
