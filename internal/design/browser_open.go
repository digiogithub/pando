package design

import (
	"context"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/auth"
)

// ResolveCreatedArtifactPresentation resolves the URL a newly created artifact
// should be opened with. It upgrades a file:// presentation to a served preview
// when this process can start a preview server.
func ResolveCreatedArtifactPresentation(ctx context.Context, artifactID string) (Presentation, error) {
	svc, err := ServiceFor("")
	if err != nil {
		return Presentation{}, err
	}
	presentation, err := svc.Presentation(ctx, artifactID, 0, "")
	if err != nil {
		return Presentation{}, err
	}
	if strings.HasPrefix(presentation.URL, "file://") {
		if _, err := svc.PublishPreview(ctx, artifactID); err == nil {
			return svc.Presentation(ctx, artifactID, 0, "")
		}
	}
	return presentation, nil
}

// AutoOpenTarget resolves what a surface should open when an artifact is
// created. It is the canvas, not the artifact: the canvas holds every artboard
// of the session and updates itself, so the second and every later artifact
// appear in the window already open instead of spawning one window each.
//
// The returned key is what the auto-opener deduplicates on. It is the canvas
// key while the canvas resolves, and the artifact id when it does not — falling
// back to the single-artifact preview keeps a machine with no canvas working
// exactly as it did before.
func AutoOpenTarget(ctx context.Context, sessionID, artifactID string) (key, url string, err error) {
	if canvasURL, cerr := CanvasPresentation(sessionID); cerr == nil && canvasURL != "" {
		return "canvas:" + sessionID, canvasURL, nil
	}
	presentation, err := ResolveCreatedArtifactPresentation(ctx, artifactID)
	if err != nil {
		return "", "", err
	}
	return artifactID, presentation.URL, nil
}

// BrowserAutoOpener deduplicates best-effort browser launches for design work
// so one target opens at most once automatically per process or session
// surface. The key is whatever the caller wants deduplicated: the session
// canvas, or a single artifact when there is no canvas.
type BrowserAutoOpener struct {
	mu     sync.Mutex
	opened map[string]struct{}
}

func NewBrowserAutoOpener() *BrowserAutoOpener {
	return &BrowserAutoOpener{opened: make(map[string]struct{})}
}

func (o *BrowserAutoOpener) Open(key, url string) error {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(url) == "" || !auth.CanOpenBrowser() {
		return nil
	}
	o.mu.Lock()
	if _, exists := o.opened[key]; exists {
		o.mu.Unlock()
		return nil
	}
	o.opened[key] = struct{}{}
	o.mu.Unlock()
	return auth.OpenBrowser(url)
}
