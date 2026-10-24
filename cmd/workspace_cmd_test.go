package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/ignore"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func execArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// ── init ───────────────────────────────────────────────────────────

func TestInitIsIdempotentAndFillsKeys(t *testing.T) {
	testSetXDG(t)
	ws := filepath.Join(t.TempDir(), "Workspace")
	if code, out, errOut := execArgs(t, "--no-color", "--quiet", "init", "--workspace", ws); code != 0 {
		t.Fatalf("init: %d %s %s", code, out, errOut)
	}
	code, out, _ := execArgs(t, "--no-color", "init", "--workspace", ws)
	if code != 0 || !strings.Contains(out, "Workspace already initialized: "+ws) {
		t.Fatalf("second init: %d %q", code, out)
	}

	// An older ws.json: some keys, a value set, a missing directory.
	cfg := filepath.Join(ws, "ws.json")
	if err := os.WriteFile(cfg, []byte(`{"schema": 1, "repo": {"roots": ["experiments"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(ws, "archive")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = execArgs(t, "--no-color", "--dry-run", "init", "--workspace", ws)
	if code != 0 || !strings.Contains(out, "Create 1 directory") || !strings.Contains(out, "missing keys in "+cfg+"; values you set are kept") {
		t.Fatalf("dry-run plan: %d %q", code, out)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `"roots": ["experiments"]`) || strings.Contains(string(b), "max_parallel") {
		t.Fatalf("--dry-run changes nothing: %s", b)
	}
	if code, out, errOut := execArgs(t, "--no-color", "--quiet", "init", "--workspace", ws); code != 0 {
		t.Fatalf("fill: %d %s %s", code, out, errOut)
	}
	got, err := wsconfig.Load(cfg)
	if err != nil || !slices.Equal(got.Repo.Roots, []string{"experiments"}) || got.Repo.MaxParallel != 8 {
		t.Fatalf("filled, values kept: %+v %v", got.Repo, err)
	}
	if _, err := os.Stat(filepath.Join(ws, "archive")); err != nil {
		t.Fatal("the missing directory is created")
	}
}

func TestInitJSONAndConfigFlag(t *testing.T) {
	testSetXDG(t)
	ws := filepath.Join(t.TempDir(), "Workspace")
	cfg := filepath.Join(t.TempDir(), "elsewhere.json")
	code, out, errOut := execArgs(t, "--json", "init", "--workspace", ws, "--config", cfg)
	if code != 0 {
		t.Fatalf("init: %d %s", code, errOut)
	}
	var env struct {
		Command string `json:"command"`
		Data    struct {
			Workspace string   `json:"workspace"`
			Config    string   `json:"config"`
			Created   []string `json:"created"`
			Existing  []string `json:"existing"`
			Actions   []struct {
				ID, Status string
			} `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if env.Command != "init" || env.Data.Workspace != ws || env.Data.Config != cfg {
		t.Fatalf("%+v", env)
	}
	if !slices.Contains(env.Data.Created, cfg) || !slices.Contains(env.Data.Created, filepath.Join(ws, ".megaignore")) || len(env.Data.Existing) != 0 || len(env.Data.Actions) != 3 {
		t.Fatalf("created=%v existing=%v actions=%v", env.Data.Created, env.Data.Existing, env.Data.Actions)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("ws.json goes where --config says: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "ws.json")); err == nil {
		t.Fatal("not in the workspace")
	}
	// Initialized, and listing it as existing now.
	_, out, _ = execArgs(t, "--json", "init", "--workspace", ws, "--config", cfg)
	_ = json.Unmarshal([]byte(out), &env)
	if len(env.Data.Created) != 0 || !slices.Contains(env.Data.Existing, cfg) {
		t.Fatalf("second run: created=%v existing=%v", env.Data.Created, env.Data.Existing)
	}
}

func TestInitRefusesAnInvalidWsJSON(t *testing.T) {
	testSetXDG(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "ws.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := execArgs(t, "init", "--workspace", ws); code != 1 || !strings.Contains(errOut, "ws.json") {
		t.Fatalf("code=%d %q", code, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "ws.json")); string(b) != "{nope" {
		t.Fatal("never replaced")
	}
}

// ── config show ────────────────────────────────────────────────────

func TestConfigShowIsResolvedAbsolute(t *testing.T) {
	ws := initWorkspace(t)
	cfgPath := filepath.Join(ws, "ws.json")
	cfg, _ := wsconfig.Load(cfgPath)
	cfg.Links = []wsconfig.Link{{System: "/home/u/.x", Original: "configs/dotfiles/x"}}
	cfg.Secret.SkipDirs = []string{"vendor"}
	if err := wsconfig.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := execArgs(t, "--workspace", ws, "config", "show")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	var shown struct {
		Workspace string `json:"workspace"`
		Config    string `json:"config"`
		Repo      struct {
			Roots []string `json:"roots"`
		} `json:"repo"`
		Secret struct {
			SkipDirs []string `json:"skip_dirs"`
		} `json:"secret"`
		Links []struct{ Original string } `json:"links"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if shown.Workspace != ws || shown.Config != cfgPath {
		t.Fatalf("workspace/config: %+v", shown)
	}
	if !slices.Equal(shown.Repo.Roots, []string{ws}) || !slices.Equal(shown.Secret.SkipDirs, []string{filepath.Join(ws, "vendor")}) ||
		len(shown.Links) != 1 || shown.Links[0].Original != filepath.Join(ws, "configs/dotfiles/x") {
		t.Fatalf("paths must be absolute: %+v", shown)
	}

	code, out, _ = execArgs(t, "--workspace", ws, "--json", "config", "show")
	var env struct {
		Command string          `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &env) != nil || env.Command != "config.show" || !strings.Contains(string(env.Data), `"workspace"`) {
		t.Fatalf("json: %d %s", code, out)
	}
}

func TestConfigShowDefaultsNeedsNoWorkspace(t *testing.T) {
	testSetXDG(t)
	code, out, errOut := execArgs(t, "--workspace", filepath.Join(t.TempDir(), "none"), "config", "show", "--defaults")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %s", code, errOut)
	}
	if !strings.Contains(out, `"root_dir": "~/Scratch"`) || !strings.Contains(out, `"roots": [`) || strings.Contains(out, `"workspace"`) {
		t.Fatalf("stored form, no workspace key:\n%s", out)
	}
}

// ── config edit ────────────────────────────────────────────────────

// fakeEditor installs a "vim" that replaces the file it is given with content,
// or (when content is empty) leaves it alone.
func fakeEditor(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "new.json")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n"
	if content != "" {
		script += "cp '" + src + "' \"$1\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "vim"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestConfigEditSavesAValidEdit(t *testing.T) {
	ws := initWorkspace(t)
	cfgPath := filepath.Join(ws, "ws.json")
	cfg, _ := wsconfig.Load(cfgPath)
	cfg.Repo.MaxParallel = 2
	b, _ := json.Marshal(cfg)
	fakeEditor(t, string(b))

	code, out, errOut := execArgs(t, "--workspace", ws, "--no-color", "config", "edit", "--editor", "vim")
	if code != 0 || !strings.Contains(out, "Saved") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	got, _ := wsconfig.Load(cfgPath)
	if got.Repo.MaxParallel != 2 {
		t.Fatalf("the edit is saved: %+v", got.Repo)
	}
	if _, err := os.Stat(filepath.Join(ws, ".ws.json.edit")); err == nil {
		t.Fatal("the copy is deleted after a save")
	}
}

func TestConfigEditRegeneratesMegaignoreWhenRulesChange(t *testing.T) {
	ws := initWorkspace(t)
	cfgPath := filepath.Join(ws, "ws.json")
	cfg, _ := wsconfig.Load(cfgPath)
	cfg.IgnoreRules.Exclude = append(cfg.IgnoreRules.Exclude, ignore.RuleEntry{Pattern: "datasets-from-edit/"})
	b, _ := json.Marshal(cfg)
	fakeEditor(t, string(b))

	code, out, errOut := execArgs(t, "--workspace", ws, "--json", "config", "edit", "--editor", "vim")
	if code != 0 || !strings.Contains(out, `"megaignore_regenerated": true`) {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if mega, _ := os.ReadFile(filepath.Join(ws, ".megaignore")); !strings.Contains(string(mega), "datasets-from-edit/") {
		t.Fatalf(".megaignore not regenerated:\n%s", mega)
	}
}

func TestConfigEditKeepsWsJSONWhenInvalid(t *testing.T) {
	ws := initWorkspace(t)
	cfgPath := filepath.Join(ws, "ws.json")
	before, _ := os.ReadFile(cfgPath)
	for name, bad := range map[string]string{
		"not json":    `{"schema": 1`,
		"unknown key": `{"schema": 1, "mystery": 1}`,
		"schema":      `{"schema": 99}`,
	} {
		fakeEditor(t, bad)
		code, _, errOut := execArgs(t, "--workspace", ws, "config", "edit", "--editor", "vim")
		if code != 1 || !strings.Contains(errOut, ".ws.json.edit") {
			t.Fatalf("%s: code=%d %q (the error names the kept copy)", name, code, errOut)
		}
		if after, _ := os.ReadFile(cfgPath); !bytes.Equal(before, after) {
			t.Fatalf("%s: ws.json must stay as it was", name)
		}
		if b, _ := os.ReadFile(filepath.Join(ws, ".ws.json.edit")); string(b) != bad {
			t.Fatalf("%s: the copy keeps the edit", name)
		}
	}
}

func TestConfigEditNoChanges(t *testing.T) {
	ws := initWorkspace(t)
	fakeEditor(t, "")
	code, out, _ := execArgs(t, "--workspace", ws, "--no-color", "config", "edit", "--editor", "vim")
	if code != 0 || !strings.Contains(out, "No changes.") {
		t.Fatalf("%d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(ws, ".ws.json.edit")); err == nil {
		t.Fatal("an unchanged copy is deleted")
	}
}

// ── version, removed commands ──────────────────────────────────────

func TestVersionJSONUsesConfigSchema(t *testing.T) {
	code, out, _ := execArgs(t, "--json", "version")
	var env struct {
		Data map[string]any `json:"data"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &env) != nil {
		t.Fatalf("%d %s", code, out)
	}
	if _, ok := env.Data["schema"]; ok {
		t.Fatalf("data.schema was renamed: %v", env.Data)
	}
	if env.Data["config_schema"] != float64(wsconfig.CurrentSchema) {
		t.Fatalf("data = %v", env.Data)
	}
	if _, out, _ := execArgs(t, "--no-color", "version"); !strings.Contains(out, "ws.json schema") {
		t.Fatalf("text: %q", out)
	}
}

func TestResetAndRestoreArePointers(t *testing.T) {
	for _, c := range []string{"reset", "restore"} {
		code, out, errOut := execArgs(t, c)
		if code != 1 || out != "" || !strings.Contains(errOut, `"`+c+`" was removed`) {
			t.Fatalf("%s: code=%d out=%q err=%q", c, code, out, errOut)
		}
	}
}

// ── ws.json has every key ──────────────────────────────────────────

// TestWsJSONHasEveryKey: spec §2.3. ws init writes the complete file, and every
// command that rewrites ws.json leaves every key in it, even when the file was
// written by an older ws that lacked some.
func TestWsJSONHasEveryKey(t *testing.T) {
	ws := initWorkspace(t)
	cfgPath := filepath.Join(ws, "ws.json")
	lacks := func(when string) {
		t.Helper()
		content, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if missing, err := wsconfig.MissingKeys(content); err != nil || len(missing) != 0 {
			t.Fatalf("after %s ws.json lacks %v (%v):\n%s", when, missing, err, content)
		}
	}
	lacks("init")

	// An old ws.json with few keys: a write fills the rest in.
	if err := os.WriteFile(cfgPath, []byte(`{"schema": 1, "repo": {"roots": ["."]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "datasets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := execArgs(t, "--workspace", ws, "--quiet", "ignore", "add", filepath.Join(ws, "datasets")); code != 0 {
		t.Fatalf("ignore add: %d %s", code, errOut)
	}
	lacks("ignore add")

	if err := os.WriteFile(cfgPath, []byte(`{"schema": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ws, "notes")
	if code, _, errOut := execArgs(t, "--workspace", ws, "--quiet", "repo", "root", "add", dir); code != 0 {
		t.Fatalf("repo root add: %d %s", code, errOut)
	}
	lacks("repo root add")

	// ws init fills in the keys of an older file and keeps its values.
	if err := os.WriteFile(cfgPath, []byte(`{"schema": 1, "scratch": {"root_dir": "~/Elsewhere"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := execArgs(t, "--workspace", ws, "--quiet", "init"); code != 0 {
		t.Fatalf("init: %d %s", code, errOut)
	}
	lacks("init on an old file")
	if cfg, _ := wsconfig.Load(cfgPath); cfg.Scratch.RootDir != "~/Elsewhere" {
		t.Fatalf("init keeps the values already set: %q", cfg.Scratch.RootDir)
	}
}
