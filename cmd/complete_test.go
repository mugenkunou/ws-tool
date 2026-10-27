package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ── resolveCompletions tests ───────────────────────────────────────

func TestCompleteTopLevel(t *testing.T) {
	comps, dir := resolveCompletions([]string{""}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp directive, got %d", dir)
	}
	if len(comps) == 0 {
		t.Fatal("expected top-level commands")
	}
	// Spot-check a few known commands.
	for _, want := range []string{"ignore", "scratch", "session", "completion", "repo", "pass"} {
		if !contains(comps, want) {
			t.Errorf("top-level completions missing %q", want)
		}
	}
}

func TestCompleteTopLevelPrefix(t *testing.T) {
	comps, dir := resolveCompletions([]string{"sc"}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp directive, got %d", dir)
	}
	for _, c := range comps {
		if !strings.HasPrefix(c, "sc") {
			t.Errorf("completion %q does not start with 'sc'", c)
		}
	}
	// "sc" matches scratch (scan and search are removed from top-level).
	if !contains(comps, "scratch") {
		t.Errorf("expected scratch in completions, got %v", comps)
	}
	if contains(comps, "scan") || contains(comps, "search") {
		t.Errorf("scan/search should NOT be top-level commands, got %v", comps)
	}
}

func TestCompleteGlobalFlags(t *testing.T) {
	comps, dir := resolveCompletions([]string{"--"}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if !contains(comps, "--json") {
		t.Fatalf("expected --json in global flags, got %v", comps)
	}
	if !contains(comps, "--workspace") {
		t.Fatalf("expected --workspace in global flags, got %v", comps)
	}
}

func TestCompleteUnknownCommand(t *testing.T) {
	comps, dir := resolveCompletions([]string{"nosuchcommand", ""}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 0 {
		t.Fatalf("expected no completions for unknown command, got %v", comps)
	}
}

// ── Subcommand completion ──────────────────────────────────────────

func TestCompleteSubcommands(t *testing.T) {
	tests := []struct {
		args      []string
		wantSome  []string // completions that must appear
		wantNone  []string // completions that must NOT appear
		directive int
	}{
		{
			args:      []string{"scratch", ""},
			wantSome:  []string{"new", "add", "ls", "rm"},
			wantNone:  []string{"search", "grep"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"scratch", "r"},
			wantSome:  []string{"rm"},
			wantNone:  []string{"new", "ls"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"ignore", ""},
			wantSome:  []string{"check", "lint", "add", "rm", "mute", "unmute", "ls", "tree", "edit"},
			wantNone:  []string{"scan", "fix"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"session", ""},
			wantSome:  []string{"record", "stop", "ls", "show", "grep", "rm"},
			wantNone:  []string{"start"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"symlink", ""},
			wantSome:  []string{"add", "link", "unlink", "ls"},
			wantNone:  []string{"rm", "scan", "fix", "sync", "reset", "git", "migrate", "edit", "backup"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"completion", ""},
			wantSome:  []string{"bash", "zsh", "fish", "enable", "disable"},
			wantNone:  []string{"install", "uninstall"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"config", ""},
			wantSome:  []string{"show", "edit"},
			wantNone:  []string{"view", "defaults"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"repo", ""},
			wantSome:  []string{"ls", "show", "git", "run", "configure", "root"},
			wantNone:  []string{"scan", "sync", "add-root", "ls-roots", "fetch", "pull", "push", "save"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"repo", "root", ""},
			wantSome:  []string{"add", "ls", "rm"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"secret", ""},
			wantSome:  []string{"ls", "show", "allow", "mute", "unmute", "store"},
			wantNone:  []string{"scan", "fix", "setup"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"trash", ""},
			wantSome:  []string{"enable", "empty", "status", "restore", "prune"},
			wantNone:  []string{"setup", "purge"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"help", "repo", ""},
			wantSome:  []string{"ls", "root"},
			directive: compDirectiveNoFileComp,
		},
	}

	for _, tt := range tests {
		name := strings.Join(tt.args, " ")
		t.Run(name, func(t *testing.T) {
			comps, dir := resolveCompletions(tt.args, globalFlags{})
			if dir != tt.directive {
				t.Errorf("directive: got %d, want %d", dir, tt.directive)
			}
			for _, want := range tt.wantSome {
				if !contains(comps, want) {
					t.Errorf("missing completion %q in %v", want, comps)
				}
			}
			for _, nope := range tt.wantNone {
				if contains(comps, nope) {
					t.Errorf("unexpected completion %q in %v", nope, comps)
				}
			}
		})
	}
}

// ── Command flag completion ────────────────────────────────────────

func TestCompleteCommandFlags(t *testing.T) {
	tests := []struct {
		args     []string
		wantSome []string
	}{
		{
			args:     []string{"scratch", "new", "--"},
			wantSome: []string{"--no-open", "--editor", "--dry-run"},
		},
		{
			args:     []string{"session", "record", "--"},
			wantSome: []string{"--no-prompt", "--dry-run"},
		},
		{
			args:     []string{"ignore", "ls", "--"},
			wantSome: []string{"--muted", "--filter", "--sort", "--wide"},
		},
		{
			args:     []string{"trash", "prune", "--"},
			wantSome: []string{"--older-than", "--dry-run"},
		},
		{
			args:     []string{"repo", "git", "--"},
			wantSome: []string{"--yes", "--dirty", "--path"},
		},
	}

	for _, tt := range tests {
		name := strings.Join(tt.args, " ")
		t.Run(name, func(t *testing.T) {
			comps, dir := resolveCompletions(tt.args, globalFlags{})
			if dir != compDirectiveNoFileComp {
				t.Errorf("directive: got %d, want %d", dir, compDirectiveNoFileComp)
			}
			for _, want := range tt.wantSome {
				if !contains(comps, want) {
					t.Errorf("missing flag %q in %v", want, comps)
				}
			}
		})
	}
}

// Global flags should also appear alongside command flags.
func TestCompleteGlobalFlagsWithCommand(t *testing.T) {
	comps, _ := resolveCompletions([]string{"scratch", "new", "--j"}, globalFlags{})
	if !contains(comps, "--json") {
		t.Fatalf("expected --json in flags, got %v", comps)
	}
}

// ── Dynamic resolver tests ─────────────────────────────────────────

func TestCompleteIgnoreCheckFileCompletion(t *testing.T) {
	// ignore check should return Default directive to allow file completion.
	_, dir := resolveCompletions([]string{"ignore", "check", ""}, globalFlags{})
	if dir != compDirectiveDefault {
		t.Fatalf("expected Default (file completion), got %d", dir)
	}
}

func TestCompleteSymlinkAdd(t *testing.T) {
	// symlink add should also allow file completion.
	_, dir := resolveCompletions([]string{"symlink", "add", ""}, globalFlags{})
	if dir != compDirectiveDefault {
		t.Fatalf("expected Default (file completion), got %d", dir)
	}
}

func TestCompleteSearchRemoved(t *testing.T) {
	// search is not a top-level command; completion should return nothing.
	comps, dir := resolveCompletions([]string{"search", ""}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp for search, got %d", dir)
	}
	if len(comps) != 0 {
		t.Fatalf("expected no completions for removed search command, got %v", comps)
	}
}

// ── Dynamic resource completion ────────────────────────────────────

func TestCompleteScratchNames(t *testing.T) {
	ctx := completionCtx{scratchIDs: []string{"2024-12-01_notes", "2024-12-02_draft", "2024-12-03_ideas"}}
	comps, dir := scratchNames(nil, "", ctx)
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 3 {
		t.Fatalf("expected 3 scratch completions, got %v", comps)
	}
	comps, _ = scratchNames([]string{"2024-12-01_notes"}, "2024", ctx)
	if len(comps) != 2 {
		t.Fatalf("expected the 2 names not yet typed, got %v", comps)
	}
}

func TestCompleteScratchTagOnlyName(t *testing.T) {
	ctx := completionCtx{scratchIDs: []string{"dns", "proxy"}}
	if comps, _ := scratchFirst(nil, "", ctx); len(comps) != 2 {
		t.Fatalf("expected both names for the first argument, got %v", comps)
	}
	if comps, _ := scratchFirst([]string{"dns"}, "", ctx); len(comps) != 0 {
		t.Fatalf("tags are free text; expected none, got %v", comps)
	}
}

func TestCompleteSessionTags(t *testing.T) {
	ctx := completionCtx{logTags: []string{"work", "personal", "project-x"}}
	comps, dir := sessionTags(nil, "", ctx)
	if dir != compDirectiveNoFileComp || len(comps) != 3 {
		t.Fatalf("expected 3 tags and NoFileComp, got %v %d", comps, dir)
	}
	if comps, _ := sessionTags(nil, "p", ctx); len(comps) != 2 {
		t.Fatalf("expected personal and project-x, got %v", comps)
	}
}

func TestCompleteSymlinkTargets(t *testing.T) {
	ctx := completionCtx{symlinks: []string{"/home/user/.vimrc", "/home/user/.bashrc"}}
	comps, dir := symlinkTargets(nil, "", ctx)
	if dir != compDirectiveNoFileComp || len(comps) != 2 {
		t.Fatalf("expected 2 system paths, got %v %d", comps, dir)
	}
}

func TestCompleteTrashAndCronTargets(t *testing.T) {
	ctx := completionCtx{trashItems: []string{"old-notes.md", "build"}}
	if comps, _ := trashTargets(nil, "o", ctx); !slices.Equal(comps, []string{"old-notes.md"}) {
		t.Fatalf("trash items: %v", comps)
	}
	comps, _ := cronTargets(nil, "", ctx)
	for _, retired := range []string{"secret-scan", "ignore-scan", "repo-sync", "session-prune"} {
		if contains(comps, retired) {
			t.Errorf("cron completion offers the old name %q", retired)
		}
	}
	for _, job := range []string{"mega-sync", "pass-backup", "sync-push"} {
		if !contains(comps, job) {
			t.Errorf("cron completion lacks %q", job)
		}
	}
}

func TestCompleteShellFlagValue(t *testing.T) {
	comps, _ := resolveCompletions([]string{"completion", "enable", "--shell", ""}, globalFlags{})
	if !slices.Equal(comps, []string{"bash", "zsh", "fish"}) {
		t.Fatalf("--shell values: %v", comps)
	}
}

// ── splitArgsForCompletion tests ───────────────────────────────────

func TestSplitArgsForCompletion(t *testing.T) {
	tests := []struct {
		name         string
		raw          []string
		wantPos      []string
		wantComplete string
	}{
		{
			name:         "empty",
			raw:          []string{""},
			wantPos:      nil,
			wantComplete: "",
		},
		{
			name:         "single command partial",
			raw:          []string{"scr"},
			wantPos:      nil,
			wantComplete: "scr",
		},
		{
			name:         "command with trailing space",
			raw:          []string{"scratch", ""},
			wantPos:      []string{"scratch"},
			wantComplete: "",
		},
		{
			name:         "command sub partial",
			raw:          []string{"scratch", "del"},
			wantPos:      []string{"scratch"},
			wantComplete: "del",
		},
		{
			name:         "strip bool flag",
			raw:          []string{"scratch", "--quiet", "new", ""},
			wantPos:      []string{"scratch", "new"},
			wantComplete: "",
		},
		{
			name:         "strip string flag with value",
			raw:          []string{"--workspace", "/home/test", "scratch", ""},
			wantPos:      []string{"scratch"},
			wantComplete: "",
		},
		{
			name:         "strip flag=value form",
			raw:          []string{"--workspace=/home/test", "scratch", ""},
			wantPos:      []string{"scratch"},
			wantComplete: "",
		},
		{
			name:         "flag completion",
			raw:          []string{"scratch", "new", "--ed"},
			wantPos:      []string{"scratch", "new"},
			wantComplete: "--ed",
		},
		{
			name:         "double dash stops stripping",
			raw:          []string{"--", "--not-a-flag", ""},
			wantPos:      []string{"--not-a-flag"},
			wantComplete: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, toComplete := splitArgsForCompletion(tt.raw)
			if toComplete != tt.wantComplete {
				t.Errorf("toComplete: got %q, want %q", toComplete, tt.wantComplete)
			}
			if len(pos) != len(tt.wantPos) {
				t.Fatalf("positional: got %v (len %d), want %v (len %d)", pos, len(pos), tt.wantPos, len(tt.wantPos))
			}
			for i := range pos {
				if pos[i] != tt.wantPos[i] {
					t.Errorf("positional[%d]: got %q, want %q", i, pos[i], tt.wantPos[i])
				}
			}
		})
	}
}

// ── Helper function tests ──────────────────────────────────────────

func TestFilterPrefix(t *testing.T) {
	candidates := []string{"apple", "apricot", "banana", "avocado"}

	got := filterPrefix(candidates, "ap")
	if len(got) != 2 || got[0] != "apple" || got[1] != "apricot" {
		t.Fatalf("filterPrefix('ap'): got %v", got)
	}

	got = filterPrefix(candidates, "")
	if len(got) != 4 {
		t.Fatalf("filterPrefix(''): expected all candidates, got %v", got)
	}

	got = filterPrefix(candidates, "xyz")
	if len(got) != 0 {
		t.Fatalf("filterPrefix('xyz'): expected empty, got %v", got)
	}
}

func TestDedupe(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b", "d"}
	got := dedupe(input)
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("dedupe: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("dedupe[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b", "c"}, "b") {
		t.Fatal("contains should find 'b'")
	}
	if contains([]string{"a", "b", "c"}, "d") {
		t.Fatal("contains should not find 'd'")
	}
}

func TestListDirNames(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "dir1"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "dir2"), 0o755)
	os.WriteFile(filepath.Join(tmp, "file1.txt"), []byte("x"), 0o644)

	names := listDirNames(tmp)
	if len(names) != 2 {
		t.Fatalf("expected 2 dirs, got %v", names)
	}

	// Non-existent directory should return nil.
	names = listDirNames(filepath.Join(tmp, "nope"))
	if names != nil {
		t.Fatalf("expected nil for non-existent dir, got %v", names)
	}
}

