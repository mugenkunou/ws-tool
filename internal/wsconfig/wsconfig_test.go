package wsconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// topLevelKeys is spec §2.3, in the file's order.
var topLevelKeys = []string{"schema", "editor", "scratch", "repo", "dotfile", "secret", "pass", "ignore", "ignore_rules", "session", "links"}

// keysOf reads the keys of a JSON object in file order.
func keysOf(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// TestSaveWritesEveryKey: a write holds every key of the default config, in the
// spec's order, even when the values are empty or defaults (spec §2.3).
func TestSaveWritesEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	cfg := WsConfig{Schema: CurrentSchema} // everything else zero: nil lists, empty strings
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(t, raw); !slices.Equal(got, topLevelKeys) {
		t.Fatalf("top-level keys %v, want %v", got, topLevelKeys)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	// Nested keys, and lists written as [] rather than null.
	want := map[string][]string{
		"scratch":      {"root_dir", "tags"},
		"repo":         {"roots", "exclude_dirs", "max_parallel"},
		"secret":       {"allowlist", "skip_dirs"},
		"pass":         {"pass_entry"},
		"ignore":       {"warn_size_mb", "crit_size_mb", "max_depth", "muted"},
		"ignore_rules": {"schema", "exclude", "safe_harbors", "suppress_defaults", "suppress_harbors"},
		"session":      {"cap_mb", "max_session_mb"},
	}
	for section, keys := range want {
		obj, ok := generic[section].(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object: %v", section, generic[section])
		}
		for _, k := range keys {
			v, present := obj[k]
			if !present {
				t.Errorf("%s.%s is missing", section, k)
			}
			if v == nil {
				t.Errorf("%s.%s is null, want a value or []", section, k)
			}
		}
	}
	git := generic["dotfile"].(map[string]any)["git"].(map[string]any)
	for _, k := range []string{"remote_url", "branch", "auto_commit", "auto_push", "pass_entry"} {
		if _, ok := git[k]; !ok {
			t.Errorf("dotfile.git.%s is missing", k)
		}
	}
	if generic["links"] == nil {
		t.Error("links is null, want []")
	}
}

// TestLinkWritesEveryKey: a link entry holds dir and sudo even when false, and
// no class.
func TestLinkWritesEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	cfg := Default()
	cfg.Links = []Link{{System: "/home/u/.bashrc", Original: "configs/dotfiles/bashrc"}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var generic struct {
		Links []map[string]any `json:"links"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	link := generic.Links[0]
	for _, k := range []string{"system", "original", "dir", "sudo"} {
		if _, ok := link[k]; !ok {
			t.Errorf("link.%s is missing: %v", k, link)
		}
	}
	if _, ok := link["class"]; ok {
		t.Errorf("link.class must not be written: %v", link)
	}
}

// TestRoundTripKeepsEveryValue: write then read gives back the same config.
func TestRoundTripKeepsEveryValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	cfg := Default()
	cfg.Editor = "code -w"
	cfg.Scratch.Tags = []string{"dns", "k8s"}
	cfg.Ignore.Muted = []string{"experiments/ml"}
	cfg.Pass.PassEntry = "git/github.com"
	cfg.Secret.SkipDirs = []string{"references/vendor"}
	cfg.Session.CapMB = 250
	cfg.Links = []Link{{System: "/etc/hosts", Original: "secrets/dotfiles/etc/hosts", Dir: false, Sudo: true}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip changed the config:\n got %+v\nwant %+v", got, cfg)
	}
}

// TestMissingKeysTakeDefaultsAndNextWriteFillsThem: a trimmed file loads with
// defaults in the gaps, and the next Save writes the full set.
func TestMissingKeysTakeDefaultsAndNextWriteFillsThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	trimmed := `{"schema": 1, "scratch": {"root_dir": "/data/scratch"}, "repo": {"max_parallel": 2}}`
	if err := os.WriteFile(path, []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scratch.RootDir != "/data/scratch" || cfg.Repo.MaxParallel != 2 {
		t.Fatalf("values already set must be kept: %+v", cfg)
	}
	def := Default()
	if cfg.Session != def.Session || cfg.Ignore.CritSizeMB != def.Ignore.CritSizeMB || cfg.Dotfile.Git.Branch != "main" {
		t.Fatalf("missing keys must take their defaults: %+v", cfg)
	}
	if !slices.Equal(cfg.Repo.Roots, []string{"."}) || !slices.Equal(cfg.Repo.ExcludeDirs, def.Repo.ExcludeDirs) {
		t.Fatalf("missing lists must take their defaults: roots=%v exclude=%v", cfg.Repo.Roots, cfg.Repo.ExcludeDirs)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if got := keysOf(t, raw); !slices.Equal(got, topLevelKeys) {
		t.Fatalf("the next write must fill every key: %v", got)
	}
	again, err := Load(path)
	if err != nil || !reflect.DeepEqual(again, cfg) {
		t.Fatalf("values changed while filling keys: %v\n%+v\n%+v", err, again, cfg)
	}
}

// TestOldLogKeyIsRead: ws.json written before the rename holds "log"; it is
// read, and only "session" is written back.
func TestOldLogKeyIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	old := `{"schema": 1, "log": {"cap_mb": 123, "max_session_mb": 45}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Session.CapMB != 123 || cfg.Session.MaxSessionMB != 45 {
		t.Fatalf("log must be read as session: %+v", cfg.Session)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), `"log"`) || !strings.Contains(string(raw), `"session"`) {
		t.Fatalf("only the new name is written:\n%s", raw)
	}

	// session wins when both are present.
	both := `{"schema": 1, "log": {"cap_mb": 1, "max_session_mb": 1}, "session": {"cap_mb": 9, "max_session_mb": 8}}`
	_ = os.WriteFile(path, []byte(both), 0o644)
	cfg, _ = Load(path)
	if cfg.Session.CapMB != 9 {
		t.Fatalf("session must win over log: %+v", cfg.Session)
	}
}

// TestEditorCmdAndClassAreNotReadOrWritten: scratch.editor_cmd and links[].class
// are gone (spec §2.3). A file that still has them loads, ignores them, and the
// next write drops them.
func TestEditorCmdAndClassAreNotReadOrWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	old := `{"schema": 1, "scratch": {"root_dir": "~/S", "editor_cmd": "emacs"},
	 "links": [{"system": "/home/u/.x", "original": "configs/dotfiles/x", "class": "secret"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor != "" {
		t.Fatalf("editor_cmd must not become the editor: %q", cfg.Editor)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, gone := range []string{"editor_cmd", `"class"`, "emacs"} {
		if strings.Contains(string(raw), gone) {
			t.Fatalf("%s must not be written:\n%s", gone, raw)
		}
	}
}

func TestConfigShowResolvesLinkOriginals(t *testing.T) {
	cfg := Default()
	cfg.Links = []Link{{System: "/home/u/.x", Original: "configs/dotfiles/x"}}
	got := WithAbsPaths(cfg, "/home/u/Workspace")
	if got.Links[0].Original != "/home/u/Workspace/configs/dotfiles/x" {
		t.Fatalf("link original not absolute: %q", got.Links[0].Original)
	}
	if cfg.Links[0].Original != "configs/dotfiles/x" {
		t.Fatal("WithAbsPaths must not modify its argument")
	}
}
