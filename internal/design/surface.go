package design

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// This file holds what the non-HTTP surfaces (CLI, TUI, ACP) need and the HTTP
// API does not. A browser always has an artifact id in hand because it listed
// them a moment ago; a person typing a command has a slug, a prefix, or part of
// a title, and every surface would otherwise reinvent the same lookup.

// ErrAmbiguousRef is returned when a human-typed reference matches more than
// one artifact. It carries the candidates so a surface can show them.
type ErrAmbiguousRef struct {
	Ref       string
	Artifacts []Artifact
}

func (e *ErrAmbiguousRef) Error() string {
	names := make([]string, 0, len(e.Artifacts))
	for _, a := range e.Artifacts {
		names = append(names, fmt.Sprintf("%s (%s)", a.Slug, a.ID))
	}
	return fmt.Sprintf("design: %q matches %d artifacts: %s", e.Ref, len(e.Artifacts), strings.Join(names, ", "))
}

// Resolve turns a human-typed reference into an artifact. It accepts, in
// order of precedence, an exact id, an exact slug, an id prefix, and finally a
// case-insensitive substring of the slug or title. An empty reference selects
// the most recently updated artifact, which is what "the one I am working on"
// means at a prompt.
//
// Precedence is ordered rather than scored on purpose: an exact id must never
// lose to a substring match on some other artifact's title.
func (s *Service) Resolve(ctx context.Context, ref string) (Artifact, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if a, err := s.store.GetArtifact(ctx, ref); err == nil {
			return a, nil
		}
	}

	all, err := s.List(ctx, false)
	if err != nil {
		return Artifact{}, err
	}
	if len(all) == 0 {
		return Artifact{}, fmt.Errorf("%w: this project has no design artifacts yet", ErrNotFound)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })

	if ref == "" {
		return all[0], nil
	}

	lower := strings.ToLower(ref)
	for _, match := range []func(Artifact) bool{
		func(a Artifact) bool { return a.Slug == ref },
		func(a Artifact) bool { return strings.HasPrefix(a.ID, ref) },
		func(a Artifact) bool {
			return strings.Contains(strings.ToLower(a.Slug), lower) ||
				strings.Contains(strings.ToLower(a.Title), lower)
		},
	} {
		var hits []Artifact
		for _, a := range all {
			if match(a) {
				hits = append(hits, a)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		default:
			return Artifact{}, &ErrAmbiguousRef{Ref: ref, Artifacts: hits}
		}
	}
	return Artifact{}, fmt.Errorf("%w: no design artifact matches %q", ErrNotFound, ref)
}

// LiveURL resolves how to show an artifact, starting a loopback preview server
// first when this process has none.
//
// It is the call every "open it" path goes through — the CLI, the TUI `o` key,
// the ACP resource link and the design_present tool — so that they all agree on
// when a listener is allowed to come into existence: on an explicit request to
// show something, never as a side effect of rendering or listing.
//
// A preview server that cannot start is not fatal: the returned Presentation
// still carries the file:// address, which is enough for a local browser.
func (s *Service) LiveURL(ctx context.Context, artifactID string, slide int) (Presentation, error) {
	if _, err := EnsurePreviewServer(); err != nil {
		// Fall through: Presentation degrades to file:// on its own.
		p, perr := s.Presentation(ctx, artifactID, slide, "")
		if perr != nil {
			return Presentation{}, perr
		}
		return p, nil
	}
	return s.Presentation(ctx, artifactID, slide, "")
}
