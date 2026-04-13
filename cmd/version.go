package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mugenkunou/ws-tool/internal/config"
	"github.com/mugenkunou/ws-tool/internal/manifest"
	"github.com/mugenkunou/ws-tool/internal/megaignore"
	"github.com/mugenkunou/ws-tool/internal/provision"
	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// appVersion is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/mugenkunou/ws-tool/cmd.appVersion=v0.2.0"
var appVersion = "dev"

const (
	configSchema   = 1
	manifestSchema = 1
)

type versionData struct {
	Version        string `json:"version"`
	ConfigSchema   int    `json:"config_schema"`
	ManifestSchema int    `json:"manifest_schema"`
	Platform       string `json:"platform"`
	GoVersion      string `json:"go_version"`
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
		Version:        appVersion,
		ConfigSchema:   configSchema,
		ManifestSchema: manifestSchema,
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion:      runtime.Version(),
	}

	if globals.json {
		return writeJSON(stdout, stderr, "version", data)
	}

	out := textOut(globals, stdout)
	nc := globals.noColor
	fmt.Fprintf(out, "%s %s\n", style.Boldf(nc, "ws"), style.Accentf(nc, "%s", data.Version))
	style.KV(out, "Config schema", fmt.Sprintf("%d", data.ConfigSchema), nc)
	style.KV(out, "Manifest schema", fmt.Sprintf("%d", data.ManifestSchema), nc)
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

func runInit(args []string, globals globalFlags, stdin io.Reader, stdout, stderr io.Writer) int {
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

	resolvedWorkspace, err := config.ExpandUserPath(workspacePath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}

	configPath := globals.config
	if configPath == "" {
		configPath = resolvedWorkspace + "/ws/config.json"
	}

	manifestPath := globals.manifest
	if manifestPath == "" {
		manifestPath = resolvedWorkspace + "/ws/manifest.json"
	}

	// Guard: if workspace is already initialized, warn and suggest reset.
	if workspace.ConfigExists(configPath) {
		if globals.json {
			return writeJSON(stdout, stderr, "init", map[string]any{
				"already_initialized": true,
				"workspace_path":     resolvedWorkspace,
				"suggestion":         "ws reset && ws init",
			})
		}
		out := textOut(globals, stdout)
		fmt.Fprintln(out, "Workspace already initialized at "+resolvedWorkspace)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "To reinitialize, run:")
		fmt.Fprintln(out, "  ws reset && ws init")
		return 0
	}

	wsDir := filepath.Join(resolvedWorkspace, "ws")
	megaignorePath := filepath.Join(resolvedWorkspace, ".megaignore")

	// Build plan: one action per file/directory to create.
	plan := Plan{Command: "init"}

	if _, err := os.Stat(wsDir); err != nil {
		plan.Actions = append(plan.Actions, Action{
			ID:          "create-ws-dir",
			Description: fmt.Sprintf("Create %s", filepath.Join("ws")+"/"),
			Execute:     func() error { return os.MkdirAll(wsDir, 0o755) },
		})
	}

	if _, err := os.Stat(configPath); err != nil {
		plan.Actions = append(plan.Actions, Action{
			ID:          "create-config",
			Description: "Create ws/config.json",
			Execute: func() error {
				if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
					return err
				}
				return config.Save(configPath, config.Default())
			},
		})
	}

	if _, err := os.Stat(manifestPath); err != nil {
		plan.Actions = append(plan.Actions, Action{
			ID:          "create-manifest",
			Description: "Create ws/manifest.json",
			Execute: func() error {
				if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
					return err
				}
				return manifest.Save(manifestPath, manifest.Default())
			},
		})
	}

	if _, err := os.Stat(megaignorePath); err != nil {
		plan.Actions = append(plan.Actions, Action{
			ID:          "create-megaignore",
			Description: "Create .megaignore",
			Execute: func() error {
				content := megaignore.EnsureFinalSentinel(megaignore.Template)
				return os.WriteFile(megaignorePath, []byte(content), 0o644)
			},
		})
	}

	if len(plan.Actions) == 0 {
		out := textOut(globals, stdout)
		fmt.Fprintln(out, "Workspace already initialized at "+resolvedWorkspace)
		return 0
	}

	if !globals.json && !globals.dryRun {
		out := textOut(globals, stdout)
		fmt.Fprintf(out, "Initialize workspace at %s\n", resolvedWorkspace)
	}

	planResult := RunPlan(plan, stdin, stdout, globals)

	// Record provisions for executed actions.
	if !globals.dryRun {
		provMap := map[string]provision.Entry{
			"create-ws-dir":     {Type: provision.TypeDir, Path: wsDir, Command: "init"},
			"create-config":     {Type: provision.TypeFile, Path: configPath, Command: "init"},
			"create-manifest":   {Type: provision.TypeFile, Path: manifestPath, Command: "init"},
			"create-megaignore": {Type: provision.TypeFile, Path: megaignorePath, Command: "init"},
		}
		provPath := provision.LedgerPath(resolvedWorkspace)
		var entries []provision.Entry
		for _, id := range planResult.ExecutedIDs() {
			if e, ok := provMap[id]; ok {
				entries = append(entries, e)
			}
		}
		if len(entries) > 0 {
			_ = provision.RecordAll(provPath, entries)
		}
	}

	if globals.json {
		return writeJSONDryRun(stdout, stderr, "init", globals.dryRun, map[string]any{
			"workspace_path": resolvedWorkspace,
			"actions":        planResult.Actions,
		})
	}

	if planResult.ExecutedCount() > 0 {
		out := textOut(globals, stdout)
		fmt.Fprintln(out, style.ResultSuccess(globals.noColor, "Workspace initialized: %s", resolvedWorkspace))
	}

	return planResult.ExitCode()
}
