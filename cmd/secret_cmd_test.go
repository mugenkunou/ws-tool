package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wsRun runs ws on a workspace and returns the exit code, stdout and stderr.
func wsRun(t *testing.T, workspace, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	full := append([]string{"--workspace", workspace}, args...)
	code := Execute(full, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeWs(t *testing.T, workspace, rel, content string) string {
	t.Helper()
	p := filepath.Join(workspace, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// wsConfig reads ws.json as a map.
func wsConfig(t *testing.T, workspace string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func secretList(t *testing.T, workspace, key string) []string {
	t.Helper()
	sec, _ := wsConfig(t, workspace)["secret"].(map[string]any)
	var out []string
	for _, v := range sec[key].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func setSecretList(t *testing.T, workspace, key string, values []string) {
	t.Helper()
	cfg := wsConfig(t, workspace)
	sec := cfg["secret"].(map[string]any)
	sec[key] = values
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(workspace, "ws.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// secretWorkspace is a workspace with one finding: configs/myapp/app.properties:3.
func secretWorkspace(t *testing.T) (workspace, file string) {
	t.Helper()
	workspace = initWorkspace(t)
	file = writeWs(t, workspace, "configs/myapp/app.properties", "# app settings\nuser=admin\npassword=hunter2\nport=8080\n")
	return workspace, file
}

// ── ls ──

func TestSecretLsListsAndNeverWritesConfig(t *testing.T) {
	workspace, file := secretWorkspace(t)
	// A stale anchor: no pattern matches that line. ls used to prune it.
	setSecretList(t, workspace, "allowlist", []string{"gone.txt:9"})
	before, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))

	code, out, errOut := wsRun(t, workspace, "", "secret", "ls", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"SEVERITY", "LOCATION", "MATCH", "WARNING", file + ":3", "password=hunter2"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls output lacks %q:\n%s", want, out)
		}
	}
	after, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if !bytes.Equal(before, after) {
		t.Fatalf("secret ls changed ws.json (it must be read-only):\n%s", after)
	}
}

func TestSecretLsJSONIsRecordArray(t *testing.T) {
	workspace, file := secretWorkspace(t)
	env := runJSON(t, workspace, []string{"secret", "ls"})
	if env.Command != "secret.ls" || env.Schema != 3 {
		t.Fatalf("envelope %+v", env)
	}
	var recs []map[string]any
	if err := json.Unmarshal(env.Data, &recs); err != nil {
		t.Fatalf("data is not an array of records: %v\n%s", err, env.Data)
	}
	if len(recs) != 1 || recs[0]["location"] != file+":3" || recs[0]["severity"] != "WARNING" {
		t.Fatalf("records: %v", recs)
	}
	if _, extra := recs[0]["violations"]; extra {
		t.Fatal("the old data.violations wrapper is gone")
	}
}

func TestSecretLsNoFindings(t *testing.T) {
	workspace := initWorkspace(t)
	code, out, _ := wsRun(t, workspace, "", "secret", "ls")
	if code != 0 || !strings.Contains(out, "No secrets found") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestSecretLsFilterAndBadField(t *testing.T) {
	workspace, _ := secretWorkspace(t)
	writeWs(t, workspace, "notes/other.txt", "api_key=Zx81qLmN0pQr9\n")
	_, out, _ := wsRun(t, workspace, "", "secret", "ls", "--filter", "other.txt")
	if !strings.Contains(out, "other.txt") || strings.Contains(out, "app.properties") {
		t.Fatalf("filter should keep one row:\n%s", out)
	}
	if code, _, errOut := wsRun(t, workspace, "", "secret", "ls", "--filter", "nope=x"); code != 1 || errOut == "" {
		t.Fatalf("an unknown field is exit 1, got %d %q", code, errOut)
	}
}

// ── show ──

func TestSecretShowPrintsContext(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, errOut := wsRun(t, workspace, "", "secret", "show", file+":3", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != file+":3  WARNING  potential secret detected" {
		t.Fatalf("header: %q", lines[0])
	}
	want := []string{
		"     1  # app settings",
		"     2  user=admin",
		" →   3  password=hunter2",
		"     4  port=8080",
	}
	if len(lines) != 1+len(want) {
		t.Fatalf("got %d lines:\n%s", len(lines), out)
	}
	for i, w := range want {
		if lines[1+i] != w {
			t.Errorf("line %d: %q, want %q", 1+i, lines[1+i], w)
		}
	}
}

func TestSecretShowFiveLinesEachSide(t *testing.T) {
	workspace := initWorkspace(t)
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		if i == 10 {
			b.WriteString("password=hunter2\n")
		} else {
			b.WriteString("line\n")
		}
	}
	file := writeWs(t, workspace, "long.txt", b.String())
	_, out, _ := wsRun(t, workspace, "", "secret", "show", file+":10")
	if n := strings.Count(out, "\n"); n != 1+11 {
		t.Fatalf("want the header and 11 lines (5 + match + 5), got %d lines:\n%s", n, out)
	}
}

func TestSecretShowMustBeACurrentFinding(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, errOut := wsRun(t, workspace, "", "secret", "show", file+":2")
	if code != 1 || out != "" || !strings.Contains(errOut, "no finding at "+file+":2") {
		t.Fatalf("a line with no match: code=%d out=%q err=%q", code, out, errOut)
	}
	// An allowed line is no longer a finding.
	if code, _, e := wsRun(t, workspace, "", "secret", "allow", file+":3", "--quiet"); code != 0 {
		t.Fatalf("allow: %d %s", code, e)
	}
	code, _, errOut = wsRun(t, workspace, "", "secret", "show", file+":3")
	if code != 1 || !strings.Contains(errOut, "no finding at") {
		t.Fatalf("an allowed line: code=%d err=%q", code, errOut)
	}
	if code, _, errOut = wsRun(t, workspace, "", "secret", "show"); code != 1 || !strings.Contains(errOut, "usage") {
		t.Fatalf("no target: code=%d err=%q", code, errOut)
	}
	if code, _, errOut = wsRun(t, workspace, "", "secret", "show", "nocolon"); code != 1 {
		t.Fatalf("a malformed anchor: code=%d err=%q", code, errOut)
	}
}

func TestSecretShowJSONHasContext(t *testing.T) {
	workspace, file := secretWorkspace(t)
	env := runJSON(t, workspace, []string{"secret", "show", file + ":3"})
	if env.Command != "secret.show" {
		t.Fatalf("command %q", env.Command)
	}
	var rec struct {
		Location string `json:"location"`
		Context  []struct {
			Number  int    `json:"number"`
			Content string `json:"content"`
			IsMatch bool   `json:"is_match"`
		} `json:"context"`
	}
	if err := json.Unmarshal(env.Data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Location != file+":3" || len(rec.Context) != 4 || !rec.Context[2].IsMatch || rec.Context[2].Number != 3 {
		t.Fatalf("record: %+v", rec)
	}
}

func TestSecretShowAnchorForms(t *testing.T) {
	workspace, file := secretWorkspace(t)
	// Relative to the current directory.
	chdir(t, filepath.Dir(file))
	if code, _, e := wsRun(t, workspace, "", "secret", "show", "./app.properties:3"); code != 0 {
		t.Fatalf("relative anchor: exit %d %s", code, e)
	}
	// ~ expands to HOME.
	t.Setenv("HOME", workspace)
	if code, _, e := wsRun(t, workspace, "", "secret", "show", "~/configs/myapp/app.properties:3"); code != 0 {
		t.Fatalf("~ anchor: exit %d %s", code, e)
	}
	// Outside the workspace is an error.
	if code, _, e := wsRun(t, workspace, "", "secret", "show", "/etc/passwd:1"); code != 1 || !strings.Contains(e, "outside the workspace") {
		t.Fatalf("outside: exit %d %s", code, e)
	}
}

// ── allow ──

func TestSecretAllowStoresWorkspaceRelativeAnchor(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, errOut := wsRun(t, workspace, "", "secret", "allow", file+":3", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	got := secretList(t, workspace, "allowlist")
	if len(got) != 1 || got[0] != "configs/myapp/app.properties:3" {
		t.Fatalf("allowlist: %v", got)
	}
	if _, out, _ = wsRun(t, workspace, "", "secret", "ls"); !strings.Contains(out, "No secrets found") {
		t.Fatalf("the allowed finding should be gone:\n%s", out)
	}
}

func TestSecretAllowRequiresATarget(t *testing.T) {
	workspace, _ := secretWorkspace(t)
	before, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	for _, flags := range [][]string{{}, {"--quiet"}, {"--json"}} {
		code, _, errOut := wsRun(t, workspace, "", append([]string{"secret", "allow"}, flags...)...)
		if code != 1 || !strings.Contains(errOut, "at least one target") {
			t.Errorf("secret allow %v: code=%d err=%q", flags, code, errOut)
		}
	}
	after, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("a bare allow changed ws.json")
	}
}

func TestSecretAllowPlanLines(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, _ := wsRun(t, workspace, "", "secret", "allow", file+":3", "--dry-run", "--no-color")
	if code != 0 || !strings.Contains(out, "Allow "+file+":3 (false positive)") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if got := secretList(t, workspace, "allowlist"); len(got) != 0 {
		t.Fatalf("--dry-run changed the allowlist: %v", got)
	}
}

func TestSecretAllowDropsStaleAnchorsLast(t *testing.T) {
	workspace, file := secretWorkspace(t)
	setSecretList(t, workspace, "allowlist", []string{"gone.txt:9", "configs/myapp/app.properties:2"})

	code, out, _ := wsRun(t, workspace, "", "secret", "allow", file+":3", "--dry-run", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	allow := strings.Index(out, "Allow "+file+":3")
	drop := strings.Index(out, "Drop 2 stale anchors from secret.allowlist")
	if allow < 0 || drop < 0 || drop < allow {
		t.Fatalf("expected the allow action, then the drop action:\n%s", out)
	}

	if code, _, e := wsRun(t, workspace, "", "secret", "allow", file+":3", "--quiet"); code != 0 {
		t.Fatalf("exit %d %s", code, e)
	}
	got := secretList(t, workspace, "allowlist")
	if len(got) != 1 || got[0] != "configs/myapp/app.properties:3" {
		t.Fatalf("only the new anchor should remain: %v", got)
	}
}

func TestSecretAllowKeepsAnchorsHiddenFromTheScan(t *testing.T) {
	workspace, file := secretWorkspace(t)
	// A finding in a muted directory is out of sight, not stale.
	writeWs(t, workspace, "vendor/lib/a.conf", "password=hunter2\n")
	setSecretList(t, workspace, "skip_dirs", []string{"vendor"})
	setSecretList(t, workspace, "allowlist", []string{"vendor/lib/a.conf:1"})
	if code, _, e := wsRun(t, workspace, "", "secret", "allow", file+":3", "--quiet"); code != 0 {
		t.Fatalf("exit %d %s", code, e)
	}
	got := secretList(t, workspace, "allowlist")
	if len(got) != 2 {
		t.Fatalf("the muted directory's anchor must survive: %v", got)
	}
}

func TestSecretAllowValidatesEveryTargetFirst(t *testing.T) {
	workspace, file := secretWorkspace(t)
	before, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	code, _, errOut := wsRun(t, workspace, "", "secret", "allow", file+":3", file+":2", "--quiet")
	if code != 1 || !strings.Contains(errOut, "no finding at "+file+":2") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	after, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("an invalid target must change nothing, even for the valid ones")
	}
}

func TestSecretAllowJSON(t *testing.T) {
	workspace, file := secretWorkspace(t)
	env := runJSON(t, workspace, []string{"secret", "allow", file + ":3"})
	if env.Command != "secret.allow" {
		t.Fatalf("command %q", env.Command)
	}
	var data struct {
		Actions []ActionStatus `json:"actions"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || len(data.Actions) != 1 || data.Actions[0].Status != "executed" {
		t.Fatalf("data: %s (%v)", env.Data, err)
	}
}

func TestSecretAllowCoversTheLineNotTheContent(t *testing.T) {
	workspace, file := secretWorkspace(t)
	wsRun(t, workspace, "", "secret", "allow", file+":3", "--quiet")
	// A different secret on the allowed line is still hidden.
	if err := os.WriteFile(file, []byte("# app settings\nuser=admin\npassword=Rk93mPq7Zx\nport=8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls"); !strings.Contains(out, "No secrets found") {
		t.Fatalf("the allow covers the line number:\n%s", out)
	}
	// Inserting a line above moves the finding: it appears again at line 4.
	if err := os.WriteFile(file, []byte("# app settings\n# new\nuser=admin\npassword=Rk93mPq7Zx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ := wsRun(t, workspace, "", "secret", "ls")
	if !strings.Contains(out, file+":4") {
		t.Fatalf("the moved finding should show at its new line:\n%s", out)
	}
}

// ── mute / unmute (per-finding; stays in secret ls, quietly) ──

func TestSecretMuteAndUnmute(t *testing.T) {
	workspace, file := secretWorkspace(t)

	if code, _, e := wsRun(t, workspace, "", "secret", "mute", file+":3", "--quiet"); code != 0 {
		t.Fatalf("mute: %d %s", code, e)
	}
	if got := secretList(t, workspace, "muted"); len(got) != 1 || got[0] != "configs/myapp/app.properties:3" {
		t.Fatalf("secret.muted is stored workspace-relative: %v", got)
	}
	// A muted finding stays in secret ls, quietly: it never vanishes the way
	// an allowed or skipped one does.
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls"); strings.Contains(out, "app.properties") {
		t.Fatalf("a muted finding should not show in the default list:\n%s", out)
	}
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls", "--muted"); !strings.Contains(out, "app.properties") || !strings.Contains(out, "INFO") {
		t.Fatalf("secret ls --muted should show it, quietly:\n%s", out)
	}

	if code, _, e := wsRun(t, workspace, "", "secret", "unmute", file+":3", "--quiet"); code != 0 {
		t.Fatalf("unmute: %d %s", code, e)
	}
	if got := secretList(t, workspace, "muted"); len(got) != 0 {
		t.Fatalf("muted after unmute: %v", got)
	}
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls"); !strings.Contains(out, "app.properties") {
		t.Fatalf("unmuted: the finding is back at full volume:\n%s", out)
	}
}

func TestSecretMutePlanLine(t *testing.T) {
	workspace, file := secretWorkspace(t)
	_, out, _ := wsRun(t, workspace, "", "secret", "mute", file+":3", "--dry-run", "--no-color")
	if !strings.Contains(out, "Mute "+file+":3 (it keeps appearing, quietly)") {
		t.Fatalf("plan line: %q", out)
	}
	if got := secretList(t, workspace, "muted"); len(got) != 0 {
		t.Fatalf("--dry-run muted: %v", got)
	}
}

func TestSecretMuteRequiresATarget(t *testing.T) {
	workspace, _ := secretWorkspace(t)
	if code, _, errOut := wsRun(t, workspace, "", "secret", "mute"); code != 1 || !strings.Contains(errOut, "at least one target") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestSecretMuteAlreadyMutedGetsNoAction(t *testing.T) {
	workspace, file := secretWorkspace(t)
	if code, _, e := wsRun(t, workspace, "", "secret", "mute", file+":3", "--quiet"); code != 0 {
		t.Fatalf("mute: %d %s", code, e)
	}
	code, out, _ := wsRun(t, workspace, "", "secret", "mute", file+":3")
	if code != 0 || !strings.Contains(out, "Already muted: "+file+":3") {
		t.Fatalf("already muted: code=%d out=%q", code, out)
	}
	env := runJSON(t, workspace, []string{"secret", "mute", file + ":3"})
	var data struct {
		Actions []ActionStatus `json:"actions"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if env.Command != "secret.mute" || len(data.Actions) != 0 {
		t.Fatalf("no action expected: %s", env.Data)
	}
}

func TestSecretUnmuteNotMutedGetsNoAction(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, _ := wsRun(t, workspace, "", "secret", "unmute", file+":3")
	if code != 0 || !strings.Contains(out, "Not muted: "+file+":3") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// ── skip / unskip (directory-level; the directory is never scanned) ──

func TestSecretSkipAndUnskip(t *testing.T) {
	workspace, file := secretWorkspace(t)
	dir := filepath.Dir(file)

	if code, _, e := wsRun(t, workspace, "", "secret", "skip", dir, "--quiet"); code != 0 {
		t.Fatalf("skip: %d %s", code, e)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 1 || got[0] != "configs/myapp" {
		t.Fatalf("skip_dirs is stored workspace-relative: %v", got)
	}
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls"); !strings.Contains(out, "No secrets found") {
		t.Fatalf("a skipped directory is not scanned:\n%s", out)
	}

	if code, _, e := wsRun(t, workspace, "", "secret", "unskip", dir, "--quiet"); code != 0 {
		t.Fatalf("unskip: %d %s", code, e)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 0 {
		t.Fatalf("skip_dirs after unskip: %v", got)
	}
	if _, out, _ := wsRun(t, workspace, "", "secret", "ls"); !strings.Contains(out, "app.properties") {
		t.Fatalf("unskipped: the finding is back:\n%s", out)
	}
}

func TestSecretSkipPlanLine(t *testing.T) {
	workspace, file := secretWorkspace(t)
	dir := filepath.Dir(file)
	_, out, _ := wsRun(t, workspace, "", "secret", "skip", dir, "--dry-run", "--no-color")
	if !strings.Contains(out, "Skip "+dir+" in secret scans") {
		t.Fatalf("plan line: %q", out)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 0 {
		t.Fatalf("--dry-run skipped: %v", got)
	}
}

func TestSecretSkipRejections(t *testing.T) {
	workspace, file := secretWorkspace(t)
	for name, target := range map[string]string{
		"the workspace itself": workspace,
		"outside":              t.TempDir(),
		"a file":               file,
		"missing":              filepath.Join(workspace, "nope"),
	} {
		code, _, errOut := wsRun(t, workspace, "", "secret", "skip", target, "--quiet")
		if code != 1 || errOut == "" {
			t.Errorf("skip %s: code=%d err=%q", name, code, errOut)
		}
	}
	if code, _, errOut := wsRun(t, workspace, "", "secret", "skip"); code != 1 || !strings.Contains(errOut, "usage") {
		t.Fatalf("no target: %d %q", code, errOut)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 0 {
		t.Fatalf("a rejected skip changed the config: %v", got)
	}
}

func TestSecretSkipAlreadySkippedGetsNoAction(t *testing.T) {
	workspace, file := secretWorkspace(t)
	configs := filepath.Join(workspace, "configs")
	writeWs(t, workspace, "configs/myapp/sub/x.txt", "x\n")
	if code, _, e := wsRun(t, workspace, "", "secret", "skip", configs, "--quiet"); code != 0 {
		t.Fatalf("skip: %d %s", code, e)
	}
	code, out, _ := wsRun(t, workspace, "", "secret", "skip", filepath.Dir(file))
	if code != 0 || !strings.Contains(out, "already skipped by "+configs) {
		t.Fatalf("skipped by a parent: code=%d out=%q", code, out)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 1 {
		t.Fatalf("no second entry: %v", got)
	}
	env := runJSON(t, workspace, []string{"secret", "skip", configs})
	var data struct {
		Actions []ActionStatus `json:"actions"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if env.Command != "secret.skip" || len(data.Actions) != 0 {
		t.Fatalf("no action expected: %s", env.Data)
	}
}

func TestSecretSkipDedupesNestedTargets(t *testing.T) {
	workspace, file := secretWorkspace(t)
	_, out, _ := wsRun(t, workspace, "", "secret", "skip", filepath.Join(workspace, "configs"), filepath.Dir(file), "--dry-run")
	if n := strings.Count(out, "[dry-run]"); n != 1 {
		t.Fatalf("a directory inside another target needs no action of its own, got %d:\n%s", n, out)
	}
}

func TestSecretUnskipAnyWrittenForm(t *testing.T) {
	workspace, file := secretWorkspace(t)
	dir := filepath.Dir(file)
	t.Setenv("HOME", filepath.Dir(workspace))
	// The same directory written four ways by hand, and another one that stays.
	setSecretList(t, workspace, "skip_dirs", []string{"configs/myapp", dir, "./configs/myapp/", "~/Workspace/configs/myapp", "vendor"})
	if code, _, e := wsRun(t, workspace, "", "secret", "unskip", dir, "--quiet"); code != 0 {
		t.Fatalf("unskip: %d %s", code, e)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 1 || got[0] != "vendor" {
		t.Fatalf("every spelling should go, and only those: %v", got)
	}
}

func TestSecretUnskipNamesTheParent(t *testing.T) {
	workspace, file := secretWorkspace(t)
	configs := filepath.Join(workspace, "configs")
	wsRun(t, workspace, "", "secret", "skip", configs, "--quiet")
	code, _, errOut := wsRun(t, workspace, "", "secret", "unskip", filepath.Dir(file), "--quiet")
	if code != 1 || !strings.Contains(errOut, configs) || !strings.Contains(errOut, "skipped by") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if got := secretList(t, workspace, "skip_dirs"); len(got) != 1 {
		t.Fatalf("nothing should change: %v", got)
	}
}

func TestSecretUnskipNotSkippedIsNothingToDo(t *testing.T) {
	workspace, file := secretWorkspace(t)
	code, out, _ := wsRun(t, workspace, "", "secret", "unskip", filepath.Dir(file))
	if code != 0 || !strings.Contains(out, "is not skipped") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestSecretUnknownVerb(t *testing.T) {
	workspace := initWorkspace(t)
	if code, _, e := wsRun(t, workspace, "", "secret", "frobnicate"); code != 1 || !strings.Contains(e, "unknown command: secret frobnicate") {
		t.Fatalf("code=%d err=%q", code, e)
	}
}
