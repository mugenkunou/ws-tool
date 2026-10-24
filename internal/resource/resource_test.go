package resource

import (
	"encoding/json"
	"strings"
	"testing"
)

type pet struct {
	Name string
	Age  int
	Tame bool
}

var pets = Schema[pet]{
	Name: "pet",
	ID:   func(p pet) string { return p.Name },
	Fields: []Field[pet]{
		{Key: "name", Title: "Name", Value: func(p pet) any { return p.Name }},
		{Key: "age", Title: "Age", Value: func(p pet) any { return p.Age }},
		{Key: "tame", Title: "Tame", Value: func(p pet) any { return p.Tame }},
	},
	Columns: []string{"name", "age"},
}

var zoo = []pet{{"rex", 3, true}, {"Ada", 10, false}, {"bo", 3, true}}

func names(ps []pet) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return strings.Join(out, ",")
}

func TestApply(t *testing.T) {
	cases := []struct {
		filter, sort, want string
	}{
		{"", "", "rex,Ada,bo"},
		{"", "name", "Ada,bo,rex"},
		{"", "-age,name", "Ada,bo,rex"},
		{"", "age", "rex,bo,Ada"}, // stable on ties
		{"tame=yes", "", "rex,bo"},
		{"tame=true", "", "rex,bo"},
		{"tame!=yes", "", "Ada"},
		{"A", "", "Ada"}, // bare term, any field, case-insensitive
		{"age=3 b", "", "bo"},
		{"false", "", ""}, // bare words match display text, not raw bools
		{"tame=false", "", "Ada"},
	}
	for _, c := range cases {
		q := Query{Terms: ParseFilter(c.filter), Sort: ParseSort(c.sort)}
		if err := pets.Validate(q); err != nil {
			t.Fatal(err)
		}
		if got := names(pets.Apply(zoo, q)); got != c.want {
			t.Errorf("filter %q sort %q = %s, want %s", c.filter, c.sort, got, c.want)
		}
	}
}

func TestValidateUnknownField(t *testing.T) {
	err := pets.Validate(Query{Sort: ParseSort("colour")})
	if err == nil || !strings.Contains(err.Error(), "name, age, tame") {
		t.Fatalf("err = %v, want unknown-field error listing fields", err)
	}
}

func TestRecordKeepsFieldOrder(t *testing.T) {
	b, err := json.Marshal(pets.Records(zoo[:1]))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `[{"name":"rex","age":3,"tame":true}]`; got != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestWriteTable(t *testing.T) {
	var b strings.Builder
	if err := pets.WriteTable(&b, zoo[:2], false); err != nil {
		t.Fatal(err)
	}
	want := "NAME  AGE\nrex   3\nAda   10\n"
	if b.String() != want {
		t.Errorf("table =\n%s\nwant\n%s", b.String(), want)
	}
	b.Reset()
	if err := pets.WriteTable(&b, zoo[:1], true); err != nil {
		t.Fatal(err)
	}
	if want := "NAME  AGE  TAME\nrex   3    yes\n"; b.String() != want {
		t.Errorf("wide table =\n%s\nwant\n%s", b.String(), want)
	}
}

// A Whole field compares the entire value: status!=ok must keep "broken".
func TestWholeFieldMatchesTheWholeValue(t *testing.T) {
	type row struct{ Status string }
	sc := Schema[row]{
		Name: "row",
		ID:   func(r row) string { return r.Status },
		Fields: []Field[row]{
			{Key: "status", Title: "Status", Value: func(r row) any { return r.Status }, Whole: true},
		},
		Columns: []string{"status"},
	}
	rows := []row{{"ok"}, {"broken"}, {"Ok"}}
	join := func(rs []row) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Status)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]string{
		"status=ok":  "ok,Ok",
		"status!=ok": "broken",
		"ok":         "ok,broken,Ok", // a bare word still matches inside any field
	}
	for filter, want := range cases {
		if got := join(sc.Apply(rows, Query{Terms: ParseFilter(filter)})); got != want {
			t.Errorf("filter %q = %s, want %s", filter, got, want)
		}
	}
}
