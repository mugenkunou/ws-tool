package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func runPlan(t *testing.T, p InitPlan) {
	t.Helper()
	if len(p.MissingDirs) > 0 {
		if err := p.CreateDirs(); err != nil {
			t.Fatal(err)
		}
	}
	if p.ConfigMissing {
		if err := p.WriteConfig(); err != nil {
			t.Fatal(err)
		}
	} else if len(p.MissingKeys) > 0 {
		if err := p.FillConfig(); err != nil {
			t.Fatal(err)
		}
	}
	if p.MegaignoreMissing {
		if err := p.WriteMegaignore(); err != nil {
			t.Fatal(err)
		}
	}
	if p.RunbooksMissing {
		if err := p.WriteRunbooks(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanInitOnAnEmptyDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	p, err := PlanInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Empty() || len(p.MissingDirs) != len(Layout) || !p.ConfigMissing || !p.MegaignoreMissing || !p.RunbooksMissing || len(p.Existing) != 0 {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("planning writes nothing")
	}
	runPlan(t, p)
	for _, rel := range []string{"ws.json", ".megaignore", "references/RUNBOOKS.md", "symlinks", "configs", "agent/skills"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	// The runbooks file starts as the purpose and an empty table, ready for runs to fill.
	rb, _ := os.ReadFile(filepath.Join(root, "references", "RUNBOOKS.md"))
	if !strings.HasPrefix(string(rb), "# Runbooks") || !strings.Contains(string(rb), "| Task | Words people use | System | Not this |") {
		t.Errorf("RUNBOOKS.md is not the seed:\n%s", rb)
	}
	// ws.json is complete: it lacks no key.
	content, _ := os.ReadFile(filepath.Join(root, "ws.json"))
	if m, _ := wsconfig.MissingKeys(content); len(m) != 0 {
		t.Errorf("init wrote an incomplete ws.json, lacking %v", m)
	}
	again, err := PlanInit(root)
	if err != nil || !again.Empty() {
		t.Fatalf("a second plan finds nothing to do: %+v %v", again, err)
	}
}

func TestPlanInitFillsKeysAndKeepsValues(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "ws.json")
	if err := os.WriteFile(cfg, []byte(`{"schema": 1, "repo": {"roots": ["experiments"], "max_parallel": 3}, "trash": {"warn_size_mb": 9}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".megaignore"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "references", "RUNBOOKS.md"), []byte("my runbooks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PlanInit(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigMissing || len(p.MissingKeys) == 0 || p.MegaignoreMissing || p.RunbooksMissing || !slices.Contains(p.Existing, cfg) {
		t.Fatalf("%+v", p)
	}
	runPlan(t, p)
	got, err := wsconfig.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Repo.Roots, []string{"experiments"}) || got.Repo.MaxParallel != 3 || got.Trash.WarnSizeMB != 9 {
		t.Fatalf("values already set are kept: %+v", got)
	}
	content, _ := os.ReadFile(cfg)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(content, &raw)
	if _, ok := raw["editor"]; !ok {
		t.Fatalf("missing keys are filled in:\n%s", content)
	}
	if mega, _ := os.ReadFile(filepath.Join(root, ".megaignore")); string(mega) != "mine\n" {
		t.Fatal("an existing .megaignore is left alone")
	}
	if rb, _ := os.ReadFile(filepath.Join(root, "references", "RUNBOOKS.md")); string(rb) != "my runbooks\n" {
		t.Fatal("an existing RUNBOOKS.md is left alone")
	}
}

func TestPlanInitRefusesAnInvalidConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ws.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInit(root); err == nil || !strings.Contains(err.Error(), "ws.json") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "ws.json")); string(b) != "{nope" {
		t.Fatal("an existing ws.json is never replaced")
	}
}

func TestWriteConfigNeverReplaces(t *testing.T) {
	root := t.TempDir()
	p, _ := PlanInit(root)
	runPlan(t, p)
	if err := p.WriteConfig(); err == nil {
		t.Fatal("WriteConfig must refuse when ws.json exists")
	}
	if err := p.WriteMegaignore(); err == nil {
		t.Fatal("WriteMegaignore must refuse when .megaignore exists")
	}
	if err := p.WriteRunbooks(); err == nil {
		t.Fatal("WriteRunbooks must refuse when RUNBOOKS.md exists")
	}
}

// Skills start from `ws layout`, so the references/ purpose is how a run finds the
// runbooks table.
func TestLayoutNamesTheRunbooks(t *testing.T) {
	for _, d := range Layout {
		if d.Path == "references" {
			if !strings.Contains(d.Purpose, "RUNBOOKS.md") || !strings.Contains(d.Purpose, "WISDOM.md") {
				t.Fatalf("references purpose must name RUNBOOKS.md and WISDOM.md: %q", d.Purpose)
			}
			return
		}
	}
	t.Fatal("no references folder in the layout")
}

func TestConfigExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	if ConfigExists(path) {
		t.Fatal("config should not exist")
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ConfigExists(path) {
		t.Fatal("config should exist")
	}
}

// TestScriptHomesMatchCommonSh: the fallbacks are those of
// scripts/lib/common.sh, and an empty variable counts as unset (the shell's :-).
func TestScriptHomesMatchCommonSh(t *testing.T) {
	env := map[string]string{"HOME": "/h", "TMPDIR": "", "XDG_RUNTIME_DIR": "/run/user/1"}
	homes := ScriptHomes(func(k string) string { return env[k] }, "/h/Scratch")
	got := map[string]string{}
	for _, v := range ScriptEnv("/h/Workspace", homes) {
		got[v.Name] = v.Value
	}
	want := map[string]string{
		"WS_ROOT": "/h/Workspace", "WS_TMP": "/h/tmp", "WS_CACHE": "/h/.cache/ws",
		"WS_RUN": "/run/user/1", "WS_SCRATCH": "/h/Scratch",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
}
