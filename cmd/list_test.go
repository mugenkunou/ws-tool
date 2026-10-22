package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mugenkunou/ws-tool/internal/dotfile"
	"github.com/mugenkunou/ws-tool/internal/resource"
	"github.com/mugenkunou/ws-tool/internal/trash"
)

// listRecords runs a list command with --json and returns its data records.
func listRecords(t *testing.T, args ...string) []map[string]any {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := Execute(append(args, "--json"), strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("%v: code=%d stderr=%s", args, code, errOut.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("%v: bad json: %v\n%s", args, err, out.String())
	}
	return payload.Data
}

// assertKeys checks a record has exactly the schema's field keys: the CLI's
// --json, its --sort/--filter names, and the TUI's fields are one list.
func assertKeys(t *testing.T, rec map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(rec))
	for k := range rec {
		got = append(got, k)
	}
	sort.Strings(got)
	want = slices.Clone(want)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("record keys = %v, want schema keys %v", got, want)
	}
}

func TestTrashLs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	workspace := initWorkspace(t)

	trashRoot := filepath.Join(home, ".local", "share", "Trash")
	for _, d := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(trashRoot, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(name string, age time.Duration, size int) {
		if err := os.WriteFile(filepath.Join(trashRoot, "files", name), bytes.Repeat([]byte("x"), size), 0o644); err != nil {
			t.Fatal(err)
		}
		info := "[Trash Info]\nPath=/old%20place/" + name + "\nDeletionDate=" + time.Now().Add(-age).Format("2006-01-02T15:04:05") + "\n"
		if err := os.WriteFile(filepath.Join(trashRoot, "info", name+".trashinfo"), []byte(info), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seed("big.bin", 2*time.Hour, 900)
	seed("note.txt", time.Hour, 10)

	recs := listRecords(t, "--workspace", workspace, "trash", "ls")
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	assertKeys(t, recs[0], trash.Schema(resource.CLIView()).Keys())
	if recs[0]["name"] != "note.txt" {
		t.Errorf("default order should be newest first, got %v", recs[0]["name"])
	}
	if recs[0]["original_path"] != "/old place/note.txt" {
		t.Errorf("original_path = %v", recs[0]["original_path"])
	}

	recs = listRecords(t, "--workspace", workspace, "trash", "ls", "--sort", "-size_bytes")
	if recs[0]["name"] != "big.bin" {
		t.Errorf("--sort -size_bytes first = %v", recs[0]["name"])
	}
	recs = listRecords(t, "--workspace", workspace, "trash", "ls", "--filter", "name=note")
	if len(recs) != 1 || recs[0]["name"] != "note.txt" {
		t.Errorf("--filter name=note = %v", recs)
	}

	var out, errOut bytes.Buffer
	if code := Execute([]string{"--workspace", workspace, "trash", "ls", "--sort", "colour"}, strings.NewReader(""), &out, &errOut); code == 0 ||
		!strings.Contains(errOut.String(), "original_path") {
		t.Errorf("unknown sort field: code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	if code := Execute([]string{"--workspace", workspace, "trash", "ls"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME") {
		t.Errorf("text table:\n%s", out.String())
	}
}

func TestDotfileLsFieldsAndFilter(t *testing.T) {
	workspace := initWorkspace(t)
	sys := t.TempDir()
	for _, name := range []string{".bashrc", "id_rsa"} { // a private key goes under secrets/
		p := filepath.Join(sys, name)
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		if code := Execute([]string{"--workspace", workspace, "dotfile", "add", p}, strings.NewReader("y\ny\n"), &out, &errOut); code != 0 {
			t.Fatalf("add %s: %s", name, errOut.String())
		}
	}

	recs := listRecords(t, "--workspace", workspace, "dotfile", "ls")
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	assertKeys(t, recs[0], dotfile.Schema(resource.CLIView()).Keys())
	for _, r := range recs {
		if r["status"] != "ok" {
			t.Errorf("%v status = %v", r["system"], r["status"])
		}
	}

	recs = listRecords(t, "--workspace", workspace, "dotfile", "ls", "--filter", "workspace_path=secrets/dotfiles")
	if len(recs) != 1 || !strings.HasSuffix(recs[0]["system"].(string), "id_rsa") {
		t.Errorf("--filter workspace_path=secrets/dotfiles = %v", recs)
	}
}
