package ignore

import (
	"strings"
	"testing"
)

// The mirrored tree of `ws symlink` keeps host names (.gitconfig, .ssh/, .config/),
// so symlinks/** is the one built-in included directory (ADR 0003 §2).

func TestSymlinksTreeSyncsHostNames(t *testing.T) {
	e := BuildEngine(DefaultUserRules())
	for _, p := range []string{
		"symlinks/home/.gitconfig",
		"symlinks/home/.ssh/id_rsa",
		"symlinks/home/.config/nvim/init.lua",
		"symlinks/system/etc/hosts",
	} {
		res := e.Evaluate(p, false)
		if !res.Included {
			t.Errorf("%s must sync, rule=%s", p, res.Rule)
		}
		if res.Rule != SymlinksIncludeRule || res.RuleSource != SourceBuiltin || !res.ByInclude {
			t.Errorf("%s: want the built-in %s to decide, got %+v", p, SymlinksIncludeRule, res)
		}
	}
	// The rule is scoped: a dot-file elsewhere is still excluded.
	if res := e.Evaluate("notes/.gitconfig", false); res.Included {
		t.Errorf("notes/.gitconfig must stay excluded: %+v", res)
	}
}

func TestSymlinksIncludeIsFixedAndNotInUserRules(t *testing.T) {
	r := DefaultUserRules()
	if len(r.Include) != 0 {
		t.Fatalf("symlinks/** is not stored in ignore_rules: %+v", r.Include)
	}
	// Suppressing every default exclude does not touch it.
	r.SuppressDefaults = []string{"symlinks/**", "+g:symlinks/**"}
	if res := BuildEngine(r).Evaluate("symlinks/home/.bashrc", false); !res.Included || res.Rule != SymlinksIncludeRule {
		t.Fatalf("the rule cannot be suppressed: %+v", res)
	}
	if got := GenerateMegaignore(DefaultUserRules()); strings.Count(got, SymlinksIncludeRule) != 1 {
		t.Fatalf(".megaignore must hold the rule once:\n%s", got)
	}
}

// TestUserIgnoredPathInsideSymlinksStillWins: user excludes come after the
// built-in include, so `ignore add` inside symlinks/ still excludes.
func TestUserIgnoredPathInsideSymlinksStillWins(t *testing.T) {
	r := DefaultUserRules()
	r.Exclude = append(r.Exclude, RuleEntry{Pattern: "symlinks/home/.cache"})
	e := BuildEngine(r)
	if res := e.Evaluate("symlinks/home/.cache", true); res.Included {
		t.Fatalf("the user's ignored path must win: %+v", res)
	}
	if res := e.Evaluate("symlinks/home/.gitconfig", false); !res.Included {
		t.Fatalf("the rest of the tree keeps syncing: %+v", res)
	}
	mega := GenerateMegaignore(r)
	if strings.Index(mega, "-:symlinks/home/.cache") < strings.Index(mega, SymlinksIncludeRule) {
		t.Fatalf("user excludes must follow the built-in include:\n%s", mega)
	}
}
