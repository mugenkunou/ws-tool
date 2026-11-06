package cmd

import (
	"flag"
	"fmt"
	"io"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/workspace"
)

// runWhere says which workspace folder a file or directory belongs in. It reads
// the name and type of the path, never its content, and writes nothing.
func runWhere(args []string, globals globalFlags, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobalFlags(fs, &globals)
	if err := parseInterspersed(fs, args); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ws where <path>")
		return 1
	}
	targets, err := resolveTargets(fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	path := targets[0]
	workspacePath, homes, err := scriptPaths(globals)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	p := workspace.Where(workspacePath, homes, path)

	if globals.json {
		code := writeJSON(stdout, stderr, "where", map[string]any{
			"path":    path,
			"folder":  p.Folder,
			"reason":  p.Reason,
			"matched": p.Matched,
			"inside":  p.Inside,
		})
		if code == 0 && !p.Matched {
			fmt.Fprintf(stderr, "no folder fits %s: ask the user\n", path)
			return 1
		}
		return code
	}
	if !p.Matched {
		fmt.Fprintf(stderr, "no folder fits %s: ask the user\n", path)
		return 1
	}
	out := textOut(globals, stdout)
	nc := globals.noColor
	first := style.Boldf(nc, "%s", p.Folder)
	if p.Inside {
		first += " " + style.Mutedf(nc, "Inside")
	}
	fmt.Fprintln(out, first)
	fmt.Fprintln(out, "  "+p.Reason)
	return 0
}
