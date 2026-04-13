package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretScanSkipDirConfig(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create secrets in a subdirectory.
	vendorDir := filepath.Join(workspace, "vendor", "lib")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "auth.conf"), []byte("password=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without skip_dirs, scan should find a violation.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "secret", "scan"}, nil, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2 (violations), got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}

	// Add vendor to skip_dirs in config.
	configPath := filepath.Join(workspace, "ws", "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		t.Fatal(err)
	}
	secretCfg := cfg["secret"].(map[string]any)
	secretCfg["skip_dirs"] = []string{"vendor"}
	cfg["secret"] = secretCfg
	updated, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}

	// With skip_dirs, scan should find no violations.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "secret", "scan"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 (no violations after skip), got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestSecretScanSkipDirFlag(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Create secrets in testdata.
	testdataDir := filepath.Join(workspace, "testdata")
	if err := os.MkdirAll(testdataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testdataDir, "creds.txt"), []byte("api_key=sk-abc123def456ghi789\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --skip-dir, scan finds violations.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "secret", "scan"}, nil, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}

	// With --skip-dir flag, scan skips.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "secret", "scan", "--skip-dir", "testdata"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 with --skip-dir, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestSecretScanSkipDirJSON(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "--json", "secret", "scan", "--skip-dir", "vendor"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, errOut.String())
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v output=%s", err, out.String())
	}
	data := result["data"].(map[string]any)
	dirs, ok := data["skipped_dirs"].([]any)
	if !ok || len(dirs) != 1 || dirs[0] != "vendor" {
		t.Fatalf("expected skipped_dirs=[vendor] in JSON, got: %v", data["skipped_dirs"])
	}
}

func TestSecretScanSkipDirMergesConfigAndFlag(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out, errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\ny\ny\ny\ny\ny\ny\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	// Set skip_dirs in config.
	configPath := filepath.Join(workspace, "ws", "config.json")
	configBytes, _ := os.ReadFile(configPath)
	var cfg map[string]any
	json.Unmarshal(configBytes, &cfg)
	secretCfg := cfg["secret"].(map[string]any)
	secretCfg["skip_dirs"] = []string{"vendor"}
	cfg["secret"] = secretCfg
	updated, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(configPath, updated, 0o644)

	// Create secrets in both dirs.
	for _, dir := range []string{"vendor", "testdata"} {
		d := filepath.Join(workspace, dir)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "secret.env"), []byte("password=realpass123\n"), 0o644)
	}

	// With only config (vendor), testdata should still be found.
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "secret", "scan"}, nil, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2 (testdata not skipped), got %d", code)
	}

	// With both config + flag, all should be skipped.
	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "secret", "scan", "--skip-dir", "testdata"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0 (both skipped), got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
