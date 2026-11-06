package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mugenkunou/ws-tool/internal/plan"
)

// AddActions builds the plan for `ws trash add <path…>` (and the TUI's add
// prompt): one action per path, each moving that path into the trash with
// SoftDelete. Relative paths are resolved against the current directory.
// Every path is checked before anything is returned, so a missing path, or the
// trash root or something inside it, stops the whole command with nothing moved.
func AddActions(paths []string) ([]plan.Action, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	var missing []string
	var actions []plan.Action
	for _, raw := range paths {
		p, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(p); err != nil {
			missing = append(missing, p)
			continue
		}
		if p == root || strings.HasPrefix(p, root+string(os.PathSeparator)) {
			return nil, fmt.Errorf("%s is the trash or inside it; it is already in the trash", p)
		}
		actions = append(actions, plan.Action{
			ID:          "trash-add-" + p,
			Description: "Move " + p + " to the trash",
			Execute:     func() error { return SoftDelete(p) },
		})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("path not found: %s; nothing was moved", strings.Join(missing, ", "))
	}
	return actions, nil
}
