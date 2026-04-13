package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/notify"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
)

var notifyHelp = cmdHelp{Usage: "ws notify <start|stop|status|test>"}

func runNotify(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, notifyHelp)
	}

	workspacePath, configPath, _, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if len(args) == 0 {
		return printUsageError(stderr, notifyHelp)
	}

	sub := args[0]

	fs := flag.NewFlagSet("notify-"+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview only")
	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if *dryRun {
		globals.dryRun = true
	}

	var state notify.State
	switch sub {
	case "start":
		return runNotifyStart(workspacePath, configPath, globals, stdin, stdout, stderr)

	case "stop":
		return runNotifyStop(workspacePath, globals, stdin, stdout, stderr)

	case "status":
		return runNotifyStatus(workspacePath, globals, stdout, stderr)

	case "test":
		state, err = notify.Test(workspacePath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if globals.json {
			return writeJSON(stdout, stderr, "notify.test", state)
		}
		fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "Test notification sent."))
		return 0

	case "daemon":
		return runNotifyDaemon(workspacePath, configPath, globals, stderr)

	default:
		fmt.Fprintf(stderr, "unknown notify subcommand: %s\n", sub)
		return 1
	}
}

func runNotifyStart(workspacePath, configPath string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	plan := Plan{Command: "notify.start"}

	var state notify.State
	var unitPath string

	plan.Actions = append(plan.Actions, Action{
		ID:          "notify-start",
		Description: "Start notification daemon",
		Execute: func() error {
			wsBinary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolving ws binary path: %w", err)
			}

			if notify.HasSystemd() {
				// Generate and install systemd unit.
				unit := notify.GenerateUnit(wsBinary, workspacePath, configPath)
				path, err := notify.InstallUnit(unit)
				if err != nil {
					return fmt.Errorf("installing systemd unit: %w", err)
				}
				unitPath = path

				if err := notify.EnableAndStart(); err != nil {
					return fmt.Errorf("enabling systemd unit: %w", err)
				}

				// Record provision for cleanup by ws reset.
				ledgerPath := provision.LedgerPath(workspacePath)
				_ = provision.Record(ledgerPath, provision.Entry{
					Type:    provision.TypeFile,
					Path:    unitPath,
					Command: "notify.start",
					Time:    time.Now().UTC().Format(time.RFC3339),
				})

				state, _ = notify.Start(workspacePath, 0)
			} else {
				// No systemd — write state only.
				// The user must run `ws notify daemon` manually or via another supervisor.
				var e error
				state, e = notify.Start(workspacePath, 0)
				if e != nil {
					return e
				}
			}
			return nil
		},
	})

	planResult := RunPlan(plan, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "notify.start", globals.dryRun, map[string]any{
			"state":     state,
			"unit_path": unitPath,
			"actions":   planResult.Actions,
		})
	}
	if planResult.WasExecuted("notify-start") {
		out := textOut(globals, stdout)
		nc := globals.noColor
		if unitPath != "" {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Created   %s", unitPath))
			fmt.Fprintln(out, style.ResultSuccess(nc, "Enabled   %s", notify.UnitName()))
			fmt.Fprintln(out, style.ResultSuccess(nc, "Started   %s", notify.UnitName()))
		} else {
			fmt.Fprintln(out, style.ResultSuccess(nc, "Notification daemon state set to active."))
			fmt.Fprintln(out, style.Warningf(nc, "systemd not available. Run `ws notify daemon` manually."))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "Test with: ws notify test"))
	}
	return planResult.ExitCode()
}

func runNotifyStop(workspacePath string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
	plan := Plan{Command: "notify.stop"}

	var state notify.State
	plan.Actions = append(plan.Actions, Action{
		ID:          "notify-stop",
		Description: "Stop notification daemon",
		Execute: func() error {
			if notify.HasSystemd() {
				if err := notify.StopAndDisable(); err != nil {
					// Non-fatal — unit might not exist.
					fmt.Fprintf(stderr, "systemd stop: %v\n", err)
				}
			}
			var e error
			state, e = notify.Stop(workspacePath)
			return e
		},
	})

	planResult := RunPlan(plan, stdin, stdout, globals)
	if globals.json {
		return writeJSONDryRun(stdout, stderr, "notify.stop", globals.dryRun, map[string]any{
			"state":   state,
			"actions": planResult.Actions,
		})
	}
	if planResult.WasExecuted("notify-stop") {
		fmt.Fprintln(textOut(globals, stdout), style.ResultSuccess(globals.noColor, "Notification daemon stopped."))
	}
	return planResult.ExitCode()
}

func runNotifyStatus(workspacePath string, globals globalFlags, stdout, stderr io.Writer) int {
	state, err := notify.Status(workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Enrich with systemd status if available.
	var sysStatus notify.SystemdStatus
	if notify.HasSystemd() {
		sysStatus, _ = notify.QueryStatus()
	}

	// Read health file for last-scan info.
	health, _ := notify.ReadHealth(workspacePath)

	if globals.json {
		return writeJSON(stdout, stderr, "notify.status", map[string]any{
			"state":   state,
			"systemd": sysStatus,
			"health":  health,
		})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor

	if sysStatus.Running {
		fmt.Fprintln(out, style.ResultSuccess(nc, "● %s — ws notification daemon", notify.UnitName()))
		fmt.Fprintf(out, "  %-16s %s\n", style.Boldf(nc, "Status:"), style.Badge("active (running)", nc))
	} else if state.Active {
		fmt.Fprintln(out, style.ResultSuccess(nc, "Notification daemon is %s.", style.Badge("active", nc)))
	} else {
		fmt.Fprintln(out, style.ResultInfo(nc, "Notification daemon is stopped."))
	}

	fmt.Fprintf(out, "  %-16s %s\n", style.Boldf(nc, "Mode:"), state.Mode)

	if !state.LastScan.IsZero() {
		fmt.Fprintf(out, "  %-16s %s\n", style.Boldf(nc, "Last scan:"), state.LastScan.Format("2006-01-02 15:04:05"))
	}
	if !state.LastAlertTime.IsZero() {
		fmt.Fprintf(out, "  %-16s %s (%s)\n", style.Boldf(nc, "Last alert:"), state.LastAlertTime.Format("2006-01-02 15:04:05"), state.LastAlert)
	}
	if !health.Timestamp.IsZero() {
		fmt.Fprintf(out, "  %-16s %s (%s ago)\n", style.Boldf(nc, "Health file:"),
			notify.HealthPath(workspacePath),
			style.Mutedf(nc, "%s", time.Since(health.Timestamp).Truncate(time.Second)))
	}
	return 0
}

func runNotifyDaemon(workspacePath, configPath string, globals globalFlags, stderr io.Writer) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Resolve manifest path from globals (same as other commands).
	_, _, manifestPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	err = notify.RunDaemon(notify.DaemonOptions{
		WorkspacePath: workspacePath,
		ConfigPath:    configPath,
		ManifestPath:  manifestPath,
		Cfg:           cfg,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
