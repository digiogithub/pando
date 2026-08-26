package design

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Export formats supported in v1. Anything richer (Figma, video, sprite sheets)
// is deliberately out of scope.
const (
	ExportHTML = "html"
	ExportPNG  = "png"
	ExportPDF  = "pdf"
)

// ExportOptions configures one export.
type ExportOptions struct {
	// Format is html, png or pdf.
	Format string
	// Dest is the output file, absolute or relative to the working directory.
	// Empty writes next to the artifact under exports/.
	Dest string
	// Slide exports a single deck slide (PNG only); -1 for the whole document.
	Slide int
	// FullPage captures beyond the viewport for PNG exports.
	FullPage bool
	// Viewport overrides the manifest viewport.
	Viewport Viewport
	// Landscape applies to PDF exports without their own @page size.
	Landscape bool
}

// ExportResult reports where an export landed.
type ExportResult struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Note   string `json:"note,omitempty"`
}

// Export writes the artifact out in one of the supported formats. HTML exports
// are self-contained single files: local stylesheets, scripts and images are
// inlined so the result can be mailed or committed on its own.
func (s *Service) Export(ctx context.Context, artifactID string, opts ExportOptions) (ExportResult, error) {
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return ExportResult{}, err
	}
	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		return ExportResult{}, err
	}
	manifest, err := ReadManifest(absDir)
	if err != nil {
		return ExportResult{}, err
	}

	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = ExportHTML
	}
	dest, err := s.exportDest(absDir, artifact, format, opts)
	if err != nil {
		return ExportResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return ExportResult{}, fmt.Errorf("design: create export dir: %w", err)
	}

	switch format {
	case ExportHTML:
		data, note, err := inlineDocument(absDir, manifest.Entry)
		if err != nil {
			return ExportResult{}, err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return ExportResult{}, fmt.Errorf("design: write export: %w", err)
		}
		return ExportResult{Format: format, Path: dest, Bytes: len(data), Note: note}, nil

	case ExportPNG:
		renderer, err := s.requireRenderer()
		if err != nil {
			return ExportResult{}, err
		}
		shot, err := renderer.Screenshot(ctx, artifact, ScreenshotOptions{
			RenderOptions: RenderOptions{Viewport: opts.Viewport},
			Slide:         opts.Slide,
			FullPage:      opts.FullPage,
		})
		if err != nil {
			return ExportResult{}, err
		}
		if err := os.WriteFile(dest, shot, 0o644); err != nil {
			return ExportResult{}, fmt.Errorf("design: write export: %w", err)
		}
		return ExportResult{Format: format, Path: dest, Bytes: len(shot)}, nil

	case ExportPDF:
		renderer, err := s.requireRenderer()
		if err != nil {
			return ExportResult{}, err
		}
		pdf, err := renderer.PrintPDF(ctx, artifact, PrintOptions{
			RenderOptions: RenderOptions{Viewport: opts.Viewport},
			Landscape:     opts.Landscape,
		})
		if err != nil {
			return ExportResult{}, err
		}
		if err := os.WriteFile(dest, pdf, 0o644); err != nil {
			return ExportResult{}, fmt.Errorf("design: write export: %w", err)
		}
		note := ""
		if artifact.Kind == KindDeck {
			breaks, berr := renderer.SlideBreaks(ctx, artifact, RenderOptions{})
			if berr == nil && !hasPageBreaks(breaks) {
				note = "no slide carries break-after/page-break-after, so the PDF will not be one page per slide; add @page and break-after: page to the print stylesheet"
			}
		}
		return ExportResult{Format: format, Path: dest, Bytes: len(pdf), Note: note}, nil

	default:
		return ExportResult{}, fmt.Errorf("design: unsupported export format %q (html, png, pdf)", format)
	}
}

func hasPageBreaks(breaks []SlideBreak) bool {
	for _, b := range breaks {
		if b.BreakAfter == "page" || b.BreakAfter == "always" {
			return true
		}
	}
	return false
}

