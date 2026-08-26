package design

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CritiqueOptions drives one critic pass. The zero value is a valid
// deterministic pass over the current version.
type CritiqueOptions struct {
	// Version critiques a specific version; 0 means the current one. Only the
	// current version can be re-rendered, so an older version is audited from
	// what was stored for it.
	Version int
	// Render overrides the render the audit runs on.
	Render RenderOptions
	// SkipRender audits without a browser: the design-system checks still run,
	// the accessibility, runtime and layout rules cannot.
	SkipRender bool
	// Round is the 1-based iteration number. Zero means "use the version
	// number", which is what an ordinary designer/critic loop wants: one
	// committed version per round.
	Round int
	// Policy overrides the resolved critique policy for this pass.
	Policy string

	// Score, Summary and Issues carry a critic's own judgement. They are
	// optional: a pass with none of them recorded is a purely deterministic
	// audit, which is exactly what a CI check wants.
	Score   float64
	Summary string
	Issues  []Issue

	// Record stores the critique against the version. Pass false for a dry
	// look that leaves no history behind.
	Record bool
}

// CritiqueReport is one complete critic pass: the evidence, the stored
// critique, and the decision the gate reached from them.
type CritiqueReport struct {
	Artifact Artifact         `json:"artifact"`
	Version  int              `json:"version"`
	Rendered bool             `json:"rendered"`
	Audit    AuditResult      `json:"audit"`
	Critique Critique         `json:"critique"`
	Decision GateDecision     `json:"decision"`
	Settings CritiqueSettings `json:"settings"`
	// Recorded is true when the critique was written to the history.
	Recorded bool `json:"recorded"`
	// RenderError explains why the render half of the audit is missing, when
	// it is. A pass that silently drops two thirds of its rules would report a
	// high score for a page nobody looked at.
	RenderError string `json:"render_error,omitempty"`
}

// CritiqueSettingsFor resolves the bounds for one artifact: the configured
// defaults, overridden by the skill's own od.critique.policy when the artifact
// was built from a template that declares one.
func (s *Service) CritiqueSettingsFor(skillID string) CritiqueSettings {
	settings := DefaultCritiqueSettings()
	if template, ok := BundledTemplate(strings.TrimSpace(skillID)); ok {
		settings = settings.WithPolicy(template.CritiquePolicy)
	}
	return settings
}

