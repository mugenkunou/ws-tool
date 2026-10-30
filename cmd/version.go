package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/buildinfo"
	"github.com/mugenkunou/ws-tool/internal/release"
	"github.com/mugenkunou/ws-tool/internal/style"
)

// appVersion is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0"
var appVersion = "dev"

type versionData struct {
	buildinfo.Info
	// AgentTmux is the installed agent-tmux's version, "" when it is missing;
	// AgentTmuxLatest is a newer release, "" when there is none.
	AgentTmux       string `json:"agent_tmux"`
	AgentTmuxLatest string `json:"agent_tmux_latest"`

	// With --check only. Latest, Status and Error are ws's own release check
	// (Status: current, outdated, dev or unknown; Error when the lookup
	// failed). AgentTmuxCheck is agent-tmux's answer.
	Latest         string          `json:"latest,omitempty"`
	Status         string          `json:"status,omitempty"`
	Error          string          `json:"error,omitempty"`
	AgentTmuxCheck *agentTmuxCheck `json:"agent_tmux_check,omitempty"`
}

// agentTmuxCheck mirrors `agent-tmux version --check --json`. ws adds two
// statuses of its own: "not_installed" and "too_old" (the installed agent-tmux
// does not know --check; Version is then read the old way).
type agentTmuxCheck struct {
	Version string `json:"version"`
	Latest  string `json:"latest"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// wsReleaseURL is GitHub's latest-release API for ws. Tests point it at an
// httptest server.
var wsReleaseURL = "https://api.github.com/repos/mugenkunou/ws-tool/releases/latest"

// executablePath is the running ws binary; tests replace it.
var executablePath = os.Executable

// checkWS compares installed with the newest ws release, always asking GitHub
// (no cache). A dev or unparseable build is never outdated; latest is still
// reported when the lookup worked.
func checkWS(ctx context.Context, installed string) (latest, status, errMsg string) {
	latest, err := release.Fetch(ctx, wsReleaseURL)
	if err != nil {
		errMsg = err.Error()
		if !release.Parseable(installed) {
			return "", agenttmux.StatusDev, errMsg
		}
		return "", agenttmux.StatusUnknown, errMsg
	}
	switch {
	case !release.Parseable(installed):
		return latest, agenttmux.StatusDev, ""
	case !release.Parseable(latest):
		return latest, agenttmux.StatusUnknown, "release tag " + latest + " is not a version"
	case release.Older(installed, latest):
		return latest, agenttmux.StatusOutdated, ""
	}
	return latest, agenttmux.StatusCurrent, ""
}

// checkAgentTmux runs agent-tmux's own check and maps every outcome to one
// agentTmuxCheck.
func checkAgentTmux(ctx context.Context) agentTmuxCheck {
	r, err := agenttmux.RunCheck(ctx)
	switch {
	case errors.Is(err, agenttmux.ErrMissing):
		return agentTmuxCheck{Status: "not_installed"}
	case err != nil:
		return agentTmuxCheck{Version: r.Version, Status: "too_old", Error: err.Error()}
	}
	return agentTmuxCheck(r)
}

// printCheck prints one binary's check: a row, and a warning line when the
// build is a dev build or the lookup failed.
func printCheck(out io.Writer, nc bool, name, installed, latest, status, errMsg string) {
	switch status {
	case agenttmux.StatusCurrent:
		style.KV(out, name, fmt.Sprintf("%s (latest %s)", installed, latest), nc)
	case agenttmux.StatusOutdated:
		style.KV(out, name, installed, nc)
		fmt.Fprintln(out, style.ResultWarning(nc, "update available: %s", latest))
	case agenttmux.StatusDev:
		style.KV(out, name, installed, nc)
		msg := "local binary is a dev build"
		if latest != "" {
			msg += ", upstream release is " + latest
		}
		if errMsg != "" {
			msg += " (release check failed: " + errMsg + ")"
		}
		fmt.Fprintln(out, style.ResultWarning(nc, "%s", msg))
	default:
		style.KV(out, name, installed, nc)
		fmt.Fprintln(out, style.ResultWarning(nc, "release check failed: %s", errMsg))
	}
}

var versionHelp = cmdHelp{
	Usage: "ws version [--short] [--check]",
	Flags: []string{
		"      --short   Print only semantic version (default: false)",
		"      --check   Check GitHub for newer ws and agent-tmux releases, ignoring the cache and WS_NO_VERSION_CHECK (default: false)",
	},
}

func runVersion(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, versionHelp)
	}

	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	short := fs.Bool("short", false, "print only semver")
	check := fs.Bool("check", false, "check for newer releases")

	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if *short {
		fmt.Fprintln(stdout, appVersion)
		return 0
	}

	data := versionData{Info: buildinfo.New(appVersion)}
	ctx := context.Background()
	if !*check { // --check reads the version from agent-tmux's own answer
		if v, err := agenttmux.Version(ctx); err == nil {
			data.AgentTmux = v
		}
	}
	var nudge agenttmux.Nudge
	var newer bool
	if *check {
		data.Latest, data.Status, data.Error = checkWS(ctx, appVersion)
		c := checkAgentTmux(ctx)
		data.AgentTmuxCheck = &c
		if c.Version != "" {
			data.AgentTmux = c.Version
		}
		if c.Status == agenttmux.StatusOutdated {
			data.AgentTmuxLatest = c.Latest
		}
	} else {
		nudge, newer = agenttmux.NudgeFor(ctx, data.AgentTmux)
		if newer {
			data.AgentTmuxLatest = nudge.Latest
		}
	}

	if globals.json {
		return writeJSON(stdout, stderr, "version", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s %s\n", style.Boldf(nc, "ws"), style.Accentf(nc, "%s", data.Version))
	style.KV(out, "ws.json schema", fmt.Sprintf("%d", data.ConfigSchema), nc)
	style.KV(out, "Platform", data.Platform, nc)
	style.KV(out, "Go version", data.GoVersion, nc)

	if *check {
		printCheck(out, nc, "ws release", data.Version, data.Latest, data.Status, data.Error)
		if data.Status == agenttmux.StatusOutdated {
			if exe, err := executablePath(); err == nil {
				for _, l := range release.WSUpgradeLines(exe) {
					fmt.Fprintln(out, "  "+l)
				}
			}
		}
		c := data.AgentTmuxCheck
		switch c.Status {
		case "not_installed":
			style.KV(out, "agent-tmux", style.Mutedf(nc, "not installed"), nc)
			for _, l := range agenttmux.InstallLines {
				fmt.Fprintln(out, "  "+l)
			}
		case "too_old":
			ver := c.Version
			if ver == "" {
				ver = "unknown version"
			}
			style.KV(out, "agent-tmux", ver, nc)
			fmt.Fprintln(out, style.ResultWarning(nc, "%s", agenttmux.ErrTooOld.Error()))
			for _, l := range agenttmux.InstallLines {
				fmt.Fprintln(out, "  "+l)
			}
		default:
			printCheck(out, nc, "agent-tmux", c.Version, c.Latest, c.Status, c.Error)
		}
		return 0
	}

	// ws session runs agent-tmux; say which, and whether a newer one is out.
	switch {
	case data.AgentTmux == "":
		style.KV(out, "agent-tmux", style.Mutedf(nc, "not installed"), nc)
		for _, l := range agenttmux.InstallLines {
			fmt.Fprintln(out, "  "+l)
		}
	case newer:
		style.KV(out, "agent-tmux", data.AgentTmux, nc)
		fmt.Fprintln(out)
		for i, l := range nudge.Lines() {
			if i == 0 {
				fmt.Fprintln(out, style.ResultWarning(nc, "%s", l))
				continue
			}
			fmt.Fprintln(out, "  "+l)
		}
	default:
		style.KV(out, "agent-tmux", data.AgentTmux, nc)
	}
	return 0
}
