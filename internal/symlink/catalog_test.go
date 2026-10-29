package symlink

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// neverSuggested is the list in docs/plans/0003/catalog.md ("Never suggested").
// whole means the folder itself may not be listed, but its children may (the
// catalog lists the safe ones). A glob is matched with path.Match.
var neverSuggested = []struct {
	path  string
	whole bool
}{
	{"~/.ssh", true}, {"~/.ssh/known_hosts*", false}, {"~/.ssh/authorized_keys", false},
	{"~/.claude", true}, {"~/.claude/.credentials.json", false}, {"~/.claude.json", false},
	{"~/.config/Code", true}, {"~/.config/Code - Insiders", true}, {"~/.config/Cursor", true},
	{"~/.gnupg", true}, {"~/.gnupg/private-keys-v1.d", false}, {"~/.gnupg/trustdb.gpg", false},
	{"~/.password-store", false},
	{"~/.docker", true}, {"~/.config/gh", true}, {"~/.config/gcloud", true}, {"~/.aws", true}, {"~/.kube", true},
	{"~/.copilot", true}, {"~/.continue", true}, {"~/.codex", true}, {"~/.gemini", true},
	{"~/.codex/auth.json", false}, {"~/.gemini/oauth_creds.json", false},
	{"~/.config/atuin/key", false}, {"~/.local/share/atuin", false},
	{"~/.cache", false}, {"~/.npm", false}, {"~/.cargo/registry", false}, {"~/.rustup", false},
	{"~/.m2/repository", false}, {"~/.gradle", true}, {"~/.local/share", false},
	{"~/.mozilla", false}, {"~/.config/chromium", false}, {"~/.config/google-chrome*", false}, {"~/.config/BraveSoftware", false},
	{"~/.vim", true}, {"~/.emacs.d", true}, {"~/.oh-my-zsh", true},
	{"~/.zcompdump*", false}, {"~/.zsh_sessions", false},
	{"/etc/passwd", false}, {"/etc/shadow", false}, {"/etc/fstab", false}, {"/etc/sudoers*", false}, {"/etc/ssh/*", false},
	{"~/.Xauthority", false}, {"~/.ICEauthority", false}, {"~/.pam_environment", false},
}

// neverNames are basename patterns: history files and backup or temp debris.
var neverNames = []string{"*history*", "*.bak*", "*.ws-backup", "*.pre-restore*", "*.tmp", "*.swp"}

// mixedFolders may never be a folder row.
var mixedFolders = []string{"~/.ssh", "~/.claude", "~/.config/Code", "~/.config/Code - Insiders", "~/.config/Cursor", "~/.gnupg", "~/.docker"}

// sample replaces each * with a letter, so a glob row becomes a concrete path.
func sample(p string) string { return strings.ReplaceAll(p, "*", "x") }

func TestCatalogNeverSuggests(t *testing.T) {
	for _, it := range Catalog {
		s := sample(it.Path)
		for _, n := range neverSuggested {
			// equal, or inside a never path
			if !n.whole || it.Path == n.path {
				for p := s; p != "~" && p != "/" && p != "."; p = path.Dir(p) {
					if ok, _ := path.Match(n.path, p); ok {
						t.Errorf("%s (%s) equals or lies inside the never-suggested %s", it.Path, it.App, n.path)
						break
					}
				}
			}
			// a parent of a never path
			if !strings.Contains(n.path, "*") && strings.HasPrefix(n.path, s+"/") {
				t.Errorf("%s (%s) is a parent of the never-suggested %s", it.Path, it.App, n.path)
			}
			if ok, _ := path.Match(n.path, s); ok && n.whole {
				t.Errorf("%s (%s) is the never-suggested whole folder %s", it.Path, it.App, n.path)
			}
		}
		for _, part := range strings.Split(s, "/") {
			for _, pat := range neverNames {
				if ok, _ := path.Match(pat, part); ok {
					t.Errorf("%s (%s): %q matches the never-suggested name %s", it.Path, it.App, part, pat)
				}
			}
		}
	}
}

func TestCatalogShape(t *testing.T) {
	seen := map[string]string{}
	for i, it := range Catalog {
		if !strings.HasPrefix(it.Path, "~/") && !strings.HasPrefix(it.Path, "/") {
			t.Errorf("%s must start with ~/ or /", it.Path)
		}
		if it.App == "" || (it.Kind != KindFile && it.Kind != KindFolder) {
			t.Errorf("incomplete row: %+v", it)
		}
		if it.OS != OSAny && it.OS != OSLinux && it.OS != OSMac {
			t.Errorf("%s: os %q", it.Path, it.OS)
		}
		if _, err := filepath.Match(it.Path, "x"); err != nil {
			t.Errorf("%s: the glob does not compile: %v", it.Path, err)
		}
		// no duplicate path per OS ("any" overlaps every OS)
		for _, os := range []string{OSLinux, OSMac} {
			if it.OS != OSAny && it.OS != os {
				continue
			}
			key := os + "|" + it.Path
			if prev, dup := seen[key]; dup {
				t.Errorf("%s is listed twice for %s (%s and %s)", it.Path, os, prev, it.App)
			}
			seen[key] = it.App
		}
		if it.Secret && it.Kind != KindFile {
			t.Errorf("%s: a secret row is a file, not a folder", it.Path)
		}
		if strings.HasPrefix(it.Path, "/etc/") && !it.Sudo {
			t.Errorf("%s is under /etc and needs Sudo", it.Path)
		}
		if it.Sudo && !strings.HasPrefix(it.Path, "/") {
			t.Errorf("%s needs Sudo but is inside $HOME", it.Path)
		}
		if it.Kind == KindFolder {
			for _, m := range mixedFolders {
				if it.Path == m {
					t.Errorf("%s is a mixed folder and may not be listed whole", it.Path)
				}
			}
		}
		if i > 0 {
			p := Catalog[i-1]
			// ~/ paths sort before absolute ones (catalog.md lists them first).
			key := func(i Item) string {
				return strings.ToLower(i.App) + "\x00" + strings.Replace(strings.ToLower(i.Path), "~/", "\x01/", 1)
			}
			a, b := key(p), key(it)
			if a > b {
				t.Errorf("not sorted by app then path: %s %s before %s %s", p.App, p.Path, it.App, it.Path)
			}
		}
	}
	if len(Catalog) < 130 || len(Catalog) > 160 {
		t.Errorf("about 130 rows expected, have %d", len(Catalog))
	}
}
