package notify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitName = "ws-notify.service"

// UnitName returns the systemd unit name.
func UnitName() string {
	return unitName
}

// UnitPath returns the path where the systemd user unit is installed.
func UnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

// GenerateUnit produces the systemd unit file content for the daemon.
func GenerateUnit(wsBinaryPath, workspacePath, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=ws workspace notification daemon
After=default.target

[Service]
Type=simple
ExecStart=%s notify daemon --workspace %s --config %s
Restart=on-failure
RestartSec=5
Environment=HOME=%s

[Install]
WantedBy=default.target
`, wsBinaryPath, workspacePath, configPath, os.Getenv("HOME"))
}

// InstallUnit writes the unit file to the systemd user directory.
func InstallUnit(content string) (string, error) {
	path, err := UnitPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// EnableAndStart reloads systemd, enables and starts the unit.
func EnableAndStart() error {
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := systemctl("enable", unitName); err != nil {
		return fmt.Errorf("enable: %w", err)
	}
	if err := systemctl("start", unitName); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// StopAndDisable stops and disables the unit.
func StopAndDisable() error {
	// Stop first (ignore error if not running).
	_ = systemctl("stop", unitName)
	if err := systemctl("disable", unitName); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	return nil
}

// RemoveUnit deletes the unit file.
func RemoveUnit() error {
	path, err := UnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = systemctl("daemon-reload")
	return nil
}

// SystemdStatus holds parsed systemd unit status.
type SystemdStatus struct {
	Active  bool   `json:"active"`
	Running bool   `json:"running"`
	Output  string `json:"output,omitempty"`
}

// QueryStatus checks the systemd unit status.
func QueryStatus() (SystemdStatus, error) {
	if !HasSystemd() {
		return SystemdStatus{}, nil
	}
	cmd := exec.Command("systemctl", "--user", "is-active", unitName)
	out, err := cmd.Output()
	status := strings.TrimSpace(string(out))

	result := SystemdStatus{
		Active:  status == "active",
		Running: status == "active",
	}

	// Get full status for the output field.
	cmd2 := exec.Command("systemctl", "--user", "status", unitName)
	fullOut, _ := cmd2.CombinedOutput()
	result.Output = strings.TrimSpace(string(fullOut))

	// is-active returns non-zero for inactive, which is not an error for us.
	if err != nil && status == "" {
		return result, err
	}
	return result, nil
}

// HasSystemd returns true if systemctl --user is available.
func HasSystemd() bool {
	_, err := exec.LookPath("systemctl")
	if err != nil {
		return false
	}
	cmd := exec.Command("systemctl", "--user", "is-system-running")
	_ = cmd.Run()
	// If systemctl exists and doesn't error fatally, user session is usable.
	return true
}

func systemctl(args ...string) error {
	fullArgs := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
