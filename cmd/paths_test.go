package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for spec "Path Rules": every printed path is absolute, and every path
// input resolves as absolute, ~, or relative to the current directory.

// chdir switches the process cwd for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestPathRuleRepoScanPrintsOneAbsolutePathPerRepo(t *testing.T) {
	workspace := initScanFixture(t, true)
	repoPath := filepath.Join(workspace, "r1")

	var out, errOut bytes.Buffer
	Execute([]string{"--workspace", workspace, "--no-color", "repo", "scan", "--no-fetch", "--check", "identity"}, strings.NewReader(""), &out, &errOut)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line for one repo, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], " "+repoPath+" ") {
		t.Fatalf("expected absolute repo path %s, got: %s", repoPath, lines[0])
	}
	if strings.Contains(out.String(), "~") {
		t.Fatalf("output must not contain tilde paths: %s", out.String())
	}
}

func TestPathRuleRepoJSONPathsAbsolute(t *testing.T) {
	workspace := initScanFixture(t, false)
	repoPath := filepath.Join(workspace, "r1")

	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--json", "repo", "scan", "--no-fetch", "--check", "identity"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("scan --json failed: %d %s", code, errOut.String())
	}
	var payload struct {
		Schema int `json:"schema"`
		Data   struct {
			Statuses []struct {
				Path string `json:"path"`
			} `json:"statuses"`
			Findings []struct {
				Repo string `json:"repo"`
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Schema != 2 {
		t.Fatalf("expected schema 2, got %d", payload.Schema)
	}
	if len(payload.Data.Statuses) != 1 || payload.Data.Statuses[0].Path != repoPath {
		t.Fatalf("expected status path %s, got %+v", repoPath, payload.Data.Statuses)
	}
	if len(payload.Data.Findings) == 0 {
		t.Fatal("expected identity findings")
	}
	for _, f := range payload.Data.Findings {
		if f.Repo != repoPath {
			t.Fatalf("expected finding repo %s, got %s", repoPath, f.Repo)
		}
	}
}

func TestPathRuleRepoTargetResolvesAgainstCwd(t *testing.T) {
	workspace := initScanFixture(t, true)
	repoPath := filepath.Join(workspace, "r1")
	args := func(target string) []string {
		return []string{"--workspace", workspace, "--no-color", "repo", "ls", target}
	}

	// Absolute works from anywhere.
	var out, errOut bytes.Buffer
	if code := Execute(args(repoPath), strings.NewReader(""), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != repoPath {
		t.Fatalf("absolute target: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// cwd-relative works.
	chdir(t, workspace)
	out.Reset()
	errOut.Reset()
	if code := Execute(args("r1"), strings.NewReader(""), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != repoPath {
		t.Fatalf("cwd-relative target: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute(args("./r1/"), strings.NewReader(""), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != repoPath {
		t.Fatalf("uncleaned cwd-relative target: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// Workspace-relative from another cwd is NOT silently accepted.
	chdir(t, t.TempDir())
	out.Reset()
	errOut.Reset()
	code := Execute(args("r1"), strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for workspace-relative target from foreign cwd, got %d out=%s", code, out.String())
	}
	cwd, _ := os.Getwd()
	if !strings.Contains(errOut.String(), "no repo at "+filepath.Join(cwd, "r1")) {
		t.Fatalf("error must show the absolute path it resolved to, got: %s", errOut.String())
	}
}

func TestPathRuleRepoTargetRejectsMultipleArgs(t *testing.T) {
	workspace := initScanFixture(t, true)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "repo", "ls", filepath.Join(workspace, "r1"), "extra"}, strings.NewReader(""), &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "expected at most one repo path") {
		t.Fatalf("expected multiple-arg error, got %d stderr=%s", code, errOut.String())
	}
}

func TestPathRuleRepoPathFilterMatchesWholeComponents(t *testing.T) {
	workspace := initScanFixture(t, true)
	repoPath := filepath.Join(workspace, "r1")

	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "repo", "ls", "--path", workspace}, strings.NewReader(""), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != repoPath {
		t.Fatalf("--path <workspace>: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	Execute([]string{"--workspace", workspace, "repo", "ls", "--path", filepath.Join(workspace, "r")}, strings.NewReader(""), &out, &errOut)
	if strings.Contains(out.String(), repoPath) {
		t.Fatalf("--path must not match a partial component, got: %s", out.String())
	}
}

func TestPathRuleIgnoreCheckNoWorkspaceFallback(t *testing.T) {
	workspace := initScanFixture(t, true)
	file := filepath.Join(workspace, "r1", "f.txt")

	// cwd-relative works and prints the absolute path.
	chdir(t, filepath.Join(workspace, "r1"))
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--no-color", "ignore", "check", "f.txt"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("cwd-relative check failed: %d %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), file) {
		t.Fatalf("expected absolute path %s in output, got: %s", file, out.String())
	}

	// Workspace-relative from a foreign cwd fails and shows the resolved path.
	chdir(t, t.TempDir())
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "ignore", "check", "r1/f.txt"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("expected exit 1 without workspace fallback, got %d out=%s", code, out.String())
	}
	cwd, _ := os.Getwd()
	if !strings.Contains(errOut.String(), filepath.Join(cwd, "r1", "f.txt")) {
		t.Fatalf("error must show absolute resolved path, got: %s", errOut.String())
	}
}

func TestPathRuleIgnoreCheckJSONAbsolute(t *testing.T) {
	workspace := initScanFixture(t, true)
	file := filepath.Join(workspace, "r1", "f.txt")
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--json", "ignore", "check", file}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("check --json failed: %d %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"path": "`+file+`"`) {
		t.Fatalf("expected absolute path in JSON, got: %s", out.String())
	}
}

func TestPathRuleSecretScanViolationsAbsolute(t *testing.T) {
	workspace := initScanFixture(t, true)
	secretFile := filepath.Join(workspace, "creds.env")
	if err := os.WriteFile(secretFile, []byte("password=letmein\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	Execute([]string{"--workspace", workspace, "--json", "secret", "scan"}, strings.NewReader(""), &out, &errOut)
	var payload struct {
		Data struct {
			Violations []struct {
				Path string `json:"path"`
			} `json:"violations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Data.Violations) == 0 {
		t.Fatalf("expected a violation for %s, got: %s", secretFile, out.String())
	}
	for _, v := range payload.Data.Violations {
		if v.Path != secretFile {
			t.Fatalf("violation path must be absolute %s, got %q", secretFile, v.Path)
		}
	}
}

func TestPathRuleSecretSkipDirResolvesAgainstCwd(t *testing.T) {
	workspace := initScanFixture(t, true)
	chdir(t, workspace)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--json", "secret", "scan", "--skip-dir", "r1"}, strings.NewReader(""), &out, &errOut); code == 1 {
		t.Fatalf("cwd-relative --skip-dir failed: %s", errOut.String())
	}
	if !strings.Contains(out.String(), filepath.Join(workspace, "r1")) {
		t.Fatalf("expected absolute skipped dir in JSON, got: %s", out.String())
	}

	// A directory outside the workspace is rejected with its absolute path.
	outside := t.TempDir()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "secret", "scan", "--skip-dir", outside}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), outside) {
		t.Fatalf("expected rejection of outside dir, got %d stderr=%s", code, errOut.String())
	}
}

