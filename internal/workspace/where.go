package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Placement is where Where says a file belongs.
type Placement struct {
	// Folder is a workspace-relative folder (`artifacts/`, `configs/<system>-<instance>/`)
	// or a home written with its variable (`$WS_SCRATCH/<name>.<yyyy-mm>/`).
	// Empty when no rule fits.
	Folder string `json:"folder"`
	Reason string `json:"reason"`
	// Matched is false when no rule fits; the caller asks the user.
	Matched bool `json:"matched"`
	// Inside is true when the path already lies in Folder.
	Inside bool `json:"inside"`
}

// maxScriptLines is the longest script that still counts as one small block
// for scripts/. A longer one is hand-written or multi-step: experiments/.
const maxScriptLines = 60

var (
	blobExts = []string{".pdf", ".pptx", ".ppt", ".docx", ".xlsx", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp",
		".zip", ".tar", ".tgz", ".gz", ".bz2", ".xz", ".7z", ".csv", ".parquet", ".sqlite", ".db"}
	configExts = []string{".yml", ".yaml", ".xml", ".env", ".properties", ".conf", ".ini", ".toml"}
	scriptExts = []string{".sh", ".bash", ".py", ".rb", ".pl", ".js", ".ts", ".go", ".rs"}
)

// Where says which folder a file or directory belongs in, by the rules of
// skills/workspace-write/write-rules.md ("Where a file goes") that its name and
// type settle. It reads no content, except to count the lines of a script, and
// writes nothing. The path may not exist yet. path is absolute.
func Where(workspacePath string, homes []Home, path string) Placement {
	return whereAt(workspacePath, homes, path, time.Now())
}

func whereAt(workspacePath string, homes []Home, path string, now time.Time) Placement {
	path = filepath.Clean(path)
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	ext := filepath.Ext(lower)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" {
		stem = name
	}
	info, statErr := os.Stat(path)
	isDir := statErr == nil && info.IsDir()
	placed := func(folder, reason string) Placement {
		return Placement{Folder: folder, Reason: reason, Matched: true}
	}

	// Already placed: under a layout folder, or in a home.
	if rel, ok := relUnder(workspacePath, path); ok {
		best := -1
		for i, d := range Layout {
			if (rel == d.Path || strings.HasPrefix(rel, d.Path+"/")) && (best < 0 || len(d.Path) > len(Layout[best].Path)) {
				best = i
			}
		}
		if best >= 0 {
			d := Layout[best]
			reason := d.Purpose
			if d.Path == "notes" {
				reason = "the user's own; read it, write elsewhere"
			}
			return Placement{Folder: d.Path + "/", Reason: "already placed: " + reason, Matched: true, Inside: true}
		}
	}
	for _, h := range homes {
		if _, ok := relUnder(h.Path, path); ok {
			return Placement{Folder: "$" + h.Var + "/", Reason: "already placed: " + h.Purpose, Matched: true, Inside: true}
		}
	}

	// A path at /etc lives at a system path, whatever its name.
	if _, ok := relUnder("/etc", path); ok {
		return placed("symlinks/", "belongs at a system path; ws symlink add "+path)
	}

	// A directory is a code project when it holds .git (a file in a worktree).
	if isDir {
		if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
			return placed("experiments/"+name+"/", "git repo: code projects live in experiments/<name>/")
		}
		return Placement{Reason: "a directory that is not a git repo; no rule fits"}
	}

	switch {
	case ext == ".har":
		return placed("$WS_SCRATCH/"+stem+"."+now.Format("2006-01")+"/",
			"capture (.har): holds live sessions and secrets; throwaway work lives in scratch")
	case slices.Contains(blobExts, ext):
		return placed("artifacts/", "raw blob ("+ext+")")
	case name == "WISDOM.md" || name == "RUNBOOKS.md":
		return placed("references/", "structured doc for agents ("+name+")")
	case isSpecName(lower):
		return placed("references/", "API spec for agents ("+name+")")
	case slices.Contains(configExts, ext) || strings.HasPrefix(lower, ".env"):
		return placed("configs/<system>-<instance>/",
			"settings an app reads as they are; a value that is a secret goes in pass instead")
	case strings.HasPrefix(name, "."):
		return placed("symlinks/", "belongs at a system path; ws symlink add "+path)
	case slices.Contains(scriptExts, ext):
		n, ok := countLines(path)
		switch {
		case !ok:
			return placed("experiments/", "script ("+ext+") not readable here; hand-written scripts start in experiments/")
		case n <= maxScriptLines:
			return placed("scripts/", fmt.Sprintf("small script (%d lines): one action, a composable block", n))
		default:
			return placed("experiments/", fmt.Sprintf("script of %d lines (over %d): hand-written or multi-step", n, maxScriptLines))
		}
	}
	return Placement{Reason: "no rule fits its name or type"}
}

// isSpecName reports whether a lower-cased base name is an OpenAPI or Swagger spec.
func isSpecName(lower string) bool {
	return strings.HasPrefix(lower, "openapi.") || strings.HasPrefix(lower, "swagger.") || strings.Contains(lower, ".openapi.")
}

// relUnder returns path relative to root, with forward slashes, when path is
// root itself or lies below it.
func relUnder(root, path string) (string, bool) {
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(filepath.Clean(root), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// countLines counts the lines of a regular file; ok is false when it cannot be read.
func countLines(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n := bytes.Count(b, []byte("\n"))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n, true
}