// Critique runs a quality pass over an artifact: render it, run every
// deterministic rule, fold in whatever judgement the caller brings, score it,
// store it and decide whether the loop should go round again.
func (s *Service) Critique(ctx context.Context, artifactID string, opts CritiqueOptions) (CritiqueReport, error) {
	artifact, err := s.Get(ctx, artifactID)
	if err != nil {
		return CritiqueReport{}, err
	}
	version := opts.Version
	if version <= 0 {
		version = artifact.CurrentVersion
	}
	if version < 1 {
		version = 1
	}

	settings := s.CritiqueSettingsFor(artifact.SkillID).WithPolicy(opts.Policy)

	input := AuditInput{
		Kind:     artifact.Kind,
		Viewport: opts.Render.Viewport,
	}
	report := CritiqueReport{Artifact: artifact, Version: version, Settings: settings}

	if !opts.SkipRender {
		rendered, err := s.Render(ctx, artifact.ID, opts.Render)
		switch {
		case err == nil:
			report.Rendered = true
			input.Rendered = true
			input.Title = rendered.Title
			input.Slides = rendered.Slides
			input.Width = rendered.Width
			input.Viewport = rendered.Viewport
			input.Nodes = rendered.Nodes
			input.Facts = rendered.Facts
			input.Console = rendered.Console
			input.Failures = rendered.Failures
			if artifact.Kind == KindDeck && s.renderer != nil {
				// Print behaviour is a separate emulation pass. A deck that
				// cannot report it is not a deck that fails the rule: the
				// rule simply does not run.
				if breaks, err := s.renderer.SlideBreaks(ctx, artifact, opts.Render); err == nil {
					input.Breaks = breaks
				}
			}
		case errors.Is(err, ErrNoBrowser):
			// A machine with no Chromium still gets the design-system half of
			// the audit rather than no audit at all.
			report.RenderError = err.Error()
		default:
			return CritiqueReport{}, err
		}
	} else {
		report.RenderError = "render skipped by the caller"
	}

	linked, findings, requires, err := s.systemUsage(ctx, artifact)
	if err != nil {
		return CritiqueReport{}, err
	}
	input.SystemLinked = linked
	input.SystemFindings = findings
	input.RequiresSystem = requires

	audit := Audit(input)
	report.Audit = audit

	critique := Critique{
		ArtifactID: artifact.ID,
		Version:    version,
		Score:      BlendScore(audit.Score, opts.Score),
		Summary:    strings.TrimSpace(opts.Summary),
		Issues:     MergeIssues(audit.Issues, opts.Issues),
	}
	if critique.Summary == "" {
		critique.Summary = audit.Summary
	}
	if !report.Rendered {
		critique.Summary += " (design-system checks only: " + report.RenderError + ")"
	}

	round := opts.Round
	if round <= 0 {
		round = version
	}
	report.Decision = settings.Gate(critique, round)
	if !report.Rendered && report.Decision.Pass && settings.Enabled && settings.Policy != PolicyNone {
		// Two thirds of the rules did not run, so this is not a verdict. It
		// does not become an "iterate" either: no amount of editing the files
		// makes a missing browser render them.
		report.Decision.Pass = false
		report.Decision.Iterate = false
		report.Decision.Reason = fmt.Sprintf(
			"scored %.1f/10 on the design-system checks alone (%s), which is not enough to call it finished",
			critique.Score, report.RenderError)
	}

	if opts.Record {
		stored, err := s.store.AddCritique(ctx, critique)
		if err != nil {
			return CritiqueReport{}, err
		}
		critique = stored
		report.Recorded = true
		s.publish(EventCritique, Event{
			ArtifactID:   artifact.ID,
			Title:        artifact.Title,
			Slug:         artifact.Slug,
			ArtifactKind: artifact.Kind,
			Version:      version,
			Summary:      critique.Summary,
			Score:        critique.Score,
		})
	}
	report.Critique = critique
	return report, nil
}

// LatestCritique returns the most recent pass over a version, or ErrNotFound
// when the version has never been critiqued. Pass version 0 for the current
// version.
func (s *Service) LatestCritique(ctx context.Context, artifactID string, version int) (Critique, error) {
	if version <= 0 {
		artifact, err := s.Get(ctx, artifactID)
		if err != nil {
			return Critique{}, err
		}
		version = artifact.CurrentVersion
	}
	return s.store.LatestCritique(ctx, artifactID, version)
}

// systemUsage reports, without touching a single file, whether the entry
// document links the design system and what values it hardcodes that a token
// already covers. ApplySystem answers the same question by also linking the
// stylesheet, which a critic pass must not do: an audit that edits the thing it
// is auditing cannot be run twice.
func (s *Service) systemUsage(ctx context.Context, artifact Artifact) (linked bool, findings []SystemFinding, requires bool, err error) {
	ds, exists, err := s.LoadSystem()
	if err != nil || !exists {
		return false, nil, false, err
	}

	absDir, err := s.AbsDir(artifact)
	if err != nil {
		return false, nil, false, err
	}
	entry := "index.html"
	if manifest, err := ReadManifest(absDir); err == nil {
		entry = manifest.Entry
	} else if !os.IsNotExist(err) {
		return false, nil, false, err
	}
	href, err := stylesheetHref(s.layout, artifact.Dir)
	if err != nil {
		return false, nil, false, err
	}
	raw, err := os.ReadFile(filepath.Join(absDir, filepath.FromSlash(entry)))
	if err != nil {
		return false, nil, false, fmt.Errorf("design: read entry document %s: %w", entry, err)
	}
	linked = strings.Contains(string(raw), href)

	findings, _, _, err = auditArtifact(absDir, ds)
	if err != nil {
		return false, nil, false, err
	}

	// A committed design system is the project saying it wants one, so every
	// artifact is expected to link it. A template that declares otherwise
	// (od.design_system.requires: false) is the one exception.
	requires = true
	if template, ok := BundledTemplate(artifact.SkillID); ok {
		requires = template.RequiresSystem
	}
	return linked, findings, requires, nil
}
