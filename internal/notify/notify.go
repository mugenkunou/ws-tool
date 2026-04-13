package notify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// State is the on-disk representation of ws/notify.state.
// It tracks daemon lifecycle and deduplication of notifications.
type State struct {
	Active          bool             `json:"active"`
	StartedAt       time.Time        `json:"started_at,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Mode            string           `json:"mode"`
	PID             int              `json:"pid,omitempty"`
	LastScan        time.Time        `json:"last_scan,omitempty"`
	LastAlert       string           `json:"last_alert,omitempty"`
	LastAlertTime   time.Time        `json:"last_alert_time,omitempty"`
	KnownViolations []ViolationKey   `json:"known_violations,omitempty"`
}

// ViolationKey uniquely identifies a violation for deduplication.
type ViolationKey struct {
	Group string `json:"group"`
	Type  string `json:"type"`
	Path  string `json:"path"`
}

// Start writes active state to disk. The actual daemon process is
// managed by the caller (systemd unit or direct fork).
func Start(workspacePath string, pid int) (State, error) {
	now := time.Now().UTC()
	state := State{
		Active:    true,
		StartedAt: now,
		UpdatedAt: now,
		Mode:      "inotify+periodic",
		PID:       pid,
	}
	if err := SaveState(statePath(workspacePath), state); err != nil {
		return State{}, err
	}
	return state, nil
}

// Stop marks the daemon as inactive.
func Stop(workspacePath string) (State, error) {
	s, err := Status(workspacePath)
	if err != nil {
		return State{}, err
	}
	now := time.Now().UTC()
	s.Active = false
	s.PID = 0
	s.UpdatedAt = now
	if err := SaveState(statePath(workspacePath), s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Status reads the current daemon state from disk.
func Status(workspacePath string) (State, error) {
	content, err := os.ReadFile(statePath(workspacePath))
	if err != nil {
		if os.IsNotExist(err) {
			return State{Active: false, Mode: "inotify+periodic"}, nil
		}
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(content, &s); err != nil {
		return State{}, err
	}
	if s.Mode == "" {
		s.Mode = "inotify+periodic"
	}
	return s, nil
}

// Test sends a test notification via notify-send if available.
func Test(workspacePath string) (State, error) {
	s, err := Status(workspacePath)
	if err != nil {
		return State{}, err
	}
	_ = SendNotification("ws", "Test notification sent")
	s.LastAlert = "test notification"
	s.LastAlertTime = time.Now().UTC()
	s.UpdatedAt = time.Now().UTC()
	if err := SaveState(statePath(workspacePath), s); err != nil {
		return State{}, err
	}
	return s, nil
}

// SendNotification sends a desktop notification via notify-send.
// Returns nil silently if notify-send is not available.
func SendNotification(title, body string) error {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return nil
	}
	cmd := exec.Command("notify-send", title, body)
	return cmd.Run()
}

// DiffViolations compares the current scan violations against the
// previously known set and returns only net-new violations.
func DiffViolations(known []ViolationKey, current []HealthViolation) []HealthViolation {
	knownSet := make(map[ViolationKey]struct{}, len(known))
	for _, k := range known {
		knownSet[k] = struct{}{}
	}

	var newViolations []HealthViolation
	for _, v := range current {
		key := ViolationKey{Group: v.Group, Type: v.Type, Path: v.Path}
		if _, exists := knownSet[key]; !exists {
			newViolations = append(newViolations, v)
		}
	}
	return newViolations
}

// ViolationKeys extracts dedup keys from a list of health violations.
func ViolationKeys(violations []HealthViolation) []ViolationKey {
	keys := make([]ViolationKey, len(violations))
	for i, v := range violations {
		keys[i] = ViolationKey{Group: v.Group, Type: v.Type, Path: v.Path}
	}
	return keys
}

// FilterByEvents filters violations to only those matching the
// configured event groups (e.g. "dotfile", "secret", "bloat", "storage").
func FilterByEvents(violations []HealthViolation, events []string) []HealthViolation {
	if len(events) == 0 {
		return violations
	}
	allowed := make(map[string]struct{}, len(events))
	for _, e := range events {
		allowed[e] = struct{}{}
	}
	var filtered []HealthViolation
	for _, v := range violations {
		eventGroup := violationEventGroup(v)
		if _, ok := allowed[eventGroup]; ok {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// violationEventGroup maps a violation to its notify event group name.
func violationEventGroup(v HealthViolation) string {
	switch v.Group {
	case "ignore":
		return "bloat"
	case "secret":
		return "secret"
	case "dotfile":
		return "dotfile"
	case "trash":
		return "storage"
	default:
		return v.Group
	}
}

func statePath(workspacePath string) string {
	return filepath.Join(workspacePath, "ws", "notify.state")
}

// SaveState writes the state to disk.
func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