func TestIsStringFlag(t *testing.T) {
	if !isStringFlag("--workspace") {
		t.Fatal("--workspace should be a string flag")
	}
	if !isStringFlag("--older-than") {
		t.Fatal("--older-than should be a string flag")
	}
	if isStringFlag("--quiet") {
		t.Fatal("--quiet should not be a string flag")
	}
	if isStringFlag("--json") {
		t.Fatal("--json should not be a string flag")
	}
}

// ── runComplete integration test ───────────────────────────────────

func TestRunCompleteProtocol(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := runComplete([]string{"scratch", ""}, globalFlags{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("runComplete returned %d, stderr=%s", code, errOut.String())
	}

	output := out.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least completions + directive, got: %q", output)
	}

	// Last line must be :<directive>.
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, ":") {
		t.Fatalf("expected last line to be directive, got: %q", last)
	}

	// Should contain subcommand completions.
	if !strings.Contains(output, "new") || !strings.Contains(output, "rm") {
		t.Fatalf("expected scratch subcommands in output, got: %q", output)
	}
}

func TestRunCompleteViaExecute(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"__complete", "ignore", ""}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("Execute __complete returned %d, stderr=%s", code, errOut.String())
	}

	output := out.String()
	if !strings.Contains(output, "check") || !strings.Contains(output, "lint") {
		t.Fatalf("expected ignore subcommands, got: %q", output)
	}
}

