package cmd

import (
	"bytes"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/app"
	"github.com/mugenkunou/ws-tool/internal/tui/nav"
)

// glossaryVerbs is spec §3.3, the closed list of verbs. `completion bash|zsh|fish`
// name shells, not verbs, and are exempt.
var glossaryVerbs = []string{
	"ls", "show", "add", "new", "rm", "edit", "open", "enable", "disable", "status",
	"mute", "unmute", "prune", "run", "git", "push", "lint", "configure",
	"apply", "link", "unlink", "tag", "check",
	"tree", "allow", "store", "init", "audit", "restore", "empty", "log",
	"scan", "install", "uninstall", "eject",
}

// subNouns are the two sub-nouns spec §3.1 allows. They stand between a noun
// and its verb.
var subNouns = []string{"root", "backup"}

// TestGlossaryVerbs: every verb in the command table is in the glossary.
func TestGlossaryVerbs(t *testing.T) {
	shells := []string{"bash", "zsh", "fish"}
	for _, c := range commands {
		if len(c.Path) < 2 {
			continue // workspace commands without a noun
		}
		verb := c.Path[len(c.Path)-1]
		if c.Path[0] == "completion" && slices.Contains(shells, verb) {
			continue
		}
		if !slices.Contains(glossaryVerbs, verb) {
			t.Errorf("%q: verb %q is not in the glossary (spec §3.3)", c.name(), verb)
		}
		for _, mid := range c.Path[1 : len(c.Path)-1] {
			if !slices.Contains(subNouns, mid) {
				t.Errorf("%q: %q is not a sub-noun (spec §3.1)", c.name(), mid)
			}
		}
	}
}

// TestHelpEveryCommand: every row's --help prints its own usage, and the noun's
// help lists every one of its verbs.
func TestHelpEveryCommand(t *testing.T) {
	testSetXDG(t)
	for _, c := range commands {
		var out, errOut bytes.Buffer
		if c.Program != "" {
			// `ws session --help` is the program's own help (TestSessionPassesEverythingThrough);
			// `ws help session` is the table row.
			if code := Execute(append([]string{"help"}, c.Path...), strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), c.usage()) {
				t.Errorf("ws help %s: exit %d, no usage %q:\n%s", c.name(), code, c.usage(), out.String())
			}
			continue
		}
		args := append(slices.Clone(c.Path), "--help")
		if code := Execute(args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Errorf("ws %s --help: exit %d, stderr %q", c.name(), code, errOut.String())
			continue
		}
		if !strings.Contains(out.String(), c.usage()) {
			t.Errorf("ws %s --help does not print its usage %q:\n%s", c.name(), c.usage(), out.String())
		}
		if c.Fields != nil {
			for _, f := range c.Fields() {
				if !strings.Contains(out.String(), f) {
					t.Errorf("ws %s --help does not name field %q", c.name(), f)
				}
			}
		}

		var viaHelp bytes.Buffer
		Execute(append([]string{"help"}, c.Path...), strings.NewReader(""), &viaHelp, &errOut)
		if viaHelp.String() != out.String() {
			t.Errorf("ws help %s differs from ws %s --help", c.name(), c.name())
		}
	}
	for _, noun := range nextWords(nil) {
		if len(nextWords([]string{noun})) == 0 {
			continue
		}
		var out, errOut bytes.Buffer
		if code := Execute([]string{noun, "--help"}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Errorf("ws %s --help: exit %d", noun, code)
		}
		for _, c := range commandsUnder([]string{noun}) {
			verb := strings.Join(c.Path[1:], " ")
			if !strings.Contains(out.String(), verb) {
				t.Errorf("ws %s --help does not list %q:\n%s", noun, verb, out.String())
			}
		}
	}
}

// TestHelpUnknownCommand: an unknown noun or verb is an error on stderr.
func TestHelpUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"help", "nonsense"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "unknown command: nonsense") || out.Len() != 0 {
		t.Fatalf("stderr %q stdout %q", errOut.String(), out.String())
	}
}

// cliCommands is every command the TUI coverage must account for: the table,
// plus the plumbing other programs call.
func cliCommands() []string {
	return append(commandNames(), plumbingCommands...)
}

