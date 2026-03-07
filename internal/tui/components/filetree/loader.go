package filetree

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lithammer/fuzzysearch/fuzzy"

	"github.com/digiogithub/pando/internal/fileutil"
)

type LoadOptions struct {
	ShowHidden bool
}

type FileTreeRefreshMsg struct {
	Root *FileNode
	Err  error
}

type LoadChildrenMsg struct {
	ParentPath string
	Children   []*FileNode
	Err        error
}

type GitStatusUpdateMsg struct {
	Statuses map[string]GitFileStatus
	Err      error
}

type FilterResultsMsg struct {
	Query string
	Root  *FileNode
	Err   error
}

type loadCandidate struct {
	path  string
	isDir bool
}

func LoadFileTree(projectPath string, opts LoadOptions) tea.Cmd {
	return func() tea.Msg {
		root := NewRootNode(projectPath)
		children, err := readDirectory(projectPath, ".", 1, opts, nil)
		if err != nil {
			return FileTreeRefreshMsg{Err: err}
		}
		root.Children = children
		root.Loaded = true
		return FileTreeRefreshMsg{Root: root}
	}
}

func LoadChildren(projectPath, parentPath string, depth int, opts LoadOptions, statuses map[string]GitFileStatus) tea.Cmd {
	statusesCopy := cloneStatuses(statuses)
	return func() tea.Msg {
		children, err := readDirectory(projectPath, parentPath, depth, opts, statusesCopy)
		return LoadChildrenMsg{ParentPath: normalizeTreePath(parentPath), Children: children, Err: err}
	}
}

func LoadGitStatus(projectPath string) tea.Cmd {
	return func() tea.Msg {
		statuses, err := loadGitStatuses(projectPath)
		return GitStatusUpdateMsg{Statuses: statuses, Err: err}
	}
}

func LoadFilteredTree(projectPath, query string, opts LoadOptions, statuses map[string]GitFileStatus) tea.Cmd {
	statusesCopy := cloneStatuses(statuses)
	trimmed := strings.TrimSpace(query)
	return func() tea.Msg {
		root, err := buildFilteredTree(projectPath, trimmed, opts, statusesCopy)
		return FilterResultsMsg{Query: trimmed, Root: root, Err: err}
	}
}

func readDirectory(projectPath, parentPath string, depth int, opts LoadOptions, statuses map[string]GitFileStatus) ([]*FileNode, error) {
	absDir := projectPath
	if parentPath != "." && parentPath != "" {
		absDir = filepath.Join(projectPath, filepath.FromSlash(parentPath))
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", absDir, err)
	}

	candidates := make([]loadCandidate, 0, len(entries))
	for _, entry := range entries {
		relPath := normalizeTreePath(filepath.ToSlash(filepath.Join(parentPath, entry.Name())))
		if !opts.ShowHidden && fileutil.SkipHidden(relPath) {
			continue
		}
		candidates = append(candidates, loadCandidate{path: relPath, isDir: entry.IsDir()})
	}

	ignored, err := ignoredPaths(projectPath, candidates)
	if err != nil {
		return nil, err
	}

	nodes := make([]*FileNode, 0, len(candidates))
	for _, entry := range entries {
		relPath := normalizeTreePath(filepath.ToSlash(filepath.Join(parentPath, entry.Name())))
		if !opts.ShowHidden && fileutil.SkipHidden(relPath) {
			continue
		}
		if ignored[relPath] {
			continue
		}

		node := NewFileNode(entry.Name(), relPath, entry.IsDir(), depth, statuses[relPath])
		if entry.IsDir() {
			node.SetExpanded(false)
		}
		nodes = append(nodes, node)
	}

	sortNodes(nodes)
	return nodes, nil
}

func buildFilteredTree(projectPath, query string, opts LoadOptions, statuses map[string]GitFileStatus) (*FileNode, error) {
	root := NewRootNode(projectPath)
	root.Loaded = true
	if query == "" {
		children, err := readDirectory(projectPath, ".", 1, opts, statuses)
		if err != nil {
			return nil, err
		}
		root.Children = children
		return root, nil
	}

	files, err := listSearchableFiles(projectPath, opts)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return root, nil
	}

	ranked := fuzzy.RankFindNormalizedFold(query, files)
	if len(ranked) == 0 {
		return root, nil
	}
	for _, match := range ranked {
		insertMatchedPath(root, match.Target, statuses)
	}
	sortTree(root)
	return root, nil
}

func insertMatchedPath(root *FileNode, relPath string, statuses map[string]GitFileStatus) {
	cleaned := normalizeTreePath(relPath)
	if cleaned == "." {
		return
	}

	parts := strings.Split(cleaned, "/")
	current := root
	var builtPath string
	for idx, part := range parts {
		if builtPath == "" {
			builtPath = part
		} else {
			builtPath = filepath.ToSlash(filepath.Join(builtPath, part))
		}
		isDir := idx < len(parts)-1
		child := findChildByName(current, part, builtPath)
		if child == nil {
			child = NewFileNode(part, builtPath, isDir, current.Depth+1, statuses[normalizeTreePath(builtPath)])
			child.Loaded = isDir
			child.SetExpanded(isDir)
			current.Children = append(current.Children, child)
		}
		if child.IsDir {
			child.SetExpanded(true)
		}
		current = child
	}
}

