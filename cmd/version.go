package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/mugenkunou/ws-tool/internal/agenttmux"
	"github.com/mugenkunou/ws-tool/internal/buildinfo"
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
}

var versionHelp = cmdHelp{
	Usage: "ws version [--short]",
	Flags: []string{"      --short   Print only semantic version (default: false)"},
}

func runVersion(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, versionHelp)
	}

	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	short := fs.Bool("short", false, "print only semver")

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
	if v, err := agenttmux.Version(ctx); err == nil {
		data.AgentTmux = v
	}
	nudge, newer := agenttmux.NudgeFor(ctx, data.AgentTmux)
	if newer {
		data.AgentTmuxLatest = nudge.Latest
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
