package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// Merge returns a filesystem that is base with the rag migrations added to
// its directory dir, so a single goose run (goose.SetBaseFS(merged);
// goose.Up(db, dir)) applies both sets in version order under one history.
//
// It fails when a rag migration has the same file name or the same goose
// version as a file already in base/dir: two migrations with one version
// would make goose skip one of them silently.
func Merge(base fs.FS, dir string) (fs.FS, error) {
	baseEntries, err := fs.ReadDir(base, dir)
	if err != nil {
		return nil, fmt.Errorf("rag migrate: read %s: %w", dir, err)
	}
	ragEntries, err := fs.ReadDir(FS, Dir)
	if err != nil {
		return nil, fmt.Errorf("rag migrate: read embedded migrations: %w", err)
	}

	versions := make(map[string]string, len(baseEntries))
	for _, e := range baseEntries {
		if v, ok := version(e.Name()); ok {
			versions[v] = e.Name()
		}
	}
	m := &mergedFS{base: base, dir: path.Clean(dir), rag: map[string]bool{}}
	for _, e := range ragEntries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if _, err := fs.Stat(base, path.Join(dir, name)); err == nil {
			return nil, fmt.Errorf("rag migrate: %s exists in both %s and the rag module", name, dir)
		}
		if v, ok := version(name); ok {
			if other, dup := versions[v]; dup {
				return nil, fmt.Errorf("rag migrate: version %s used by both %s and %s", v, other, name)
			}
			versions[v] = name
		}
		m.rag[name] = true
	}
	m.entries = append(append([]fs.DirEntry{}, baseEntries...), filterFiles(ragEntries)...)
	slices.SortFunc(m.entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return m, nil
}

// version returns the goose version prefix of a migration file name.
func version(name string) (string, bool) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return "", false
	}
	return name[:i], true
}

func filterFiles(entries []fs.DirEntry) []fs.DirEntry {
	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e)
		}
	}
	return out
}

// mergedFS serves base, except that its directory dir also lists, and opens,
// the rag migrations.
type mergedFS struct {
	base    fs.FS
	dir     string
	rag     map[string]bool
	entries []fs.DirEntry
}

// ragPath maps name to the rag module's copy when name is a rag migration
// inside dir.
func (m *mergedFS) ragPath(name string) (string, bool) {
	if !fs.ValidPath(name) || path.Dir(name) != m.dir || !m.rag[path.Base(name)] {
		return "", false
	}
	return path.Join(Dir, path.Base(name)), true
}

func (m *mergedFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if p, ok := m.ragPath(name); ok {
		return FS.Open(p)
	}
	f, err := m.base.Open(name)
	if err != nil || name != m.dir {
		return f, err
	}
	return &mergedDir{File: f, entries: slices.Clone(m.entries)}, nil
}

// mergedDir is the merged migration directory opened as a file: base's
// directory handle, listing the merged entries.
type mergedDir struct {
	fs.File
	entries []fs.DirEntry
	offset  int
}

func (d *mergedDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.offset:]
	if n <= 0 {
		d.offset = len(d.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	d.offset += n
	return rest[:n], nil
}

func (m *mergedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if name == m.dir {
		return slices.Clone(m.entries), nil
	}
	return fs.ReadDir(m.base, name)
}

func (m *mergedFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	if p, ok := m.ragPath(name); ok {
		return fs.ReadFile(FS, p)
	}
	return fs.ReadFile(m.base, name)
}

func (m *mergedFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if p, ok := m.ragPath(name); ok {
		return fs.Stat(FS, p)
	}
	return fs.Stat(m.base, name)
}
