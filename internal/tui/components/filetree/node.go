package filetree

import (
	"path/filepath"
	"strings"

	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
)

type GitFileStatus int

const (
	GitStatusClean GitFileStatus = iota
	GitStatusModified
	GitStatusAdded
	GitStatusDeleted
	GitStatusUntracked
	GitStatusRenamed
)

type FileNode struct {
	Name       string
	Path       string
	IsDir      bool
	IsExpanded bool
	Children   []*FileNode
	GitStatus  GitFileStatus
	Depth      int
	Icon       string
	Loaded     bool
}

func NewRootNode(projectPath string) *FileNode {
	name := filepath.Base(projectPath)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = projectPath
	}

	return &FileNode{
		Name:       name,
		Path:       ".",
		IsDir:      true,
		IsExpanded: true,
		Depth:      0,
		Icon:       directoryIcon(true),
	}
}

func NewFileNode(name, path string, isDir bool, depth int, status GitFileStatus) *FileNode {
	return &FileNode{
		Name:      name,
		Path:      normalizeTreePath(path),
		IsDir:     isDir,
		GitStatus: status,
		Depth:     depth,
		Icon:      iconForNode(name, isDir, false),
	}
}

func (n *FileNode) SetExpanded(expanded bool) {
	n.IsExpanded = expanded
	n.Icon = iconForNode(n.Name, n.IsDir, expanded)
}

func (s GitFileStatus) Priority() int {
	switch s {
	case GitStatusDeleted:
		return 5
	case GitStatusModified:
		return 4
	case GitStatusAdded:
		return 3
	case GitStatusRenamed:
		return 2
	case GitStatusUntracked:
		return 1
	default:
		return 0
	}
}

func MergeGitStatus(current, next GitFileStatus) GitFileStatus {
	if next.Priority() > current.Priority() {
		return next
	}
	return current
}

func normalizeTreePath(path string) string {
	if path == "" || path == "." {
		return "."
	}
	cleaned := filepath.Clean(path)
	cleaned = filepath.ToSlash(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "" {
		return "."
	}
	return cleaned
}

func iconForNode(name string, isDir, expanded bool) string {
	if isDir {
		return directoryIcon(expanded)
	}
	return tuistyles.FileIconFor(name)
}

func directoryIcon(expanded bool) string {
	if expanded {
		return tuistyles.FolderOpenIcon
	}
	return tuistyles.FolderIcon
}
