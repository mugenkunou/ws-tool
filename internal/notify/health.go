package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// HealthSummary is the schema written to ws/health.json after every scan.
// Consumed by ws tui and shell prompt integrations for near-real-time
// workspace health without running scans themselves.
type HealthSummary struct {
	Timestamp       time.Time          `json:"timestamp"`
	Trigger         string             `json:"trigger"` // "periodic", "inotify", "mega-sync", "manual"
	Summary         HealthSubsystems   `json:"summary"`
	ViolationsCount int                `json:"violations_count"`
	Violations      []HealthViolation  `json:"violations,omitempty"`
}

// HealthSubsystems mirrors the subsystem breakdown from ws scan.
type HealthSubsystems struct {
	Ignore  HealthCounts `json:"ignore"`
	Secret  HealthCounts `json:"secret"`
	Dotfile HealthCounts `json:"dotfile"`
	Log     HealthLog    `json:"log"`
	Trash   HealthTrash  `json:"trash"`
}

// HealthCounts holds critical/warning tallies for a subsystem.
type HealthCounts struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
}

// HealthLog captures log subsystem state.
type HealthLog struct {
	Active     bool `json:"active"`
	CapPercent int  `json:"cap_percent"`
}

// HealthTrash captures trash subsystem state.
type HealthTrash struct {
	Configured bool `json:"configured"`
	Warnings   int  `json:"warnings"`
}

// HealthViolation is a single violation entry in the health file.
type HealthViolation struct {
	Group    string `json:"group"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message,omitempty"`
	SizeMB   int    `json:"size_mb,omitempty"`
}

// HealthPath returns the path to ws/health.json for a workspace.
func HealthPath(workspacePath string) string {
	return filepath.Join(workspacePath, "ws", "health.json")
}

// WriteHealth writes the health summary to ws/health.json.
func WriteHealth(workspacePath string, h HealthSummary) error {
	path := HealthPath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// ReadHealth reads the health summary from ws/health.json.
// Returns a zero-value HealthSummary if the file does not exist.
func ReadHealth(workspacePath string) (HealthSummary, error) {
	content, err := os.ReadFile(HealthPath(workspacePath))
	if err != nil {
		if os.IsNotExist(err) {
			return HealthSummary{}, nil
		}
		return HealthSummary{}, err
	}
	var h HealthSummary
	if err := json.Unmarshal(content, &h); err != nil {
		return HealthSummary{}, err
	}
	return h, nil
}
