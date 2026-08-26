package design

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Applying a design system to an artifact is two separate jobs, and they are
// kept separate on purpose. Linking the stylesheet is mechanical and safe, so
// it is done. Replacing hardcoded values with tokens is a design judgement —
// the same hex can be the brand colour in one place and a deliberate one-off in
// another — so it is reported and left to whoever is designing.

// SystemFinding is one hardcoded value that a token already covers.
type SystemFinding struct {
	// File is the artifact-relative file the value was found in.
	File string `json:"file"`
	// Line is the 1-indexed line.
	Line int `json:"line"`
	// Property is the CSS property, when the value came from a declaration.
	Property string `json:"property,omitempty"`
	// Value is the literal as written.
	Value string `json:"value"`
	// Token is the custom property that should replace it.
	Token string `json:"token"`
}

// ApplyResult reports what applying the system did and what it found.
type ApplyResult struct {
	ArtifactID string `json:"artifact_id"`
	System     string `json:"system"`
	// Stylesheet is the artifact-relative href of the linked stylesheet.
	Stylesheet string `json:"stylesheet"`
	// Linked is true when this call added the link, false when it was already
	// there.
	Linked bool `json:"linked"`
	// Entry is the artifact-relative entry document.
	Entry string `json:"entry"`
	// Findings are hardcoded values a token already covers.
	Findings []SystemFinding `json:"findings,omitempty"`
	// Scanned counts the files audited.
	Scanned int `json:"scanned"`
	// Truncated is true when the finding list was cut short.
	Truncated bool `json:"truncated,omitempty"`
}

// maxApplyFindings bounds the audit. The list is read by a model, and a
// thousand findings is a wall of text that gets skipped rather than acted on.
const maxApplyFindings = 60

// ApplySystem links the design-system stylesheet into an artifact's entry
// document and audits the artifact for values a token already covers.
func (s *Service) ApplySystem(ctx context.Context, artifactID string) (ApplyResult, error) {
	artifact, err := s.Get(ctx, artifactID)
	if err != nil {
		return ApplyResult{}, err
	}
	absDir, err := s.AbsDir(artifact)
	if err != nil {
		return ApplyResult{}, err
	}
	ds, exists, err := s.LoadSystem()
	if err != nil {
		return ApplyResult{}, err
	}
	if !exists {
		return ApplyResult{}, fmt.Errorf("design: no design system committed yet; run design_system init or extract one first")
	}

	entry := "index.html"
	if manifest, err := ReadManifest(absDir); err == nil {
		entry = manifest.Entry
	} else if !os.IsNotExist(err) {
		return ApplyResult{}, err
	}
	href, err := stylesheetHref(s.layout, artifact.Dir)
	if err != nil {
		return ApplyResult{}, err
	}

	linked, err := ensureStylesheetLink(filepath.Join(absDir, filepath.FromSlash(entry)), href)
	if err != nil {
		return ApplyResult{}, err
	}

	findings, scanned, truncated, err := auditArtifact(absDir, ds)
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		ArtifactID: artifact.ID,
		System:     ds.Name,
		Stylesheet: href,
		Linked:     linked,
		Entry:      entry,
		Findings:   findings,
		Scanned:    scanned,
		Truncated:  truncated,
	}, nil
}

// stylesheetHref returns the href an artifact should use to reach the shared
// stylesheet. It is relative because an artifact directory is committed and
// opened directly from disk as often as it is served.
func stylesheetHref(layout Layout, artifactDir string) (string, error) {
	from := filepath.Join(layout.WorkingDir, filepath.FromSlash(artifactDir))
	to := filepath.Join(layout.SystemPath(), SystemStylesheet)
	rel, err := filepath.Rel(from, to)
	if err != nil {
		return "", fmt.Errorf("design: locate %s from %s: %w", SystemStylesheet, artifactDir, err)
	}
	return filepath.ToSlash(rel), nil
}

var headClosePattern = regexp.MustCompile(`(?i)</head\s*>`)

