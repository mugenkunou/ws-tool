package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// scriptPaths resolves the workspace and the homes outside it. It needs no
// initialized workspace: without ws.json, scratch is the default root.
func scriptPaths(globals globalFlags) (string, []workspace.Home, error) {
	workspacePath, configPath, err := resolveWorkspacePaths(globals)
	if err != nil {
		return "", nil, err
	}
	cfg := wsconfig.Default()
	if workspace.ConfigExists(configPath) {
		if cfg, err = wsconfig.Load(configPath); err != nil {
			return "", nil, err
		}
	}
	scratchRoot, err := wsconfig.ResolvePath(workspacePath, cfg.Scratch.RootDir)
	if err != nil {
		return "", nil, err
	}
	return workspacePath, workspace.ScriptHomes(os.Getenv, scratchRoot), nil
}

// parseNoArgs parses the flags of a command that takes no arguments.
func parseNoArgs(name string, args []string, globals *globalFlags, stderr io.Writer) bool {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ws %s takes no arguments (got %q)\n", name, fs.Arg(0))
		return false
	}
	return true
}

// runLayout prints the workspace folders with their purpose, then the homes
// outside the workspace, as this machine resolves them.
func runLayout(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if !parseNoArgs("layout", args, &globals, stderr) {
		return 1
	}
	workspacePath, homes, err := scriptPaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	folders := workspace.LayoutFolders(workspacePath)
	if globals.json {
		return writeJSON(stdout, stderr, "layout", map[string]any{
			"workspace": workspacePath,
			"folders":   folders,
			"homes":     homes,
		})
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	width := 0
	for _, f := range folders {
		width = max(width, len(f.Path))
	}
	for _, h := range homes {
		width = max(width, len(h.Path))
	}
	fmt.Fprintln(out, style.Boldf(nc, "Workspace")+" "+workspacePath)
	for _, f := range folders {
		purpose := f.Purpose
		if !f.Exists {
			purpose += style.Mutedf(nc, " (missing; ws init creates it)")
		}
		fmt.Fprintf(out, "  %-*s  %s\n", width, f.Path, purpose)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.Boldf(nc, "Homes")+style.Mutedf(nc, " outside the workspace (ws env exports them)"))
	for _, h := range homes {
		fmt.Fprintf(out, "  %-*s  %-10s  %s: %s\n", width, h.Path, h.Var, h.Name, h.Purpose)
	}
	return 0
}

// runEnv prints the paths scripts use as shell lines, for
// eval "$(ws env)" in scripts/lib/common.sh.
func runEnv(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	if !parseNoArgs("env", args, &globals, stderr) {
		return 1
	}
	workspacePath, homes, err := scriptPaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	vars := workspace.ScriptEnv(workspacePath, homes)
	if globals.json {
		data := make(map[string]string, len(vars))
		for _, v := range vars {
			data[v.Name] = v.Value
		}
		return writeJSON(stdout, stderr, "env", data)
	}
	// Not textOut: the lines are the output scripts eval, even under --quiet.
	for _, v := range vars {
		fmt.Fprintf(stdout, "export %s=%s\n", v.Name, shellQuote(v.Value))
	}
	return 0
}

// shellQuote quotes s for POSIX shells: single quotes, and each quote inside
// closes them, adds an escaped quote and reopens them.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
