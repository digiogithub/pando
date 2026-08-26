package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/permission"
)

// Design Studio tool names.
const (
	DesignCreateToolName     = "design_create"
	DesignPatchToolName      = "design_patch"
	DesignRenderToolName     = "design_render"
	DesignScreenshotToolName = "design_screenshot"
	DesignInspectToolName    = "design_inspect"
	DesignVersionsToolName   = "design_versions"
	DesignExportToolName     = "design_export"
	DesignCanvasToolName     = "design_canvas"
	DesignSystemToolName     = "design_system"
	DesignPresentToolName    = "design_present"
	// DesignCritiqueToolName is declared in design_critique.go, and
	// DesignSkillsToolName in design_skills.go.
)

// DesignToolNames lists every Design Studio tool, in the order surfaces should
// present them.
var DesignToolNames = []string{
	DesignCreateToolName,
	DesignRenderToolName,
	DesignInspectToolName,
	DesignPatchToolName,
	DesignScreenshotToolName,
	DesignVersionsToolName,
	DesignExportToolName,
	DesignCanvasToolName,
	DesignSystemToolName,
	DesignCritiqueToolName,
	DesignSkillsToolName,
	DesignPresentToolName,
}

// designTool carries what every Design Studio tool needs. The design service
// itself is resolved per call from the process-wide provider, so a tool built
// before the subsystem is wired still works once it is.
type designTool struct {
	permissions permission.Service
}

// service resolves the session-bound design service for this call.
func (t *designTool) service(ctx context.Context) (*design.Service, error) {
	sessionID, _ := GetContextValues(ctx)
	return design.ServiceFor(sessionID)
}

// requireWrite asks the user to authorise a mutation of the artifact directory.
// Design artifacts live in the user's working tree, so they are gated exactly
// like any other file write.
func (t *designTool) requireWrite(ctx context.Context, toolName, action, path, description string, params any) error {
	if t.permissions == nil {
		return nil
	}
	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return fmt.Errorf("session ID and message ID are required for %s", toolName)
	}
	granted := t.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        path,
		ToolName:    toolName,
		Action:      action,
		Description: description,
		Params:      params,
	})
	if !granted {
		return permission.ErrorPermissionDenied
	}
	return nil
}

// designError turns the recurring design failures into guidance the model can
// act on instead of a bare error string.
func designError(err error) ToolResponse {
	switch {
	case errors.Is(err, design.ErrNoProvider):
		return NewTextErrorResponse("the Design Studio is not available in this process (no database is attached)")
	case errors.Is(err, design.ErrNoBrowser):
		return NewTextErrorResponse("no Chromium-based browser was found; install Chrome, Chromium or Edge, or set internalTools.browserExecutable, then retry")
	case errors.Is(err, design.ErrNoIndex):
		return NewTextErrorResponse(err.Error())
	case errors.Is(err, design.ErrNotFound):
		return NewTextErrorResponse("not found: " + err.Error())
	default:
		return NewTextErrorResponse(err.Error())
	}
}

// --- design_create ---

type DesignCreateParams struct {
	Title        string            `json:"title"`
	Kind         string            `json:"kind,omitempty"`
	Slug         string            `json:"slug,omitempty"`
	Skill        string            `json:"skill,omitempty"`
	DesignSystem string            `json:"design_system,omitempty"`
	Entry        string            `json:"entry,omitempty"`
	Files        map[string]string `json:"files,omitempty"`
}

type designCreateTool struct{ designTool }

// NewDesignCreateTool returns the artifact-creation tool.
func NewDesignCreateTool(permissions permission.Service) BaseTool {
	return &designCreateTool{designTool{permissions: permissions}}
}

