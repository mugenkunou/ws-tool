package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecuteHelp(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"help"}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	if !strings.Contains(out.String(), "Workspace manager") {
		t.Fatalf("unexpected help output: %s", out.String())
	}
}

func TestVersionShort(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"version", "--short"}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	if strings.TrimSpace(out.String()) != appVersion {
		t.Fatalf("expected version %s, got %s", appVersion, out.String())
	}
}

func TestHelpFlags(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"-h"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("global -h should succeed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Global Flags") {
		t.Fatalf("expected global help with flags, got: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"version", "-h"}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("version -h should succeed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--short") {
		t.Fatalf("expected version help output, got: %s", out.String())
	}
}

func TestVersionJSON(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"--json", "version"}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	var payload struct {
		WSVersion string `json:"ws_version"`
		Schema    int    `json:"schema"`
		Data      struct {
			Platform string `json:"platform"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if payload.WSVersion != appVersion {
		t.Fatalf("expected ws_version %s, got %s", appVersion, payload.WSVersion)
	}
	if payload.Schema != 1 {
		t.Fatalf("expected schema 1, got %d", payload.Schema)
	}
	if payload.Data.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("unexpected platform %s", payload.Data.Platform)
	}
}

func TestInitAndConfigView(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "Workspace")

	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("init failed: exit=%d stderr=%s", exit, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	exit = Execute([]string{"--workspace", workspace, "config", "view"}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("config view failed: exit=%d stderr=%s", exit, errOut.String())
	}
	if !strings.Contains(out.String(), "\"config_schema\": 1") {
		t.Fatalf("unexpected config output: %s", out.String())
	}
}

func TestConfigViewRequiresInit(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "Workspace")

	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"--workspace", workspace, "config", "view"}, strings.NewReader("y\n"), &out, &errOut)
	if exit == 0 {
		t.Fatal("expected non-zero exit for uninitialized workspace")
	}
	if !strings.Contains(errOut.String(), "workspace not initialized") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestConfigDefaultsFlag(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	exit := Execute([]string{"config", "--defaults"}, strings.NewReader("y\n"), &out, &errOut)
	if exit != 0 {
		t.Fatalf("config --defaults failed: exit=%d stderr=%s", exit, errOut.String())
	}
	if !strings.Contains(out.String(), "\"config_schema\": 1") {
		t.Fatalf("unexpected defaults output: %s", out.String())
	}
}

func TestConfigDumpIsRejected(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "Workspace")

	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := Execute([]string{"init", "--workspace", workspace}, strings.NewReader("y\n"), &out, &errOut); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"--workspace", workspace, "config", "dump"}, strings.NewReader("y\n"), &out, &errOut); code == 0 {
		t.Fatalf("config dump should fail, got code=%d stdout=%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "unknown config subcommand") {
		t.Fatalf("unexpected dump error output: %s", errOut.String())
	}
}
