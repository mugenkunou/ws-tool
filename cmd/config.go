package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mugenkunou/ws-tool/internal/editor"
	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

// runConfig dispatches `ws config show|edit`.
func runConfig(args []string, globals globalFlags, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ws config <show|edit>")
		return 1
	}
	switch args[0] {
	case "show":
		return runConfigShow(args[1:], globals, stdout, stderr)
	case "edit":
		return runConfigEdit(args[1:], globals, stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command: config %s\n", args[0])
	fmt.Fprintln(stderr, "usage: ws config <show|edit>")
	return 1
}

// configView is ws.json as ws resolves it: every path absolute, plus where it
// came from.
type configView struct {
	wsconfig.WsConfig
	Workspace string `json:"workspace"`
	Config    string `json:"config"`
}

func runConfigShow(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	defaults := fs.Bool("defaults", false, "print the built-in defaults")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ws config show takes no arguments (got %q)\n", fs.Arg(0))
		return 1
	}

	var view any
	if *defaults {
		// Stored form, so the output can be written to a ws.json; the one
		// documented exception to absolute output.
		view = wsconfig.Default()
	} else {
		workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		cfg, err := wsconfig.Load(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		view = configView{WsConfig: wsconfig.WithAbsPaths(cfg, workspacePath), Workspace: workspacePath, Config: configPath}
	}

	if globals.json {
		return writeJSON(stdout, stderr, "config.show", view)
	}
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(textOut(globals, stdout), string(encoded))
	return 0
}

// runConfigEdit opens a copy of ws.json and saves it only if it is still valid
// and ws.json did not change on disk meanwhile.
func runConfigEdit(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config-edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	editorFlag := fs.String("editor", "", "editor command")
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ws config edit takes no arguments (got %q)\n", fs.Arg(0))
		return 1
	}

	workspacePath, configPath, err := requireWorkspaceInitialized(globals, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	original, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cfg, err := wsconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	copyPath := filepath.Join(filepath.Dir(configPath), ".ws.json.edit")
	if err := os.WriteFile(copyPath, original, 0o644); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	editorOut := stdout
	if globals.json {
		editorOut = stderr
	}
	if err := editor.Open(editorFor(*editorFlag, cfg), copyPath, editorOut); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	edited, err := os.ReadFile(copyPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if bytes.Equal(edited, original) {
		_ = os.Remove(copyPath)
		if globals.json {
			return writeJSON(stdout, stderr, "config.edit", map[string]any{"config": configPath, "changed": false, "megaignore_regenerated": false})
		}
		fmt.Fprintln(textOut(globals, stdout), "No changes.")
		return 0
	}

	newCfg, err := wsconfig.Validate(edited)
	if err != nil {
		fmt.Fprintf(stderr, "%v\nws.json is unchanged; your edit is kept in %s\n", err, copyPath)
		return 1
	}
	if now, err := os.ReadFile(configPath); err != nil || !bytes.Equal(now, original) {
		fmt.Fprintf(stderr, "ws.json changed on disk while the editor was open; it is unchanged. Your edit is kept in %s\n", copyPath)
		return 1
	}
	if err := os.Rename(copyPath, configPath); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	regenerated := false
	if !ignoreRulesEqual(cfg.IgnoreRules, newCfg.IgnoreRules) {
		if err := ignore.WriteMegaignore(filepath.Join(workspacePath, ".megaignore"), newCfg.IgnoreRules); err != nil {
			fmt.Fprintf(stderr, "ws.json saved, but .megaignore could not be regenerated: %v\n", err)
			return 1
		}
		regenerated = true
	}

	if globals.json {
		return writeJSON(stdout, stderr, "config.edit", map[string]any{"config": configPath, "changed": true, "megaignore_regenerated": regenerated})
	}
	out := textOut(globals, stdout)
	fmt.Fprintln(out, "Saved "+configPath)
	if regenerated {
		fmt.Fprintln(out, "Regenerated "+filepath.Join(workspacePath, ".megaignore"))
	}
	return 0
}

func ignoreRulesEqual(a, b ignore.UserRules) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
