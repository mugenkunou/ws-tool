package symlink

import (
	"path"
	"path/filepath"
	"strings"
)

// Storage under symlinks/ mirrors the host path (ADR 0003 §2):
//
//	~/.gitconfig  →  symlinks/home/.gitconfig
//	/etc/hosts    →  symlinks/system/etc/hosts
//
// The path is a pure function of the host path, so two files never collide and any
// file under symlinks/ maps back to its host path by removing the prefix.
const (
	MirrorHome   = SymlinksRoot + "/home"
	MirrorSystem = SymlinksRoot + "/system"
)

// MirrorPath is the workspace-relative, slash-separated storage path of a host
// path. A path inside home becomes symlinks/home/<relative path>; any other
// absolute path becomes symlinks/system/<path without the leading slash>. The path
// is cleaned first, so a trailing slash makes no difference.
//
// It returns "" for a path that has no storage path: one that is not absolute, that
// still holds ".." after cleaning, or that is home itself (home is never one entry).
func MirrorPath(host, home string) string {
	h := filepath.Clean(host)
	if !filepath.IsAbs(h) || hasDotDot(h) {
		return ""
	}
	if home != "" {
		hm := filepath.Clean(home)
		if h == hm {
			return ""
		}
		if rel, ok := within(hm, h); ok {
			return MirrorHome + "/" + filepath.ToSlash(rel)
		}
	}
	rel := strings.TrimPrefix(filepath.ToSlash(h), "/")
	if rel == "" {
		return "" // the filesystem root
	}
	return MirrorSystem + "/" + rel
}

// HostPath reverses MirrorPath: the host path whose storage path is original (a
// workspace-relative path under symlinks/home/ or symlinks/system/). It returns ""
// for anything else, including the two prefixes alone.
func HostPath(original, home string) string {
	o := path.Clean(filepath.ToSlash(original))
	switch {
	case strings.HasPrefix(o, MirrorHome+"/"):
		rel := strings.TrimPrefix(o, MirrorHome+"/")
		if home == "" || hasDotDot(rel) {
			return ""
		}
		return filepath.Join(home, filepath.FromSlash(rel))
	case strings.HasPrefix(o, MirrorSystem+"/"):
		rel := strings.TrimPrefix(o, MirrorSystem+"/")
		if hasDotDot(rel) {
			return ""
		}
		return filepath.FromSlash("/" + rel)
	}
	return ""
}

// within returns child relative to dir, and whether child lies strictly inside it.
func within(dir, child string) (string, bool) {
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(child, prefix) {
		return "", false
	}
	return strings.TrimPrefix(child, prefix), true
}

func hasDotDot(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
