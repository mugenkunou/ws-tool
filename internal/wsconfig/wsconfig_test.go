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
var topLevelKeys = []string{"schema", "editor", "scratch", "repo", "secret", "pass", "ignore", "ignore_rules", "session", "symlinks"}

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
		"ignore_rules": {"schema", "exclude", "include", "suppress_defaults"},
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
	if generic["symlinks"] == nil {
		t.Error("symlinks is null, want []")
	}
}

// TestLinkWritesEveryKey: a link entry holds dir and sudo even when false, and
// no class.
func TestLinkWritesEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.json")
	cfg := Default()
	cfg.Symlinks = []Link{{System: "/home/u/.bashrc", Original: "symlinks/home/bashrc"}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var generic struct {
		Symlinks []map[string]any `json:"symlinks"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	link := generic.Symlinks[0]
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
	cfg.Symlinks = []Link{{System: "/etc/hosts", Original: "symlinks/system/etc/hosts", Dir: false, Sudo: true}}
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
	if cfg.Session != def.Session || cfg.Ignore.CritSizeMB != def.Ignore.CritSizeMB {
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

func TestConfigShowResolvesLinkOriginals(t *testing.T) {
	cfg := Default()
	cfg.Symlinks = []Link{{System: "/home/u/.x", Original: "symlinks/home/x"}}
	got := WithAbsPaths(cfg, "/home/u/Workspace")
	if got.Symlinks[0].Original != "/home/u/Workspace/symlinks/home/x" {
		t.Fatalf("link original not absolute: %q", got.Symlinks[0].Original)
	}
	if cfg.Symlinks[0].Original != "symlinks/home/x" {
		t.Fatal("WithAbsPaths must not modify its argument")
	}
}

// TestSymlinksKeyAndHomeForm: the registry is written under "symlinks", with ~ for
// $HOME, and read back absolute.
func TestSymlinksKeyAndHomeForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "ws.json")

	cfg := Default()
	cfg.Symlinks = []Link{
		{System: filepath.Join(home, ".gitconfig"), Original: "symlinks/home/.gitconfig"},
		{System: "/etc/hosts", Original: "symlinks/system/etc/hosts", Sudo: true},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"system": "~/.gitconfig"`) || !strings.Contains(string(raw), `"system": "/etc/hosts"`) || strings.Contains(string(raw), home) {
		t.Fatalf("inside $HOME is written with ~, outside as is:\n%s", raw)
	}
	got, err := Load(path)
	if err != nil || got.Symlinks[0].System != filepath.Join(home, ".gitconfig") || got.Symlinks[1].System != "/etc/hosts" {
		t.Fatalf("read back absolute: %v %+v", err, got.Symlinks)
	}
	if cfg.Symlinks[0].System != filepath.Join(home, ".gitconfig") {
		t.Fatal("Save must not modify its argument")
	}
}
