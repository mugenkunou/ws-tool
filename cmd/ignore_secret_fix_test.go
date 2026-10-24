package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// `secret fix` is gone (spec §3.5). Its batch forms stay as aliases: they run
// the commands that replace them, on every current finding.

func TestSecretFixAllowlistBatch(t *testing.T) {
	workspace := initWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "creds.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Execute(
		[]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "allowlist", "--quiet"},
		strings.NewReader(""), &out, &errOut,
	)
	if code != 0 {
		t.Fatalf("secret fix allowlist: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	cfg, err := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "creds.env:1") {
		t.Fatalf("expected creds.env:1 in the allowlist, got: %s", cfg)
	}

	env := runJSON(t, workspace, []string{"secret", "ls"})
	if strings.TrimSpace(string(env.Data)) != "[]" {
		t.Fatalf("after the batch allow, ls should list nothing: %s", env.Data)
	}
}

func TestSecretFixPrintsTheNewCommand(t *testing.T) {
	workspace := initWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "creds.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	Execute([]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "allowlist"}, strings.NewReader("y\n"), &out, &errOut)
	if !strings.Contains(errOut.String(), `ws: "secret fix" is now "secret allow`) {
		t.Fatalf("the alias should name its replacement on stderr: %q", errOut.String())
	}
}

func TestSecretFixDryRunChangesNothing(t *testing.T) {
	workspace := initWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "creds.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "allowlist", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), "[dry-run]") {
		t.Fatalf("dry-run: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	after, _ := os.ReadFile(filepath.Join(workspace, "ws.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("--dry-run changed ws.json")
	}
}

func TestSecretFixNoFindingsExitsZero(t *testing.T) {
	workspace := initWorkspace(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "secret", "fix", "--secret-mode", "allowlist"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("nothing to fix is exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No secrets found") {
		t.Fatalf("expected the empty-list message, got: %s", out.String())
	}
}

// TestSecretFixRewrites pins the words each batch form becomes.
func TestSecretFixRewrites(t *testing.T) {
	workspace := initWorkspace(t)
	for name, body := range map[string]string{
		"a.env":      "password=hunter2\ntoken: abcdefghij1234\n",
		"dir/b.conf": "api_key=Zx81qLmN0pQr\n",
	} {
		p := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := globalFlags{workspace: workspace}

	got, _, done := resolveAlias([]string{"secret", "fix", "--secret-mode", "allowlist"}, &g, &bytes.Buffer{})
	want := []string{"secret", "allow",
		filepath.Join(workspace, "a.env") + ":1", filepath.Join(workspace, "a.env") + ":2",
		filepath.Join(workspace, "dir", "b.conf") + ":1"}
	if done || !slices.Equal(got, want) {
		t.Fatalf("allowlist rewrote to %v, want %v", got, want)
	}

	got, _, done = resolveAlias([]string{"secret", "fix", "--secret-mode=exclude"}, &g, &bytes.Buffer{})
	want = []string{"ignore", "add", filepath.Join(workspace, "a.env"), filepath.Join(workspace, "dir", "b.conf")}
	if done || !slices.Equal(got, want) {
		t.Fatalf("exclude rewrote to %v, want %v (one target per file)", got, want)
	}

	// --skip-dir hides a directory from the scan that picks the findings.
	got, _, _ = resolveAlias([]string{"secret", "fix", "--secret-mode", "exclude", "--skip-dir", filepath.Join(workspace, "dir")}, &g, &bytes.Buffer{})
	want = []string{"ignore", "add", filepath.Join(workspace, "a.env")}
	if !slices.Equal(got, want) {
		t.Fatalf("--skip-dir: rewrote to %v, want %v", got, want)
	}
}

// TestSecretFixWithoutModeIsGone: the interactive loop has no successor, and
// the pass mode has none either (secret store takes exactly one finding).
func TestSecretFixWithoutModeIsGone(t *testing.T) {
	workspace := initWorkspace(t)
	for _, args := range [][]string{
		{"secret", "fix"},
		{"secret", "fix", "--dry-run"},
		{"secret", "fix", "--secret-mode", "pass"},
		{"secret", "fix", "--secret-mode", "invalid"},
	} {
		var out, errOut bytes.Buffer
		code := Execute(append([]string{"--workspace", workspace}, args...), strings.NewReader(""), &out, &errOut)
		if code != 1 || !strings.Contains(errOut.String(), `"secret fix" is gone`) || out.Len() != 0 {
			t.Errorf("ws %v: code=%d stdout=%q stderr=%q, want exit 1 and the removed-command line", args, code, out.String(), errOut.String())
		}
	}
}

func TestSecretSetupIsPassInit(t *testing.T) {
	workspace := initWorkspace(t)
	shims := newPassShims(t)
	shims.initStore(t)
	var out, errOut bytes.Buffer
	code := Execute([]string{"--workspace", workspace, "secret", "setup"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("secret setup: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), `"secret setup" is now "pass init"`) {
		t.Fatalf("stderr should name the new command: %q", errOut.String())
	}
}
