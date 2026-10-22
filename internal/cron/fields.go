package cron

import (
	"fmt"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// Job is one built-in cron job or preset with its install state and last
// run: the record `ws cron ls` prints and the TUI cron screen lists.
type Job struct {
	Name        string
	Description string
	Schedule    string // empty for presets
	Preset      bool
	Members     []string // jobs a preset installs
	Installed   bool     // false when the crontab is unreadable
	CrontabOK   bool     // the crontab could be read
	LastRun     RunRecord
	Interval    time.Duration // between runs; 0 for presets
}

// NextRun is when the job should next run, or zero if unknown.
func (j Job) NextRun() time.Time {
	if j.LastRun.Time.IsZero() || j.Interval == 0 {
		return time.Time{}
	}
	return j.LastRun.Time.Add(j.Interval)
}

// Jobs lists every built-in job and preset, in AllNames order. crontabErr
// is the preflight failure, if the crontab cannot be read (install state is
// then unknown).
func Jobs() (jobs []Job, crontabErr error) {
	crontabErr = Preflight()
	statePath, _ := StateFilePath()
	for _, name := range AllNames() {
		j := Job{Name: name, CrontabOK: crontabErr == nil}
		if b, ok := Builtins[name]; ok {
			j.Description, j.Schedule = b.Description, b.Schedule
			j.Interval = time.Duration(b.IntervalSecs) * time.Second
		} else if members, ok := Presets[name]; ok {
			j.Preset, j.Members = true, members
			j.Description = "installs: " + strings.Join(members, ", ")
		}
		if j.CrontabOK {
			if j.Preset {
				j.Installed = presetInstalled(name)
			} else {
				j.Installed, _ = HasJob(name)
			}
		}
		if !j.Preset {
			j.LastRun, _ = LastRun(statePath, name)
		}
		jobs = append(jobs, j)
	}
	return jobs, crontabErr
}

func presetInstalled(name string) bool {
	jobs, err := Resolve(name)
	if err != nil || len(jobs) == 0 {
		return false
	}
	for _, j := range jobs {
		if ok, _ := HasJob(j.Name); !ok {
			return false
		}
	}
	return true
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
			{Key: "installed", Title: "State", Value: func(j Job) any { return j.Installed },
				Text: func(j Job) string {
					switch {
					case !j.CrontabOK:
						return "crontab n/a"
					case j.Installed:
						return "installed"
					}
					return "not installed"
				}},
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
				Text: func(j Job) string { return ago(j.NextRun()) }},
			{Key: "description", Title: "Description", Value: func(j Job) any { return j.Description }},
			{Key: "preset", Title: "Preset", Value: func(j Job) any { return j.Preset }},
		},
		Columns: []string{"name", "schedule", "installed", "last_run", "description"},
	}
}
