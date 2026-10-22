// Package fsutil holds the file moves that more than one command needs, so there
// is one implementation of "rename, or copy and remove across filesystems".
package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Move renames src to dst. When they are on different filesystems it copies
// src and then removes it, and a failed copy leaves src untouched. A symlink is
// moved as the link itself, like mv. dst must not exist.
func Move(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
		if err := Copy(src, dst); err != nil {
			_ = os.RemoveAll(dst) // do not leave a half copy behind
			return err
		}
		return os.RemoveAll(src)
	}
	return err
}

// Copy copies a file, directory tree or symlink to dst, keeping modes. dst must
// not exist: Copy never overwrites.
func Copy(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := Copy(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
