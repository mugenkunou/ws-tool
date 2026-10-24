// Package resource describes a domain's list records once — their fields —
// so the CLI (text and --json) and the TUI show, filter, and sort them the
// same way. See HANDBOOK.md §7.5.
//
// A Schema is plain data: every field has a key (the JSON key and the name
// used by --sort / --filter and the TUI filter), a title, the raw value, and
// its plain display text. Presentation (colour, column widths) belongs to the
// caller.
package resource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Field is one property of a record.
type Field[T any] struct {
	Key   string         // JSON key; also the --sort / --filter name
	Title string         // column and detail heading
	Value func(T) any    // raw value for JSON and sorting
	Text  func(T) string // plain display text; nil renders Value
}

// Schema describes a record type.
type Schema[T any] struct {
	Name    string         // record noun, e.g. "symlink"
	ID      func(T) string // stable identity across reloads
	Fields  []Field[T]
	Columns []string // field keys shown in tables (CLI text and TUI), in order
}

// View carries what display text depends on: the clock and how paths are
// shown (the CLI prints them absolute, the TUI shortens them to ~/…).
type View struct {
	Now  func() time.Time
	Path func(string) string
}

// CLIView shows absolute paths against the real clock.
func CLIView() View {
	return View{Now: time.Now, Path: func(p string) string { return p }}
}

// Field returns the field with key k.
func (s Schema[T]) Field(k string) (Field[T], bool) {
	for _, f := range s.Fields {
		if f.Key == k {
			return f, true
		}
	}
	return Field[T]{}, false
}

// Keys lists every field key in declaration order.
func (s Schema[T]) Keys() []string {
	out := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		out[i] = f.Key
	}
	return out
}

// ColumnFields returns the fields named by Columns.
func (s Schema[T]) ColumnFields() []Field[T] {
	out := make([]Field[T], 0, len(s.Columns))
	for _, k := range s.Columns {
		if f, ok := s.Field(k); ok {
			out = append(out, f)
		}
	}
	return out
}

// Show is the field's display text for item.
func (f Field[T]) Show(item T) string {
	if f.Text != nil {
		return f.Text(item)
	}
	return Plain(f.Value(item))
}

// Display returns the display text of field k for item ("" if unknown).
func (s Schema[T]) Display(item T, k string) string {
	if f, ok := s.Field(k); ok {
		return f.Show(item)
	}
	return ""
}

// Plain renders a raw value as plain text.
func Plain(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "yes"
		}
		return ""
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case time.Time:
		if v.IsZero() {
			return ""
		}
		return v.Local().Format("2006-01-02 15:04")
	case []string:
		return strings.Join(v, ", ")
	}
	return fmt.Sprint(v)
}

// Record is one item's fields in declaration order; it marshals to a JSON
// object with keys in that order.
type Record []KV

// KV is one field of a Record.
type KV struct {
	Key   string
	Value any
}

// MarshalJSON writes the record as an ordered JSON object.
func (r Record) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range r {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.Key)
		v, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", kv.Key, err)
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Record builds item's JSON record.
func (s Schema[T]) Record(item T) Record {
	r := make(Record, len(s.Fields))
	for i, f := range s.Fields {
		r[i] = KV{Key: f.Key, Value: f.Value(item)}
	}
	return r
}

// Records builds the JSON records of items.
func (s Schema[T]) Records(items []T) []Record {
	out := make([]Record, len(items))
	for i, it := range items {
		out[i] = s.Record(it)
	}
	return out
}

// Term is one filter condition. An empty Key matches any field.
type Term struct {
	Key    string
	Value  string
	Negate bool
}

// SortKey orders by one field.
type SortKey struct {
	Key  string
	Desc bool
}

// Query is a filter and an ordering.
type Query struct {
	Terms []Term
	Sort  []SortKey
}

// Empty reports whether the query changes nothing.
func (q Query) Empty() bool { return len(q.Terms) == 0 && len(q.Sort) == 0 }

// ParseFilter parses whitespace-separated terms, all of which must match:
// "key=text" (field contains text), "key!=text" (does not), or bare "text"
// (any field contains it). Matching ignores case.
func ParseFilter(s string) []Term {
	var out []Term
	for w := range strings.FieldsSeq(s) {
		if i := strings.Index(w, "!="); i > 0 {
			out = append(out, Term{Key: strings.ToLower(w[:i]), Value: w[i+2:], Negate: true})
		} else if i := strings.Index(w, "="); i > 0 {
			out = append(out, Term{Key: strings.ToLower(w[:i]), Value: w[i+1:]})
		} else {
			out = append(out, Term{Value: w})
		}
	}
	return out
}

// ParseSort parses comma-separated keys; a leading "-" sorts descending.
func ParseSort(s string) []SortKey {
	var out []SortKey
	for k := range strings.SplitSeq(s, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		desc := strings.HasPrefix(k, "-")
		k = strings.TrimPrefix(k, "-")
		if k != "" {
			out = append(out, SortKey{Key: k, Desc: desc})
		}
	}
	return out
}

