package cmd

import (
	"strings"
	"testing"
)

// TestIgnoreCheckSymlinksTreeSyncsAndNamesTheRule: ws init gives the workspace the
// built-in symlinks/** include (plan 0003 step 2.4), so a dot-file in the mirrored
// tree syncs, and `ignore check` names the rule. A user's `ignore add` inside it
// still excludes.
func TestIgnoreCheckSymlinksTreeSyncsAndNamesTheRule(t *testing.T) {
	e := newIgEnv(t)
	e.write(t, "symlinks/home/.gitconfig", "[user]\n")
	e.write(t, "symlinks/home/.cache/junk", "x")

	code, out, errOut := e.run("", "ignore", "check", e.abs("symlinks/home/.gitconfig"))
	if code != 0 || !strings.Contains(out, "SYNCED") || !strings.Contains(out, "+g:symlinks/**") || !strings.Contains(out, "built-in") {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}

	e.do(t, "ignore", "add", e.abs("symlinks/home/.cache"))
	_, out, _ = e.run("", "ignore", "check", e.abs("symlinks/home/.cache/junk"))
	igHas(t, "ignored inside symlinks/", out, "IGNORED")
	if mega := e.readFile(t, ".megaignore"); strings.Count(mega, "+g:symlinks/**") != 1 ||
		strings.Index(mega, "-:symlinks/home/.cache") < strings.Index(mega, "+g:symlinks/**") {
		t.Fatalf(".megaignore:\n%s", mega)
	}
}

// TestSymlinksTreeIsSeenByLintAndSecretLs: every consumer of the rules sees the
// built-in include. A big file under symlinks/ is a muted lint warning (not a live
// one), and `secret ls` scans the dot-files there instead of skipping them as
// excluded.
func TestSymlinksTreeIsSeenByLintAndSecretLs(t *testing.T) {
	e := newIgEnv(t)
	e.bigFile(t, "symlinks/home/.config/big.bin")
	e.bigFile(t, "notes/big.bin")
	code, env := e.jsonOf(t, "ignore", "lint")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, r := range env["data"].([]any) {
		if p := r.(map[string]any)["path"].(string); strings.Contains(p, "symlinks/") {
			t.Fatalf("a warning under symlinks/ must be muted, got a live row for %s", p)
		}
	}
	if code, out, _ := e.run("", "ignore", "ls", "--muted"); code != 0 || !strings.Contains(out, "symlinks/home/.config/big.bin") {
		t.Fatalf("the muted view lists it: exit %d\n%s", code, out)
	}

	e.write(t, "symlinks/home/.aws/credentials", "[default]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\naws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n")
	code, out, errOut := e.run("", "secret", "ls", "--json")
	if code != 0 || !strings.Contains(out, "symlinks/home/.aws/credentials") {
		t.Fatalf("secret ls must scan the dot-files under symlinks/: exit %d out=%s err=%s", code, out, errOut)
	}
}
