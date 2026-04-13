package provision

import (
	"bufio"
	"os"
	"strings"
)

// UndoResult describes what happened when undoing a single entry.
type UndoResult struct {
	Entry   Entry  `json:"entry"`
	Action  string `json:"action"`  // "removed", "restored", "skipped", "failed"
	Message string `json:"message"` // human-readable detail
}

// Undo reverses a single provision entry. It checks current state before
// acting and never fails fatally — returns a result describing what happened.
func Undo(e Entry) UndoResult {
	switch e.Type {
	case TypeFile:
		return undoFile(e)
	case TypeDir:
		return undoDir(e)
	case TypeSymlink:
		return undoSymlink(e)
	case TypeConfigLine:
		return undoConfigLine(e)
	case TypeGitExclude:
		return undoConfigLine(e) // same mechanism: remove a line from a file
	default:
		return UndoResult{Entry: e, Action: "skipped", Message: "unknown provision type"}
	}
}

func undoFile(e Entry) UndoResult {
	if _, err := os.Lstat(e.Path); err != nil {
		return UndoResult{Entry: e, Action: "skipped", Message: "already absent"}
	}
	if err := os.Remove(e.Path); err != nil {
		return UndoResult{Entry: e, Action: "failed", Message: err.Error()}
	}
	return UndoResult{Entry: e, Action: "removed", Message: "deleted"}
}

func undoDir(e Entry) UndoResult {
	info, err := os.Stat(e.Path)
	if err != nil {
		return UndoResult{Entry: e, Action: "skipped", Message: "already absent"}
	}
	if !info.IsDir() {
		return UndoResult{Entry: e, Action: "skipped", Message: "not a directory"}
	}
	if err := os.RemoveAll(e.Path); err != nil {
		return UndoResult{Entry: e, Action: "failed", Message: err.Error()}
	}
	return UndoResult{Entry: e, Action: "removed", Message: "deleted"}
}

func undoSymlink(e Entry) UndoResult {
	info, err := os.Lstat(e.Path)
	if err != nil {
		return UndoResult{Entry: e, Action: "skipped", Message: "already absent"}
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return UndoResult{Entry: e, Action: "skipped", Message: "not a symlink (overwritten)"}
	}
	if err := os.Remove(e.Path); err != nil {
		return UndoResult{Entry: e, Action: "failed", Message: err.Error()}
	}
	return UndoResult{Entry: e, Action: "removed", Message: "symlink removed"}
}

func undoConfigLine(e Entry) UndoResult {
	if e.Line == "" {
		return UndoResult{Entry: e, Action: "skipped", Message: "no line recorded"}
	}

	content, err := os.ReadFile(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return UndoResult{Entry: e, Action: "skipped", Message: "file absent"}
		}
		return UndoResult{Entry: e, Action: "failed", Message: err.Error()}
	}

	original := string(content)
	if !strings.Contains(original, e.Line) {
		return UndoResult{Entry: e, Action: "skipped", Message: "line not found"}
	}

	// Remove all occurrences of the exact line.
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(original))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != strings.TrimSpace(e.Line) {
			result = append(result, scanner.Text())
		}
	}

	newContent := strings.Join(result, "\n")
	if len(result) > 0 {
		newContent += "\n"
	}

	if err := os.WriteFile(e.Path, []byte(newContent), 0o644); err != nil {
		return UndoResult{Entry: e, Action: "failed", Message: err.Error()}
	}
	return UndoResult{Entry: e, Action: "removed", Message: "line removed"}
}
