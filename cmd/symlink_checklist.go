package cmd

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/style"
	"github.com/mugenkunou/ws-tool/internal/symlink"
)

// printChecklist writes the checklist in the shape the plan shows.
func printChecklist(out io.Writer, rows []symlink.CheckRow, home string, noColor bool) {
	fmt.Fprintln(out, "Found on this machine, not symlinked yet:")
	appW := 0
	for _, r := range rows {
		appW = max(appW, len(r.App))
	}
	for i, r := range rows {
		box := "[ ]"
		if r.Checked {
			box = "[x]"
		}
		var tags []string
		if r.Secret {
			tags = append(tags, "🔑 secret")
		}
		if r.Sudo {
			tags = append(tags, "🔒 sudo")
		}
		line := fmt.Sprintf("  %s %d  %-*s  %s", box, i+1, appW, r.App, r.Label(home))
		if len(tags) > 0 {
			line += "   " + style.Warningf(noColor, "%s", strings.Join(tags, " "))
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out, style.Mutedf(noColor, "Stored under symlinks/ at the same path as on this machine."))
}

// parseChecklistAnswer reads the answer: Enter (the checked rows), all, none, or a
// comma list of row numbers. ok is false for anything else, and quit for q.
func parseChecklistAnswer(line string, rows []symlink.CheckRow) (chosen []int, ok, quit bool) {
	line = strings.TrimSpace(line)
	switch strings.ToLower(line) {
	case "q":
		return nil, true, true
	case "":
		for i, r := range rows {
			if r.Checked {
				chosen = append(chosen, i)
			}
		}
		return chosen, true, false
	case "all":
		for i := range rows {
			chosen = append(chosen, i)
		}
		return chosen, true, false
	case "none":
		return nil, true, false
	}
	for _, part := range strings.Split(line, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > len(rows) {
			return nil, false, false
		}
		if i := n - 1; !slices.Contains(chosen, i) {
			chosen = append(chosen, i)
		}
	}
	slices.Sort(chosen)
	return chosen, true, false
}

// askChecklist prints the checklist and asks until the answer is understood. It
// returns the chosen rows, or cancelled when the person typed q or input ended.
func askChecklist(stdin io.Reader, out io.Writer, rows []symlink.CheckRow, home string, noColor bool) (chosen []symlink.CheckRow, cancelled bool) {
	printChecklist(out, rows, home, noColor)
	for {
		fmt.Fprintf(out, "%s ", style.Mutedf(noColor, "Enter = checked ones · all · none · 1,3 · q quits :"))
		line, err := readLine(stdin)
		if err != nil && strings.TrimSpace(line) == "" {
			fmt.Fprintln(out)
			return nil, true
		}
		idx, ok, quit := parseChecklistAnswer(line, rows)
		switch {
		case quit:
			return nil, true
		case !ok:
			fmt.Fprintf(out, "Not understood: %q. Answer Enter, all, none, or numbers like 1,3.\n", strings.TrimSpace(line))
			continue
		}
		for _, i := range idx {
			chosen = append(chosen, rows[i])
		}
		return chosen, false
	}
}

// addOptionsFor turns checked rows into one add per item.
func addOptionsFor(rows []symlink.CheckRow, base symlink.AddOptions) []symlink.AddOptions {
	var opts []symlink.AddOptions
	for _, r := range rows {
		for _, e := range r.Items {
			o := base
			o.Host, o.Dir = e.System, e.Dir || e.Kind == symlink.KindFolder
			opts = append(opts, o)
		}
	}
	return opts
}
