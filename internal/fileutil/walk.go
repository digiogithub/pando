package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
)

// maxSymlinkWalkDepth bounds how deep a walk may descend through symlinked
// directories. It is a safety net on top of the visited-set cycle detection for
// pathological layouts (e.g. deeply chained links).
const maxSymlinkWalkDepth = 64

// symlinkDirEntry wraps a symlink whose target is a directory so that callers
// see it as a regular directory entry, while Name/Info still describe the link.
type symlinkDirEntry struct {
	fs.DirEntry
	target fs.FileInfo
}

func (e symlinkDirEntry) IsDir() bool                { return true }
func (e symlinkDirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (e symlinkDirEntry) Info() (fs.FileInfo, error) { return e.target, nil }

// rootDirEntry adapts a FileInfo into a DirEntry for the walk root.
type rootDirEntry struct{ info fs.FileInfo }

func (e rootDirEntry) Name() string               { return e.info.Name() }
func (e rootDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e rootDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e rootDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

// WalkFollowSymlinks walks the file tree rooted at root like filepath.WalkDir,
// but follows symbolic links that point to directories — both when the root
// itself is a link and for links found during the walk. Paths reported to fn
// are logical (they keep the link name, they are not rewritten to the target),
// which is what users and tools expect.
//
// Symlink loops are avoided by tracking the resolved real path of every
// directory that has been entered; a link pointing at an already-visited
// directory is reported as an entry but not descended into.
func WalkFollowSymlinks(root string, fn fs.WalkDirFunc) error {
	info, err := os.Stat(root)
	if err != nil {
		return fn(root, nil, err)
	}

	if !info.IsDir() {
		return fn(root, rootDirEntry{info: info}, nil)
	}

	visited := make(map[string]struct{})
	if real, rerr := filepath.EvalSymlinks(root); rerr == nil {
		visited[real] = struct{}{}
	}

	err = walkFollow(root, rootDirEntry{info: info}, fn, visited, 0)
	if err == filepath.SkipDir || err == filepath.SkipAll {
		return nil
	}
	return err
}

// walkFollow reports dir itself and then recurses into its entries.
func walkFollow(dir string, d fs.DirEntry, fn fs.WalkDirFunc, visited map[string]struct{}, depth int) error {
	if err := fn(dir, d, nil); err != nil {
		if err == filepath.SkipDir {
			return nil
		}
		return err
	}

	if depth >= maxSymlinkWalkDepth {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Mirror filepath.WalkDir: report the error against the directory and let
		// the callback decide whether to abort the whole walk.
		if cbErr := fn(dir, d, err); cbErr != nil && cbErr != filepath.SkipDir {
			return cbErr
		}
		return nil
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		if entry.Type()&fs.ModeSymlink == 0 {
			if entry.IsDir() {
				if err := walkFollow(path, entry, fn, visited, depth+1); err != nil {
					return err
				}
				continue
			}
			if err := fn(path, entry, nil); err != nil {
				if err == filepath.SkipDir {
					continue
				}
				return err
			}
			continue
		}

		// Symlink: resolve it to decide whether it behaves as a file or a directory.
		target, terr := os.Stat(path)
		if terr != nil || !target.IsDir() {
			// Broken link or link to a file: report as-is, never descend.
			if err := fn(path, entry, nil); err != nil {
				if err == filepath.SkipDir {
					continue
				}
				return err
			}
			continue
		}

		linkEntry := symlinkDirEntry{DirEntry: entry, target: target}

		real, rerr := filepath.EvalSymlinks(path)
		if rerr != nil {
			if err := fn(path, linkEntry, nil); err != nil && err != filepath.SkipDir {
				return err
			}
			continue
		}
		if _, seen := visited[real]; seen {
			// Already walked this directory through another path: report the link
			// but do not descend, otherwise the walk would loop forever.
			if err := fn(path, linkEntry, nil); err != nil && err != filepath.SkipDir {
				return err
			}
			continue
		}
		visited[real] = struct{}{}

		if err := walkFollow(path, linkEntry, fn, visited, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// IsDirEntry reports whether the entry is a directory, resolving symlinks so a
// link pointing at a directory is treated as one. dir is the directory holding
// the entry.
func IsDirEntry(dir string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, entry.Name()))
	return err == nil && info.IsDir()
}
