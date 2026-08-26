package design

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/diff"
)

// PatchFilePlan is the pending rewrite of one artifact file.
type PatchFilePlan struct {
	// RelPath is relative to the artifact directory; Path is absolute.
	RelPath string        `json:"rel_path"`
	Path    string        `json:"-"`
	Old     string        `json:"-"`
	New     string        `json:"-"`
	Changes []PatchChange `json:"changes"`
}

// Diff renders a unified diff of the pending rewrite.
func (p PatchFilePlan) Diff() (string, int, int) {
	return diff.GenerateDiff(p.Old, p.New, p.RelPath)
}

// PatchPlan is the resolved, not-yet-written result of a design_patch call. It
// exists so the tool layer can show a real diff in the permission prompt before
// anything touches the user's working tree.
type PatchPlan struct {
	Artifact Artifact        `json:"artifact"`
	Files    []PatchFilePlan `json:"files"`
}

// Empty reports whether the plan would change nothing.
func (p *PatchPlan) Empty() bool {
	for _, f := range p.Files {
		if f.Old != f.New {
			return false
		}
	}
	return true
}

// PreparePatch resolves patch operations against the artifact's source files
// and returns the rewritten content without writing anything.
//
// Operations addressed by node_id are resolved through the node index of the
// artifact's current version, which is what turns a UI selection
// (design://<node_id>) into a source edit.
func (s *Service) PreparePatch(ctx context.Context, artifactID string, ops []PatchOp) (*PatchPlan, error) {
	if len(ops) == 0 {
		return nil, errors.New("design: no patch operations given")
	}
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		return nil, err
	}
	manifest, err := ReadManifest(absDir)
	if err != nil {
		return nil, err
	}

	// Group the operations by target file, resolving node ids on the way.
	byFile := map[string][]PatchOp{}
	var order []string
	for i := range ops {
		op := ops[i]
		if op.Op == "" {
			return nil, fmt.Errorf("op %d: \"op\" is required", i+1)
		}
		if op.NodeID != "" {
			node, err := s.store.GetNode(ctx, artifactID, artifact.CurrentVersion, op.NodeID)
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("op %d: node %s is not in the index of version %d; run design_render first", i+1, op.NodeID, artifact.CurrentVersion)
			}
			if err != nil {
				return nil, err
			}
			if node.Selector == "" {
				return nil, fmt.Errorf("op %d: node %s has no selector to patch", i+1, op.NodeID)
			}
			op.Selector = node.Selector
		}
		if op.Selector == "" {
			return nil, fmt.Errorf("op %d: either \"node_id\" or \"selector\" is required", i+1)
		}
		file := op.File
		if file == "" {
			file = manifest.Entry
		}
		file = filepath.ToSlash(filepath.Clean(file))
		if _, seen := byFile[file]; !seen {
			order = append(order, file)
		}
		byFile[file] = append(byFile[file], op)
	}
	sort.Strings(order)

	plan := &PatchPlan{Artifact: artifact}
	for _, rel := range order {
		abs, err := safeArtifactPath(absDir, rel)
		if err != nil {
			return nil, err
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("design: read %s: %w", rel, err)
		}
		out, changes, err := ApplyPatch(src, byFile[rel])
		if err != nil {
			return nil, fmt.Errorf("design: patch %s: %w", rel, err)
		}
		plan.Files = append(plan.Files, PatchFilePlan{
			RelPath: rel,
			Path:    abs,
			Old:     string(src),
			New:     string(out),
			Changes: changes,
		})
	}
	return plan, nil
}

// ApplyPatchPlan writes a prepared plan to disk. When commit is true a new
// version (a directory-scoped snapshot) is recorded and its number returned;
// otherwise the returned version is 0 and the change stays uncommitted, exactly
// like an ordinary edit of the files.
func (s *Service) ApplyPatchPlan(ctx context.Context, plan *PatchPlan, summary string, commit bool) (int, error) {
	if plan == nil {
		return 0, errors.New("design: nil patch plan")
	}
	for _, f := range plan.Files {
		if f.Old == f.New {
			continue
		}
		info, err := os.Stat(f.Path)
		mode := os.FileMode(0o644)
		if err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(f.Path, []byte(f.New), mode); err != nil {
			return 0, fmt.Errorf("design: write %s: %w", f.RelPath, err)
		}
	}
	if !commit {
		return 0, nil
	}
	if summary == "" {
		summary = "patch"
	}
	version, err := s.CommitVersion(ctx, plan.Artifact.ID, summary)
	if err != nil {
		return 0, err
	}
	return version.Number, nil
}

// Patch is the one-shot convenience path used by the CLI and tests: prepare,
// write, optionally commit. The tool layer uses PreparePatch/ApplyPatchPlan so
// it can gate the write on a permission prompt showing the diff.
func (s *Service) Patch(ctx context.Context, artifactID string, ops []PatchOp, summary string, commit bool) (*PatchPlan, int, error) {
	plan, err := s.PreparePatch(ctx, artifactID, ops)
	if err != nil {
		return nil, 0, err
	}
	version, err := s.ApplyPatchPlan(ctx, plan, summary, commit)
	if err != nil {
		return nil, 0, err
	}
	return plan, version, nil
}

// safeArtifactPath joins a file name to the artifact directory, refusing any
// path that would escape it.
func safeArtifactPath(absDir, rel string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) || cleaned == ".." || hasParentPrefix(cleaned) {
		return "", fmt.Errorf("design: %q escapes the artifact directory", rel)
	}
	target := filepath.Join(absDir, cleaned)
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(absDir)
	if err != nil {
		return "", err
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("design: %q escapes the artifact directory", rel)
	}
	return resolved, nil
}
