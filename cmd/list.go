package cmd

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/resource"
)

// listFlags are the --sort / --filter flags every list command takes. Their
// field names are the command's resource.Schema keys, the same ones the TUI
// filter (/) and sort (> <) use.
type listFlags struct {
	sort   string
	filter stringSliceFlag
	wide   bool
}

func registerListFlags(fs *flag.FlagSet, defaultSort string) *listFlags {
	lf := &listFlags{}
	fs.StringVar(&lf.sort, "sort", defaultSort, "sort by field (comma-separated; -field descends)")
	fs.Var(&lf.filter, "filter", "keep rows matching key=text, key!=text, or text (repeatable)")
	fs.BoolVar(&lf.wide, "wide", false, "show every field as a column")
	return lf
}

func (lf *listFlags) query() resource.Query {
	return resource.Query{
		Terms: resource.ParseFilter(strings.Join(lf.filter, " ")),
		Sort:  resource.ParseSort(lf.sort),
	}
}

// listFlagHelp documents --sort / --filter for a list command, naming the
// schema's field keys.
func listFlagHelp(cmd string, keys []string) []string {
	return []string{
		"      --sort fields    " + cmd + ": sort by field; comma-separated, -field descends",
		"      --filter expr    " + cmd + ": key=text, key!=text, or text (repeatable; all must match)",
		"      --wide           " + cmd + ": show every field as a column",
		"                       fields: " + strings.Join(keys, ", "),
	}
}

// applyList filters and sorts items by --filter / --sort, reporting an
// unknown field on stderr (ok is then false). Commands with their own text
// layout (the scans) use it before printing.
func applyList[T any](schema resource.Schema[T], items []T, lf *listFlags, stderr io.Writer) ([]T, bool) {
	q := lf.query()
	if err := schema.Validate(q); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return nil, false
	}
	return schema.Apply(items, q), true
}

// renderList filters, sorts, and prints items: --json writes the schema's
// records, text writes its columns as a table (empty prints none).
func renderList[T any](globals globalFlags, command string, schema resource.Schema[T], items []T, lf *listFlags, none string, stdout, stderr io.Writer) int {
	return renderListIn(globals, command, "", schema, items, lf, none, stdout, stderr)
}

// renderListIn is renderList with the --json records under data.<key>, for
// commands whose JSON shape predates the shared schemas.
func renderListIn[T any](globals globalFlags, command, key string, schema resource.Schema[T], items []T, lf *listFlags, none string, stdout, stderr io.Writer) int {
	items, ok := applyList(schema, items, lf, stderr)
	if !ok {
		return 1
	}
	if globals.json {
		if key != "" {
			return writeJSON(stdout, stderr, command, map[string]any{key: schema.Records(items)})
		}
		return writeJSON(stdout, stderr, command, schema.Records(items))
	}
	out := textOut(globals, stdout)
	if len(items) == 0 {
		fmt.Fprintln(out, none)
		return 0
	}
	if err := schema.WriteTable(out, items, lf.wide); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
