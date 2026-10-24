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
}

func TestPlanInitOnAnEmptyDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	p, err := PlanInit(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Empty() || len(p.MissingDirs) != len(layoutDirs) || !p.ConfigMissing || !p.MegaignoreMissing || len(p.Existing) != 0 {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("planning writes nothing")
	}
	runPlan(t, p)
	for _, rel := range []string{"ws.json", ".megaignore", "symlinks", "configs", "secrets", "data/sessions", "agent/skills"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	// ws.json is complete: it lacks no key.
	content, _ := os.ReadFile(filepath.Join(root, "ws.json"))
	if m, _ := wsconfig.MissingKeys(content); len(m) != 0 {
		t.Errorf("init wrote an incomplete ws.json, lacking %v", m)
	}
	again, err := PlanInit(root, "")
	if err != nil || !again.Empty() {
		t.Fatalf("a second plan finds nothing to do: %+v %v", again, err)
	}
}

func TestPlanInitFillsKeysAndKeepsValues(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "ws.json")
	if err := os.WriteFile(cfg, []byte(`{"schema": 1, "repo": {"roots": ["experiments"], "max_parallel": 3}, "session": {"cap_mb": 9}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".megaignore"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PlanInit(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigMissing || len(p.MissingKeys) == 0 || p.MegaignoreMissing || !slices.Contains(p.Existing, cfg) {
		t.Fatalf("%+v", p)
	}
	runPlan(t, p)
	got, err := wsconfig.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Repo.Roots, []string{"experiments"}) || got.Repo.MaxParallel != 3 || got.Session.CapMB != 9 {
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
}

func TestPlanInitUsesTheConfigPath(t *testing.T) {
	root := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "mine.json")
	p, err := PlanInit(root, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	runPlan(t, p)
	if _, err := os.Stat(elsewhere); err != nil {
		t.Fatalf("ws.json goes where --config says: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ws.json")); err == nil {
		t.Fatal("and nowhere else")
	}
}

func TestPlanInitRefusesAnInvalidConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ws.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanInit(root, ""); err == nil || !strings.Contains(err.Error(), "ws.json") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "ws.json")); string(b) != "{nope" {
		t.Fatal("an existing ws.json is never replaced")
	}
}

func TestWriteConfigNeverReplaces(t *testing.T) {
	root := t.TempDir()
	p, _ := PlanInit(root, "")
	runPlan(t, p)
	if err := p.WriteConfig(); err == nil {
		t.Fatal("WriteConfig must refuse when ws.json exists")
	}
	if err := p.WriteMegaignore(); err == nil {
		t.Fatal("WriteMegaignore must refuse when .megaignore exists")
	}
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
