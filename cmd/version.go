package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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

// statusCell is the short inline status after an item's version: yellow for
// something to do, grey for everything else, "" for nothing to say.
func statusCell(nc bool, it versions.Item) string {
	warn := func(format string, a ...any) string { return style.Warningf(nc, format, a...) }
	grey := func(format string, a ...any) string { return style.Mutedf(nc, format, a...) }
	switch it.State {
	case versions.StateCurrent:
		return grey("up to date")
	case versions.StateOutdated:
		return warn("\u2192 %s available", it.Latest)
	case versions.StateDev:
		if it.Latest != "" {
			return grey("dev build \u00b7 upstream %s", it.Latest)
		}
		return grey("dev build")
	case versions.StateTooOld:
		return warn("too old to check \u00b7 upgrade")
	case versions.StateNotInstalled:
		return warn("not installed")
	case versions.StateUnknown:
		if it.Error != "" {
			return grey("check failed: %s", it.Error)
		}
		return grey("latest not checked yet")
	}
	return ""
}

// versionRow is "<name>  <version>  <status>", the name padded so rows line up.
func versionRow(nc bool, name string, it versions.Item) string {
	row := style.Boldf(nc, "%-10s", name)
	if it.State != versions.StateNotInstalled {
		v := it.Installed
		if v == "" {
			v = "unknown version"
		}
		row += "  " + style.Accentf(nc, "%s", v)
	}
	if cell := statusCell(nc, it); cell != "" {
		row += "  " + cell
	}
	return row
}

// printSteps writes a suggestion block: a blank line, a title, then the lines
// greyed, commands behind a "$ " prompt. Lines that are not commands (the
// make install alternative) are passed with a leading "#".
func printSteps(out io.Writer, nc bool, title string, steps []string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Boldf(nc, "%s", title))
	for _, l := range steps {
		if rest, ok := strings.CutPrefix(l, "#"); ok {
			fmt.Fprintln(out, "  "+style.Mutedf(nc, "%s", strings.TrimSpace(rest)))
			continue
		}
		fmt.Fprintln(out, "  "+style.Mutedf(nc, "$ %s", l))
	}
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
	fmt.Fprintln(out, versionRow(nc, "ws", rep.WS))
	fmt.Fprintln(out, versionRow(nc, "agent-tmux", rep.AgentTmux))
	fmt.Fprintln(out)
	style.KV(out, "ws.json schema", fmt.Sprintf("%d", data.ConfigSchema), nc)
	style.KV(out, "Platform", data.Platform, nc)
	style.KV(out, "Go version", data.GoVersion, nc)

	if rep.WS.State == versions.StateOutdated {
		if exe, err := executablePath(); err == nil {
			printSteps(out, nc, "upgrade ws", release.WSUpgradeSteps(exe))
		}
	}
	switch rep.AgentTmux.State {
	case versions.StateNotInstalled, versions.StateTooOld, versions.StateOutdated:
		verb := "install"
		if rep.AgentTmux.State == versions.StateOutdated {
			verb = "upgrade"
		}
		printSteps(out, nc, verb+" agent-tmux", agenttmux.InstallSteps())
	}

	if !*check && (!rep.Known() || versions.Stale()) {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.Mutedf(nc, "run 'ws version --check' to look up the latest versions now"))
	}
	return 0
}
