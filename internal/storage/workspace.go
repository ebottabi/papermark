/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */
package storage

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileInfo describes one workspace file.
type FileInfo struct {
	Path       string
	Size       int64
	ModifiedAt time.Time
}

// ListFiles returns the regular files under root (symlinks excluded), sorted by
// path. A missing root lists nothing.
func ListFiles(root string) ([]FileInfo, error) {
	out := []FileInfo{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, FileInfo{Path: filepath.ToSlash(rel), Size: info.Size(), ModifiedAt: info.ModTime()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// OpenFile opens rel inside root for reading. It returns ErrEscape when rel or
// its real path (a symlink the agent made could point anywhere) leaves root,
// and fs.ErrNotExist when it is missing or not a regular file.
func OpenFile(root, rel string) (*os.File, fs.FileInfo, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, fs.ErrNotExist
	}
	inside := func(p string) bool { return strings.HasPrefix(p, realRoot+string(filepath.Separator)) }
	p := filepath.Join(realRoot, rel)
	if !inside(p) {
		return nil, nil, ErrEscape
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, nil, fs.ErrNotExist
	}
	if !inside(real) {
		return nil, nil, ErrEscape
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, nil, fs.ErrNotExist
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, fs.ErrNotExist
	}
	return f, info, nil
}

// CopyDir copies the tree at src to dst (files, directories and symlinks as
// links). A missing src copies nothing.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == src {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(p, target)
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
