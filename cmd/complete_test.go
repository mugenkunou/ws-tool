package cmd

import (
	"os"
	"path/filepath"
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
	for _, want := range []string{"ignore", "scratch", "log", "completions"} {
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
	// "sc" matches scan and scratch (search/secret start with "se").
	if !contains(comps, "scratch") || !contains(comps, "scan") {
		t.Errorf("expected scratch, scan in completions, got %v", comps)
	}
	if contains(comps, "search") || contains(comps, "secret") {
		t.Errorf("search/secret should NOT match 'sc' prefix, got %v", comps)
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
			wantSome:  []string{"new", "ls", "prune", "rm"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"scratch", "r"},
			wantSome:  []string{"rm"},
			wantNone:  []string{"new", "ls", "prune"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"ignore", ""},
			wantSome:  []string{"check", "scan", "fix", "ls", "tree", "edit", "generate"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"log", ""},
			wantSome:  []string{"start", "stop", "ls", "show", "search", "scan", "prune"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"notify", ""},
			wantSome:  []string{"start", "stop", "status", "test"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"dotfile", ""},
			wantSome:  []string{"add", "rm", "ls", "scan", "fix", "git"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"completions", ""},
			wantSome:  []string{"bash", "zsh", "fish", "install", "uninstall"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"config", ""},
			wantSome:  []string{"view", "defaults"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"repo", ""},
			wantSome:  []string{"ls", "scan", "fetch", "fix", "pull", "sync", "run"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"secret", ""},
			wantSome:  []string{"scan", "fix"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"trash", ""},
			wantSome:  []string{"enable", "status"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"context", ""},
			wantSome:  []string{"create"},
			directive: compDirectiveNoFileComp,
		},
		{
			args:      []string{"capture", ""},
			wantSome:  []string{"edit", "ls"},
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
			args:     []string{"log", "start", "--"},
			wantSome: []string{"--tag", "--quiet-start"},
		},
		{
			args:     []string{"ignore", "generate", "--"},
			wantSome: []string{"--merge", "--force", "--dry-run"},
		},
		{
			args:     []string{"search", "--"},
			wantSome: []string{"--type", "--path", "--context"},
		},
		{
			args:     []string{"capture", "--"},
			wantSome: []string{"--location", "--dry-run"},
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

func TestCompleteDotfileAdd(t *testing.T) {
	// dotfile add should also allow file completion.
	_, dir := resolveCompletions([]string{"dotfile", "add", ""}, globalFlags{})
	if dir != compDirectiveDefault {
		t.Fatalf("expected Default (file completion), got %d", dir)
	}
}

func TestCompleteDotfileGitSubSub(t *testing.T) {
	comps, dir := resolveCompletions([]string{"dotfile", "git", ""}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if !contains(comps, "setup") || !contains(comps, "status") {
		t.Fatalf("expected git sub-subcommands, got %v", comps)
	}
}

func TestCompleteSearchNoCompletions(t *testing.T) {
	comps, dir := resolveCompletions([]string{"search", ""}, globalFlags{})
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp for search, got %d", dir)
	}
	if len(comps) != 0 {
		t.Fatalf("expected no completions for search positional, got %v", comps)
	}
}

// ── Dynamic resource completion with temp workspace ────────────────

func TestCompleteScratchDelete(t *testing.T) {
	// Create a temp dir simulating the scratch directory with a few scratch entries.
	tmp := t.TempDir()
	for _, name := range []string{"2024-12-01_notes", "2024-12-02_draft", "2024-12-03_ideas"} {
		os.MkdirAll(filepath.Join(tmp, name), 0o755)
	}

	ctx := completionCtx{
		scratchIDs: listDirNames(tmp),
	}
	comps, dir := completeScratch("rm", nil, "", ctx)
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 3 {
		t.Fatalf("expected 3 scratch completions, got %v", comps)
	}
}

func TestCompleteScratchDeletePrefix(t *testing.T) {
	ctx := completionCtx{
		scratchIDs: []string{"2024-12-01_notes", "2024-12-02_draft", "2025-01-01_new"},
	}
	comps, _ := completeScratch("rm", nil, "2024", ctx)
	if len(comps) != 2 {
		t.Fatalf("expected 2 completions with prefix '2024', got %v", comps)
	}
}

func TestCompleteLogShow(t *testing.T) {
	ctx := completionCtx{
		logTags: []string{"work", "personal", "project-x"},
	}
	comps, dir := completeLog("show", nil, "", ctx)
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 3 {
		t.Fatalf("expected 3 log tag completions, got %v", comps)
	}
}

func TestCompleteLogShowPrefix(t *testing.T) {
	ctx := completionCtx{
		logTags: []string{"work", "personal", "project-x"},
	}
	comps, _ := completeLog("show", nil, "p", ctx)
	if len(comps) != 2 {
		t.Fatalf("expected personal and project-x, got %v", comps)
	}
}

func TestCompleteDotfileRm(t *testing.T) {
	ctx := completionCtx{
		dotfiles: []string{"vimrc", "/home/user/.vimrc", "bashrc", "/home/user/.bashrc"},
	}
	comps, dir := completeDotfile("rm", nil, "", ctx)
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 4 {
		t.Fatalf("expected 4 dotfile completions, got %v", comps)
	}
}

func TestCompleteCaptureLocationFlag(t *testing.T) {
	ctx := completionCtx{
		captureLocations: []string{"personal", "work", "archive"},
	}
	// Simulate: ws capture -l <TAB>
	comps, dir := completeCapture("", []string{"-l"}, "", ctx)
	if dir != compDirectiveNoFileComp {
		t.Fatalf("expected NoFileComp, got %d", dir)
	}
	if len(comps) != 3 {
		t.Fatalf("expected 3 location completions, got %v", comps)
	}
}

func TestCompleteCaptureLocationPrefix(t *testing.T) {
	ctx := completionCtx{
		captureLocations: []string{"personal", "work", "archive"},
	}
	comps, _ := completeCapture("", []string{"--location"}, "p", ctx)
	if len(comps) != 1 || comps[0] != "personal" {
		t.Fatalf("expected [personal], got %v", comps)
	}
}

func TestCompleteCaptureEditFileCompletion(t *testing.T) {
	_, dir := resolveCompletions([]string{"capture", "edit", ""}, globalFlags{})
	if dir != compDirectiveDefault {
		t.Fatalf("expected Default (file completion) for capture edit, got %d", dir)
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
	if !isStringFlag("--tag") {
		t.Fatal("--tag should be a string flag")
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
	if !strings.Contains(output, "check") || !strings.Contains(output, "scan") {
		t.Fatalf("expected ignore subcommands, got: %q", output)
	}
}