// String renders the query back in the syntax ParseFilter / ParseSort read.
func (q Query) String() string {
	var parts []string
	for _, t := range q.Terms {
		switch {
		case t.Key == "":
			parts = append(parts, t.Value)
		case t.Negate:
			parts = append(parts, t.Key+"!="+t.Value)
		default:
			parts = append(parts, t.Key+"="+t.Value)
		}
	}
	return strings.Join(parts, " ")
}

// Validate reports a query that names a field the schema does not have.
func (s Schema[T]) Validate(q Query) error {
	check := func(k string) error {
		if _, ok := s.Field(k); !ok {
			return fmt.Errorf("unknown %s field %q (fields: %s)", s.Name, k, strings.Join(s.Keys(), ", "))
		}
		return nil
	}
	for _, t := range q.Terms {
		if t.Key != "" {
			if err := check(t.Key); err != nil {
				return err
			}
		}
	}
	for _, k := range q.Sort {
		if err := check(k.Key); err != nil {
			return err
		}
	}
	return nil
}

// Apply returns the items matching q's terms, stably ordered by q's sort
// keys. Unknown keys match nothing and sort as equal; call Validate first to
// report them.
func (s Schema[T]) Apply(items []T, q Query) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		if s.Match(it, q.Terms) {
			out = append(out, it)
		}
	}
	if len(q.Sort) > 0 {
		slices.SortStableFunc(out, func(a, b T) int {
			for _, k := range q.Sort {
				f, ok := s.Field(k.Key)
				if !ok {
					continue
				}
				c := Compare(f.Value(a), f.Value(b))
				if c == 0 {
					c = strings.Compare(f.Show(a), f.Show(b))
				}
				if k.Desc {
					c = -c
				}
				if c != 0 {
					return c
				}
			}
			return 0
		})
	}
	return out
}

// Match reports whether item satisfies every term.
func (s Schema[T]) Match(item T, terms []Term) bool {
	for _, t := range terms {
		if s.matchTerm(item, t) == t.Negate {
			return false
		}
	}
	return true
}

// matchTerm: a bare word matches any field's display text; key=text also
// matches the raw value (so sudo=true and size_bytes=1024 work), except nil.
func (s Schema[T]) matchTerm(item T, t Term) bool {
	want := strings.ToLower(t.Value)
	shown := func(f Field[T]) bool { return strings.Contains(strings.ToLower(f.Show(item)), want) }
	if t.Key != "" {
		f, ok := s.Field(t.Key)
		if !ok {
			return false
		}
		if shown(f) {
			return true
		}
		v := f.Value(item)
		return v != nil && strings.Contains(strings.ToLower(fmt.Sprint(v)), want)
	}
	return slices.ContainsFunc(s.Fields, shown)
}

// Compare orders two raw values of the same type. Values of other or mixed
// types compare equal (callers fall back to display text).
func Compare(a, b any) int {
	switch a := a.(type) {
	case string:
		if b, ok := b.(string); ok {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		}
	case int:
		if b, ok := b.(int); ok {
			return cmpOrdered(a, b)
		}
	case int64:
		if b, ok := b.(int64); ok {
			return cmpOrdered(a, b)
		}
	case float64:
		if b, ok := b.(float64); ok {
			return cmpOrdered(a, b)
		}
	case time.Duration:
		if b, ok := b.(time.Duration); ok {
			return cmpOrdered(a, b)
		}
	case bool:
		if b, ok := b.(bool); ok && a != b {
			if a {
				return 1
			}
			return -1
		}
	case time.Time:
		if b, ok := b.(time.Time); ok {
			return a.Compare(b)
		}
	case []string:
		if b, ok := b.([]string); ok {
			return strings.Compare(strings.Join(a, ","), strings.Join(b, ","))
		}
	}
	return 0
}

func cmpOrdered[V int | int64 | float64 | time.Duration](a, b V) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// WriteTable prints items as an aligned plain-text table of the schema's
// columns (every field when wide), with a header row.
func (s Schema[T]) WriteTable(w io.Writer, items []T, wide bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	cols := s.ColumnFields()
	if wide {
		cols = s.Fields
	}
	head := make([]string, len(cols))
	for i, f := range cols {
		head[i] = strings.ToUpper(f.Title)
	}
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, it := range items {
		cells := make([]string, len(cols))
		for i, f := range cols {
			cells[i] = strings.ReplaceAll(f.Show(it), "\t", " ")
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// WriteDetail prints every field of item as "Title  text" lines.
func (s Schema[T]) WriteDetail(w io.Writer, item T) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range s.Fields {
		fmt.Fprintf(tw, "%s\t%s\n", f.Title, f.Show(item))
	}
	return tw.Flush()
}
