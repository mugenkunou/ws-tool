package dotfile

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// dirExpandThreshold is the maximum number of regular files in a directory at
// which we expand to individual file entries. Above this, ExpandDir collapses
// to immediate children so the UI stays manageable.
const dirExpandThreshold = 20

// DirEntry is one candidate file or subdirectory returned by ExpandDir.
type DirEntry struct {
	// Name is the path relative to the root passed to ExpandDir (slash-separated).
	Name string
	// AbsPath is the absolute filesystem path.
	AbsPath string
	// IsDir is true when this entry is a subdirectory (collapsed view only).
	IsDir bool
	// FileCount is the number of regular files contained (1 for plain files).
	FileCount int
	// Size is the total byte count.
	Size int64
	// Class is the heuristic classification.
	Class FileClass
}

// ExpandDir analyzes absPath and returns entries for the interactive selection UI.
//
// When the directory contains ≤ dirExpandThreshold regular files in total, it
// returns those individual files (expanded == true). Otherwise it returns the
// immediate children of absPath with aggregated stats (expanded == false), so
// the user makes decisions at the folder level rather than per-file.
func ExpandDir(absPath string) (entries []DirEntry, expanded bool, err error) {
	var allFiles []string
	walkErr := filepath.WalkDir(absPath, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.Type().IsRegular() {
			allFiles = append(allFiles, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, false, walkErr
	}

	if len(allFiles) <= dirExpandThreshold {
		expanded = true
		for _, f := range allFiles {
			info, _ := os.Stat(f)
			sz := int64(0)
			if info != nil {
				sz = info.Size()
			}
			rel, _ := filepath.Rel(absPath, f)
			entries = append(entries, DirEntry{
				Name:      filepath.ToSlash(rel),
				AbsPath:   f,
				IsDir:     false,
				FileCount: 1,
				Size:      sz,
				Class:     ClassifyFile(f),
			})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		return entries, true, nil
	}

	// Collapse to immediate children.
	children, readErr := os.ReadDir(absPath)
	if readErr != nil {
		return nil, false, readErr
	}
	for _, child := range children {
		childAbs := filepath.Join(absPath, child.Name())
		var count int
		var total int64
		var class FileClass
		if child.IsDir() {
			count, total = dirStats(childAbs)
			class = ClassifyDir(child.Name(), count, total)
		} else if child.Type().IsRegular() {
			info, _ := child.Info()
			count = 1
			if info != nil {
				total = info.Size()
			}
			class = ClassifyFile(childAbs)
		} else {
			continue // skip symlinks, sockets, etc.
		}
		entries = append(entries, DirEntry{
			Name:      child.Name(),
			AbsPath:   childAbs,
			IsDir:     child.IsDir(),
			FileCount: count,
			Size:      total,
			Class:     class,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, false, nil
}

// CollectFiles returns all regular file absolute paths under entry.
// For a plain file entry it returns just entry.AbsPath.
// For a directory entry it walks the tree recursively.
func CollectFiles(entry DirEntry) ([]string, error) {
	if !entry.IsDir {
		return []string{entry.AbsPath}, nil
	}
	var files []string
	err := filepath.WalkDir(entry.AbsPath, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// dirStats returns the regular file count and total byte size of a directory tree.
func dirStats(absPath string) (count int, totalBytes int64) {
	filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			info, statErr := d.Info()
			if statErr == nil {
				totalBytes += info.Size()
			}
			count++
		}
		return nil
	})
	return
}
