package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/buildinfo"
	"github.com/mugenkunou/ws-tool/internal/release"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/versions"
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

	// Latest, Status and Error are ws's own release state (Status: current,
	// outdated, dev or unknown; Error when the last lookup failed).
	// AgentTmuxCheck is the same for agent-tmux, with the extra statuses
	// not_installed and too_old. CheckedAt is when the cache was last filled
	// by a successful lookup; absent when never.
	Latest         string          `json:"latest,omitempty"`
	Status         string          `json:"status"`
	Error          string          `json:"error,omitempty"`
	AgentTmuxCheck *agentTmuxCheck `json:"agent_tmux_check"`
	CheckedAt      *time.Time      `json:"checked_at,omitempty"`
}

type agentTmuxCheck struct {
	Version string `json:"version"`
	Latest  string `json:"latest"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// executablePath is the running ws binary; tests replace it.
var executablePath = os.Executable

// itemNote is the parenthetical after an item's version, "" when there is
// nothing to say (never checked).
func itemNote(it versions.Item) string {
	switch it.State {
	case versions.StateCurrent:
		return "up to date"
	case versions.StateOutdated:
		return "latest " + it.Latest + " \u2014 update available"
	case versions.StateDev:
		msg := "local build is a dev build"
		if it.Latest != "" {
			msg += "; upstream release is " + it.Latest
		}
		return msg
	case versions.StateTooOld:
		return "too old to check \u2014 upgrade"
	case versions.StateUnknown:
		if it.Error != "" {
			return "release check failed: " + it.Error
		}
	}
	return ""
}

// versionValue is "<installed>  (<note>)".
func versionValue(nc bool, installed string, it versions.Item) string {
	if installed == "" {
		installed = "unknown version"
	}
	if note := itemNote(it); note != "" {
		installed += "  " + style.Mutedf(nc, "(%s)", note)
	}
	return installed
}

var versionHelp = cmdHelp{
	Usage: "ws version [--short] [--check]",
	Flags: []string{
		"      --short   Print only semantic version (default: false)",
		"      --check   Look up the latest ws and agent-tmux releases now, ignoring the cache and WS_NO_VERSION_CHECK (default: false)",
	},
}

func runVersion(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, versionHelp)
	}

	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	short := fs.Bool("short", false, "print only semver")
	check := fs.Bool("check", false, "look up the latest releases now")
	refresh := fs.Bool("refresh-cache", false, "refresh the version cache silently (internal)")

	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	ctx := context.Background()
	if *refresh {
		versions.Refresh(ctx, appVersion)
		return 0
	}
	if *short {
		fmt.Fprintln(stdout, appVersion)
		return 0
	}

	// Plain `ws version` reads the cache and never waits for the network;
	// --check asks now.
	var rep versions.Report
	if *check {
		rep = versions.Refresh(ctx, appVersion)
	} else {
		rep = versions.Cached(ctx, appVersion)
		defer versions.SpawnRefresh()
	}

	data := versionData{Info: buildinfo.New(appVersion)}
	data.AgentTmux = rep.AgentTmux.Installed
	if rep.AgentTmux.State == versions.StateOutdated {
		data.AgentTmuxLatest = rep.AgentTmux.Latest
	}
	data.Latest, data.Status, data.Error = rep.WS.Latest, string(rep.WS.State), rep.WS.Error
	data.AgentTmuxCheck = &agentTmuxCheck{
		Version: rep.AgentTmux.Installed, Latest: rep.AgentTmux.Latest,
		Status: string(rep.AgentTmux.State), Error: rep.AgentTmux.Error,
	}
	if rep.Known() {
		t := rep.CheckedAt
		data.CheckedAt = &t
	}

	if globals.json {
		return writeJSON(stdout, stderr, "version", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s  %s\n", style.Boldf(nc, "ws"), versionValue(nc, style.Accentf(nc, "%s", rep.WS.Installed), rep.WS))
	if rep.WS.State == versions.StateOutdated {
		if exe, err := executablePath(); err == nil {
			for _, l := range release.WSUpgradeLines(exe) {
				fmt.Fprintln(out, "  "+l)
			}
		}
	}
	style.KV(out, "ws.json schema", fmt.Sprintf("%d", data.ConfigSchema), nc)
	style.KV(out, "Platform", data.Platform, nc)
	style.KV(out, "Go version", data.GoVersion, nc)

	at := rep.AgentTmux
	if at.State == versions.StateNotInstalled {
		style.KV(out, "agent-tmux", style.Mutedf(nc, "not installed"), nc)
	} else {
		style.KV(out, "agent-tmux", versionValue(nc, at.Installed, at), nc)
	}
	switch at.State {
	case versions.StateNotInstalled, versions.StateTooOld, versions.StateOutdated:
		for _, l := range agenttmux.InstallLines {
			fmt.Fprintln(out, "  "+l)
		}
	}

	if !*check && (!rep.Known() || versions.Stale()) {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "latest versions unknown yet \u2014 run 'ws version --check' to look them up now"))
	}
	return 0
}
