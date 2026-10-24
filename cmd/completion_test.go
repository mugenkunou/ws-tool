package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionsBash(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"completion", "bash"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("completions bash failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "complete -o default -F _ws_completions ws") {
		t.Fatalf("expected bash completion registration, got: %s", output)
	}
	if !strings.Contains(output, "ws __complete") {
		t.Fatalf("expected __complete protocol call in bash script, got: %s", output)
	}
}

func TestCompletionsZsh(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"completion", "zsh"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("completions zsh failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "compdef _ws ws") {
		t.Fatalf("expected zsh compdef registration, got: %s", output)
	}
	if !strings.Contains(output, "ws __complete") {
		t.Fatalf("expected __complete protocol call in zsh script, got: %s", output)
	}
}

func TestCompletionsFish(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"completion", "fish"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("completions fish failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "complete -c ws") {
		t.Fatalf("expected fish complete registration, got: %s", output)
	}
	if !strings.Contains(output, "ws __complete") {
		t.Fatalf("expected __complete protocol call in fish script, got: %s", output)
	}
}

func TestCompletionsInvalidShell(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"completion", "powershell"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid shell, got=%d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command: completion powershell") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestCompletionsJSON(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"--json", "completion", "fish"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("completions json failed: code=%d stderr=%s", code, errOut.String())
	}

	var payload struct {
		Command string `json:"command"`
		Data    struct {
			Shell  string `json:"shell"`
			Script string `json:"script"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload.Command != "completion.fish" {
		t.Fatalf("unexpected command field: %s", payload.Command)
	}
	if payload.Data.Shell != "fish" {
		t.Fatalf("unexpected shell field: %s", payload.Data.Shell)
	}
	if !strings.Contains(payload.Data.Script, "complete -c ws") {
		t.Fatalf("unexpected fish completion script: %s", payload.Data.Script)
	}
}

// ── Install / Uninstall tests ──────────────────────────────────────

func TestCompletionsInstallIdempotent(t *testing.T) {
	// Set up a temp rc file.
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".bashrc")
	os.WriteFile(rcFile, []byte("# existing config\nexport FOO=bar\n"), 0o644)

	// Monkey-patch completionRCInfo by using env var trick — not feasible
	// without refactoring, so we test the underlying helpers instead.

	// Test containsCompletionMarker — should be false initially.
	content, _ := os.ReadFile(rcFile)
	if containsCompletionMarker(string(content)) {
		t.Fatal("expected no marker in fresh rc file")
	}

	// Simulate install by appending the block.
	block := "\n" + completionMarkerStart + "\n" + `eval "$(ws completions bash)"` + "\n" + completionMarkerEnd + "\n"
	os.WriteFile(rcFile, append(content, []byte(block)...), 0o644)

	// Now marker should be detected.
	content, _ = os.ReadFile(rcFile)
	if !containsCompletionMarker(string(content)) {
		t.Fatal("expected marker after install")
	}

	// Original content should be preserved.
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Fatal("original content lost after install")
	}
}

func TestCompletionsUninstallCleansUp(t *testing.T) {
	original := "# existing config\nexport FOO=bar\n"
	block := "\n" + completionMarkerStart + "\n" + `eval "$(ws completions bash)"` + "\n" + completionMarkerEnd + "\n"
	full := original + block

	cleaned := removeMarkerBlock(full)
	if containsCompletionMarker(cleaned) {
		t.Fatal("marker should be removed after uninstall")
	}
	if !strings.Contains(cleaned, "export FOO=bar") {
		t.Fatal("original content lost after uninstall")
	}
}

func TestRemoveStaleCompletionLines(t *testing.T) {
	input := `# my bashrc
export PATH=$PATH:/usr/local/bin
eval "$(ws completions bash)"
source <(ws completions bash)
# some other stuff
alias ll='ls -la'
`
	cleaned := removeStaleCompletionLines(input)
	if strings.Contains(cleaned, `eval "$(ws completions bash)"`) {
		t.Fatal("stale eval line should be removed")
	}
	if strings.Contains(cleaned, `source <(ws completions bash)`) {
		t.Fatal("stale source line should be removed")
	}
	if !strings.Contains(cleaned, "alias ll=") {
		t.Fatal("non-completion lines should be preserved")
	}
	if !strings.Contains(cleaned, "export PATH=") {
		t.Fatal("non-completion lines should be preserved")
	}
}

func TestRemoveStalePreservesComments(t *testing.T) {
	input := `# ws completions are set up below
export PATH=$PATH:/usr/local/bin
`
	cleaned := removeStaleCompletionLines(input)
	if !strings.Contains(cleaned, "# ws completions are set up below") {
		t.Fatal("comment lines mentioning ws completions should be preserved")
	}
}

func TestCompletionsInstallIntegration(t *testing.T) {
	// Use --dry-run to test the full flow without writing.
	// Point HOME to a temp dir so we don't hit the real ~/.bashrc.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"completion", "enable", "--shell", "bash", "--dry-run"}, strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("completion enable --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Fatalf("expected dry-run output, got: %s", out.String())
	}
}

func TestWithCompletionBlock(t *testing.T) {
	block := completionMarkerStart + "\neval \"$(ws completion bash)\"\n" + completionMarkerEnd + "\n"

	// Appended after existing content, once.
	got := withCompletionBlock("export A=1\n", block)
	if got != "export A=1\n\n"+block {
		t.Fatalf("append:\n%s", got)
	}
	if again := withCompletionBlock(got, block); again != got {
		t.Fatalf("a block holding the current line is left alone:\n%s", again)
	}

	// An older line in the block is rewritten in place.
	old := "export A=1\n" + completionMarkerStart + "\neval \"$(ws completions bash)\"\n" + completionMarkerEnd + "\nexport B=2\n"
	got = withCompletionBlock(old, block)
	if strings.Contains(got, "ws completions") || !strings.Contains(got, "ws completion bash") || !strings.Contains(got, "export B=2") || strings.Count(got, completionMarkerStart) != 1 {
		t.Fatalf("rewrite:\n%s", got)
	}

	// Unmarked lines that load ws completion are removed.
	got = withCompletionBlock("source <(ws completions bash)\nalias ll=ls\n", block)
	if strings.Contains(got, "source <(ws completions") || !strings.Contains(got, "alias ll=ls") || !strings.Contains(got, completionMarkerStart) {
		t.Fatalf("unmarked:\n%s", got)
	}

	// An empty rc file gets the block alone.
	if got := withCompletionBlock("", block); got != block {
		t.Fatalf("empty:\n%s", got)
	}
}

func TestCompletionEnableDisableRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(rc, []byte("export FOO=bar\neval \"$(ws completions bash)\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(append([]string{"--no-color"}, args...), strings.NewReader(""), &out, &errOut)
		return code, out.String() + errOut.String()
	}

	if code, out := run("--quiet", "completion", "enable", "--shell", "bash"); code != 0 {
		t.Fatalf("enable: %d %s", code, out)
	}
	b, _ := os.ReadFile(rc)
	if strings.Count(string(b), "ws completion bash") != 1 || strings.Contains(string(b), "ws completions") || !strings.Contains(string(b), "export FOO=bar") {
		t.Fatalf("after enable:\n%s", b)
	}
	if code, out := run("completion", "enable", "--shell", "bash"); code != 0 || !strings.Contains(out, "already enabled") {
		t.Fatalf("second enable: %d %s", code, out)
	}
	if code, out := run("--quiet", "completion", "disable", "--shell", "bash"); code != 0 {
		t.Fatalf("disable: %d %s", code, out)
	}
	b, _ = os.ReadFile(rc)
	if strings.Contains(string(b), "completion") || !strings.Contains(string(b), "export FOO=bar") {
		t.Fatalf("after disable:\n%s", b)
	}
	if code, out := run("completion", "disable", "--shell", "bash"); code != 0 || !strings.Contains(out, "not enabled") {
		t.Fatalf("second disable: %d %s", code, out)
	}
	if code, _ := run("completion", "enable", "--shell", "powershell"); code != 1 {
		t.Fatalf("an unsupported shell exits 1, got %d", code)
	}
}

// The old spelling keeps working, silently, because rc files call it at startup.
func TestOldCompletionsSpellingIsSilent(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"completions", "bash"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "_ws_completions") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
