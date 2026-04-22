package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureRequiresInit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := Execute([]string{"--workspace", workspace, "capture", "-e"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit code without init, got 0")
	}
}

func TestCaptureLsDefaultOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "ls"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture ls failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "default") {
		t.Fatalf("expected 'default' in capture ls output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "captures.md") {
		t.Fatalf("expected 'captures.md' in capture ls output, got: %s", out.String())
	}
}

func TestCaptureLsJson(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "ls", "--json"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture ls --json failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\"locations\"") {
		t.Fatalf("expected JSON with 'locations' key, got: %s", out.String())
	}
}

func TestCaptureLsWithLocations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	personalDir := filepath.Join(t.TempDir(), "Personal")
	configPath := filepath.Join(workspace, "ws", "config.json")
	configContent := `{
  "config_schema": 1,
  "capture": {
    "max_attach_mb": 5,
    "locations": {
      "personal": "` + personalDir + `"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "ls"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture ls failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "default") {
		t.Fatalf("expected 'default' in output, got: %s", output)
	}
	if !strings.Contains(output, "personal") {
		t.Fatalf("expected 'personal' in output, got: %s", output)
	}
}

func TestCaptureEditOpensEditor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "-e"}, strings.NewReader("\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture -e failed: code=%d stderr=%s", code, errOut.String())
	}
}

func TestCaptureEditWithLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	personalDir := filepath.Join(t.TempDir(), "Personal")
	configPath := filepath.Join(workspace, "ws", "config.json")
	configContent := `{
  "config_schema": 1,
  "capture": {
    "max_attach_mb": 5,
    "locations": {
      "personal": "` + personalDir + `"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "personal", "-e"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture personal -e failed: code=%d stderr=%s", code, errOut.String())
	}

	personalCapturesPath := filepath.Join(personalDir, "captures", "captures.md")
	if _, err := os.Stat(personalCapturesPath); err != nil {
		t.Fatalf("personal captures.md should exist after edit: %v", err)
	}
}

func TestCapturePipeStdin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	pipedInput := "kubectl get pods -o wide\nNAME       READY   STATUS\napi-pod    1/1     Running\n"
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader(pipedInput), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture with piped stdin failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, err := os.ReadFile(capturesPath)
	if err != nil {
		t.Fatalf("failed to read captures file: %v", err)
	}
	if !strings.Contains(string(content), "kubectl get pods") {
		t.Fatalf("captures file should contain piped content, got: %s", string(content))
	}
	if !strings.Contains(string(content), "---") {
		t.Fatalf("captures file should contain entry separator, got: %s", string(content))
	}
}

func TestCapturePipeWithLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	personalDir := filepath.Join(t.TempDir(), "Personal")
	configPath := filepath.Join(workspace, "ws", "config.json")
	configContent := `{
  "config_schema": 1,
  "capture": {
    "max_attach_mb": 5,
    "locations": {
      "personal": "` + personalDir + `"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	pipedInput := "personal note content\n"
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "personal", "--quiet"}, strings.NewReader(pipedInput), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture to personal location failed: code=%d stderr=%s", code, errOut.String())
	}

	personalCapturesPath := filepath.Join(personalDir, "captures", "captures.md")
	content, err := os.ReadFile(personalCapturesPath)
	if err != nil {
		t.Fatalf("failed to read personal captures file: %v", err)
	}
	if !strings.Contains(string(content), "personal note content") {
		t.Fatalf("personal captures file should contain the content, got: %s", string(content))
	}
}

func TestCaptureMultiplePipedEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("first entry\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("first capture failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("second entry\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("second capture failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, err := os.ReadFile(capturesPath)
	if err != nil {
		t.Fatalf("failed to read captures file: %v", err)
	}
	if !strings.Contains(string(content), "first entry") {
		t.Fatalf("captures file should contain first entry, got: %s", string(content))
	}
	if !strings.Contains(string(content), "second entry") {
		t.Fatalf("captures file should contain second entry, got: %s", string(content))
	}
	if strings.Count(string(content), "---") < 2 {
		t.Fatalf("expected at least 2 entry separators, got: %s", string(content))
	}
}

func TestCaptureDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	pipedInput := "this should not be written\n"
	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--dry-run"}, strings.NewReader(pipedInput), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	if _, err := os.Stat(capturesPath); err == nil {
		content, _ := os.ReadFile(capturesPath)
		if strings.Contains(string(content), "this should not be written") {
			t.Fatalf("dry-run should not write content to captures file")
		}
	}
}

func TestCaptureDefaultLocationReserved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	configPath := filepath.Join(workspace, "ws", "config.json")
	configContent := `{
  "config_schema": 1,
  "capture": {
    "locations": {
      "default": "/tmp/bad"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "ls"}, strings.NewReader(""), &out, &errOut)
	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "reserved") && code == 0 {
		if !strings.Contains(out.String(), "captures.md") {
			t.Fatalf("default location should always point to ws/captures/captures.md regardless of config override")
		}
	}
}

func TestCaptureHelpOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture --help failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "capture") {
		t.Fatalf("help output should mention 'capture', got: %s", output)
	}
	if !strings.Contains(output, "--amend") {
		t.Fatalf("help output should mention '--amend', got: %s", output)
	}
	if !strings.Contains(output, "--edit") {
		t.Fatalf("help output should mention '--edit', got: %s", output)
	}
}

func TestCaptureCreatesDirectoryOnFirstUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	if _, err := os.Stat(capturesPath); err == nil {
		t.Fatalf("captures/captures.md should not exist before first capture")
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("first capture ever\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("first capture failed: code=%d stderr=%s", code, errOut.String())
	}

	if _, err := os.Stat(capturesPath); err != nil {
		t.Fatalf("captures/captures.md should exist after first capture: %v", err)
	}

	content, _ := os.ReadFile(capturesPath)
	if !strings.HasPrefix(string(content), "# Captures") {
		t.Fatalf("captures.md should start with '# Captures' header, got: %s", string(content))
	}
}

func TestCaptureAmendPipedText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("original content\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("first capture failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "capture", "--amend", "--quiet"}, strings.NewReader("additional context\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("amend failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, err := os.ReadFile(capturesPath)
	if err != nil {
		t.Fatalf("failed to read captures file: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "original content") {
		t.Fatalf("captures file should contain original content, got: %s", s)
	}
	if !strings.Contains(s, "additional context") {
		t.Fatalf("captures file should contain amended content, got: %s", s)
	}
	if strings.Count(s, "---") != 1 {
		t.Fatalf("expected exactly 1 entry separator, got %d in: %s", strings.Count(s, "---"), s)
	}
}

func TestCaptureAmendShortFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("base entry\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("first capture failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "capture", "-a", "--quiet"}, strings.NewReader("extra info\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("amend with -a failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, _ := os.ReadFile(capturesPath)
	s := string(content)
	if !strings.Contains(s, "extra info") {
		t.Fatalf("captures file should contain amended content, got: %s", s)
	}
	if strings.Count(s, "---") != 1 {
		t.Fatalf("expected 1 entry separator, got %d in: %s", strings.Count(s, "---"), s)
	}
}

func TestCaptureAmendNoExistingEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--amend", "--quiet"}, strings.NewReader("orphan content\n"), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit code when amending with no existing entry")
	}
}

func TestCaptureAmendDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("existing entry\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("first capture failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "capture", "--amend", "--dry-run"}, strings.NewReader("should not appear\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("amend --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, _ := os.ReadFile(capturesPath)
	if strings.Contains(string(content), "should not appear") {
		t.Fatalf("dry-run amend should not write content to captures file")
	}
}

func TestCaptureAmendWithLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	workDir := filepath.Join(t.TempDir(), "Work")
	configPath := filepath.Join(workspace, "ws", "config.json")
	configContent := `{
  "config_schema": 1,
  "capture": {
    "max_attach_mb": 5,
    "locations": {
      "work": "` + workDir + `"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "work", "--quiet"}, strings.NewReader("work content\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture to work failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"--workspace", workspace, "capture", "work", "-a", "--quiet"}, strings.NewReader("work amendment\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("amend to work failed: code=%d stderr=%s", code, errOut.String())
	}

	workCapturesPath := filepath.Join(workDir, "captures", "captures.md")
	content, err := os.ReadFile(workCapturesPath)
	if err != nil {
		t.Fatalf("failed to read work captures file: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "work content") {
		t.Fatalf("work captures should contain original content, got: %s", s)
	}
	if !strings.Contains(s, "work amendment") {
		t.Fatalf("work captures should contain amendment, got: %s", s)
	}
	if strings.Count(s, "---") != 1 {
		t.Fatalf("expected 1 entry separator, got %d in: %s", strings.Count(s, "---"), s)
	}
}

func TestCapturePositionalLocationDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(t.TempDir(), "Workspace")
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Execute([]string{"--workspace", workspace, "capture", "--quiet"}, strings.NewReader("default location content\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("capture failed: code=%d stderr=%s", code, errOut.String())
	}

	capturesPath := filepath.Join(workspace, "ws", "captures", "captures.md")
	content, _ := os.ReadFile(capturesPath)
	if !strings.Contains(string(content), "default location content") {
		t.Fatalf("content should be in default location, got: %s", string(content))
	}
}
