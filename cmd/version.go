package cmd

import (
	"flag"
	"fmt"
	"io"
	"runtime"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// appVersion is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0"
var appVersion = "dev"

type versionData struct {
	Version      string `json:"version"`
	ConfigSchema int    `json:"config_schema"`
	Platform     string `json:"platform"`
	GoVersion    string `json:"go_version"`
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

	data := versionData{
		Version:      appVersion,
		ConfigSchema: wsconfig.CurrentSchema,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion:    runtime.Version(),
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

	return 0
}
