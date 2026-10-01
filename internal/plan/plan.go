// Package plan is the Action Plan pattern shared by every front end: a write
// command builds a Plan of independently confirmable Actions, the front end
// (CLI prompts, TUI checklist) decides which run, and a PlanResult records
// the outcome.
package plan

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

// Action outcome values for ActionStatus.Status.
const (
	StatusExecuted = "executed"
	StatusSkipped  = "skipped"
	StatusFailed   = "failed"
	StatusDryRun   = "dry-run"
)

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
