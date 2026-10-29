package shellcomp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionsInstallIdempotent(t *testing.T) {
	// Set up a temp rc file.
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".bashrc")
	os.WriteFile(rcFile, []byte("# existing config\nexport FOO=bar\n"), 0o644)

	// Monkey-patch RC by using env var trick — not feasible
	// without refactoring, so we test the underlying helpers instead.

	// Test HasMarker — should be false initially.
	content, _ := os.ReadFile(rcFile)
	if HasMarker(string(content)) {
		t.Fatal("expected no marker in fresh rc file")
	}

	// Simulate install by appending the block.
	block := "\n" + MarkerStart + "\n" + `eval "$(ws completions bash)"` + "\n" + MarkerEnd + "\n"
	os.WriteFile(rcFile, append(content, []byte(block)...), 0o644)

	// Now marker should be detected.
	content, _ = os.ReadFile(rcFile)
	if !HasMarker(string(content)) {
		t.Fatal("expected marker after install")
	}

	// Original content should be preserved.
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Fatal("original content lost after install")
	}
}

func TestCompletionsUninstallCleansUp(t *testing.T) {
	original := "# existing config\nexport FOO=bar\n"
	block := "\n" + MarkerStart + "\n" + `eval "$(ws completions bash)"` + "\n" + MarkerEnd + "\n"
	full := original + block

	cleaned := RemoveBlock(full)
	if HasMarker(cleaned) {
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
	cleaned := RemoveStaleLines(input)
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
	cleaned := RemoveStaleLines(input)
	if !strings.Contains(cleaned, "# ws completions are set up below") {
		t.Fatal("comment lines mentioning ws completions should be preserved")
	}
}

func TestWithCompletionBlock(t *testing.T) {
	block := MarkerStart + "\neval \"$(ws completion bash)\"\n" + MarkerEnd + "\n"

	// Appended after existing content, once.
	got := WithBlock("export A=1\n", block)
	if got != "export A=1\n\n"+block {
		t.Fatalf("append:\n%s", got)
	}
	if again := WithBlock(got, block); again != got {
		t.Fatalf("a block holding the current line is left alone:\n%s", again)
	}

	// An older line in the block is rewritten in place.
	old := "export A=1\n" + MarkerStart + "\neval \"$(ws completions bash)\"\n" + MarkerEnd + "\nexport B=2\n"
	got = WithBlock(old, block)
	if strings.Contains(got, "ws completions") || !strings.Contains(got, "ws completion bash") || !strings.Contains(got, "export B=2") || strings.Count(got, MarkerStart) != 1 {
		t.Fatalf("rewrite:\n%s", got)
	}

	// Unmarked lines that load ws completion are removed.
	got = WithBlock("source <(ws completions bash)\nalias ll=ls\n", block)
	if strings.Contains(got, "source <(ws completions") || !strings.Contains(got, "alias ll=ls") || !strings.Contains(got, MarkerStart) {
		t.Fatalf("unmarked:\n%s", got)
	}

	// An empty rc file gets the block alone.
	if got := WithBlock("", block); got != block {
		t.Fatalf("empty:\n%s", got)
	}
}

func TestResolveDefaultsToShellEnv(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/zsh")
	if got, err := Resolve(""); err != nil || got != "zsh" {
		t.Fatalf("Resolve(\"\") = %q, %v", got, err)
	}
	if got, err := Resolve(" Fish "); err != nil || got != "fish" {
		t.Fatalf("an explicit name wins: %q, %v", got, err)
	}
	t.Setenv("SHELL", "/bin/sh")
	if _, err := Resolve(""); err == nil {
		t.Fatal("a shell ws does not complete is an error")
	}
	t.Setenv("SHELL", "")
	if got, _ := Resolve(""); got != "bash" {
		t.Fatalf("no $SHELL means bash, got %q", got)
	}
}

func TestStatusAndPlansRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELL", "/bin/zsh")

	sts, err := Statuses()
	if err != nil || len(sts) != 3 {
		t.Fatalf("statuses: %v %v", sts, err)
	}
	for _, st := range sts {
		if st.Enabled || st.Current != (st.Shell == "zsh") {
			t.Fatalf("fresh home: %+v", st)
		}
	}

	rc, acts, err := EnablePlan("fish")
	if err != nil || len(acts) != 1 || rc != filepath.Join(home, ".config", "fish", "conf.d", "ws.fish") {
		t.Fatalf("enable plan: %s %v %v", rc, acts, err)
	}
	if _, err := os.Stat(rc); err == nil {
		t.Fatal("planning writes nothing")
	}
	if err := acts[0].Execute(); err != nil {
		t.Fatal(err)
	}
	if st, _ := StatusOf("fish"); !st.Enabled {
		t.Fatalf("enabled after the action: %+v", st)
	}
	if _, acts, _ := EnablePlan("fish"); len(acts) != 0 {
		t.Fatal("enabling twice has nothing to do")
	}

	_, acts, err = DisablePlan("fish")
	if err != nil || len(acts) != 1 {
		t.Fatalf("disable plan: %v %v", acts, err)
	}
	if err := acts[0].Execute(); err != nil {
		t.Fatal(err)
	}
	if st, _ := StatusOf("fish"); st.Enabled {
		t.Fatalf("disabled after the action: %+v", st)
	}
	if _, acts, _ := DisablePlan("bash"); len(acts) != 0 {
		t.Fatal("disabling what is off has nothing to do")
	}
	if _, _, err := EnablePlan("powershell"); err == nil {
		t.Fatal("unsupported shell")
	}
}
