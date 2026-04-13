package cmd

import (
	"fmt"
	"io"

	"github.com/mugenkunou/ws-tool/internal/style"
)

// Action represents a single discrete mutation a command intends to perform.
// Each action is independently confirmable in interactive mode.
type Action struct {
	ID          string       // unique within the plan, e.g. "create-config"
	Description string       // human-readable: "Create ws/config.json"
	Execute     func() error // the actual mutation (called only after consent)
}

// Plan is an ordered list of actions a command intends to perform.
type Plan struct {
	Command string
	Actions []Action
}

// ActionStatus records the outcome of a single action after plan execution.
type ActionStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "executed", "skipped", "failed", "dry-run"
	Error  string `json:"error,omitempty"`
}

// PlanResult is the outcome of running a plan.
type PlanResult struct {
	Command string         `json:"command"`
	DryRun  bool           `json:"dry_run,omitempty"`
	Actions []ActionStatus `json:"actions"`
	Aborted bool           `json:"aborted,omitempty"`
}

// WasExecuted returns true if the action with the given ID was executed
// successfully.
func (r PlanResult) WasExecuted(id string) bool {
	for _, a := range r.Actions {
		if a.ID == id && a.Status == "executed" {
			return true
		}
	}
	return false
}

// HasFailures returns true if any action failed.
func (r PlanResult) HasFailures() bool {
	for _, a := range r.Actions {
		if a.Status == "failed" {
			return true
		}
	}
	return false
}

// ExecutedIDs returns the IDs of actions that were executed successfully.
func (r PlanResult) ExecutedIDs() []string {
	var out []string
	for _, a := range r.Actions {
		if a.Status == "executed" {
			out = append(out, a.ID)
		}
	}
	return out
}

// FailedCount returns the number of failed actions.
func (r PlanResult) FailedCount() int {
	n := 0
	for _, a := range r.Actions {
		if a.Status == "failed" {
			n++
		}
	}
	return n
}

// ExecutedCount returns the number of successfully executed actions.
func (r PlanResult) ExecutedCount() int {
	n := 0
	for _, a := range r.Actions {
		if a.Status == "executed" {
			n++
		}
	}
	return n
}

// ExitCode returns the appropriate exit code for the plan result.
//
//	0 = all succeeded (or all skipped by user choice)
//	1 = all failed or infrastructure error
//	3 = partial success (some executed, some failed)
func (r PlanResult) ExitCode() int {
	executed := 0
	failed := 0
	for _, a := range r.Actions {
		switch a.Status {
		case "executed":
			executed++
		case "failed":
			failed++
		}
	}
	if failed > 0 && executed > 0 {
		return 3 // partial success
	}
	if failed > 0 {
		return 1 // all failed
	}
	return 0
}

// RunPlan executes a plan with per-action confirmation.
//
// Behavior by mode:
//
//	--dry-run:          prints plan, executes nothing
//	--json or --quiet:  auto-accepts all actions
//	interactive:        prompts per action (y/n/a/q)
//
// Prompt keys:
//
//	y (default/Enter) = yes, execute this action
//	n                 = no, skip this action
//	a                 = all, accept all remaining actions
//	q                 = quit, skip all remaining actions
func RunPlan(plan Plan, stdin io.Reader, stdout io.Writer, globals globalFlags) PlanResult {
	nc := globals.noColor
	result := PlanResult{
		Command: plan.Command,
		DryRun:  globals.dryRun,
		Actions: make([]ActionStatus, 0, len(plan.Actions)),
	}

	if len(plan.Actions) == 0 {
		return result
	}

	out := textOut(globals, stdout)

	// --dry-run: show plan, skip execution entirely.
	if globals.dryRun {
		for _, a := range plan.Actions {
			fmt.Fprintf(out, "  %s %s\n", style.Mutedf(nc, "[dry-run]"), a.Description)
			result.Actions = append(result.Actions, ActionStatus{
				ID:     a.ID,
				Status: "dry-run",
			})
		}
		return result
	}

	// Auto-accept in quiet/json mode.
	autoAccept := globals.json || globals.quiet || globals.autoAccept
	acceptAll := false

	for _, a := range plan.Actions {
		// After quit, skip everything remaining.
		if result.Aborted {
			result.Actions = append(result.Actions, ActionStatus{
				ID:     a.ID,
				Status: "skipped",
			})
			continue
		}

		accepted := autoAccept || acceptAll
		if !accepted {
			choice := promptChoice(stdin, stdout, globals, a.Description, "[y/n/a/q]", "ynaq", "y")
			switch choice {
			case "y":
				accepted = true
			case "n":
				result.Actions = append(result.Actions, ActionStatus{
					ID:     a.ID,
					Status: "skipped",
				})
				fmt.Fprintf(out, "  %s %s\n",
					style.Mutedf(nc, "[-]"),
					style.Mutedf(nc, "%s", a.Description))
				continue
			case "a":
				accepted = true
				acceptAll = true
			case "q":
				result.Aborted = true
				result.Actions = append(result.Actions, ActionStatus{
					ID:     a.ID,
					Status: "skipped",
				})
				continue
			}
		}

		if accepted {
			err := a.Execute()
			if err != nil {
				result.Actions = append(result.Actions, ActionStatus{
					ID:     a.ID,
					Status: "failed",
					Error:  err.Error(),
				})
				fmt.Fprintf(out, "  %s %s: %s\n",
					style.IconCross(nc),
					a.Description,
					style.Errorf(nc, "%s", err.Error()))
			} else {
				result.Actions = append(result.Actions, ActionStatus{
					ID:     a.ID,
					Status: "executed",
				})
				fmt.Fprintf(out, "  %s %s\n", style.IconCheck(nc), a.Description)
			}
		}
	}

	return result
}
