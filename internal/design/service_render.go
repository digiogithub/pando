package design

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/design/preview"
)

// WithRenderer attaches a renderer to the service. It is optional: everything
// that does not need a browser (create, versions, checkout, diff) works without
// one, so a machine with no Chromium keeps a usable Design Studio.
func (s *Service) WithRenderer(r *Renderer) *Service {
	s.renderer = r
	return s
}

// Renderer returns the attached renderer, or nil.
func (s *Service) Renderer() *Renderer { return s.renderer }

// Render renders an artifact and stores the resulting structure index against
// its current version, so a later Inspect (or a UI selection) resolves without
// re-rendering.
func (s *Service) Render(ctx context.Context, artifactID string, opts RenderOptions) (RenderResult, error) {
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return RenderResult{}, err
	}
	if s.renderer == nil {
		return RenderResult{}, fmt.Errorf("%w: no renderer attached", ErrNoBrowser)
	}

	result, err := s.renderer.Render(ctx, artifact, opts)
	if err != nil {
		return RenderResult{}, err
	}

	version := artifact.CurrentVersion
	if version < 1 {
		version = 1
	}
	if err := s.store.ReplaceNodes(ctx, artifact.ID, version, result.Nodes); err != nil {
		return RenderResult{}, err
	}

	// A deck's slide count is a render product; keep the committed manifest in
	// step so the UI and the exporter agree on how many slides there are.
	if artifact.Kind == KindDeck {
		if err := s.syncManifestSlides(artifact, result.Slides); err != nil {
			return RenderResult{}, err
		}
	}

	// A render is what makes an artifact viewable, so it is also where the
	// preview grant is refreshed: any surface listening on the event stream
	// gets a URL that is live right now.
	// Only an already-running preview server is refreshed here. Rendering must
	// not be the thing that opens a listener: a render happens in tests, in
	// batch exports and in headless critic passes, none of which want a socket.
	url := ""
	if server := PreviewServer(); server != nil {
		if grant, err := s.publishPreviewOn(server, artifact); err == nil {
			if u, err := server.URL(grant.ArtifactID, preview.URLOptions{}); err == nil {
				url = u
			}
		}
	}
	s.publish(EventRender, Event{
		ArtifactID:   artifact.ID,
		Title:        artifact.Title,
		Slug:         artifact.Slug,
		ArtifactKind: artifact.Kind,
		Version:      version,
		Nodes:        len(result.Nodes),
		Slides:       result.Slides,
		URL:          url,
	})
	return result, nil
}

// Inspect returns a filtered, paged view of a version's stored index. Pass
// version 0 for the artifact's current version.
func (s *Service) Inspect(ctx context.Context, artifactID string, version int, opts InspectOptions) (InspectResult, error) {
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return InspectResult{}, err
	}
	if version <= 0 {
		version = artifact.CurrentVersion
	}

	nodes, err := s.store.ListNodes(ctx, artifactID, version, -1)
	if err != nil {
		return InspectResult{}, err
	}
	if len(nodes) == 0 {
		// A distinct sentinel, not ErrNotFound: the artifact exists, it simply
		// has not been rendered. An agent must be told to render; a UI panel
		// wants to show "no index yet" rather than an error, and the two can
		// only tell those cases apart if they are different errors.
		return InspectResult{}, fmt.Errorf("%w: %s v%d has no index yet — render it first",
			ErrNoIndex, artifactID, version)
	}

	result := Inspect(nodes, opts)
	result.ArtifactID = artifactID
	result.Version = version
	return result, nil
}

// Node resolves one indexed node, which is what a design://<node_id> selection
// from the UI turns into. Pass version 0 for the current version.
func (s *Service) Node(ctx context.Context, artifactID string, version int, nodeID string) (Node, error) {
	if version <= 0 {
		artifact, err := s.store.GetArtifact(ctx, artifactID)
		if err != nil {
			return Node{}, err
		}
		version = artifact.CurrentVersion
	}
	return s.store.GetNode(ctx, artifactID, version, nodeID)
}

// syncManifestSlides records the observed slide count in pando-design.json.
func (s *Service) syncManifestSlides(a Artifact, slides int) error {
	absDir, err := s.layout.AbsDir(a.Dir)
	if err != nil {
		return err
	}
	manifest, err := ReadManifest(absDir)
	if err != nil {
		// A missing or unreadable manifest is not worth failing a render over.
		return nil //nolint:nilerr // the artifact directory belongs to the user
	}
	if manifest.Deck != nil && manifest.Deck.Slides == slides {
		return nil
	}
	if manifest.Deck == nil {
		manifest.Deck = &DeckSpec{Navigation: "horizontal"}
	}
	manifest.Deck.Slides = slides
	return WriteManifest(absDir, manifest)
}
