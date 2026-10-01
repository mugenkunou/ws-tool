package cmd

import (
	"fmt"
	"io"

	"github.com/mugenkunou/ws-tool/internal/plan"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// The plan types live in internal/plan so the TUI shares them.
type (
	Action       = plan.Action
	Plan         = plan.Plan
	ActionStatus = plan.ActionStatus
	PlanResult   = plan.PlanResult
)

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
