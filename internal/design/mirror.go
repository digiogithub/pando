package design

import (
	"context"
	"fmt"
	"strings"
)

// A design system is knowledge, not just configuration: the brand it describes
// outlives the project directory it was extracted in. Mirroring it into the
// knowledge base makes it searchable and wiki-linkable, so a later project can
// find "the system we built for X" instead of re-deriving it from the same
// website. The mirror is best-effort by design — a knowledge base that is not
// wired, or that is momentarily unavailable, must never fail a token edit.

// SystemMirror is the slice of the knowledge base the design system needs. It
// is an interface so internal/design does not depend on the RAG stack, which
// carries an embedding provider and a database of its own.
type SystemMirror interface {
	AddDocument(ctx context.Context, filePath, content string, metadata map[string]interface{}) error
}

// MirrorPath is where a design system is mirrored in the knowledge base.
func MirrorPath(name string) string {
	slug := Slugify(name)
	if slug == "" {
		slug = "default"
	}
	return "pando/design-systems/" + slug + ".md"
}

// MirrorSystem writes the design system to the knowledge base. It returns the
// document path so a caller can report where it went, and an empty path when no
// mirror is wired.
func (s *Service) MirrorSystem(ctx context.Context, ds DesignSystem, source ExtractSource, target string) (string, error) {
	if s.mirror == nil {
		return "", nil
	}
	path := MirrorPath(ds.Name)
	metadata := map[string]interface{}{
		"tags": []string{"design", "design-system"},
	}
	if source != "" {
		metadata["source"] = string(source)
	}
	if target != "" {
		metadata["origin"] = target
	}
	if err := s.mirror.AddDocument(ctx, path, ds.mirrorDocument(source, target), metadata); err != nil {
		return "", fmt.Errorf("design: mirror design system to the knowledge base: %w", err)
	}
	return path, nil
}

// mirrorDocument renders the system as a knowledge-base document. It repeats
// the tokens rather than linking the file because the knowledge base is read
// from other projects, where that file does not exist.
func (ds DesignSystem) mirrorDocument(source ExtractSource, target string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Design system — %s\n\n", ds.Name)
	if source != "" {
		fmt.Fprintf(&b, "Extracted from %s", source)
		if target != "" {
			fmt.Fprintf(&b, " `%s`", target)
		}
		b.WriteString(".\n\n")
	}
	b.WriteString("Reusable across projects: copy the tokens into `")
	b.WriteString(SystemTokensFile)
	b.WriteString("` and Pando regenerates the stylesheet.\n\n")
	b.WriteString(ds.TokenSection())
	b.WriteString("\n\n## Stylesheet\n\n```css\n")
	b.WriteString(ds.CSS())
	b.WriteString("```\n")
	return b.String()
}
