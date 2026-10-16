package cmd

import (
	"flag"
	"fmt"
	"io"
	"runtime"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// appVersion is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0"
var appVersion = "dev"

type versionData struct {
	Version    string `json:"version"`
	Schema     int    `json:"schema"`
	Platform   string `json:"platform"`
	GoVersion  string `json:"go_version"`
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
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if *short {
		fmt.Fprintln(stdout, appVersion)
		return 0
	}

	data := versionData{
		Version:   appVersion,
		Schema:    wsconfig.CurrentSchema,
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion: runtime.Version(),
	}

	if globals.json {
		return writeJSON(stdout, stderr, "version", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s %s\n", style.Boldf(nc, "ws"), style.Accentf(nc, "%s", data.Version))
	style.KV(out, "Schema", fmt.Sprintf("%d", data.Schema), nc)
	style.KV(out, "Platform", data.Platform, nc)
	style.KV(out, "Go version", data.GoVersion, nc)

	return 0
}

var initHelp = cmdHelp{
	Usage: "ws init [flags]",
	Flags: []string{
		"  -w, --workspace string   Workspace root path (default: ~/Workspace)",
		"      --dry-run            Preview actions without writing files (default: false)",
	},
}

func runInit(args []string, globals globalFlags, _ io.Reader, stdout, stderr io.Writer) int {
	if hasHelpArg(args) {
		return printCmdHelp(stdout, initHelp)
	}

	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	workspaceArg := fs.String("workspace", globals.workspace, "workspace root")
	workspaceShort := fs.String("w", globals.workspace, "workspace root")
	dryRun := fs.Bool("dry-run", globals.dryRun, "preview actions")

	registerGlobalFlags(fs, &globals)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *dryRun {
		globals.dryRun = true
	}

	workspacePath := *workspaceArg
	if workspacePath == "" {
		workspacePath = *workspaceShort
	}
	if workspacePath == "" {
		workspacePath = "~/Workspace"
	}

	resolvedWorkspace, err := wsconfig.ExpandUserPath(workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	// Guard: if workspace is already initialized, warn and suggest reinit.
	configPath := wsconfig.ConfigPath(resolvedWorkspace)
	if workspace.ConfigExists(configPath) {
		if globals.json {
			return writeJSON(stdout, stderr, "init", map[string]any{
				"already_initialized": true,
				"workspace_path":      resolvedWorkspace,
				"suggestion":          "remove configs/ws.json and re-run ws init",
			})
		}
		out := textOut(globals, stdout)
		fmt.Fprintln(out, "Workspace already initialized at "+resolvedWorkspace)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "To reinitialize, remove configs/ws.json and re-run ws init.")
		return 0
	}

	if globals.dryRun {
		out := textOut(globals, stdout)
		fmt.Fprintf(out, "[dry-run] Would initialize workspace at %s\n", resolvedWorkspace)
		return 0
	}

	result, err := workspace.Init(workspace.InitOptions{
		WorkspacePath: resolvedWorkspace,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	if globals.json {
		return writeJSON(stdout, stderr, "init", map[string]any{
			"workspace_path": resolvedWorkspace,
			"created":        result.Created,
		})
	}

	out := textOut(globals, stdout)
	fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Workspace initialized: %s", resolvedWorkspace))
	return 0
}
