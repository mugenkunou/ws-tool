package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// passShims is a stand-in machine for the pass commands: shell shims for pass
// and gpg on a temp PATH, a temp password store, a temp GNUPGHOME and a temp
// global git config. Nothing here touches the real store, keyring or git
// config (the real pass once overwrote entries; tests never run it).
type passShims struct {
	bin   string // dir of the shims, first on PATH
	store string // PASSWORD_STORE_DIR
	log   string // PASS_SHIM_LOG: one line per pass call
	keys  string // GPG_SHIM_KEYS: the colon listing gpg prints
	home  string
	gitCf string // GIT_CONFIG_GLOBAL
}

const passShim = `#!/bin/sh
# Test stand-in for pass(1). Entries are plain files named <entry>.gpg.
store="${PASSWORD_STORE_DIR:?}"
echo "$*" >> "${PASS_SHIM_LOG:?}"
case "$1" in
init)
	mkdir -p "$store"
	echo "$2" > "$store/.gpg-id"
	;;
git)
	shift
	if [ "$1" = init ]; then
		git init -q -b main "$store"
	else
		git -C "$store" "$@"
	fi
	;;
insert)
	shift
	[ "$1" = "-m" ] && shift
	name="$1"
	f="$store/$name.gpg"
	if [ -e "$f" ]; then
		echo "pass shim: $name already exists" >&2
		exit 1
	fi
	mkdir -p "$(dirname "$f")"
	cat > "$f"
	;;
show)
	cat "$store/$2.gpg"
	;;
*)
	echo "pass shim: unsupported: $*" >&2
	exit 1
	;;
esac
`

const gpgShim = `#!/bin/sh
# Test stand-in for gpg: only the secret key listing.
case "$*" in
*--list-secret-keys*) cat "${GPG_SHIM_KEYS:?}" ;;
*) exit 1 ;;
esac
`

// gitCmd is a git command. Tests only point it at temp files and repositories,
// with the temp global config of newPassShims.
func gitCmd(args ...string) *exec.Cmd { return exec.Command("git", args...) }

// setGlobal sets a key in the temp global git config.
func (s *passShims) setGlobal(key, value string) error {
	out, err := gitCmd("config", "--file", s.gitCf, key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config --file %s %s: %v: %s", s.gitCf, key, err, out)
	}
	return nil
}

// newPassShims puts the shims on PATH and points every pass-related variable
// at temp locations. The store is not initialized until initStore.
func newPassShims(t *testing.T) *passShims {
	t.Helper()
	root := t.TempDir()
	s := &passShims{
		bin:   filepath.Join(root, "bin"),
		store: filepath.Join(root, "home", ".password-store"),
		log:   filepath.Join(root, "pass.log"),
		keys:  filepath.Join(root, "keys.txt"),
		home:  filepath.Join(root, "home"),
		gitCf: filepath.Join(root, "gitconfig"),
	}
	for _, d := range []string{s.bin, s.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"pass": passShim, "gpg": gpgShim} {
		if err := os.WriteFile(filepath.Join(s.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(s.gitCf, []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.setKeys(t, "3AA5C34371567BD2:Ada <ada@example.com>")
	if err := os.WriteFile(s.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run from a plain directory: a git command in a worktree checkout whose
	// .git file points outside the test container dies before it reads config.
	chdir(t, root)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root)) // never look above the test's temp dirs

	t.Setenv("PATH", s.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", s.home)
	t.Setenv("PASSWORD_STORE_DIR", s.store)
	t.Setenv("PASS_SHIM_LOG", s.log)
	t.Setenv("GPG_SHIM_KEYS", s.keys)
	t.Setenv("GNUPGHOME", filepath.Join(root, "gnupg"))
	t.Setenv("GIT_CONFIG_GLOBAL", s.gitCf)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("WS_CRONTAB_FILE", filepath.Join(root, "crontab"))
	if err := os.WriteFile(filepath.Join(root, "crontab"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

// setKeys sets the GPG secret keys the gpg shim lists, as "ID:Name" pairs.
func (s *passShims) setKeys(t *testing.T, keys ...string) {
	t.Helper()
	var b strings.Builder
	for _, k := range keys {
		id, name, _ := strings.Cut(k, ":")
		b.WriteString("sec:u:255:22:" + id + ":1:::::\n")
		b.WriteString("uid:u::::1::HASH::" + name + ":\n")
	}
	if err := os.WriteFile(s.keys, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initStore makes the store initialized (a .gpg-id) without git.
func (s *passShims) initStore(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(s.store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.store, ".gpg-id"), []byte("3AA5C34371567BD2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initGit makes the store a git repository on main, as pass git init does.
func (s *passShims) initGit(t *testing.T) {
	t.Helper()
	s.git(t, s.store, "init", "-q", "-b", "main")
	s.git(t, s.store, "config", "user.email", "t@example.com")
	s.git(t, s.store, "config", "user.name", "t")
}

// entry writes a pass entry (the shim's plain file stands in for the .gpg).
func (s *passShims) entry(t *testing.T, name, content string) {
	t.Helper()
	p := filepath.Join(s.store, name+".gpg")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// calls is every pass invocation so far, one line each.
func (s *passShims) calls(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(s.log)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// git runs the real git in dir (a temp repository, with the temp config).
func (s *passShims) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if args[0] == "init" { // -C needs the directory to exist
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// bareRemote makes a bare repository and returns its path (a local "remote").
func (s *passShims) bareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}
