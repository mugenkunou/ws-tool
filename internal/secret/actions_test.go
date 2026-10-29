package secret

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/pass"
	"github.com/mugenkunou/ws-tool/internal/wsconfig"
)

func newConfig(t *testing.T) (ws, cfgPath string) {
	t.Helper()
	ws = filepath.Join(t.TempDir(), "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(ws, "ws.json")
	if err := wsconfig.Save(cfgPath, wsconfig.Default()); err != nil {
		t.Fatal(err)
	}
	return ws, cfgPath
}

func TestParseAnchor(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	home := t.TempDir()
	t.Setenv("HOME", home)

	rel, line, err := ParseAnchor(ws, filepath.Join(ws, "configs", "a.properties")+":3")
	if err != nil || rel != "configs/a.properties" || line != 3 {
		t.Fatalf("absolute: %q %d %v", rel, line, err)
	}
	// ~ resolves against HOME.
	t.Setenv("HOME", filepath.Dir(ws))
	if rel, _, err = ParseAnchor(ws, "~/Workspace/x/y.env:12"); err != nil || rel != "x/y.env" {
		t.Fatalf("~: %q %v", rel, err)
	}
	// Relative to the current directory.
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(filepath.Join(ws, "sub")); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	if rel, _, err = ParseAnchor(ws, "./f.txt:1"); err != nil || rel != "sub/f.txt" {
		t.Fatalf("relative: %q %v", rel, err)
	}
	// A path with a colon in it keeps everything before the last one.
	if rel, line, err = ParseAnchor(ws, "./a:b.txt:7"); err != nil || rel != "sub/a:b.txt" || line != 7 {
		t.Fatalf("colon in the name: %q %d %v", rel, line, err)
	}

	for _, bad := range []string{"", "file", ":3", "file:", "file:0", "file:-1", "file:x", "/etc/passwd:1", ws + ":1"} {
		if _, _, err := ParseAnchor(ws, bad); err == nil {
			t.Errorf("ParseAnchor(%q) should fail", bad)
		}
	}
}

func TestFindingAtAndAnchors(t *testing.T) {
	vs := []Violation{{Path: "a.env", Line: 1}, {Path: "b/c.env", Line: 4}}
	if v, ok := FindingAt(vs, "b/c.env", 4); !ok || v.Line != 4 {
		t.Fatal("finding not found")
	}
	if _, ok := FindingAt(vs, "b/c.env", 5); ok {
		t.Fatal("line 5 is no finding")
	}
	if got := Anchor(vs[1]); got != "b/c.env:4" {
		t.Fatalf("Anchor = %q", got)
	}
	if got := AbsAnchor("/ws", vs[1]); got != "/ws/b/c.env:4" {
		t.Fatalf("AbsAnchor = %q", got)
	}
}

func TestAllowActionsOrderAndStaleDrop(t *testing.T) {
	ws, cfg := newConfig(t)
	findings := []Violation{{Path: "a.env", Line: 1}, {Path: "b.env", Line: 2}}
	acts := AllowActions(cfg, ws, findings, []string{"gone:1", "gone:2"})
	if len(acts) != 3 {
		t.Fatalf("want 2 allow actions and one drop, got %d", len(acts))
	}
	if !strings.HasPrefix(acts[0].Description, "Allow "+ws+"/a.env:1") || acts[2].Description != "Drop 2 stale anchors from secret.allowlist" {
		t.Fatalf("descriptions: %q, %q", acts[0].Description, acts[2].Description)
	}
	if one := AllowActions(cfg, ws, nil, []string{"gone:1"}); one[0].Description != "Drop 1 stale anchor from secret.allowlist" {
		t.Fatalf("singular: %q", one[0].Description)
	}

	// Seed the stale ones, run everything, only the new anchors remain.
	c, _ := wsconfig.Load(cfg)
	c.Secret.Allowlist = []string{"gone:1", "gone:2"}
	if err := wsconfig.Save(cfg, c); err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if err := a.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	c, _ = wsconfig.Load(cfg)
	if !slices.Equal(c.Secret.Allowlist, []string{"a.env:1", "b.env:2"}) {
		t.Fatalf("allowlist: %v", c.Secret.Allowlist)
	}
}

func TestStaleAnchors(t *testing.T) {
	ws, _ := newConfig(t)
	if err := os.MkdirAll(filepath.Join(ws, "vendor", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(ws, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.env", "x=1\npassword=hunter2\n")
	write("vendor/lib/v.env", "password=hunter2\n")

	opts := ScanOptions{WorkspacePath: ws, SkipDirs: []string{"vendor"}}
	allow := []string{
		"a.env:2",            // still a finding: current
		"a.env:1",            // no pattern matches that line: stale
		"deleted.env:3",      // the file is gone: stale
		"vendor/lib/v.env:1", // in a muted directory: out of sight, kept
		"vendor:1",           // a file named like a muted directory: kept
		"nocolon",            // malformed: stale
	}
	stale, err := StaleAnchors(opts, allow)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(stale)
	if want := []string{"a.env:1", "deleted.env:3", "nocolon"}; !slices.Equal(stale, want) {
		t.Fatalf("stale = %v, want %v", stale, want)
	}
	if got, _ := StaleAnchors(opts, nil); got != nil {
		t.Fatalf("an empty allowlist has nothing stale: %v", got)
	}
}

func TestSkippedBy(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "Workspace")
	t.Setenv("HOME", filepath.Dir(ws))
	dirs := []string{"configs", filepath.Join(ws, "abs/dir"), "~/Workspace/tilde"}

	for rel, want := range map[string]struct {
		by    string
		exact bool
		ok    bool
	}{
		"configs":       {"configs", true, true},
		"configs/app":   {"configs", false, true},
		"abs/dir":       {"abs/dir", true, true},
		"abs/dir/x/y":   {"abs/dir", false, true},
		"tilde":         {"tilde", true, true},
		"tilde/deeper":  {"tilde", false, true},
		"configs2":      {"", false, false},
		"other/configs": {"", false, false},
	} {
		by, exact, ok := SkippedBy(ws, dirs, rel)
		if by != want.by || exact != want.exact || ok != want.ok {
			t.Errorf("SkippedBy(%q) = %q %v %v, want %+v", rel, by, exact, ok, want)
		}
	}
}

func TestSkipUnskipActions(t *testing.T) {
	ws, cfg := newConfig(t)
	home := filepath.Dir(ws)
	t.Setenv("HOME", home)
	if err := SkipAction(cfg, ws, "vendor").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := SkipAction(cfg, ws, "vendor").Execute(); err != nil { // idempotent
		t.Fatal(err)
	}
	c, _ := wsconfig.Load(cfg)
	if !slices.Equal(c.Secret.SkipDirs, []string{"vendor"}) {
		t.Fatalf("skip_dirs: %v", c.Secret.SkipDirs)
	}
	c.Secret.SkipDirs = []string{"vendor", filepath.Join(ws, "vendor"), "~/Workspace/vendor", "other"}
	if err := wsconfig.Save(cfg, c); err != nil {
		t.Fatal(err)
	}
	if a := UnskipAction(cfg, ws, "vendor"); a.Description != "Scan "+filepath.Join(ws, "vendor")+" again" {
		t.Fatalf("description %q", a.Description)
	} else if err := a.Execute(); err != nil {
		t.Fatal(err)
	}
	c, _ = wsconfig.Load(cfg)
	if !slices.Equal(c.Secret.SkipDirs, []string{"other"}) {
		t.Fatalf("only the other entry should stay: %v", c.Secret.SkipDirs)
	}
}

func TestMuteUnmuteActions(t *testing.T) {
	ws, cfg := newConfig(t)
	v := Violation{Path: "a.env", Line: 1}
	if err := MuteActions(cfg, ws, []Violation{v})[0].Execute(); err != nil {
		t.Fatal(err)
	}
	if err := MuteActions(cfg, ws, []Violation{v})[0].Execute(); err != nil { // idempotent
		t.Fatal(err)
	}
	c, _ := wsconfig.Load(cfg)
	if !slices.Equal(c.Secret.Muted, []string{"a.env:1"}) {
		t.Fatalf("muted: %v", c.Secret.Muted)
	}
	if err := UnmuteActions(cfg, ws, []Violation{v})[0].Execute(); err != nil {
		t.Fatal(err)
	}
	c, _ = wsconfig.Load(cfg)
	if len(c.Secret.Muted) != 0 {
		t.Fatalf("muted after unmute: %v", c.Secret.Muted)
	}
}

func TestScanFindsPrivateKeyHeaders(t *testing.T) {
	ws, _ := newConfig(t)
	if err := os.WriteFile(filepath.Join(ws, "id.txt"), []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-- password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, err := Scan(ScanOptions{WorkspacePath: ws})
	if err != nil {
		t.Fatal(err)
	}
	// The header is a finding; an SQL-style "--" comment still is not.
	if len(vs) != 1 || vs[0].Line != 1 {
		t.Fatalf("findings: %+v", vs)
	}
}

func TestAuditMessagesNameCurrentCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PASSWORD_STORE_DIR", filepath.Join(dir, "none"))
	for _, f := range pass.AuditPassStore(pass.CheckPass()).Findings {
		for _, old := range []string{"ws secret setup", "secret git", "pass git"} {
			if strings.Contains(f.Message, old) {
				t.Errorf("audit message %q names the old command %q", f.Message, old)
			}
		}
	}
}