// requireRenderer returns the attached renderer, with an actionable error when
// no browser is available.
func (s *Service) requireRenderer() (*Renderer, error) {
	if s.renderer == nil {
		return nil, ErrNoBrowser
	}
	if !s.renderer.Available() {
		return nil, ErrNoBrowser
	}
	return s.renderer, nil
}

// exportDest resolves the output path, defaulting to exports/<slug>.<ext>
// inside the artifact directory.
func (s *Service) exportDest(absDir string, a Artifact, format string, opts ExportOptions) (string, error) {
	if opts.Dest != "" {
		dest := opts.Dest
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(s.layout.WorkingDir, filepath.FromSlash(dest))
		}
		return filepath.Clean(dest), nil
	}
	name := a.Slug
	if opts.Slide > 0 {
		name = fmt.Sprintf("%s-slide-%d", a.Slug, opts.Slide)
	}
	return filepath.Join(absDir, "exports", name+"."+format), nil
}

var (
	linkStylesheetRe = regexp.MustCompile(`(?is)<link\b[^>]*?rel\s*=\s*["']?stylesheet["']?[^>]*?>`)
	scriptSrcRe      = regexp.MustCompile(`(?is)<script\b[^>]*?\bsrc\s*=\s*["']([^"']+)["'][^>]*?>\s*</script\s*>`)
	imgSrcRe         = regexp.MustCompile(`(?is)<img\b[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	hrefRe           = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']+)["']`)
)

// inlineDocument produces a single self-contained HTML file. Remote references
// are left alone: rewriting them would change what the design depends on.
func inlineDocument(absDir, entry string) ([]byte, string, error) {
	entryPath, err := safeArtifactPath(absDir, entry)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, "", fmt.Errorf("design: read entry %s: %w", entry, err)
	}
	doc := string(raw)
	var skipped []string

	doc = linkStylesheetRe.ReplaceAllStringFunc(doc, func(tag string) string {
		m := hrefRe.FindStringSubmatch(tag)
		if len(m) < 2 {
			return tag
		}
		css, err := readLocalAsset(absDir, m[1])
		if err != nil {
			skipped = append(skipped, m[1])
			return tag
		}
		return "<style>\n" + string(css) + "\n</style>"
	})

	doc = scriptSrcRe.ReplaceAllStringFunc(doc, func(tag string) string {
		m := scriptSrcRe.FindStringSubmatch(tag)
		if len(m) < 2 {
			return tag
		}
		js, err := readLocalAsset(absDir, m[1])
		if err != nil {
			skipped = append(skipped, m[1])
			return tag
		}
		return "<script>\n" + string(js) + "\n</script>"
	})

	doc = imgSrcRe.ReplaceAllStringFunc(doc, func(tag string) string {
		m := imgSrcRe.FindStringSubmatch(tag)
		if len(m) < 2 {
			return tag
		}
		data, err := readLocalAsset(absDir, m[1])
		if err != nil {
			skipped = append(skipped, m[1])
			return tag
		}
		ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(m[1])))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		uri := "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
		return strings.Replace(tag, m[1], uri, 1)
	})

	note := ""
	if len(skipped) > 0 {
		note = "left as external references: " + strings.Join(dedupe(skipped), ", ")
	}
	return []byte(doc), note, nil
}

// readLocalAsset reads an artifact-relative asset, refusing absolute URLs and
// anything outside the artifact directory.
func readLocalAsset(absDir, ref string) ([]byte, error) {
	if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "//") {
		return nil, fmt.Errorf("design: not a local asset: %q", ref)
	}
	if i := strings.Index(ref, "://"); i > 0 {
		return nil, fmt.Errorf("design: remote asset: %q", ref)
	}
	clean := ref
	if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}
	if strings.HasPrefix(clean, "/") {
		return nil, fmt.Errorf("design: root-relative asset: %q", ref)
	}
	path, err := safeArtifactPath(absDir, clean)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