func findChildByName(parent *FileNode, name, path string) *FileNode {
	for _, child := range parent.Children {
		if child.Name == name && child.Path == normalizeTreePath(path) {
			return child
		}
	}
	return nil
}

func loadGitStatuses(projectPath string) (map[string]GitFileStatus, error) {
	cmd := exec.Command("git", "-C", projectPath, "status", "--porcelain=v1", "--untracked-files=all")
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if strings.Contains(strings.TrimSpace(string(output)), "not a git repository") {
				return map[string]GitFileStatus{}, nil
			}
		}
		return nil, fmt.Errorf("git status: %w", err)
	}

	statuses := make(map[string]GitFileStatus)
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" || len(line) < 3 {
			continue
		}
		statusCode := line[:2]
		pathPart := strings.TrimSpace(line[3:])
		if strings.Contains(pathPart, " -> ") {
			parts := strings.Split(pathPart, " -> ")
			pathPart = parts[len(parts)-1]
		}
		pathPart = normalizeTreePath(pathPart)
		status := parseGitStatus(statusCode)
		statuses[pathPart] = MergeGitStatus(statuses[pathPart], status)
		propagateStatusToParents(statuses, pathPart, status)
	}
	return statuses, nil
}

func parseGitStatus(code string) GitFileStatus {
	switch {
	case strings.Contains(code, "?"):
		return GitStatusUntracked
	case strings.Contains(code, "D"):
		return GitStatusDeleted
	case strings.Contains(code, "A"):
		return GitStatusAdded
	case strings.Contains(code, "R"):
		return GitStatusRenamed
	case strings.TrimSpace(code) != "":
		return GitStatusModified
	default:
		return GitStatusClean
	}
}

func propagateStatusToParents(statuses map[string]GitFileStatus, relPath string, status GitFileStatus) {
	parent := filepath.Dir(filepath.FromSlash(relPath))
	for parent != "." && parent != string(filepath.Separator) {
		normalized := normalizeTreePath(filepath.ToSlash(parent))
		statuses[normalized] = MergeGitStatus(statuses[normalized], status)
		parent = filepath.Dir(parent)
	}
	statuses["."] = MergeGitStatus(statuses["."], status)
}

func ignoredPaths(projectPath string, candidates []loadCandidate) (map[string]bool, error) {
	ignored := make(map[string]bool)
	if len(candidates) == 0 {
		return ignored, nil
	}

	input := make([]string, 0, len(candidates))
	lookup := make(map[string]string, len(candidates)*2)
	for _, candidate := range candidates {
		lookup[candidate.path] = candidate.path
		input = append(input, candidate.path)
		if candidate.isDir {
			withSlash := candidate.path + "/"
			lookup[withSlash] = candidate.path
			input = append(input, withSlash)
		}
	}

	cmd := exec.Command("git", "-C", projectPath, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(input, "\n") + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			trimmed := strings.TrimSpace(stderr.String())
			if exitErr.ExitCode() == 1 {
				return ignored, nil
			}
			if strings.Contains(trimmed, "not a git repository") {
				return ignored, nil
			}
		}
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if original, ok := lookup[line]; ok {
			ignored[original] = true
		}
	}
	return ignored, nil
}

func listSearchableFiles(projectPath string, opts LoadOptions) ([]string, error) {
	if files, err := gitTrackedAndUntrackedFiles(projectPath, opts); err == nil {
		return files, nil
	}
	return walkFiles(projectPath, opts)
}

func gitTrackedAndUntrackedFiles(projectPath string, opts LoadOptions) ([]string, error) {
	cmd := exec.Command("git", "-C", projectPath, "ls-files", "--cached", "--others", "--exclude-standard")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	files := make([]string, 0)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = normalizeTreePath(line)
		if line == "." || line == "" {
			continue
		}
		if !opts.ShowHidden && fileutil.SkipHidden(line) {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		files = append(files, line)
	}
	sort.Strings(files)
	return files, nil
}

func walkFiles(projectPath string, opts LoadOptions) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(projectPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(projectPath, path)
		if err != nil {
			return err
		}
		rel = normalizeTreePath(filepath.ToSlash(rel))
		if rel == "." {
			return nil
		}
		if !opts.ShowHidden && fileutil.SkipHidden(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func cloneStatuses(statuses map[string]GitFileStatus) map[string]GitFileStatus {
	if len(statuses) == 0 {
		return nil
	}
	cloned := make(map[string]GitFileStatus, len(statuses))
	for path, status := range statuses {
		cloned[path] = status
	}
	return cloned
}

func sortTree(root *FileNode) {
	if root == nil {
		return
	}
	sortNodes(root.Children)
	for _, child := range root.Children {
		sortTree(child)
	}
}

func sortNodes(nodes []*FileNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
}
