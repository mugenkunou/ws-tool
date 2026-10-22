// Package trash implements ws's soft-delete subsystem. ws shares the user's XDG
// trash directory ($XDG_DATA_HOME/Trash, default ~/.local/share/Trash) with the
// desktop environment: items deleted via ws (scratch rm, session rm, ...), via a
// shell `rm` configured by `ws trash enable`, VS Code and the file manager's
// Delete all land in the same place, in the freedesktop layout (files/ and
// info/*.trashinfo, https://specifications.freedesktop.org/trash-spec). One list
// shows them all, and `ws trash empty` and the desktop's "Empty Trash" operate on
// the same content. There is no other trash root.
package trash

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mugenkunou/ws-tool/internal/fsutil"
)

// Root returns the user's XDG trash directory: $XDG_DATA_HOME/Trash, defaulting
// to ~/.local/share/Trash. It is the only trash root ws uses.
func Root() (string, error) {
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Join(dataHome, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

func filesDir(root string) string { return filepath.Join(root, "files") }
func infoDir(root string) string  { return filepath.Join(root, "info") }

// SoftDelete moves path into the trash so it is recoverable and visible to the
// desktop's file manager. This is the "soft-delete first" principle: ws-internal
// delete paths (scratch rm, session rm, ...) call SoftDelete instead of
// os.RemoveAll, unconditionally. When the trash cannot be located, or the move
// fails, it returns the error: it never deletes for good as a fallback, which a
// failed lookup used to do silently.
func SoftDelete(path string) error {
	root, err := Root()
	if err != nil {
		return fmt.Errorf("find the trash: %w", err)
	}
	_, err = moveToTrash(root, path)
	return err
}

// moveToTrash moves path into root's files/ directory and writes a matching
// freedesktop .trashinfo sidecar into root's info/ directory. It returns the name
// the item was stored under (after collision resolution).
func moveToTrash(root, path string) (string, error) {
	if err := os.MkdirAll(filesDir(root), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(infoDir(root), 0o755); err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	base := filepath.Base(absPath)
	name := uniqueTrashName(root, base)
	dest := filepath.Join(filesDir(root), name)

	if err := fsutil.Move(absPath, dest); err != nil {
		return "", err
	}
	if err := writeTrashInfo(root, name, absPath, time.Now()); err != nil {
		return name, err
	}
	return name, nil
}

// uniqueTrashName returns a name for base that doesn't collide with an existing
// files/ entry or info/ sidecar, appending ".2", ".3", … as needed: the same
// scheme gio and Nautilus use.
func uniqueTrashName(root, base string) string {
	name := base
	for n := 2; ; n++ {
		_, filesErr := os.Lstat(filepath.Join(filesDir(root), name))
		_, infoErr := os.Lstat(filepath.Join(infoDir(root), name+".trashinfo"))
		if os.IsNotExist(filesErr) && os.IsNotExist(infoErr) {
			return name
		}
		name = fmt.Sprintf("%s.%d", base, n)
	}
}

// writeTrashInfo writes the freedesktop .trashinfo sidecar for a trashed item:
// original path (percent-encoded per the spec) and deletion timestamp.
func writeTrashInfo(root, name, originalPath string, deletedAt time.Time) error {
	encoded := (&url.URL{Path: originalPath}).EscapedPath()
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		encoded, deletedAt.Format("2006-01-02T15:04:05"))
	return os.WriteFile(filepath.Join(infoDir(root), name+".trashinfo"), []byte(content), 0o644)
}
