package cmd

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/tui/app"
)

// commandHelps maps each top-level command with subcommands to its help.
var commandHelps = map[string]cmdHelp{
	"completions":           completionsHelp,
	"config":                configHelp,
	"cron":                  cronHelp,
	"dotfile":               dotfileHelp,
	"git-credential-helper": credentialHelp,
	"ignore":                ignoreHelp,
	"log":                   logHelp,
	"repo":                  repoHelp,
	"scratch":               scratchHelp,
	"secret":                secretHelp,
	"trash":                 trashHelp,
}

var (
	usageList  = regexp.MustCompile(`<([a-z-]+(?:\|[a-z-]+)+)>`)
	subcmdLine = regexp.MustCompile(`^  ([a-z][a-z-]*)(?: [^ ]+)*?\s{2,}\S`)
)

// subcommands reads a command's subcommands from its help: the <a|b|c> list
// in Usage, else the "  name   description" lines.
func subcommands(h cmdHelp) []string {
	if m := usageList.FindStringSubmatch(h.Usage); m != nil {
		return strings.Split(m[1], "|")
	}
	var out []string
	for _, l := range h.Subcommands {
		if m := subcmdLine.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// cliCommands is every "top" and "top sub" the CLI offers.
func cliCommands() []string {
	var out []string
	for _, top := range topLevelCommands {
		h, ok := commandHelps[top]
		if !ok {
			out = append(out, top)
			continue
		}
		for _, sub := range subcommands(h) {
			out = append(out, top+" "+sub)
		}
	}
	return out
}

// TestCLICoverage: every CLI command has a TUI coverage entry (a key, the
// screen's list, or a stated reason), and every entry names a real command.
// Adding a CLI command fails here until the TUI side is decided.
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

// TestCompletersOfferEverySubcommand: shell completion offers exactly the
// subcommands the help lists (aliases included).
func TestCompletersOfferEverySubcommand(t *testing.T) {
	for top, h := range commandHelps {
		want := subcommands(h)
		got := slices.Clone(completers[top].subcommands)
		if top == "git-credential-helper" {
			want = slices.DeleteFunc(want, func(s string) bool { return s == "get" || s == "store" || s == "erase" })
		}
		sort.Strings(want)
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: completion offers %v, help lists %v", top, got, want)
		}
	}
}