// ensureStylesheetLink inserts the stylesheet link into a document's head,
// unless a link to the same file is already there. A document with no head is
// an error rather than a silent no-op: the caller asked for the system to be
// applied, and quietly not applying it is the worst outcome.
func ensureStylesheetLink(entryPath, href string) (bool, error) {
	raw, err := os.ReadFile(entryPath)
	if err != nil {
		return false, fmt.Errorf("design: read entry document %s: %w", entryPath, err)
	}
	body := string(raw)
	if strings.Contains(body, href) {
		return false, nil
	}
	link := fmt.Sprintf("<link rel=\"stylesheet\" href=\"%s\">", escapeAttr(href))
	loc := headClosePattern.FindStringIndex(body)
	if loc == nil {
		return false, fmt.Errorf("design: %s has no </head> to link the design system from", filepath.Base(entryPath))
	}
	indent := "  "
	updated := body[:loc[0]] + indent + link + "\n" + body[loc[0]:]
	if err := os.WriteFile(entryPath, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("design: write %s: %w", entryPath, err)
	}
	return true, nil
}

// auditableExtensions are the artifact files whose literals are worth checking.
var auditableExtensions = map[string]bool{
	".html": true, ".css": true, ".js": true, ".svg": true,
}

// auditArtifact walks an artifact and reports every literal that a token
// already covers. The generated stylesheet is skipped: it is where the tokens
// are defined, so every value in it would match itself.
func auditArtifact(absDir string, ds DesignSystem) ([]SystemFinding, int, bool, error) {
	index := tokenValueIndex(ds)
	var findings []SystemFinding
	scanned := 0
	truncated := false

	err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != absDir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !auditableExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxExtractFileBytes {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		scanned++
		rel, relErr := filepath.Rel(absDir, path)
		if relErr != nil {
			rel = path
		}
		for _, f := range auditFile(filepath.ToSlash(rel), string(raw), index) {
			if len(findings) >= maxApplyFindings {
				truncated = true
				return fs.SkipAll
			}
			findings = append(findings, f)
		}
		return nil
	})
	if err != nil {
		return nil, 0, false, fmt.Errorf("design: audit %s: %w", absDir, err)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, scanned, truncated, nil
}

// tokenValueIndex maps a normalised token value to its custom-property name, so
// a literal can be looked up in one step.
func tokenValueIndex(ds DesignSystem) map[string]string {
	index := map[string]string{}
	for group, values := range ds.Tokens {
		for name, value := range values {
			key := normalizeTokenValue(group, value)
			if key == "" {
				continue
			}
			// First writer wins so the index is stable: with two tokens holding
			// the same value, the alphabetically first name is reported.
			if existing, ok := index[key]; !ok || fmt.Sprintf("--%s-%s", group, name) < existing {
				index[key] = fmt.Sprintf("--%s-%s", group, name)
			}
		}
	}
	return index
}

// normalizeTokenValue puts a value into the form literals are compared in:
// colours as #rrggbb, everything else lowercased and unquoted.
func normalizeTokenValue(group, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if group == "color" {
		if c, ok := parseColor(value); ok {
			return c.hex()
		}
		return ""
	}
	return strings.ToLower(normalizeFontStack(value))
}

// auditFile reports the literals in one file that a token already covers.
func auditFile(rel, body string, index map[string]string) []SystemFinding {
	var out []SystemFinding
	for i, line := range strings.Split(body, "\n") {
		// A line that already uses a token is doing the right thing, and its
		// fallback value would otherwise be reported as a violation of itself.
		if strings.Contains(line, "var(--") {
			continue
		}
		for _, m := range declPattern.FindAllStringSubmatch(line, -1) {
			property, value := strings.ToLower(m[1]), strings.TrimSpace(m[2])
			if property == "font-family" {
				if token, ok := index[strings.ToLower(normalizeFontStack(value))]; ok {
					out = append(out, SystemFinding{File: rel, Line: i + 1, Property: property, Value: value, Token: token})
				}
				continue
			}
			for _, literal := range colorLiteral.FindAllString(value, -1) {
				c, ok := parseColor(literal)
				if !ok {
					continue
				}
				if token, ok := index[c.hex()]; ok {
					out = append(out, SystemFinding{File: rel, Line: i + 1, Property: property, Value: literal, Token: token})
				}
			}
		}
	}
	return out
}
