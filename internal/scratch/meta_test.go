package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetaSaveLoad(t *testing.T) {
	dir := t.TempDir()
	m := Meta{Tags: []string{"k8s", "debug"}, Created: time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)}
	if err := SaveMeta(dir, m); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	got, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "k8s" || got.Tags[1] != "debug" {
		t.Fatalf("unexpected tags: %v", got.Tags)
	}
	if got.Created.IsZero() {
		t.Fatal("created should not be zero")
	}
}

func TestLoadMetaMissing(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("load missing meta: %v", err)
	}
	if len(m.Tags) != 0 {
		t.Fatalf("expected empty tags, got %v", m.Tags)
	}
}

func TestMetaSeededOnNew(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	res, err := New(NewOptions{RootDir: root, Name: "test-seed"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	m, err := LoadMeta(res.Path)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}
	if m.Created.IsZero() {
		t.Fatal("meta.Created should be set on new scratch")
	}
	if len(m.Tags) != 0 {
		t.Fatalf("expected no tags on new scratch, got %v", m.Tags)
	}
}

func TestTagsLoadSaveMerge(t *testing.T) {
	wsDir := filepath.Join(t.TempDir(), "ws.json")
	tc, err := LoadTags(wsDir)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(tc.Tags) != 0 {
		t.Fatalf("expected empty tags: %v", tc.Tags)
	}

	MergeTags(&tc, []string{"k8s", "Docker", "  k8s  "})
	if len(tc.Tags) != 2 {
		t.Fatalf("expected 2 tags after merge, got %v", tc.Tags)
	}
	if tc.Tags[0] != "docker" || tc.Tags[1] != "k8s" {
		t.Fatalf("unexpected sorted tags: %v", tc.Tags)
	}

	if err := SaveTags(wsDir, tc); err != nil {
		t.Fatalf("save: %v", err)
	}
	tc2, err := LoadTags(wsDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(tc2.Tags) != 2 {
		t.Fatalf("expected 2 tags after reload, got %v", tc2.Tags)
	}

	added := MergeTags(&tc2, []string{"k8s", "networking"})
	if !added {
		t.Fatal("expected added=true for new tag")
	}
	if len(tc2.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", tc2.Tags)
	}

	notAdded := MergeTags(&tc2, []string{"k8s"})
	if notAdded {
		t.Fatal("expected added=false for duplicate")
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"K8S", "k8s"},
		{"  Docker ", "docker"},
		{"", ""},
	}
	for _, c := range cases {
		got := NormalizeTag(c.in)
		if got != c.want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestListIncludesTags(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Scratch")
	res, err := New(NewOptions{RootDir: root, Name: "tagged-dir"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	meta, _ := LoadMeta(res.Path)
	meta.Tags = []string{"k8s", "debug"}
	if err := SaveMeta(res.Path, meta); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	list, err := List(ListOptions{RootDir: root, SortBy: "name"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if len(list[0].Tags) != 2 || list[0].Tags[0] != "k8s" {
		t.Fatalf("expected tags [k8s debug], got %v", list[0].Tags)
	}
}

func TestSaveTagsKeepsUnreadableConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "ws.json")
	broken := []byte("{\"links\": [ not json")
	if err := os.WriteFile(cfgPath, broken, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := SaveTags(cfgPath, TagCollection{Tags: []string{"k8s"}}); err == nil {
		t.Fatal("expected an error for an unparsable ws.json")
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != string(broken) {
		t.Fatalf("ws.json was overwritten: %q", got)
	}
}
