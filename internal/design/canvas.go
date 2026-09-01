package design

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/design/preview"
)

// The canvas is the "watch it being built" surface: one read-only window
// holding every artifact of a session as an artboard, laid out side by side and
// refreshing itself while the agent works.
//
// The page lives in internal/design/preview; this file is the half that knows
// the design model. It answers one question for the preview server — "what
// artboards does this session have right now?" — and mints the URL a surface
// opens.

// renderingSet tracks the artifacts a render is currently running on, so the
// canvas can badge them instead of showing a stale document with no explanation
// of why it has not changed yet.
var renderingSet = struct {
	mu sync.Mutex
	m  map[string]int
}{m: make(map[string]int)}

// markRendering records that a render started, and returns the function that
// records it finishing. Renders nest (a critique pass renders again inside a
// render), so this counts rather than flags.
func markRendering(artifactID string) func() {
	if artifactID == "" {
		return func() {}
	}
	renderingSet.mu.Lock()
	renderingSet.m[artifactID]++
	renderingSet.mu.Unlock()
	return func() {
		renderingSet.mu.Lock()
		if renderingSet.m[artifactID] <= 1 {
			delete(renderingSet.m, artifactID)
		} else {
			renderingSet.m[artifactID]--
		}
		renderingSet.mu.Unlock()
	}
}

func isRendering(artifactID string) bool {
	renderingSet.mu.Lock()
	defer renderingSet.mu.Unlock()
	return renderingSet.m[artifactID] > 0
}

// CanvasArtboards is the [preview.Options.Artboards] provider. It is installed
// on every preview server in this process, which is what lets the preview
// package serve a canvas without importing the design model.
//
// An empty session id means "everything in this project", which is what a CLI
// invocation and a fresh window both want; a session id narrows the canvas to
// the artifacts that session created.
func CanvasArtboards(sessionID string) ([]preview.Artboard, error) {
	svc, err := ServiceFor("")
	if err != nil {
		return nil, err
	}
	server := PreviewServer()
	if server == nil {
		return nil, fmt.Errorf("design: no preview server is running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return svc.Artboards(ctx, server, sessionID)
}

// Artboards builds the canvas model against an explicit server. It is the half
// of CanvasArtboards that resolves nothing from the process, which is what makes
// it testable and what lets a caller that already holds both use them.
func (s *Service) Artboards(ctx context.Context, server *preview.Server, sessionID string) ([]preview.Artboard, error) {
	artifacts, err := s.List(ctx, false)
	if err != nil {
		return nil, err
	}
	if sessionID != "" {
		filtered := artifacts[:0:0]
		for _, a := range artifacts {
			if a.SessionID == sessionID {
				filtered = append(filtered, a)
			}
		}
		// A session that has not created anything yet still gets the project's
		// artboards: an empty canvas that stays empty while the designer folder
		// is full of work reads as a bug, not as a filter.
		if len(filtered) > 0 {
			artifacts = filtered
		}
	}

	// Oldest first, so a new artboard lands at the end of the canvas and the
	// ones the user has already positioned in their view do not shift.
	sort.SliceStable(artifacts, func(i, j int) bool {
		return artifacts[i].CreatedAt.Before(artifacts[j].CreatedAt)
	})

	boards := make([]preview.Artboard, 0, len(artifacts))
	for _, artifact := range artifacts {
		boards = append(boards, s.artboard(server, artifact))
	}
	return boards, nil
}

// artboard describes one artifact for the canvas, publishing it on the way so
// the frame has a URL to load. A failure is reported on the board rather than
// aborting the whole canvas: one broken artifact must not blank the window.
func (s *Service) artboard(server *preview.Server, artifact Artifact) preview.Artboard {
	board := preview.Artboard{
		ID:        artifact.ID,
		Title:     artifact.Title,
		Slug:      artifact.Slug,
		Kind:      string(artifact.Kind),
		Version:   artifact.CurrentVersion,
		Status:    "ready",
		UpdatedAt: artifact.UpdatedAt,
		Width:     DefaultViewport.W,
		Height:    DefaultViewport.H,
	}

	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		board.Status = "error"
		board.Note = err.Error()
		return board
	}
	if manifest, err := ReadManifest(absDir); err == nil {
		if manifest.Preview.Viewport.W > 0 {
			board.Width = manifest.Preview.Viewport.W
		}
		if manifest.Preview.Viewport.H > 0 {
			board.Height = manifest.Preview.Viewport.H
		}
	}

	grant, err := s.publishPreviewOn(server, artifact)
	if err != nil {
		board.Status = "error"
		board.Note = err.Error()
		return board
	}
	url, err := server.URL(artifact.ID, preview.URLOptions{})
	if err != nil {
		board.Status = "error"
		board.Note = err.Error()
		return board
	}
	board.URL = url
	// The frame reloads when this number changes. The server's own revision
	// only moves on a render or a committed version, which would leave the
	// canvas frozen through the edits in between — exactly the part a watcher
	// wants to see — so the directory's newest write counts too.
	board.Revision = server.Revision(artifact.ID) + dirRevision(absDir)

	entry := filepath.Join(absDir, filepath.FromSlash(grant.Entry))
	if info, err := os.Stat(entry); err != nil || info.IsDir() {
		board.Status = "error"
		board.Note = "the entry document has not been written yet"
		board.URL = ""
	} else if isRendering(artifact.ID) {
		board.Status = "building"
	}
	return board
}

// dirRevision folds an artifact directory's newest modification time into a
// number the canvas can compare between polls. It walks two levels deep and
// stops after a few hundred entries: an artifact directory is a handful of
// files, and a directory that is not is not worth a full walk once a second.
func dirRevision(absDir string) uint64 {
	const maxEntries = 256
	var newest int64
	seen := 0

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if seen >= maxEntries {
				return
			}
			seen++
			if entry.IsDir() {
				if depth > 0 {
					walk(filepath.Join(dir, entry.Name()), depth-1)
				}
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if unix := info.ModTime().Unix(); unix > newest {
				newest = unix
			}
		}
	}
	walk(absDir, 1)
	if newest < 0 {
		return 0
	}
	return uint64(newest)
}

// CanvasPresentation publishes the canvas of a session and returns its URL,
// starting a loopback preview server when this process has none. It is the one
// call every "open the canvas" path goes through — the tool, the CLI, the TUI
// key, the WebUI button and the auto-open — so they all agree on when a
// listener is allowed to come into existence.
func CanvasPresentation(sessionID string) (string, error) {
	server, err := EnsurePreviewServer()
	if err != nil {
		return "", err
	}
	if _, err := server.PublishCanvas(sessionID); err != nil {
		return "", err
	}
	return server.CanvasURL(sessionID)
}