// ── Flag-value completion tests (Issue #5) ────────────────────────

func TestFlagValueCompletionWorkspace(t *testing.T) {
	// ws --workspace <TAB> should return FilterDirs so only directories are shown.
	_, dir := resolveCompletions([]string{"--workspace", ""}, globalFlags{})
	if dir != compDirectiveFilterDirs {
		t.Fatalf("--workspace <TAB>: expected FilterDirs (%d), got %d", compDirectiveFilterDirs, dir)
	}
}

func TestFlagValueCompletionWorkspaceShort(t *testing.T) {
	_, dir := resolveCompletions([]string{"-w", ""}, globalFlags{})
	if dir != compDirectiveFilterDirs {
		t.Fatalf("-w <TAB>: expected FilterDirs, got %d", dir)
	}
}

func TestFlagValueCompletionBoolFlag(t *testing.T) {
	// --quiet is a bool flag; should fall through to normal completion (not a value).
	comps, dir := resolveCompletions([]string{"--quiet", ""}, globalFlags{})
	// Should offer top-level commands (not treat "" as a flag value).
	if dir == compDirectiveDefault || dir == compDirectiveFilterDirs {
		t.Fatalf("--quiet <TAB>: bool flag should not trigger value completion, got dir=%d comps=%v", dir, comps)
	}
}

func TestFlagValueCompletionEqualForm(t *testing.T) {
	// --workspace=/etc/ stays as a single token; should not enter value-completion path.
	// The word being completed starts with "/etc/" which is not a "-", so normal flow.
	comps, dir := resolveCompletions([]string{"--workspace=/etc/"}, globalFlags{})
	// This is treated as the partial word; prev doesn't exist, so no flag-value path.
	_ = comps
	_ = dir
	// Just ensure it doesn't panic and returns something sensible.
}