func TestPathRuleConfigViewResolved(t *testing.T) {
	workspace := initScanFixture(t, true)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "--json", "config", "view"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("config view failed: %d %s", code, errOut.String())
	}
	var payload struct {
		Data struct {
			Workspace string `json:"workspace"`
			Scratch   struct {
				RootDir string `json:"root_dir"`
			} `json:"scratch"`
			Repo struct {
				Roots       []string `json:"roots"`
				ExcludeDirs []string `json:"exclude_dirs"`
			} `json:"repo"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Data.Workspace != workspace {
		t.Fatalf("workspace = %q, want %q", payload.Data.Workspace, workspace)
	}
	if !filepath.IsAbs(payload.Data.Scratch.RootDir) {
		t.Fatalf("scratch.root_dir must be absolute, got %q", payload.Data.Scratch.RootDir)
	}
	for _, p := range append(payload.Data.Repo.Roots, payload.Data.Repo.ExcludeDirs...) {
		if !filepath.IsAbs(p) {
			t.Fatalf("repo path values must be absolute, got %q", p)
		}
	}
}

func TestPathRuleInitStoresAbsoluteWorkspace(t *testing.T) {
	testSetXDG(t)
	parent := t.TempDir()
	chdir(t, parent)
	var out, errOut bytes.Buffer
	if code := Execute([]string{"init", "--workspace", "Workspace"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: %d %s", code, errOut.String())
	}
	cfgPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ws-tool", "config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace != filepath.Join(parent, "Workspace") {
		t.Fatalf("config workspace = %q, want absolute %q", cfg.Workspace, filepath.Join(parent, "Workspace"))
	}
}