// TestCLICoverage: every CLI command has a TUI coverage entry (a key, a `:`
// command, the screen's list, or a stated reason), and every entry names a
// real command. Adding a command fails here until the TUI side is decided.
func TestCLICoverage(t *testing.T) {
	cli := cliCommands()
	covered := map[string]bool{}
	for _, c := range app.CLICoverage {
		if covered[c.CLI] {
			t.Errorf("coverage lists %q twice", c.CLI)
		}
		covered[c.CLI] = true
		if !slices.Contains(cli, c.CLI) {
			t.Errorf("coverage lists %q, which the CLI does not have", c.CLI)
		}
	}
	for _, c := range cli {
		if !covered[c] {
			t.Errorf("CLI command %q has no TUI coverage entry (internal/tui/app/coverage.go)", c)
		}
	}
}

// TestCompletersOfferEverySubcommand: shell completion offers exactly the verbs
// the help lists, at every level, and no hidden alias.
func TestCompletersOfferEverySubcommand(t *testing.T) {
	var prefixes [][]string
	prefixes = append(prefixes, nil)
	seen := map[string]bool{}
	for _, c := range commands {
		for n := 1; n < len(c.Path); n++ {
			key := strings.Join(c.Path[:n], " ")
			if !seen[key] {
				seen[key] = true
				prefixes = append(prefixes, c.Path[:n])
			}
		}
	}
	for _, prefix := range prefixes {
		want := slices.Clone(nextWords(prefix))
		args := append(slices.Clone(prefix), "")
		got, _ := resolveCompletions(args, globalFlags{})
		got = slices.Clone(got)
		sort.Strings(want)
		sort.Strings(got)
		if prefix == nil {
			want = slices.Clone(topLevelCommands)
			sort.Strings(want)
		}
		if !slices.Equal(got, want) {
			t.Errorf("ws %s: completion offers %v, help lists %v", strings.Join(prefix, " "), got, want)
		}
	}
}

// TestCompletionFlagsMatchHelp: a flag is completed exactly when help documents it.
func TestCompletionFlagsMatchHelp(t *testing.T) {
	for _, c := range commands {
		args := append(slices.Clone(c.Path), "--")
		got, _ := resolveCompletions(args, globalFlags{})
		for _, f := range commandFlags(c) {
			if !strings.HasPrefix(f, "--") {
				continue // short flags are offered after a single "-"
			}
			if !slices.Contains(got, f) {
				t.Errorf("ws %s: completion does not offer documented flag %s", c.name(), f)
			}
		}
	}
}

// TestDispatchOnlyRunsTableCommands: Execute runs a command only when the table
// holds it, so a verb wired into a dispatcher but missing from the table fails
// as unknown instead of slipping past help, completion and the TUI coverage.
func TestDispatchOnlyRunsTableCommands(t *testing.T) {
	for _, c := range commands {
		if unknown, _ := unknownCommand(append(slices.Clone(c.Path), "arg", "--flag")); unknown {
			t.Errorf("table row %q is rejected as unknown", c.name())
		}
	}
	for _, tc := range []struct {
		args    []string
		unknown string // "" when the words must pass
	}{
		{[]string{"repo"}, ""},
		{[]string{"repo", "root"}, ""},
		{[]string{"pass", "backup", "--help"}, ""},
		{[]string{"git-credential-helper", "future-op"}, ""},
		{[]string{"__complete", "repo", ""}, ""},
		{[]string{"repo", "frobnicate"}, "repo frobnicate"},
		{[]string{"repo", "root", "frobnicate", "x"}, "repo root frobnicate"},
		{[]string{"completion", "powershell"}, "completion powershell"},
		{[]string{"nonsense"}, "nonsense"},
	} {
		unknown, name := unknownCommand(tc.args)
		if unknown != (tc.unknown != "") || name != tc.unknown {
			t.Errorf("unknownCommand(%q) = %v, %q; want unknown %q", tc.args, unknown, name, tc.unknown)
		}
	}
}

// TestEveryNounHasATUIHome: every CLI noun, and every command without one, is
// a TUI screen's noun or is hosted by one (nav.Spec.Hosts), so a new noun
// cannot land with its commands scattered or parked nowhere.
func TestEveryNounHasATUIHome(t *testing.T) {
	seen := map[string]bool{}
	words := append(commandNames(), plumbingCommands...)
	for _, n := range nounSummaries {
		words = append(words, n.Name)
	}
	for _, n := range workspaceSummaries {
		words = append(words, n.Name)
	}
	for _, w := range words {
		noun := strings.Fields(w)[0]
		if seen[noun] {
			continue
		}
		seen[noun] = true
		if _, ok := nav.ForNoun(noun); !ok {
			t.Errorf("CLI noun %q has no TUI screen: name a screen after it or host it (Hosts in internal/tui/nav/nav.go specs)", noun)
		}
	}
}
