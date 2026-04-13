package cmd

import (
	"fmt"
	"io"
	"strings"
)

const helpFlag = "  -h, --help    Show help"

// cmdHelp describes the help text for a command.
// Use String() to render the full help block sent to stdout.
// Use UsageError() for the lowercase one-liner sent to stderr.
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

// UsageError returns the lowercase usage line for stderr.
func (h cmdHelp) UsageError() string {
	return "usage: " + h.Usage
}

// printCmdHelp writes the full help text to stdout and returns 0.
func printCmdHelp(stdout io.Writer, h cmdHelp) int {
	fmt.Fprintln(stdout, h.String())
	return 0
}

// printUsageError writes the usage-error line to stderr and returns 1.
func printUsageError(stderr io.Writer, h cmdHelp) int {
	fmt.Fprintln(stderr, h.UsageError())
	return 1
}
