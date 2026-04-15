package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mugenkunou/ws-tool/internal/ignore"
)

func TestSecretScanDetectsPatterns(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}

	file := filepath.Join(ws, "app.env")
	content := "FOO=bar\npassword=hunter2\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	engine, err := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected at least one violation")
	}
	if violations[0].Type != "secret" {
		t.Fatalf("unexpected type: %s", violations[0].Type)
	}
}

func TestSecretAllowlistSuppressesFinding(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}

	rel := "secret.txt"
	file := filepath.Join(ws, rel)
	if err := os.WriteFile(file, []byte("api_key=abc123\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	engine, err := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}

	allow := map[string]struct{}{rel + ":1": {}}
	violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: allow})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no violations after allowlist, got %+v", violations)
	}
}

func TestSecretScanLongLineDoesNotFail(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}
	long := strings.Repeat("A", 200000) + " password=hunter2"
	if err := os.WriteFile(filepath.Join(ws, "long.txt"), []byte(long+"\n"), 0o644); err != nil {
		t.Fatalf("write long file failed: %v", err)
	}
	engine, err := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}
	violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("scan failed on long line: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected secret violation from long line")
	}
}

func TestSecretScanSkipsBrokenSymlinks(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644); err != nil {
		t.Fatalf("write megaignore failed: %v", err)
	}

	// Create a broken symlink to a file.
	if err := os.Symlink(filepath.Join(ws, "gone.txt"), filepath.Join(ws, "broken-link")); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}

	// Create a broken symlink to a directory.
	if err := os.Symlink(filepath.Join(ws, "no-such-dir"), filepath.Join(ws, "broken-dir")); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}

	// Normal file with a secret — should still be found.
	if err := os.WriteFile(filepath.Join(ws, "app.env"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	engine, err := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
	if err != nil {
		t.Fatalf("load engine failed: %v", err)
	}
	violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("scan must not fail on broken symlinks, got: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected at least one violation from app.env")
	}
}

func TestSecretScanDetectsNewPatterns(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"token", "token=abc123secret\n"},
		{"auth", "auth: myauthvalue99\n"},
		{"access_key", "access_key=AKIAIOSFODNN7EXAMPLE\n"},
		{"client_secret", "client_secret=cs_live_abcdefg\n"},
		{"connection_string", "connection_string=Server=mydb;Password=s3cret\n"},
		{"github_token_prefix", "GITHUB_TOKEN=ghp_ABCDEFdummy\n"},
		{"aws_key_prefix", "some config AKIA1234567890ABCDEF in this line\n"}, //gitleaks:allow
		{"slack_token", "SLACK=xoxb-1234567890-abcdef\n"},
		{"openai_key", "key is sk-ABCDEF0123456789WXYZ\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			ws := filepath.Join(tmp, "Workspace")
			os.MkdirAll(ws, 0o755)
			os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
			os.WriteFile(filepath.Join(ws, "test.env"), []byte(tc.content), 0o644)
			engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
			violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("expected violation for %q", tc.content)
			}
		})
	}
}

func TestSecretScanSkipsCommentLines(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"hash_comment", "# password=hunter2\n"},
		{"double_slash", "// password=hunter2\n"},
		{"sql_comment", "-- password=hunter2\n"},
		{"semicolon", "; password=hunter2\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			ws := filepath.Join(tmp, "Workspace")
			os.MkdirAll(ws, 0o755)
			os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
			os.WriteFile(filepath.Join(ws, "test.cfg"), []byte(tc.content), 0o644)
			engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
			violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(violations) != 0 {
				t.Fatalf("expected no violations for comment line %q, got %d", tc.content, len(violations))
			}
		})
	}
}

func TestSecretScanSkipsPlaceholders(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"angle_bracket", "password=<YOUR_PASSWORD>\n"},
		{"env_ref", "password=${DB_PASSWORD}\n"},
		{"template_var", "password={{password}}\n"},
		{"changeme", "password=changeme\n"},
		{"example", "password=example\n"},
		{"xxx", "password=xxx\n"},
		{"your_password", "password=your_password\n"},
		{"todo", "password=TODO\n"},
		{"quoted_placeholder", "password=\"<INSERT_HERE>\"\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			ws := filepath.Join(tmp, "Workspace")
			os.MkdirAll(ws, 0o755)
			os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
			os.WriteFile(filepath.Join(ws, "test.cfg"), []byte(tc.content), 0o644)
			engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
			violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(violations) != 0 {
				t.Fatalf("expected no violations for placeholder %q, got %d", tc.content, len(violations))
			}
		})
	}
}

func TestSecretScanRealSecretNotFilteredAsPlaceholder(t *testing.T) {
	cases := []string{
		"password=hunter2\n",
		"password=s3cret!!\n",
		"api_key=sk-12345abcdef\n",
		"secret=my-production-token\n",
	}
	for _, content := range cases {
		tmp := t.TempDir()
		ws := filepath.Join(tmp, "Workspace")
		os.MkdirAll(ws, 0o755)
		os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
		os.WriteFile(filepath.Join(ws, "test.env"), []byte(content), 0o644)
		engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
		violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if len(violations) == 0 {
			t.Fatalf("expected violation for real secret %q", content)
		}
	}
}

func TestSecretScanSkipsLowEntropyValues(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"repeated_fake", "CLIENT_SECRET=FAKEKEYFAKEKEY\n"},
		{"all_same_char", "password=aaaaaaaaa\n"},
		{"simple_repeat", "token=abababababab\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			ws := filepath.Join(tmp, "Workspace")
			os.MkdirAll(ws, 0o755)
			os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
			os.WriteFile(filepath.Join(ws, "test.env"), []byte(tc.content), 0o644)
			engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
			violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(violations) != 0 {
				t.Fatalf("expected no violations for low-entropy %q, got %d", tc.content, len(violations))
			}
		})
	}
}

func TestSecretScanRealSecretNotFilteredAsLowEntropy(t *testing.T) {
	cases := []string{
		"password=hunter2\n",
		"password=s3cret!!\n",
		"api_key=sk-12345abcdef\n",
		"secret=my-production-token\n",
	}
	for _, content := range cases {
		tmp := t.TempDir()
		ws := filepath.Join(tmp, "Workspace")
		os.MkdirAll(ws, 0o755)
		os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
		os.WriteFile(filepath.Join(ws, "test.env"), []byte(content), 0o644)
		engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
		violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if len(violations) == 0 {
			t.Fatalf("expected violation for real secret %q", content)
		}
	}
}

func TestIsLowEntropy(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"FAKEKEYFAKEKEY", true},
		{"aaaaaaaaa", true},
		{"abababababab", true},
		{"hunter2", false},
		{"s3cret!!", false},
		{"sk-12345abcdef", false},
		{"my-production-token", false},
		{"abc", false},     // too short
		{"test", false},    // too short to judge entropy
		{"letmein", false}, // too short to judge entropy
	}
	for _, tc := range cases {
		got := isLowEntropy(tc.value)
		if got != tc.want {
			t.Errorf("isLowEntropy(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestSecretScanDetectsShortRealPassword(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "Workspace")
	os.MkdirAll(ws, 0o755)
	os.WriteFile(filepath.Join(ws, ".megaignore"), []byte("-s:*\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "app.env"), []byte("password=test\n"), 0o644)
	engine, _ := ignore.LoadEngine(filepath.Join(ws, ".megaignore"))
	violations, err := Scan(ScanOptions{WorkspacePath: ws, Engine: engine, Allowlist: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("expected violation for password=test")
	}
}
