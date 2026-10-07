package code

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
)

// canonicalRoot returns rootPath as an absolute, cleaned path with symlinks
// resolved when possible, so two spellings of one directory compare equal.
func canonicalRoot(rootPath string) string {
	p := strings.TrimSpace(rootPath)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

// ResolveProjectID returns the project id rootPath must be indexed under.
// Callers derive ids in different ways (directory name, sanitized full path,
// configured id), so without this one directory ends up indexed twice under
// two ids, or one id is moved back and forth between two directories, and
// every switch re-embeds the whole tree. The rules are:
//
//  1. rootPath is already registered: its existing id, whatever was requested
//     (the most recently indexed one if a previous version left several).
//  2. requestedID is free, or registered for a directory that no longer
//     exists (a moved project): requestedID.
//  3. requestedID belongs to another existing directory: requestedID plus a
//     short hash of rootPath, so neither project overwrites the other.
func (c *CodeIndexer) ResolveProjectID(ctx context.Context, requestedID, rootPath string) (string, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return "", fmt.Errorf("code: project_id is required")
	}
	root := canonicalRoot(rootPath)
	if root == "" {
		return "", fmt.Errorf("code: project path is required")
	}

	rows, err := c.db.QueryContext(ctx, `
		SELECT project_id, root_path FROM code_projects
		ORDER BY (last_indexed_at IS NULL), last_indexed_at DESC, updated_at DESC`)
	if err != nil {
		return "", fmt.Errorf("code: list projects: %w", err)
	}
	defer rows.Close()

	var samePath []string
	requestedRoot := ""
	requestedExists := false
	for rows.Next() {
		var id, existingRoot string
		if err := rows.Scan(&id, &existingRoot); err != nil {
			return "", fmt.Errorf("code: scan project: %w", err)
		}
		canonical := canonicalRoot(existingRoot)
		if canonical == root {
			samePath = append(samePath, id)
		}
		if id == requestedID {
			requestedExists = true
			requestedRoot = canonical
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("code: list projects: %w", err)
	}

	if len(samePath) > 0 {
		if len(samePath) > 1 {
			logging.Warn("code: directory indexed under several project ids, using the most recent",
				"path", root, "project_id", samePath[0], "duplicates", strings.Join(samePath[1:], ","))
		}
		return samePath[0], nil
	}
	if !requestedExists || requestedRoot == "" {
		return requestedID, nil
	}
	if _, statErr := os.Stat(requestedRoot); errors.Is(statErr, os.ErrNotExist) {
		return requestedID, nil
	}

	sum := sha256.Sum256([]byte(root))
	return requestedID + "_" + hex.EncodeToString(sum[:4]), nil
}

// projectRootMatches reports whether projectID is registered for rootPath,
// comparing canonical paths.
func (c *CodeIndexer) projectRootMatches(ctx context.Context, projectID, rootPath string) (bool, error) {
	var existingRoot string
	err := c.db.QueryRowContext(ctx, `SELECT root_path FROM code_projects WHERE project_id = ?`, projectID).Scan(&existingRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("code: get project: %w", err)
	}
	return canonicalRoot(existingRoot) == canonicalRoot(rootPath), nil
}
