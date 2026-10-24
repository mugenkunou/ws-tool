package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run is wsRun without a workspace: the pass commands need none.
func passRun(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	return runPlumbing(t, stdin, args...)
}

// ── status ──

func TestPassStatusNotInitialized(t *testing.T) {
	newPassShims(t)
	code, out, errOut := passRun(t, "", "pass", "status", "--no-color")
	if code != 0 || errOut != "" {
		t.Fatalf("a missing piece is reported, not an error: code=%d err=%q", code, errOut)
	}
	for _, want := range []string{"[ok] gpg available", "[ok] pass installed", "[err] store initialized", "[err] git-backed", "Next: ws pass init"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestPassStatusHintsFollowTheFirstMissingPiece(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	_, out, _ := passRun(t, "", "pass", "status", "--no-color")
	for _, want := range []string{
		"[warn] no remote",
		"secrets live only on this machine",
		"Next: ws pass backup enable --remote <url>   (use a private repository)",
		"[err] credential helper  not connected",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}

	shims.git(t, shims.store, "remote", "add", "origin", "git@github.com:me/pass.git")
	_, out, _ = passRun(t, "", "pass", "status", "--no-color")
	if !strings.Contains(out, "[ok] remote") || !strings.Contains(out, "git@github.com:me/pass.git") {
		t.Fatalf("the remote URL is shown:\n%s", out)
	}
	if !strings.Contains(out, "Next: ws credential enable") {
		t.Fatalf("the credential helper is next:\n%s", out)
	}
}

func TestPassStatusLayout(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	shims.entry(t, "a", "x")
	shims.entry(t, "b", "x")
	_, out, _ := passRun(t, "", "pass", "status", "--no-color")
	for _, want := range []string{
		"[ok] store initialized  " + shims.store + ", 2 entries",
		fmt.Sprintf("[warn] %-17s  secrets live only on this machine", "no remote"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestPassStatusJSONKeys(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	env := runJSON(t, t.TempDir(), []string{"pass", "status"})
	if env.Command != "pass.status" || env.Schema != 3 {
		t.Fatalf("envelope %+v", env)
	}
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	want := []string{"gpg_available", "pass_installed", "initialized", "store_path", "entry_count",
		"git_backed", "git_remote", "remote_url", "cred_helper", "cred_helper_value"}
	if len(data) != len(want) {
		t.Fatalf("keys %v, want exactly %v", data, want)
	}
	for _, k := range want {
		if _, ok := data[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if data["initialized"] != true || data["store_path"] != shims.store || data["cred_helper"] != false {
		t.Fatalf("data: %v", data)
	}
}

func TestPassStatusWorksWithoutAWorkspace(t *testing.T) {
	newPassShims(t)
	missing := filepath.Join(t.TempDir(), "no-workspace")
	code, _, errOut := passRun(t, "", "--workspace", missing, "pass", "status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestPassStatusWorkspaceCopyTip(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	workspace := initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store)
	t.Setenv("PATH", shims.bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// None: an info line and the command, never an error.
	code, out, _ := passRun(t, "", "--workspace", workspace, "pass", "status", "--no-color")
	want := "Tip: ws symlink add ~/.password-store --dir"
	if code != 0 || !strings.Contains(out, "[i] workspace copy") || !strings.Contains(out, "none: the store syncs only through its git remote") || !strings.Contains(out, want) {
		t.Fatalf("code=%d, want the info line and %q:\n%s", code, want, out)
	}
	if strings.Contains(out, "[warn] workspace") || strings.Contains(out, "[err] workspace") {
		t.Fatalf("the workspace copy is advice, never a warning:\n%s", out)
	}

	// The store moved into the workspace and linked back (symlink add --dir).
	target := filepath.Join(workspace, "symlinks", "home", ".password-store")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(shims.store, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, shims.store); err != nil {
		t.Fatal(err)
	}
	_, out, _ = passRun(t, "", "--workspace", workspace, "pass", "status", "--no-color")
	if !strings.Contains(out, "[ok] workspace copy") || !strings.Contains(out, target) || strings.Contains(out, "Tip:") {
		t.Fatalf("a linked store reads as a copy, with no tip:\n%s", out)
	}
}

// ── audit ──

func TestPassAudit(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	shims.entry(t, "git/github.com", "short") // under 450 bytes: likely a token alone
	big := strings.Repeat("x", 500)
	shims.entry(t, "git/gitlab.com", big)
	shims.entry(t, "web/site", "short") // only git/ entries are audited

	code, out, _ := passRun(t, "", "pass", "audit", "--no-color")
	if code != 0 {
		t.Fatalf("findings are not an error: exit %d", code)
	}
	if !strings.Contains(out, "[warn] git/github.com  entry may lack metadata: add `username: <user>` on line 2") {
		t.Fatalf("entry finding:\n%s", out)
	}
	if !strings.Contains(out, "[warn] pass store has no git remote") {
		t.Fatalf("store-wide finding:\n%s", out)
	}
	if strings.Contains(out, "gitlab.com") || strings.Contains(out, "web/site") {
		t.Fatalf("only short git/ entries are findings:\n%s", out)
	}

	env := runJSON(t, t.TempDir(), []string{"pass", "audit"})
	if env.Command != "pass.audit" {
		t.Fatalf("command %q", env.Command)
	}
	var data struct {
		Findings []struct {
			Entry   string `json:"entry"`
			Message string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || len(data.Findings) != 2 {
		t.Fatalf("data: %s (%v)", env.Data, err)
	}
	if data.Findings[0].Entry != "" || data.Findings[1].Entry != "git/github.com" {
		t.Fatalf("store-wide findings have an empty entry: %+v", data.Findings)
	}
}

func TestPassAuditCleanStore(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	shims.git(t, shims.store, "remote", "add", "origin", "x")
	shims.entry(t, "web/site", "short")
	_, out, _ := passRun(t, "", "pass", "audit", "--no-color")
	if !strings.Contains(out, "No problems") {
		t.Fatalf("out %q", out)
	}
}

func TestPassAuditNeverDecrypts(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.entry(t, "git/github.com", "short")
	passRun(t, "", "pass", "audit")
	if strings.Contains(shims.calls(t), "show") {
		t.Fatalf("audit must not run pass show: %q", shims.calls(t))
	}
}

// ── init ──

func TestPassInitPlanAndResult(t *testing.T) {
	shims := newPassShims(t)
	code, out, errOut := passRun(t, "", "pass", "init", "--dry-run", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		"Initialize the pass store " + shims.store + " with GPG key 3AA5C34371567BD2",
		"Turn on git versioning for the pass store",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(shims.store); err == nil {
		t.Fatal("--dry-run created the store")
	}

	code, out, errOut = passRun(t, "", "pass", "init", "--quiet")
	if code != 0 {
		t.Fatalf("init: %d %s%s", code, out, errOut)
	}
	if got := read(t, filepath.Join(shims.store, ".gpg-id")); strings.TrimSpace(got) != "3AA5C34371567BD2" {
		t.Fatalf(".gpg-id: %q", got)
	}
	if _, err := os.Stat(filepath.Join(shims.store, ".git")); err != nil {
		t.Fatalf("the store should be git-backed: %v", err)
	}
	calls := shims.calls(t)
	if !strings.Contains(calls, "init 3AA5C34371567BD2") || !strings.Contains(calls, "git init") {
		t.Fatalf("pass calls: %q", calls)
	}
}

func TestPassInitIsIdempotent(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	code, out, _ := passRun(t, "", "pass", "init", "--no-color")
	if code != 0 || !strings.Contains(out, "already set up") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	env := runJSON(t, t.TempDir(), []string{"pass", "init"})
	var data struct {
		Actions []ActionStatus `json:"actions"`
		Pass    map[string]any `json:"pass"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || env.Command != "pass.init" || len(data.Actions) != 0 || data.Pass["initialized"] != true {
		t.Fatalf("envelope %s (%v)", env.Data, err)
	}
	if strings.Contains(shims.calls(t), "init") {
		t.Fatalf("nothing to do must call nothing: %q", shims.calls(t))
	}
}

func TestPassInitOnlyAddsGitToAnExistingStore(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	_, out, _ := passRun(t, "", "pass", "init", "--dry-run", "--no-color")
	if strings.Contains(out, "Initialize the pass store") || !strings.Contains(out, "Turn on git versioning") {
		t.Fatalf("plan:\n%s", out)
	}
}

func TestPassInitRequirements(t *testing.T) {
	shims := newPassShims(t)

	// No gpg.
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "pass"), []byte(passShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", only)
	code, _, errOut := passRun(t, "", "pass", "init")
	if code != 1 || !strings.Contains(errOut, "gnupg") {
		t.Fatalf("no gpg: code=%d err=%q", code, errOut)
	}

	// No pass.
	only = t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "gpg"), []byte(gpgShim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", only)
	code, _, errOut = passRun(t, "", "pass", "init")
	if code != 1 || !strings.Contains(errOut, "package pass") {
		t.Fatalf("no pass: code=%d err=%q", code, errOut)
	}

	// No secret key.
	t.Setenv("PATH", shims.bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	shims.setKeys(t)
	code, _, errOut = passRun(t, "", "pass", "init")
	if code != 1 || !strings.Contains(errOut, "gpg --full-generate-key") {
		t.Fatalf("no key: code=%d err=%q", code, errOut)
	}
	if _, err := os.Stat(shims.store); err == nil {
		t.Fatal("nothing should be created")
	}
}

func TestPassInitSeveralKeys(t *testing.T) {
	shims := newPassShims(t)
	shims.setKeys(t, "AAAA1111:First <a@x>", "BBBB2222:Second <b@x>")

	// Asks for a number.
	code, out, errOut := passRun(t, "2\n", "pass", "init", "--no-color", "--dry-run")
	if code != 0 || !strings.Contains(out, "1) AAAA1111") || !strings.Contains(out, "2) BBBB2222") || !strings.Contains(out, "with GPG key BBBB2222") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	// An answer that is not a key number stops.
	if code, _, e := passRun(t, "9\n", "pass", "init", "--dry-run"); code != 1 || !strings.Contains(e, "between 1 and 2") {
		t.Fatalf("bad number: %d %q", code, e)
	}
	// --quiet and --json take the first and say which, on stderr.
	for _, flag := range []string{"--quiet", "--json"} {
		code, out, errOut = passRun(t, "", "pass", "init", flag, "--dry-run")
		if code != 0 || !strings.Contains(errOut, "Using GPG key AAAA1111") {
			t.Fatalf("%s: code=%d err=%q", flag, code, errOut)
		}
		if flag == "--json" {
			var env jsonEnvelope
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("stdout must stay the envelope: %v\n%s", err, out)
			}
		}
	}
}

func TestPassInitHiddenGitRemote(t *testing.T) {
	shims := newPassShims(t)
	remote := shims.bareRemote(t)
	_, out, _ := passRun(t, "", "pass", "init", "--git-remote", remote, "--dry-run", "--no-color")
	if !strings.Contains(out, "Set the backup remote to "+remote) {
		t.Fatalf("the hidden flag adds the remote action:\n%s", out)
	}
	if code, _, e := passRun(t, "", "pass", "init", "--git-remote", remote, "--quiet"); code != 0 {
		t.Fatalf("init: %d %s", code, e)
	}
	if got := shims.git(t, shims.store, "remote", "get-url", "origin"); strings.TrimSpace(got) != remote {
		t.Fatalf("origin = %q", got)
	}
}

func TestPassInitWorksWithoutAWorkspace(t *testing.T) {
	newPassShims(t)
	missing := filepath.Join(t.TempDir(), "none")
	if code, _, e := passRun(t, "", "--workspace", missing, "pass", "init", "--quiet"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
}

func TestSecretSetupGitRemoteAlias(t *testing.T) {
	shims := newPassShims(t)
	remote := shims.bareRemote(t)
	workspace := initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store)
	_, out, errOut := passRun(t, "", "--workspace", workspace, "secret", "setup", "--git-remote", remote, "--dry-run")
	if !strings.Contains(errOut, `"secret setup" is now "pass init"`) || !strings.Contains(out, "Set the backup remote to "+remote) {
		t.Fatalf("out=%q err=%q", out, errOut)
	}
}

// ── backup ──

func TestPassBackupEnableNeedsAnInitializedStore(t *testing.T) {
	newPassShims(t)
	code, _, errOut := passRun(t, "", "pass", "backup", "enable", "--remote", "x", "--quiet")
	if code != 1 || !strings.Contains(errOut, "ws pass init") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPassBackupEnableNeedsARemote(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	for _, flag := range []string{"--quiet", "--json"} {
		code, _, errOut := passRun(t, "", "pass", "backup", "enable", flag)
		if code != 1 || !strings.Contains(errOut, "--remote <url> is required") {
			t.Errorf("%s: code=%d err=%q", flag, code, errOut)
		}
	}
	// A blank answer to the prompt exits 1 too.
	code, out, errOut := passRun(t, "\n", "pass", "backup", "enable", "--no-color")
	if code != 1 || !strings.Contains(out, "Remote URL of a PRIVATE repository") || !strings.Contains(errOut, "a remote is required") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPassBackupEnablePromptsForTheURL(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	code, out, errOut := passRun(t, remote+"\ny\n", "pass", "backup", "enable", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d out=%q err=%q", code, out, errOut)
	}
	if got := strings.TrimSpace(shims.git(t, shims.store, "remote", "get-url", "origin")); got != remote {
		t.Fatalf("origin = %q", got)
	}
}

func TestPassBackupEnablePlanAndHints(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	remote := shims.bareRemote(t)

	_, out, _ := passRun(t, "", "pass", "backup", "enable", "--remote", remote, "--dry-run", "--no-color")
	for _, want := range []string{"Turn on git versioning for the pass store", "Set the backup remote to " + remote} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(shims.store, ".git")); err == nil {
		t.Fatal("--dry-run turned on git")
	}

	code, out, errOut := passRun(t, "y\ny\n", "pass", "backup", "enable", "--remote", remote, "--no-color")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"ws pass backup push", "ws cron add pass-backup", "pushes every 30 minutes"} {
		if !strings.Contains(out, want) {
			t.Errorf("the closing hints lack %q:\n%s", want, out)
		}
	}
	// enable does not push.
	if got := shims.git(t, remote, "branch", "-a"); strings.TrimSpace(got) != "" {
		t.Fatalf("enable pushed: %q", got)
	}

	code, out, _ = passRun(t, "", "pass", "backup", "enable", "--remote", remote, "--no-color")
	if code != 0 || !strings.Contains(out, "already enabled") {
		t.Fatalf("second enable: code=%d out=%q", code, out)
	}
}

func TestPassBackupEnableUsesTheExistingOrigin(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	shims.git(t, shims.store, "remote", "add", "origin", remote)
	code, out, _ := passRun(t, "", "pass", "backup", "enable", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	// Changing the URL sets origin (it used to fail on `remote add`).
	other := shims.bareRemote(t)
	if code, _, e := passRun(t, "", "pass", "backup", "enable", "--remote", other, "--quiet"); code != 0 {
		t.Fatalf("set-url: %d %s", code, e)
	}
	if got := strings.TrimSpace(shims.git(t, shims.store, "remote", "get-url", "origin")); got != other {
		t.Fatalf("origin = %q, want %q", got, other)
	}
}

func TestPassBackupPassEntryIsKeptInWsJSON(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	workspace := initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store)
	remote := shims.bareRemote(t)
	code, _, errOut := passRun(t, "", "--workspace", workspace, "pass", "backup", "enable", "--remote", remote, "--pass-entry", "git/me", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	cfg := wsConfig(t, workspace)
	if got := cfg["pass"].(map[string]any)["pass_entry"]; got != "git/me" {
		t.Fatalf("pass.pass_entry = %v", got)
	}
	env := runJSON(t, workspace, []string{"pass", "backup", "status"})
	var data map[string]any
	_ = json.Unmarshal(env.Data, &data)
	if data["pass_entry"] != "git/me" {
		t.Fatalf("status data: %v", data)
	}
}

func TestPassBackupStatusLayout(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	shims.entry(t, "git/github.com", "tok")
	shims.entry(t, "aws/prod", "tok")
	shims.git(t, shims.store, "add", "-A")
	shims.git(t, shims.store, "commit", "-q", "-m", "Add given password for git/github.com to store.")

	_, out, _ := passRun(t, "", "pass", "backup", "status", "--no-color")
	if !strings.Contains(out, "Backup        disabled") || !strings.Contains(out, "ws pass backup enable --remote <url>") {
		t.Fatalf("disabled:\n%s", out)
	}

	remote := shims.bareRemote(t)
	shims.git(t, shims.store, "remote", "add", "origin", remote)
	_, out, _ = passRun(t, "", "pass", "backup", "status", "--no-color")
	for _, want := range []string{
		"Backup        enabled",
		"Repository    " + shims.store + "  (2 entries)",
		"Remote        " + remote,
		"Branch        main",
		"Ahead/behind  ",
		"Last commit   ",
		"Recent commits",
		"Add given password for git/github.com to store.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Auto-commit") {
		t.Fatalf("pass has no auto-commit setting:\n%s", out)
	}

	env := runJSON(t, t.TempDir(), []string{"pass", "backup", "status", "-n", "5"})
	if env.Command != "pass.backup.status" {
		t.Fatalf("command %q", env.Command)
	}
	var data map[string]any
	_ = json.Unmarshal(env.Data, &data)
	for _, k := range []string{"enabled", "initialized", "repo_path", "entry_count", "remote_url", "branch", "ahead", "behind", "pass_entry", "uncommitted", "last_commit", "commits"} {
		if _, ok := data[k]; !ok {
			t.Errorf("status data lacks %q: %v", k, data)
		}
	}
	if data["entry_count"] != float64(2) || data["enabled"] != true {
		t.Fatalf("data: %v", data)
	}
	if _, auto := data["auto_commit"]; auto {
		t.Fatal("pass.backup.status has no auto_commit")
	}
}

func TestPassBackupPush(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	shims.entry(t, "web/site", "tok")
	shims.git(t, shims.store, "add", "-A")
	shims.git(t, shims.store, "commit", "-q", "-m", "first")

	// Not enabled yet.
	if code, _, errOut := passRun(t, "", "pass", "backup", "push", "--quiet"); code != 1 || !strings.Contains(errOut, "ws pass backup enable --remote <url>") {
		t.Fatalf("not enabled: code=%d err=%q", code, errOut)
	}
	if code, _, e := passRun(t, "", "pass", "backup", "enable", "--remote", remote, "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s", code, e)
	}

	// Something pending: push commits it, then pushes.
	shims.entry(t, "web/other", "tok")
	_, out, _ := passRun(t, "", "pass", "backup", "push", "--dry-run", "--no-color")
	if !strings.Contains(out, "Commit pending changes and push main to "+remote) {
		t.Fatalf("plan line:\n%s", out)
	}
	if got := shims.git(t, remote, "branch", "-a"); strings.TrimSpace(got) != "" {
		t.Fatalf("--dry-run pushed: %q", got)
	}

	code, _, errOut := passRun(t, "", "pass", "backup", "push", "-m", "ws test push", "--quiet")
	if code != 0 {
		t.Fatalf("push: %d %s", code, errOut)
	}
	if got := shims.git(t, remote, "log", "-1", "--format=%s", "main"); strings.TrimSpace(got) != "ws test push" {
		t.Fatalf("the pending change should be committed with -m and pushed: %q", got)
	}

	// Nothing pending: only the push; JSON reports committed=false pushed=true.
	env := runJSON(t, t.TempDir(), []string{"pass", "backup", "push"})
	var data struct {
		Committed bool           `json:"committed"`
		Pushed    bool           `json:"pushed"`
		RemoteURL string         `json:"remote_url"`
		Branch    string         `json:"branch"`
		Actions   []ActionStatus `json:"actions"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || env.Command != "pass.backup.push" {
		t.Fatalf("envelope: %s (%v)", env.Data, err)
	}
	if data.Committed || !data.Pushed || data.RemoteURL != remote || data.Branch != "main" || len(data.Actions) != 1 {
		t.Fatalf("data: %+v", data)
	}
}

func TestPassBackupPushFailureKeepsTheCommit(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	shims.entry(t, "web/site", "tok")
	if code, _, e := passRun(t, "", "pass", "backup", "enable", "--remote", filepath.Join(t.TempDir(), "does-not-exist.git"), "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s", code, e)
	}
	code, _, _ := passRun(t, "", "pass", "backup", "push", "--quiet")
	if code != 1 {
		t.Fatalf("a failed push exits 1, got %d", code)
	}
	if got := shims.git(t, shims.store, "log", "-1", "--format=%s"); strings.TrimSpace(got) != "ws pass backup push" {
		t.Fatalf("the commit stays local: %q", got)
	}
}

func TestPassBackupDisable(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	shims.entry(t, "web/site", "tok")
	workspace := initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store)
	if code, _, e := passRun(t, "", "--workspace", workspace, "pass", "backup", "enable", "--remote", remote, "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s", code, e)
	}
	if code, _, e := passRun(t, "", "--workspace", workspace, "pass", "backup", "push", "--quiet"); code != 0 {
		t.Fatalf("push: %d %s", code, e)
	}
	// A local commit that never reached the remote.
	shims.entry(t, "web/more", "tok")
	shims.git(t, shims.store, "add", "-A")
	shims.git(t, shims.store, "commit", "-q", "-m", "local only")
	// The cron job is installed.
	if code, _, e := passRun(t, "", "--workspace", workspace, "cron", "add", "pass-backup", "--quiet"); code != 0 {
		t.Fatalf("cron add: %d %s", code, e)
	}
	crontab := read(t, os.Getenv("WS_CRONTAB_FILE"))
	if !strings.Contains(crontab, "pass-backup") {
		t.Fatalf("the cron job should be installed:\n%s", crontab)
	}

	_, out, _ := passRun(t, "", "--workspace", workspace, "pass", "backup", "disable", "--dry-run", "--no-color")
	for _, want := range []string{"Remove the backup remote " + remote, "Remove pass-backup from the crontab"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if code, out, e := passRun(t, "", "--workspace", workspace, "pass", "backup", "disable", "--no-color", "--quiet"); code != 0 {
		t.Fatalf("disable: %d %s%s", code, out, e)
	}
	if got := strings.TrimSpace(shims.git(t, shims.store, "remote")); got != "" {
		t.Fatalf("the remote should be gone: %q", got)
	}
	if strings.Contains(read(t, os.Getenv("WS_CRONTAB_FILE")), "pass-backup") {
		t.Fatal("the pass-backup cron block should be gone")
	}
	// The store, its entries and its history stay.
	if _, err := os.Stat(filepath.Join(shims.store, "web", "more.gpg")); err != nil {
		t.Fatal("entries stay")
	}
	if got := shims.git(t, shims.store, "log", "-1", "--format=%s"); strings.TrimSpace(got) != "local only" {
		t.Fatalf("history stays: %q", got)
	}
	// The remote repository itself is not touched.
	if got := shims.git(t, remote, "log", "-1", "--format=%s", "main"); strings.TrimSpace(got) == "local only" {
		t.Fatal("the remote repository must not be modified")
	}

	// Not enabled now.
	code, out, _ := passRun(t, "", "--workspace", workspace, "pass", "backup", "disable", "--no-color")
	if code != 0 || !strings.Contains(out, "not enabled") {
		t.Fatalf("second disable: code=%d out=%q", code, out)
	}
}

func TestPassBackupDisableSaysHowManyCommitsAreUnpushed(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	shims.entry(t, "a", "x")
	if code, _, e := passRun(t, "", "pass", "backup", "enable", "--remote", remote, "--quiet"); code != 0 {
		t.Fatalf("enable: %d %s", code, e)
	}
	if code, _, e := passRun(t, "", "pass", "backup", "push", "--quiet"); code != 0 {
		t.Fatalf("push: %d %s", code, e)
	}
	shims.entry(t, "b", "x")
	shims.git(t, shims.store, "add", "-A")
	shims.git(t, shims.store, "commit", "-q", "-m", "two")
	_, out, _ := passRun(t, "y\n", "pass", "backup", "disable", "--no-color")
	if !strings.Contains(out, "1 commit(s) were not pushed; they stay local.") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestPassBackupJSONCommandNames(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	remote := shims.bareRemote(t)
	// In order: push and disable need the backup enabled.
	for _, c := range []struct {
		command string
		args    []string
	}{
		{"pass.backup.enable", []string{"pass", "backup", "enable", "--remote", remote}},
		{"pass.backup.push", []string{"pass", "backup", "push"}},
		{"pass.backup.disable", []string{"pass", "backup", "disable"}},
	} {
		env := runJSON(t, t.TempDir(), c.args)
		if env.Command != c.command || env.Schema != 3 {
			t.Errorf("%v: envelope %+v, want command %s", c.args, env, c.command)
		}
	}
}

func TestPassBackupUnknownVerb(t *testing.T) {
	newPassShims(t)
	if code, _, errOut := passRun(t, "", "pass", "backup", "frobnicate"); code != 1 || !strings.Contains(errOut, "unknown command: pass backup frobnicate") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if code, _, errOut := passRun(t, "", "pass", "frobnicate"); code != 1 || !strings.Contains(errOut, "unknown command: pass frobnicate") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

// ── the old commands ──

func TestOldSecretCommandsAreAliases(t *testing.T) {
	shims := newPassShims(t)
	shims.initStore(t)
	shims.initGit(t)
	workspace := initWorkspace(t)
	t.Setenv("PASSWORD_STORE_DIR", shims.store)
	for old, want := range map[string]string{
		"status":     `"secret status" is now "pass status"`,
		"git status": `"secret git status" is now "pass backup status"`,
		"git log":    `"secret git log" is now "pass backup status"`,
	} {
		args := append([]string{"--workspace", workspace, "secret"}, strings.Fields(old)...)
		code, _, errOut := passRun(t, "", args...)
		if code != 0 || !strings.Contains(errOut, want) {
			t.Errorf("secret %s: code=%d stderr=%q, want %q", old, code, errOut, want)
		}
	}
	code, out, errOut := passRun(t, "", "--workspace", workspace, "secret", "scan", "--pass", "--no-color")
	if code != 0 || !strings.Contains(errOut, `"secret scan" is now "pass audit"`) || !strings.Contains(out, "no git remote") {
		t.Fatalf("secret scan --pass: code=%d out=%q err=%q", code, out, errOut)
	}
	// secret git remote <url> --pass-entry e
	remote := shims.bareRemote(t)
	_, out, _ = passRun(t, "", "--workspace", workspace, "secret", "git", "remote", remote, "--pass-entry", "e", "--dry-run", "--no-color")
	if !strings.Contains(out, "Set the backup remote to "+remote) {
		t.Fatalf("secret git remote:\n%s", out)
	}
}

func TestSecretScanExitsZeroWithFindings(t *testing.T) {
	workspace, _ := secretWorkspace(t)
	code, out, _ := wsRun(t, workspace, "", "secret", "scan")
	if code != 0 || !strings.Contains(out, "password=hunter2") {
		t.Fatalf("exit 2 is retired: code=%d out=%q", code, out)
	}
}
