package wsconfig

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	full, _ := json.Marshal(Default())
	if _, err := Validate(full); err != nil {
		t.Fatalf("the defaults are valid: %v", err)
	}
	bad := map[string]string{
		"not json":       `{"schema": 1`,
		"unknown key":    `{"schema": 1, "bogus": true}`,
		"unknown nested": `{"schema": 1, "repo": {"nope": 1}}`,
		"schema zero":    `{"schema": 0}`,
		"newer schema":   `{"schema": 99}`,
		"two values":     `{"schema": 1} {"schema": 1}`,
	}
	for name, content := range bad {
		if _, err := Validate([]byte(content)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestValidateRejectsTheOldLogKey(t *testing.T) {
	if _, err := Validate([]byte(`{"schema": 1, "log": {"cap_mb": 7}}`)); err == nil {
		t.Fatal(`"log" is not a key: want an error`)
	}
}

func TestMissingKeys(t *testing.T) {
	full, _ := json.Marshal(Default())
	if m, err := MissingKeys(full); err != nil || len(m) != 0 {
		t.Fatalf("a complete file lacks nothing: %v %v", m, err)
	}
	m, err := MissingKeys([]byte(`{"schema": 1, "repo": {"roots": ["."]}, "session": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"editor", "scratch", "repo.max_parallel", "repo.exclude_dirs", "pass", "symlinks"} {
		if !slices.Contains(m, want) {
			t.Errorf("missing %q not reported: %v", want, m)
		}
	}
	for _, not := range []string{"schema", "repo.roots", "session", "repo"} {
		if slices.Contains(m, not) {
			t.Errorf("%q is present: %v", not, m)
		}
	}
	if strings.Contains(strings.Join(m, " "), "scratch.") {
		t.Errorf("a missing object is one key, not its leaves: %v", m)
	}
}

// TestValidateRejectsOldLinksKey: the old "links" key is an unknown field.
func TestValidateRejectsOldLinksKey(t *testing.T) {
	old := `{"schema": 1, "links": [{"system": "/etc/hosts", "original": "symlinks/system/etc/hosts", "dir": false, "sudo": true}]}`
	_, err := Validate([]byte(old))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("want an unknown-field error, got %v", err)
	}
	if _, err := Validate([]byte(`{"schema": 1, "frobnicate": 1}`)); err == nil {
		t.Fatal("an unknown key is an error")
	}
}
