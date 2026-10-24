package cmd

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
)

const helpFlag = "  -h, --help    Show help"

// cmdHelp is the hand-written help of the commands that have not moved onto
// the command table yet (spec §4.2 is rendered from commands.go). It is only
// used for usage-error lines and for hidden commands.
type cmdHelp struct {
	Usage       string   // e.g. "ws ignore <scan|check|fix|ls|tree|edit|generate>"
	Description string   // optional long description
	Subcommands []string // optional "  name   description" lines
	Flags       []string // extra flags (helpFlag is always appended)
	SetupFlags  []string // optional separate section (e.g. trash setup flags)
}

// String builds the full multi-line help text.
func (h cmdHelp) String() string {
	var parts []string

	parts = append(parts, "Usage:", "  "+h.Usage, "")

	if h.Description != "" {
		parts = append(parts, "Description:", "  "+h.Description, "")
	}

	if len(h.Subcommands) > 0 {
		parts = append(parts, "Subcommands:")
		parts = append(parts, h.Subcommands...)
		parts = append(parts, "")
	}

	if len(h.SetupFlags) > 0 {
		parts = append(parts, "Setup flags:")
		parts = append(parts, h.SetupFlags...)
		parts = append(parts, helpFlag)
	} else {
		parts = append(parts, "Flags:")
		parts = append(parts, h.Flags...)
		parts = append(parts, helpFlag)
	}

	return strings.Join(parts, "\n")
}

// printCmdHelp writes the full help text to stdout and returns 0.
func printCmdHelp(stdout io.Writer, h cmdHelp) int {
	fmt.Fprintln(stdout, h.String())
	return 0
}

// ── Help rendered from the command table ───────────────────────────

// globalFlagLines are the global flags, as `ws help` lists them (spec §4.1).
var globalFlagLines = []string{
	"  -w, --workspace string   Path to workspace root (default: $WS_WORKSPACE or ~/Workspace)",
	"  -q, --quiet              Errors only; write commands accept every action",
	"      --verbose            Show internal decisions on stderr",
	"      --json               Machine-readable output; write commands accept every action",
	"      --dry-run            Print the plan, change nothing (ignored by read commands)",
	"      --no-color           No colors or Unicode icons",
	"  -h, --help               Show help",
}

// listFlagDocs are the flags every `ls` takes (spec §4.3).
var listFlagDocs = []flagDoc{
	{Name: "--filter", Arg: "<expr>", Desc: "key=text, key!=text, or text (repeatable; all must match)"},
	{Name: "--sort", Arg: "<fields>", Desc: "order rows; -field descends"},
	{Name: "--wide", Desc: "show every field as a column"},
}

// helpRequested reports whether args hold -h or --help before any "--".
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// leadingWords returns the words before the first flag.
func leadingWords(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		out = append(out, a)
	}
	return out
}

// handleHelp renders help when the arguments ask for it: `ws`, `ws help
// [noun [verb…]]`, `ws <noun> --help`, `ws <noun> <verb…> --help`, and a bare
// `ws <noun>`. handled is false when args are an ordinary command (or a hidden
// one the table does not list), so the caller runs it.
func handleHelp(args []string, noColor bool, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 {
		writeOverview(stdout, noColor)
		return 0, true
	}
	if args[0] == "help" {
		words := leadingWords(args[1:])
		if len(words) == 0 && helpRequested(args[1:]) {
			words = []string{"help"} // ws help --help
		}
		return renderHelpFor(words, noColor, stdout, stderr), true
	}
	if args[0] == "-h" || args[0] == "--help" {
		writeOverview(stdout, noColor)
		return 0, true
	}

	words := leadingWords(args)
	wantsHelp := helpRequested(args)
	if !wantsHelp && !(len(args) == 1 && isNoun(args[0])) {
		return 0, false
	}
	// Hidden commands have no table row; their own handler prints help.
	if !tableKnows(words) {
		return 0, false
	}
	return renderHelpFor(words, noColor, stdout, stderr), true
}

// isNoun reports whether word has verbs of its own in the table.
func isNoun(word string) bool { return len(nextWords([]string{word})) > 0 }

// tableKnows reports whether words are a command, or a prefix of one.
func tableKnows(words []string) bool {
	if len(words) == 0 {
		return true
	}
	return len(commandsUnder(words)) > 0
}

// renderHelpFor prints help for a command, a group of commands, or the
// overview. An unknown word is an error on stderr.
func renderHelpFor(words []string, noColor bool, stdout, stderr io.Writer) int {
	if len(words) == 0 {
		writeOverview(stdout, noColor)
		return 0
	}
	if c, ok := findCommand(words); ok {
		writeCommandHelp(stdout, c, noColor)
		return 0
	}
	if len(commandsUnder(words)) > 0 {
		writeGroupHelp(stdout, words, noColor)
		return 0
	}
	fmt.Fprintf(stderr, "unknown command: %s\n", strings.Join(words, " "))
	fmt.Fprintln(stderr, "usage: ws <noun> <verb> [targets…] [flags]   (ws help lists the nouns)")
	return 1
}

