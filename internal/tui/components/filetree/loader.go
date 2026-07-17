package filetree

import (
	"bytes"
	"fmt"
	"io"
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
	path string
	// isDir is true for real directories and for symlinks resolving to one.
	isDir bool
	// isSymlink marks entries git cannot be asked about with a trailing slash:
	// "git check-ignore" fails with "pathspec is beyond a symbolic link" and
	// aborts the whole batch.
	isSymlink bool
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

// LoadFileTreeExpanded reloads the tree from disk re-reading the children of
// every directory the user had expanded, so a refresh (manual or automatic)
// does not collapse the view back to the root level.
func LoadFileTreeExpanded(projectPath string, opts LoadOptions, expanded map[string]bool, statuses map[string]GitFileStatus) tea.Cmd {
	expandedCopy := cloneExpanded(expanded)
	statusesCopy := cloneStatuses(statuses)
	return func() tea.Msg {
		root := NewRootNode(projectPath)
		children, err := readDirectoryExpanded(projectPath, ".", 1, opts, expandedCopy, statusesCopy)
		if err != nil {
			return FileTreeRefreshMsg{Err: err}
		}
		root.Children = children
		root.Loaded = true
		return FileTreeRefreshMsg{Root: root}
	}
}

func readDirectoryExpanded(projectPath, parentPath string, depth int, opts LoadOptions, expanded map[string]bool, statuses map[string]GitFileStatus) ([]*FileNode, error) {
	nodes, err := readDirectory(projectPath, parentPath, depth, opts, statuses)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if !node.IsDir || !expanded[node.Path] {
			continue
		}
		children, err := readDirectoryExpanded(projectPath, node.Path, depth+1, opts, expanded, statuses)
		if err != nil {
			// A directory that disappeared or became unreadable between the
			// listing and the recursion is left collapsed rather than failing
			// the whole refresh.
			continue
		}
		node.Children = children
		node.Loaded = true
		node.SetExpanded(true)
	}
	return nodes, nil
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
		return GitStatusUpdateMsg{Statuses: loadGitStatuses(projectPath)}
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
		candidates = append(candidates, loadCandidate{
			path:      relPath,
			isDir:     fileutil.IsDirEntry(absDir, entry),
			isSymlink: entry.Type()&fs.ModeSymlink != 0,
		})
	}

	ignored := ignoredPaths(projectPath, candidates)

	nodes := make([]*FileNode, 0, len(candidates))
	for _, entry := range entries {
		relPath := normalizeTreePath(filepath.ToSlash(filepath.Join(parentPath, entry.Name())))
		if !opts.ShowHidden && fileutil.SkipHidden(relPath) {
			continue
		}
		if ignored[relPath] {
			continue
		}

		isDir := fileutil.IsDirEntry(absDir, entry)
		node := NewFileNode(entry.Name(), relPath, isDir, depth, statuses[relPath])
		if isDir {
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

// loadGitStatuses reads the working-tree status. Like ignoredPaths it never
// fails: git status is decoration, so a directory that is not a repository, a
// missing git binary or any other git failure just means no status colors.
func loadGitStatuses(projectPath string) map[string]GitFileStatus {
	cmd := exec.Command("git", "-C", projectPath, "status", "--porcelain=v1", "--untracked-files=all")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return map[string]GitFileStatus{}
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
	return statuses
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

// ignoredPaths asks git which of the candidates are .gitignore'd. It never
// fails: when git cannot answer (not a repository, no git binary) nothing is
// reported as ignored and the tree lists everything.
func ignoredPaths(projectPath string, candidates []loadCandidate) map[string]bool {
	ignored := make(map[string]bool)
	if len(candidates) == 0 {
		return ignored
	}

	input := make([]string, 0, len(candidates))
	lookup := make(map[string]string, len(candidates)*2)
	for _, candidate := range candidates {
		lookup[candidate.path] = candidate.path
		input = append(input, candidate.path)
		// The trailing-slash form is what matches directory-only patterns such
		// as "reports/", but git refuses it for a symlink ("pathspec is beyond a
		// symbolic link") and that fatal aborts the whole batch, leaving every
		// later candidate unchecked.
		if candidate.isDir && !candidate.isSymlink {
			withSlash := candidate.path + "/"
			lookup[withSlash] = candidate.path
			input = append(input, withSlash)
		}
	}

	cmd := exec.Command("git", "-C", projectPath, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(input, "\n") + "\n")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		// Only a clean exit is trustworthy. Exit 1 means nothing matched, exit
		// 128 means this is not a repository or git hit a fatal, and a missing
		// binary is not an error either — in every case the tree lists
		// everything rather than applying .gitignore.
		//
		// Partial output must NOT be honored: a fatal aborts the batch midway,
		// so the paths printed before it would be hidden while the ones after it
		// stayed visible, making the tree depend on listing order.
		return ignored
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
	return ignored
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
	err := fileutil.WalkFollowSymlinks(projectPath, func(path string, d fs.DirEntry, err error) error {
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

func cloneExpanded(expanded map[string]bool) map[string]bool {
	if len(expanded) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(expanded))
	for path, value := range expanded {
		cloned[path] = value
	}
	return cloned
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
