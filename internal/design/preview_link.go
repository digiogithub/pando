package design

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/design/preview"
	"github.com/digiogithub/pando/internal/logging"
)

// previewServer is the process-wide preview server. The API server installs its
// own mounted instance at start-up; processes without one (plain TUI, ACP, CLI)
// get a loopback server created on first use by EnsurePreviewServer.
var previewServer atomic.Pointer[preview.Server]

// loopbackOnce guards the lazy loopback fallback so two concurrent presents do
// not race two listeners into existence.
var loopbackOnce sync.Mutex

// SetPreviewServer installs the process-wide preview server. The API server
// calls it with an instance mounted on its own listener, so previews live on
// the Pando origin and inherit its bind address and its authentication.
func SetPreviewServer(s *preview.Server) {
	previewServer.Store(s)
	logging.Debug("design: preview server installed")
}

// PreviewServer returns the installed preview server, or nil.
func PreviewServer() *preview.Server { return previewServer.Load() }

// EnsurePreviewServer returns the installed preview server, starting a loopback
// one if there is none. Surfaces that need a URL call it; nothing starts a
// listener merely because the design package was imported.
func EnsurePreviewServer() (*preview.Server, error) {
	if s := previewServer.Load(); s != nil {
		return s, nil
	}
	loopbackOnce.Lock()
	defer loopbackOnce.Unlock()
	if s := previewServer.Load(); s != nil {
		return s, nil
	}
	server := preview.New(PreviewOptions(nil, nil))
	if err := server.StartLoopback(); err != nil {
		return nil, err
	}
	previewServer.Store(server)
	logging.Debug("design: loopback preview server started", "addr", server.Addr())
	return server, nil
}

// ClosePreviewServer stops and forgets the process-wide preview server.
func ClosePreviewServer() {
	if s := previewServer.Swap(nil); s != nil {
		s.Close()
	}
}

// PreviewOptions builds the options every preview server in this process
// shares. baseURL and access may be nil for the loopback fallback, which serves
// its own origin and is unreachable from the network by construction.
func PreviewOptions(baseURL func() string, access func() error) preview.Options {
	return preview.Options{
		BaseURL: baseURL,
		Access:  access,
		Inject:  previewStampScript(),
	}
}

// PublishPreview registers an artifact with the preview server and returns the
// grant. It is idempotent: re-publishing an artifact keeps its token, so a
// preview already open in a browser survives every iteration.
func (s *Service) PublishPreview(ctx context.Context, artifactID string) (preview.Grant, error) {
	server, err := EnsurePreviewServer()
	if err != nil {
		return preview.Grant{}, err
	}
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return preview.Grant{}, err
	}
	return s.publishPreviewOn(server, artifact)
}

// publishPreviewOn registers an artifact with an explicit server. It is the
// half of PublishPreview that never starts a listener.
func (s *Service) publishPreviewOn(server *preview.Server, artifact Artifact) (preview.Grant, error) {
	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		return preview.Grant{}, err
	}
	entry := "index.html"
	if manifest, err := ReadManifest(absDir); err == nil && manifest.Entry != "" {
		entry = manifest.Entry
	}
	return server.Publish(artifact.ID, artifact.SessionID, absDir, filepath.ToSlash(entry))
}

// previewStampScript is injected into every ?bridge=1 preview document. It runs
// the *same* walker the renderer uses, so the data-pando-id a user clicks in
// the browser is the id the stored index knows — the selection protocol would
// be meaningless if the two numbered elements differently.
//
// It additionally stamps data-pando-slide (1-based) on deck slides, which the
// bridge uses for navigation and for resolving a #slide-N deep link. The
// renderer has no need for that attribute: it reads the slide list directly.
func previewStampScript() []byte {
	opts := normalizeRenderOptions(RenderOptions{}, Artifact{Kind: KindDeck})
	script, err := buildIndexScript(opts, DefaultSlideSelector)
	if err != nil {
		// The payload is a fixed map of primitives; a marshalling failure here
		// is impossible in practice and must not take the preview down.
		logging.Warn("design: preview stamp script unavailable", "error", err)
		return nil
	}
	// The walker measures boxes, so it must not run before the document has
	// parsed. Registering here — from an inline tag emitted ahead of the
	// deferred bridge — guarantees the stamping listener runs before the
	// bridge's own, which is what lets the bridge see the ids on first paint.
	return []byte(fmt.Sprintf(`(function(){function stamp(){try{%s;
var slides=document.querySelectorAll(%q);
for(var i=0;i<slides.length;i++){slides[i].setAttribute('data-pando-slide',String(i+1));}
}catch(e){}}
if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',stamp);}else{stamp();}})();`, script, DefaultSlideSelector))
}