func (t *designCreateTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignCreateToolName,
		Description: `Create a design artifact: a directory in the working tree holding an entry HTML document, its assets and a pando-design.json manifest.

WHEN TO USE THIS TOOL:
- The user asks for a landing page, a prototype, a slide deck or any visual artifact you should be able to render, look at and iterate on.

HOW TO USE:
- Give a title. kind is "web" (a page or prototype) or "deck" (slides exported one per PDF page).
- Prefer starting from a design template: pass its name as "skill" and the artifact is scaffolded from it, with the kind it declares. Run design_skills to see them, and design_skills action "show" to read the one you picked BEFORE building.
- Optionally seed the artifact with files: a map of artifact-relative paths to content. Omit it to get the template scaffold, or a minimal one when no template is named.

AFTER CREATING:
- Run design_render to render it and build the node index, then design_inspect to read the resulting layout, and design_patch or edit to iterate.

NOTES:
- The files belong to the user's repository: they are committed, diffed and edited like any other source. Version 1 is recorded immediately.`,
		Parameters: map[string]any{
			"title":         map[string]any{"type": "string", "description": "Human-readable title of the artifact"},
			"kind":          map[string]any{"type": "string", "enum": []string{"web", "deck"}, "description": "Artifact kind (default \"web\")"},
			"slug":          map[string]any{"type": "string", "description": "Directory name; derived from the title when omitted"},
			"skill":         map[string]any{"type": "string", "description": "Design template this artifact follows; see design_skills (optional)"},
			"design_system": map[string]any{"type": "string", "description": "Identifier of the design system to follow (optional)"},
			"entry":         map[string]any{"type": "string", "description": "Entry document name (default \"index.html\")"},
			"files": map[string]any{
				"type":                 "object",
				"description":          "Seed files: artifact-relative path to file content",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		Required: []string{"title"},
	}
}

func (t *designCreateTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignCreateParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Title) == "" {
		return NewTextErrorResponse("title is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	kind := design.Kind(strings.ToLower(strings.TrimSpace(params.Kind)))
	template, hasTemplate := design.BundledTemplate(strings.TrimSpace(params.Skill))
	if kind == "" && hasTemplate {
		// The template declares the surface it builds. Defaulting to web here
		// would turn a deck template into a page and drop its print styles.
		kind = template.Kind
	}
	if kind == "" {
		kind = design.KindWeb
	}
	if !design.ValidKind(kind) {
		return NewTextErrorResponse(fmt.Sprintf("unsupported kind %q (web, deck)", params.Kind)), nil
	}

	slug := design.Slugify(params.Slug)
	if slug == "" {
		slug = design.Slugify(params.Title)
	}
	relDir := svc.Layout().RelDir(slug)
	if err := t.requireWrite(ctx, DesignCreateToolName, "create", relDir,
		fmt.Sprintf("Create design artifact %s in %s", params.Title, relDir),
		map[string]any{"dir": relDir, "kind": string(kind), "files": len(params.Files)}); err != nil {
		return ToolResponse{}, err
	}

	artifact, err := svc.Create(ctx, design.CreateParams{
		Title:        params.Title,
		Kind:         kind,
		Slug:         params.Slug,
		SkillID:      params.Skill,
		DesignSystem: params.DesignSystem,
		Entry:        params.Entry,
		Files:        params.Files,
	})
	if err != nil {
		return designError(err), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Created %s artifact %q\n", artifact.Kind, artifact.Title)
	fmt.Fprintf(&b, "id: %s\ndir: %s\nversion: %d\n", artifact.ID, artifact.Dir, artifact.CurrentVersion)
	if hasTemplate && len(params.Files) == 0 {
		fmt.Fprintf(&b, "scaffolded from template: %s\n", template.Name)
		if template.RequiresSystem {
			b.WriteString("the template expects a committed design system: run design_system first if there is none\n")
		}
		if len(template.Craft) > 0 {
			fmt.Fprintf(&b, "read first: design_skills action \"show\" name %q, then craft %s\n",
				template.Name, strings.Join(template.Craft, ", "))
		}
	}
	fmt.Fprintf(&b, "\nNext: design_render with artifact_id %s, then design_inspect to read the layout.", artifact.ID)
	return WithResponseMetadata(NewTextResponse(b.String()), artifact), nil
}

// --- design_versions ---

type DesignVersionsParams struct {
	ArtifactID string `json:"artifact_id,omitempty"`
	Action     string `json:"action,omitempty"`
	Version    int    `json:"version,omitempty"`
	From       int    `json:"from,omitempty"`
	To         int    `json:"to,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Session    bool   `json:"session_only,omitempty"`
}

type designVersionsTool struct{ designTool }

// NewDesignVersionsTool returns the artifact history tool.
func NewDesignVersionsTool(permissions permission.Service) BaseTool {
	return &designVersionsTool{designTool{permissions: permissions}}
}

func (t *designVersionsTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignVersionsToolName,
		Description: `List design artifacts and manage the version history of one of them.

ACTIONS:
- "artifacts" (default when artifact_id is omitted): list the artifacts of this project.
- "list": the version history of an artifact, with the score of its last critique.
- "commit": record the current state of the artifact directory as the next version.
- "checkout": restore the artifact directory to a previous version.
- "diff": compare two versions.

NOTES:
- A version is a snapshot scoped to the artifact directory, so a checkout can never revert work elsewhere in the repository. The current state is snapshotted before a checkout overwrites it.`,
		Parameters: map[string]any{
			"artifact_id":  map[string]any{"type": "string", "description": "Artifact to operate on; omit to list artifacts"},
			"action":       map[string]any{"type": "string", "enum": []string{"artifacts", "list", "commit", "checkout", "diff"}, "description": "What to do (default \"list\", or \"artifacts\" without artifact_id)"},
			"version":      map[string]any{"type": "integer", "description": "Version number for \"checkout\""},
			"from":         map[string]any{"type": "integer", "description": "Base version for \"diff\""},
			"to":           map[string]any{"type": "integer", "description": "Target version for \"diff\""},
			"summary":      map[string]any{"type": "string", "description": "Description recorded with \"commit\""},
			"session_only": map[string]any{"type": "boolean", "description": "Restrict \"artifacts\" to the current session"},
		},
	}
}

func (t *designVersionsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignVersionsParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	action := strings.ToLower(strings.TrimSpace(params.Action))
	if action == "" {
		action = "list"
		if params.ArtifactID == "" {
			action = "artifacts"
		}
	}
	if action != "artifacts" && params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required for action " + action), nil
	}

	switch action {
	case "artifacts":
		artifacts, err := svc.List(ctx, params.Session)
		if err != nil {
			return designError(err), nil
		}
		if len(artifacts) == 0 {
			return NewTextResponse("No design artifacts yet. Create one with design_create."), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d design artifact(s):\n", len(artifacts))
		for _, a := range artifacts {
			fmt.Fprintf(&b, "- %s  %s  v%d  %s  (%s)\n", a.ID, a.Kind, a.CurrentVersion, a.Title, a.Dir)
		}
		return WithResponseMetadata(NewTextResponse(b.String()), artifacts), nil

	case "list":
		versions, err := svc.Versions(ctx, params.ArtifactID)
		if err != nil {
			return designError(err), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d version(s) of %s:\n", len(versions), params.ArtifactID)
		for _, v := range versions {
			score := ""
			if v.Critique != nil {
				score = fmt.Sprintf("  score %.1f", v.Critique.Score)
			}
			fmt.Fprintf(&b, "- v%d  %s%s\n", v.Number, v.Summary, score)
		}
		return WithResponseMetadata(NewTextResponse(b.String()), versions), nil

	case "commit":
		artifact, err := svc.Get(ctx, params.ArtifactID)
		if err != nil {
			return designError(err), nil
		}
		if err := t.requireWrite(ctx, DesignVersionsToolName, "commit", artifact.Dir,
			fmt.Sprintf("Record a new version of %s", artifact.Title),
			map[string]any{"artifact_id": artifact.ID, "summary": params.Summary}); err != nil {
			return ToolResponse{}, err
		}
		version, err := svc.CommitVersion(ctx, params.ArtifactID, params.Summary)
		if err != nil {
			return designError(err), nil
		}
		return WithResponseMetadata(NewTextResponse(fmt.Sprintf("Recorded version %d of %s", version.Number, artifact.Title)), version), nil

	case "checkout":
		if params.Version <= 0 {
			return NewTextErrorResponse("version is required for checkout"), nil
		}
		artifact, err := svc.Get(ctx, params.ArtifactID)
		if err != nil {
			return designError(err), nil
		}
		if err := t.requireWrite(ctx, DesignVersionsToolName, "checkout", artifact.Dir,
			fmt.Sprintf("Restore %s to version %d (only %s is touched)", artifact.Title, params.Version, artifact.Dir),
			map[string]any{"artifact_id": artifact.ID, "version": params.Version}); err != nil {
			return ToolResponse{}, err
		}
		if err := svc.Checkout(ctx, params.ArtifactID, params.Version); err != nil {
			return designError(err), nil
		}
		return NewTextResponse(fmt.Sprintf("Restored %s to version %d", artifact.Title, params.Version)), nil

	case "diff":
		if params.From <= 0 || params.To <= 0 {
			return NewTextErrorResponse("from and to are required for diff"), nil
		}
		entries, err := svc.Diff(ctx, params.ArtifactID, params.From, params.To)
		if err != nil {
			return designError(err), nil
		}
		if len(entries) == 0 {
			return NewTextResponse(fmt.Sprintf("v%d and v%d are identical", params.From, params.To)), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d change(s) between v%d and v%d:\n", len(entries), params.From, params.To)
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s %s\n", e.Type, e.Path)
		}
		return WithResponseMetadata(NewTextResponse(b.String()), entries), nil

	default:
		return NewTextErrorResponse("unknown action " + action), nil
	}
}

// --- design_system ---

type DesignSystemParams struct {
	Action string                       `json:"action,omitempty"`
	Name   string                       `json:"name,omitempty"`
	Tokens map[string]map[string]string `json:"tokens,omitempty"`
	Fonts  []string                     `json:"fonts,omitempty"`
	// Source and Target drive "extract".
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	// ArtifactID drives "apply".
	ArtifactID string `json:"artifact_id,omitempty"`
}

type designSystemTool struct{ designTool }

// NewDesignSystemTool returns the shared design-token tool.
func NewDesignSystemTool(permissions permission.Service) BaseTool {
	return &designSystemTool{designTool{permissions: permissions}}
}

func (t *designSystemTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignSystemToolName,
		Description: `Read, build or apply the design system shared by the artifacts of this project.

The system is three committed files under the design system directory: tokens.json (the source of truth), the system.css it generates as CSS custom properties (--<group>-<name>), and DESIGN.md, the written contract. Editing tokens regenerates the stylesheet and the token table in DESIGN.md; prose in DESIGN.md is never overwritten.

ACTIONS:
- "get" (default): return the current tokens and the path artifacts should link.
- "init": write the default token set if none exists.
- "set": merge tokens in. A token given an empty value is removed.
- "extract": build the system from something that already looks right, and write it. Needs "source" plus "target":
    - source "code": scan a directory of stylesheets and components (target defaults to the project root).
    - source "url": render an http(s) page and read its computed styles.
    - source "image": read a palette out of a screenshot or logo. Colours only.
    - source "text": read a written style guide, either a file path or a bundled example name.
- "apply": link the stylesheet into an artifact entry document and report the values in it that a token already covers. Needs "artifact_id".
- "examples": list the bundled style guides usable as an extraction target.

USAGE IN AN ARTIFACT:
- Link the returned stylesheet path from the artifact entry document and use var(--color-accent), var(--space-md), and so on. Changing a token then updates every artifact on its next render.`,
		Parameters: map[string]any{
			"action":      map[string]any{"type": "string", "enum": []string{"get", "init", "set", "extract", "apply", "examples"}, "description": "What to do (default \"get\")"},
			"source":      map[string]any{"type": "string", "enum": []string{"code", "url", "image", "text"}, "description": "Where to extract from (\"extract\" only)"},
			"target":      map[string]any{"type": "string", "description": "Directory, URL, image path, style-guide path or bundled example name (\"extract\" only)"},
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to apply the system to (\"apply\" only)"},
			"name":        map[string]any{"type": "string", "description": "Name of the design system (\"set\" only)"},
			"tokens": map[string]any{
				"type":                 "object",
				"description":          "Token groups to merge, e.g. {\"color\": {\"accent\": \"#2f6feb\"}}",
				"additionalProperties": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
			"fonts": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Stylesheet URLs imported ahead of the custom properties",
			},
		},
	}
}

func (t *designSystemTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignSystemParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	if action == "" {
		action = "get"
	}
	// Listing the bundled guides reads nothing from the project, so it is
	// answered before the service is resolved: a user asking what is available
	// should get an answer even where the subsystem was never wired.
	if action == "examples" {
		return NewTextResponse(describeSystemExamples()), nil
	}

	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	ds, exists, err := svc.LoadSystem()
	if err != nil {
		return designError(err), nil
	}

	switch action {
	case "get":
		return WithResponseMetadata(NewTextResponse(describeSystem(svc, ds, exists)), ds), nil

	case "extract":
		source := design.ExtractSource(strings.ToLower(strings.TrimSpace(params.Source)))
		if source == "" {
			source = design.SourceCode
		}
		if err := t.requireWrite(ctx, DesignSystemToolName, "extract",
			filepath.Dir(svc.SystemRelPath(design.SystemTokensFile)),
			fmt.Sprintf("Replace the design system with one extracted from %s %s", source, params.Target),
			map[string]any{"source": source, "target": params.Target}); err != nil {
			return ToolResponse{}, err
		}
		result, err := svc.ExtractSystem(ctx, design.ExtractOptions{
			Source: source,
			Target: params.Target,
			Name:   params.Name,
		})
		if err != nil {
			return designError(err), nil
		}
		if _, _, err := svc.SaveSystem(result.System); err != nil {
			return designError(err), nil
		}
		mirrored, mirrorErr := svc.MirrorSystem(ctx, result.System, result.Source, result.Target)
		return WithResponseMetadata(NewTextResponse(describeExtraction(svc, result, mirrored, mirrorErr)), result), nil

	case "apply":
		if strings.TrimSpace(params.ArtifactID) == "" {
			return NewTextErrorResponse("apply needs artifact_id"), nil
		}
		artifact, err := svc.Get(ctx, params.ArtifactID)
		if err != nil {
			return designError(err), nil
		}
		if err := t.requireWrite(ctx, DesignSystemToolName, "apply", artifact.Dir,
			"Link the design system into "+artifact.Title,
			map[string]any{"artifact_id": artifact.ID}); err != nil {
			return ToolResponse{}, err
		}
		result, err := svc.ApplySystem(ctx, artifact.ID)
		if err != nil {
			return designError(err), nil
		}
		return WithResponseMetadata(NewTextResponse(describeApply(result)), result), nil

	case "init", "set":
		relTokens := svc.SystemRelPath(design.SystemTokensFile)
		if err := t.requireWrite(ctx, DesignSystemToolName, "update", filepath.Dir(relTokens),
			"Update the shared design system tokens",
			map[string]any{"action": action, "tokens": params.Tokens}); err != nil {
			return ToolResponse{}, err
		}
		if action == "init" {
			if exists {
				return WithResponseMetadata(NewTextResponse("A design system already exists.\n\n"+describeSystem(svc, ds, true)), ds), nil
			}
			ds = design.DefaultDesignSystem()
			if params.Name != "" {
				ds.Name = params.Name
			}
			ds.Fonts = params.Fonts
			if _, _, err := svc.SaveSystem(ds); err != nil {
				return designError(err), nil
			}
			return WithResponseMetadata(NewTextResponse("Initialised the design system.\n\n"+describeSystem(svc, ds, true)), ds), nil
		}
		if len(params.Tokens) == 0 && len(params.Fonts) == 0 && params.Name == "" {
			return NewTextErrorResponse("set needs tokens, fonts or a name"), nil
		}
		if len(params.Fonts) > 0 {
			ds.Fonts = params.Fonts
			if _, _, err := svc.SaveSystem(ds); err != nil {
				return designError(err), nil
			}
		}
		updated, err := svc.SetSystemTokens(params.Name, params.Tokens)
		if err != nil {
			return designError(err), nil
		}
		return WithResponseMetadata(NewTextResponse("Updated the design system.\n\n"+describeSystem(svc, updated, true)), updated), nil

	default:
		return NewTextErrorResponse("unknown action " + action), nil
	}
}

func describeSystem(svc *design.Service, ds design.DesignSystem, exists bool) string {
	var b strings.Builder
	if !exists {
		b.WriteString("No design system file yet; showing the default that design_system init would write.\n\n")
	}
	fmt.Fprintf(&b, "design system: %s\n", ds.Name)
	fmt.Fprintf(&b, "stylesheet: %s\n", svc.SystemRelPath(design.SystemStylesheet))
	fmt.Fprintf(&b, "tokens: %s\n\n", svc.SystemRelPath(design.SystemTokensFile))
	groups := make([]string, 0, len(ds.Tokens))
	for g := range ds.Tokens {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		names := make([]string, 0, len(ds.Tokens[g]))
		for n := range ds.Tokens[g] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "  --%s-%s: %s\n", g, n, ds.Tokens[g][n])
		}
	}
	return b.String()
}

// describeSystemExamples lists the bundled style guides.
func describeSystemExamples() string {
	names := design.ExampleSystemNames()
	if len(names) == 0 {
		return "No bundled style guides are available in this build."
	}
	var b strings.Builder
	b.WriteString("Bundled style guides. Use one as an extraction target:\n")
	b.WriteString("  design_system(action: \"extract\", source: \"text\", target: \"<name>\")\n\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- %s — %s\n", name, design.ExampleSystemTitle(name))
	}
	return b.String()
}

// describeExtraction reports what an extraction read and what it produced. The
// notes matter as much as the tokens: they say what the source could not tell
// us, which is where a human still has to decide.
func describeExtraction(svc *design.Service, result design.ExtractResult, mirrored string, mirrorErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Extracted the design system from %s", result.Source)
	if result.Target != "" {
		fmt.Fprintf(&b, " %s", result.Target)
	}
	b.WriteString(" and wrote it.\n\n")
	if len(result.Scanned) > 0 {
		limit := len(result.Scanned)
		if limit > 10 {
			limit = 10
		}
		fmt.Fprintf(&b, "read %d source(s): %s", len(result.Scanned), strings.Join(result.Scanned[:limit], ", "))
		if len(result.Scanned) > limit {
			fmt.Fprintf(&b, ", +%d more", len(result.Scanned)-limit)
		}
		b.WriteString("\n")
	}
	for _, note := range result.Notes {
		fmt.Fprintf(&b, "note: %s\n", note)
	}
	switch {
	case mirrorErr != nil:
		fmt.Fprintf(&b, "note: not mirrored to the knowledge base: %v\n", mirrorErr)
	case mirrored != "":
		fmt.Fprintf(&b, "mirrored to the knowledge base as %s\n", mirrored)
	}
	b.WriteString("\n")
	b.WriteString(describeSystem(svc, result.System, true))
	return b.String()
}

// describeApply reports the link and the audit.
func describeApply(result design.ApplyResult) string {
	var b strings.Builder
	if result.Linked {
		fmt.Fprintf(&b, "Linked %s from %s.\n", result.Stylesheet, result.Entry)
	} else {
		fmt.Fprintf(&b, "%s already links %s.\n", result.Entry, result.Stylesheet)
	}
	fmt.Fprintf(&b, "design system: %s\naudited %d file(s)\n", result.System, result.Scanned)
	if len(result.Findings) == 0 {
		b.WriteString("\nNo hardcoded values that a token already covers.\n")
		return b.String()
	}
	b.WriteString("\nHardcoded values a token already covers. Replace each with var(<token>):\n")
	for _, f := range result.Findings {
		fmt.Fprintf(&b, "  %s:%d", f.File, f.Line)
		if f.Property != "" {
			fmt.Fprintf(&b, " %s", f.Property)
		}
		fmt.Fprintf(&b, " %s -> var(%s)\n", f.Value, f.Token)
	}
	if result.Truncated {
		b.WriteString("  ... more findings not listed\n")
	}
	return b.String()
}
