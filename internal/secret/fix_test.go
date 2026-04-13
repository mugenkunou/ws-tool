package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetFileContext(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetFileContext(file, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx) != 5 {
		t.Fatalf("expected 5 context lines, got %d", len(ctx))
	}
	if ctx[0].Number != 2 || ctx[4].Number != 6 {
		t.Fatalf("unexpected line range: %d-%d", ctx[0].Number, ctx[len(ctx)-1].Number)
	}
	if !ctx[2].IsMatch {
		t.Fatal("expected line 4 to be marked as match")
	}
	if ctx[0].IsMatch {
		t.Fatal("expected line 2 to not be marked as match")
	}
}

func TestGetFileContextAtStart(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "test.txt")
	content := "first\nsecond\nthird\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetFileContext(file, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ctx[0].Number != 1 {
		t.Fatalf("expected start at line 1, got %d", ctx[0].Number)
	}
	if !ctx[0].IsMatch {
		t.Fatal("expected line 1 to be the match")
	}
}

func TestExtractSecretValue(t *testing.T) {
	tests := []struct {
		snippet  string
		expected string
	}{
		{"password=hunter2", "hunter2"},
		{"PASSWORD = s3cret!!", "s3cret!!"},
		{"api_key: sk-12345", "sk-12345"},
		{"API-KEY=tok_abc", "tok_abc"},
		{"secret = mysecretvalue", "mysecretvalue"},
		{"nothing here", ""},
		{"", ""},
		// Quotes should be stripped.
		{`password="hunter2"`, "hunter2"},
		{`password='s3cret'`, "s3cret"},
		{"password=`backtick`", "backtick"},
		// Trailing comments should be stripped.
		{"password=hunter2 # production", "hunter2"},
		{"password=hunter2 // inline comment", "hunter2"},
		// Quotes + trailing comment.
		{`password="hunter2" # prod`, "hunter2"},
		// New key names.
		{"token=mytoken123", "mytoken123"},
		{"auth: bearer_xyz", "bearer_xyz"},
		{"access_key=AKIAEXAMPLE", "AKIAEXAMPLE"},
		{"client_secret=cs_live_abc", "cs_live_abc"},
	}
	for _, tt := range tests {
		got := ExtractSecretValue(tt.snippet)
		if got != tt.expected {
			t.Errorf("ExtractSecretValue(%q) = %q, want %q", tt.snippet, got, tt.expected)
		}
	}
}

func TestSuggestPassEntry(t *testing.T) {
	tests := []struct {
		relPath  string
		snippet  string
		expected string
	}{
		{"app.env", "password=hunter2", "app/password"},
		{"configs/myapp/db.properties", "api_key=sk-xxx", "myapp/db/api_key"},
		{"settings.yaml", "secret=token123", "settings/secret"},
	}
	for _, tt := range tests {
		got := SuggestPassEntry(tt.relPath, tt.snippet)
		if got != tt.expected {
			t.Errorf("SuggestPassEntry(%q, %q) = %q, want %q", tt.relPath, tt.snippet, got, tt.expected)
		}
	}
}

func TestPruneAllowlist(t *testing.T) {
	violations := []Violation{
		{Path: "app.env", Line: 2},
		{Path: "config.yml", Line: 5},
	}
	allowlist := []string{
		"app.env:2",      // still valid
		"config.yml:5",   // still valid
		"old.txt:10",     // stale — file/line no longer matches
		"deleted.env:1",  // stale
	}

	kept, pruned := PruneAllowlist(allowlist, violations)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d: %v", len(kept), kept)
	}
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d: %v", len(pruned), pruned)
	}
	if kept[0] != "app.env:2" || kept[1] != "config.yml:5" {
		t.Fatalf("unexpected kept entries: %v", kept)
	}
	if pruned[0] != "old.txt:10" || pruned[1] != "deleted.env:1" {
		t.Fatalf("unexpected pruned entries: %v", pruned)
	}
}

func TestPruneAllowlistAllStale(t *testing.T) {
	violations := []Violation{}
	allowlist := []string{"gone.txt:1", "removed.env:5"}

	kept, pruned := PruneAllowlist(allowlist, violations)
	if len(kept) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(kept))
	}
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d", len(pruned))
	}
}

func TestPruneAllowlistEmpty(t *testing.T) {
	kept, pruned := PruneAllowlist(nil, nil)
	if len(kept) != 0 || len(pruned) != 0 {
		t.Fatalf("expected empty results, got kept=%v pruned=%v", kept, pruned)
	}
}

func TestSanitizeEntryName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"myapp/password", "myapp/password"},
		{"my app/pass word", "my_app/pass_word"},
		{"dir:name/key", "dir_name/key"},
		{".hidden/.dotfile/secret", "hidden/dotfile/secret"},
		{"a\\b\\c", "a_b_c"},
		{"clean/path/ok", "clean/path/ok"},
		{"///", "entry"},
		{"...", "entry"},
		{"a/b\x00c/d", "a/bc/d"},
		{"normal-name_123/key", "normal-name_123/key"},
	}
	for _, tt := range tests {
		got := SanitizeEntryName(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeEntryName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSuggestPassEntryWithPathologicalNames(t *testing.T) {
	tests := []struct {
		relPath  string
		snippet  string
		expected string
	}{
		// Spaces in directory name.
		{"my dir/app.env", "password=x", "my_dir/app/password"},
		// Leading dot.
		{".config/secrets.yaml", "api_key=x", "config/secrets/api_key"},
		// Colons in filename (e.g. Windows-style).
		{"conf/db:prod.env", "secret=x", "conf/db_prod/secret"},
	}
	for _, tt := range tests {
		got := SuggestPassEntry(tt.relPath, tt.snippet)
		if got != tt.expected {
			t.Errorf("SuggestPassEntry(%q, %q) = %q, want %q", tt.relPath, tt.snippet, got, tt.expected)
		}
	}
}
