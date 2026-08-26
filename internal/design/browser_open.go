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

// BrowserAutoOpener deduplicates best-effort browser launches for design
// artifacts so one artifact opens at most once automatically per process or
// session surface.
type BrowserAutoOpener struct {
	mu     sync.Mutex
	opened map[string]struct{}
}

func NewBrowserAutoOpener() *BrowserAutoOpener {
	return &BrowserAutoOpener{opened: make(map[string]struct{})}
}

func (o *BrowserAutoOpener) Open(artifactID, url string) error {
	if strings.TrimSpace(artifactID) == "" || strings.TrimSpace(url) == "" || !auth.CanOpenBrowser() {
		return nil
	}
	o.mu.Lock()
	if _, exists := o.opened[artifactID]; exists {
		o.mu.Unlock()
		return nil
	}
	o.opened[artifactID] = struct{}{}
	o.mu.Unlock()
	return auth.OpenBrowser(url)
}