func writeOverview(stdout io.Writer, noColor bool) {
	fmt.Fprintln(stdout, style.Boldf(noColor, "ws")+style.Mutedf(noColor, " — Workspace manager"))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Usage:"))
	fmt.Fprintf(stdout, "  %s <noun> <verb> [targets…] [flags]\n", style.Accentf(noColor, "ws"))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Nouns:"))
	for _, n := range nounSummaries {
		fmt.Fprintf(stdout, "  %-11s %s\n", style.Accentf(noColor, "%s", n.Name), n.Summary)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Workspace:"))
	for _, n := range workspaceSummaries {
		fmt.Fprintf(stdout, "  %-11s %s\n", style.Accentf(noColor, "%s", n.Name), n.Summary)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Global Flags:"))
	for _, f := range globalFlagLines {
		fmt.Fprintln(stdout, f)
	}
}

// writeGroupHelp lists the commands under a noun (or a sub-noun such as
// `repo root`), one line each, then the flags that several of them share.
func writeGroupHelp(stdout io.Writer, prefix []string, noColor bool) {
	cmds := commandsUnder(prefix)
	fmt.Fprintln(stdout, style.Boldf(noColor, "Usage:"))
	fmt.Fprintf(stdout, "  ws %s <verb> [targets…] [flags]\n\n", strings.Join(prefix, " "))
	fmt.Fprintln(stdout, style.Boldf(noColor, "Commands:"))

	lines := make([][2]string, len(cmds))
	width := 0
	for i, c := range cmds {
		left := strings.Join(c.Path[len(prefix):], " ")
		if c.Args != "" {
			left += " " + c.Args
		}
		lines[i] = [2]string{left, c.Summary}
		width = max(width, len(left))
	}
	for _, l := range lines {
		fmt.Fprintf(stdout, "  %-*s  %s\n", width, l[0], l[1])
	}

	if shared := sharedFlags(cmds); len(shared) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Boldf(noColor, "Flags shared by these commands:"))
		writeFlagDocs(stdout, shared)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Run `ws %s <verb> --help` for a command's own usage.\n", strings.Join(prefix, " "))
	fmt.Fprintln(stdout, helpFlag)
}

// sharedFlags returns the flags that at least two of cmds document, in the
// order of first appearance.
func sharedFlags(cmds []command) []flagDoc {
	count := map[string]int{}
	var order []flagDoc
	for _, c := range cmds {
		for _, f := range c.Flags {
			if count[f.Name] == 0 {
				order = append(order, f)
			}
			count[f.Name]++
		}
	}
	var out []flagDoc
	for _, f := range order {
		if count[f.Name] >= 2 {
			out = append(out, f)
		}
	}
	return out
}

// writeCommandHelp prints one command's usage, arguments, own flags and, for
// the lists, the field names --filter, --sort and --wide accept.
func writeCommandHelp(stdout io.Writer, c command, noColor bool) {
	fmt.Fprintln(stdout, style.Boldf(noColor, "Usage:"))
	fmt.Fprintf(stdout, "  %s\n\n", c.usage())
	fmt.Fprintf(stdout, "%s\n", c.Summary)
	if c.RO {
		fmt.Fprintln(stdout, style.Mutedf(noColor, "Read-only: it never changes anything."))
	}
	flags := slices.Clone(c.Flags)
	if c.Fields != nil {
		flags = append(flags, listFlagDocs...)
	}
	if len(flags) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Boldf(noColor, "Flags:"))
		writeFlagDocs(stdout, flags)
	}
	if len(c.Examples) > 0 {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Boldf(noColor, "Examples:"))
		for _, ex := range c.Examples {
			fmt.Fprintf(stdout, "  %s\n", ex)
		}
	}
	if c.Fields != nil {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, wrapHelp("Fields (for --filter, --sort and --wide): "+strings.Join(c.Fields(), ", "), "  "))
	}
	if !c.RO {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, wrapHelp("Also: --dry-run prints the plan and changes nothing; --quiet and --json accept every action.", "  "))
	}
	fmt.Fprintln(stdout, helpFlag)
}

// helpWidth is the column limit of help text.
const helpWidth = 80

// wrapHelp breaks text at spaces so no line is longer than helpWidth; later lines
// start with indent.
func wrapHelp(text, indent string) string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case len([]rune(line))+1+len([]rune(w)) > helpWidth:
			lines = append(lines, line)
			line = indent + w
		default:
			line += " " + w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func writeFlagDocs(stdout io.Writer, flags []flagDoc) {
	left := make([]string, len(flags))
	width := 0
	for i, f := range flags {
		l := f.Name
		if f.Short != "" && f.Short != f.Name {
			l = f.Short + ", " + f.Name
		} else if !strings.HasPrefix(f.Name, "--") {
			l = f.Name
		}
		if f.Arg != "" {
			l += " " + f.Arg
		}
		left[i] = l
		width = max(width, len(l))
	}
	for i, f := range flags {
		fmt.Fprintf(stdout, "  %-*s  %s\n", width, left[i], f.Desc)
	}
}

func hasHelpArg(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}
