package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
)

// --- design_render ---

type DesignRenderParams struct {
	ArtifactID string `json:"artifact_id"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	WaitMS     int    `json:"wait_ms,omitempty"`
	MaxNodes   int    `json:"max_nodes,omitempty"`
	MaxDepth   int    `json:"max_depth,omitempty"`
	PrintMedia bool   `json:"print_media,omitempty"`
}

type designRenderTool struct{ designTool }

// NewDesignRenderTool returns the render/index tool.
func NewDesignRenderTool() BaseTool { return &designRenderTool{} }

func (t *designRenderTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignRenderToolName,
		Description: `Render a design artifact in a headless browser and index its live structure.

WHAT IT DOES:
- Loads the artifact entry document, waits for it to settle, then walks the rendered DOM recording every element's selector, role, text and layout box, plus the console messages and failed requests the page produced.
- The index is stored against the artifact's current version, which is what design_inspect reads and what lets design_patch resolve a node id back to a source edit.

WHEN TO USE THIS TOOL:
- After creating an artifact, and after every batch of edits. Rendering is how you find out what the design actually looks like instead of guessing from the source.

RETURNS:
- A summary: title, viewport, node count, deck slide count, console errors and failed requests. Read the structure itself with design_inspect and look at it with design_screenshot.

NOTES:
- The index attribute is stamped on the live DOM only; artifact files are never rewritten by a render.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to render"},
			"width":       map[string]any{"type": "integer", "description": "Viewport width in CSS pixels (defaults to the manifest viewport)"},
			"height":      map[string]any{"type": "integer", "description": "Viewport height in CSS pixels"},
			"wait_ms":     map[string]any{"type": "integer", "description": "Extra settle time after load, in milliseconds"},
			"max_nodes":   map[string]any{"type": "integer", "description": "Cap on indexed nodes (default 400)"},
			"max_depth":   map[string]any{"type": "integer", "description": "Cap on indexed depth (default 14)"},
			"print_media": map[string]any{"type": "boolean", "description": "Render under print emulation, as an export would"},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designRenderTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignRenderParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	result, err := svc.Render(ctx, params.ArtifactID, design.RenderOptions{
		Viewport:   design.Viewport{W: params.Width, H: params.Height},
		Wait:       time.Duration(params.WaitMS) * time.Millisecond,
		MaxNodes:   params.MaxNodes,
		MaxDepth:   params.MaxDepth,
		PrintMedia: params.PrintMedia,
	})
	if err != nil {
		return designError(err), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Rendered %q at %dx%d\n", result.Title, result.Viewport.W, result.Viewport.H)
	fmt.Fprintf(&b, "nodes indexed: %d", len(result.Nodes))
	if result.Truncated {
		b.WriteString(" (truncated: raise max_nodes/max_depth to index more)")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "document height: %.0fpx\n", result.Height)
	if result.Slides > 0 {
		fmt.Fprintf(&b, "slides: %d\n", result.Slides)
	}
	if errs := consoleErrors(result.Console); len(errs) > 0 {
		fmt.Fprintf(&b, "\nconsole errors (%d):\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(&b, "  [%s] %s\n", e.Level, e.Message)
		}
	}
	if len(result.Failures) > 0 {
		fmt.Fprintf(&b, "\nfailed requests (%d):\n", len(result.Failures))
		for _, f := range result.Failures {
			fmt.Fprintf(&b, "  %s %s\n", f.URL, f.Error)
		}
	}
	b.WriteString("\nNext: design_inspect to read the structure, design_screenshot to look at it.")

	result.Nodes = nil // the index is large; read it through design_inspect
	return WithResponseMetadata(NewTextResponse(b.String()), result), nil
}

func consoleErrors(entries []design.ConsoleEntry) []design.ConsoleEntry {
	var out []design.ConsoleEntry
	for _, e := range entries {
		if e.Level == "error" || e.Level == "exception" || e.Level == "assert" {
			out = append(out, e)
		}
	}
	return out
}

// --- design_inspect ---

type DesignInspectParams struct {
	ArtifactID    string   `json:"artifact_id"`
	Version       int      `json:"version,omitempty"`
	NodeID        string   `json:"node_id,omitempty"`
	Selector      string   `json:"selector,omitempty"`
	Text          string   `json:"text,omitempty"`
	Slide         int      `json:"slide,omitempty"`
	Depth         int      `json:"depth,omitempty"`
	Offset        int      `json:"offset,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	IncludeStyles bool     `json:"include_styles,omitempty"`
	StyleProps    []string `json:"style_props,omitempty"`
	MaxTextLen    int      `json:"max_text_len,omitempty"`
}

type designInspectTool struct{ designTool }

// NewDesignInspectTool returns the structure inspector.
func NewDesignInspectTool() BaseTool { return &designInspectTool{} }

func (t *designInspectTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignInspectToolName,
		Description: `Read the structure index built by the last design_render: one line per node with its id, selector, role, layout box and text.

WHEN TO USE THIS TOOL:
- To find the node you want to change ("the hero heading", "the third card"), and to check what the browser actually laid out — overlapping boxes, zero-height sections, text that overflows.

FILTERS:
- node_id restricts the result to one node and its descendants; selector and text match on substrings; slide restricts to one deck slide; depth limits how deep the result goes.

TOKEN BUDGET:
- Results are paged (default 40 nodes) and computed styles are omitted unless include_styles is set. Narrow with a filter before raising the limit.

NEXT:
- Pass a node id straight to design_patch: the same id resolves back to the element in the source file.`,
		Parameters: map[string]any{
			"artifact_id":    map[string]any{"type": "string", "description": "Artifact to inspect"},
			"version":        map[string]any{"type": "integer", "description": "Version to read (default: the current one)"},
			"node_id":        map[string]any{"type": "string", "description": "Restrict to this node and its descendants"},
			"selector":       map[string]any{"type": "string", "description": "Match nodes whose selector or role contains this text"},
			"text":           map[string]any{"type": "string", "description": "Match nodes whose text contains this text"},
			"slide":          map[string]any{"type": "integer", "description": "Restrict to one deck slide (1-based); omit for all"},
			"depth":          map[string]any{"type": "integer", "description": "Limit how far below the root nodes the result descends"},
			"offset":         map[string]any{"type": "integer", "description": "Page offset"},
			"limit":          map[string]any{"type": "integer", "description": "Nodes per page (default 40, max 200)"},
			"include_styles": map[string]any{"type": "boolean", "description": "Include the computed-style subset"},
			"style_props":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Narrow which style properties are returned"},
			"max_text_len":   map[string]any{"type": "integer", "description": "Truncate node text to this many characters"},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designInspectTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignInspectParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}
	slide := params.Slide
	if slide == 0 {
		slide = -1
	}
	result, err := svc.Inspect(ctx, params.ArtifactID, params.Version, design.InspectOptions{
		NodeID:        params.NodeID,
		Selector:      params.Selector,
		Text:          params.Text,
		Slide:         slide,
		Depth:         params.Depth,
		Offset:        params.Offset,
		Limit:         params.Limit,
		IncludeStyles: params.IncludeStyles,
		StyleProps:    params.StyleProps,
		MaxTextLen:    params.MaxTextLen,
	})
	if err != nil {
		return designError(err), nil
	}
	response := NewTextResponse(result.Text())
	response = WithResponseMetadata(response, map[string]any{
		"artifact_id": result.ArtifactID,
		"version":     result.Version,
		"pagination": PaginationInfo{
			TotalItems:    result.Total,
			ReturnedItems: len(result.Nodes),
			Offset:        result.Offset,
			Limit:         result.Limit,
			HasMore:       result.NextOffset >= 0,
		},
	})
	return response, nil
}

// --- design_patch ---

type DesignPatchParams struct {
	ArtifactID string           `json:"artifact_id"`
	Ops        []design.PatchOp `json:"ops"`
	Summary    string           `json:"summary,omitempty"`
	Commit     bool             `json:"commit,omitempty"`
}

type designPatchTool struct{ designTool }

// NewDesignPatchTool returns the selector-to-source patch tool.
func NewDesignPatchTool(permissions permission.Service) BaseTool {
	return &designPatchTool{designTool{permissions: permissions}}
}

func (t *designPatchTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignPatchToolName,
		Description: `Apply targeted edits to a design artifact's source, addressing elements by the node id from design_inspect or by a CSS selector.

WHEN TO USE THIS TOOL:
- To change what you just looked at: "make this heading larger", "swap the accent colour of that button", "drop the third card". You inspect, you get a node id, you patch it.
- For structural or wholesale rewrites, use the normal write/edit tools instead: this tool is for surgical changes.

OPERATIONS (one per entry in ops):
- set_text, set_html: replace the element's content.
- set_attr, remove_attr: manage one attribute.
- set_style: merge inline style declarations (a declaration with an empty value is removed).
- add_class, remove_class.
- insert_html with position before|after|prepend|append.
- replace_outer, remove: replace or delete the whole element.

HOW IT EDITS:
- The source file is spliced, not reformatted: every byte outside the targeted element is preserved, so the diff stays reviewable and the file stays yours.
- A selector that matches several elements is refused unless the operation sets "all": true.
- Elements that only exist because a script created them cannot be patched; edit the script instead.

AFTER PATCHING:
- Run design_render again to refresh the index, then look at the result. Set commit to record a version once you are happy.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to patch"},
			"ops": map[string]any{
				"type":        "array",
				"description": "Operations to apply, in order",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"node_id":  map[string]any{"type": "string", "description": "Node id from design_inspect"},
						"selector": map[string]any{"type": "string", "description": "CSS selector (tag, #id, .class, [attr=value], :nth-of-type(n), > and descendant combinators)"},
						"file":     map[string]any{"type": "string", "description": "Artifact-relative file to edit (default: the entry document)"},
						"op":       map[string]any{"type": "string", "enum": []string{"set_text", "set_html", "set_attr", "remove_attr", "set_style", "add_class", "remove_class", "insert_html", "replace_outer", "remove"}},
						"attr":     map[string]any{"type": "string", "description": "Attribute name for set_attr/remove_attr"},
						"value":    map[string]any{"type": "string", "description": "Text for set_text, or attribute value for set_attr"},
						"style":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Declarations for set_style"},
						"class":    map[string]any{"type": "string", "description": "Class name for add_class/remove_class"},
						"html":     map[string]any{"type": "string", "description": "Markup for set_html/insert_html/replace_outer"},
						"position": map[string]any{"type": "string", "enum": []string{"before", "after", "prepend", "append"}, "description": "Where insert_html puts the markup"},
						"all":      map[string]any{"type": "boolean", "description": "Apply to every match instead of failing on an ambiguous selector"},
					},
					"required": []string{"op"},
				},
			},
			"summary": map[string]any{"type": "string", "description": "Description recorded with the version when commit is set"},
			"commit":  map[string]any{"type": "boolean", "description": "Record a new version after applying the patch"},
		},
		Required: []string{"artifact_id", "ops"},
	}
}

func (t *designPatchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignPatchParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	if len(params.Ops) == 0 {
		return NewTextErrorResponse("ops is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	plan, err := svc.PreparePatch(ctx, params.ArtifactID, params.Ops)
	if err != nil {
		return designError(err), nil
	}
	if plan.Empty() {
		return NewTextResponse("The patch changes nothing: every operation matched an element that already has the requested state."), nil
	}

	var b strings.Builder
	var totalAdd, totalDel int
	for _, f := range plan.Files {
		if f.Old == f.New {
			continue
		}
		fileDiff, add, del := f.Diff()
		totalAdd += add
		totalDel += del
		rel := filepath.ToSlash(filepath.Join(plan.Artifact.Dir, f.RelPath))
		if err := t.requireWrite(ctx, DesignPatchToolName, "update", rel,
			fmt.Sprintf("Patch %s", rel),
			EditPermissionsParams{FilePath: rel, Diff: fileDiff}); err != nil {
			return ToolResponse{}, err
		}
		fmt.Fprintf(&b, "%s: %d change(s)\n", rel, len(f.Changes))
		for _, c := range f.Changes {
			detail := c.Detail
			if detail == "" {
				detail = c.Op
			}
			fmt.Fprintf(&b, "  - %s\n", detail)
		}
	}

	version, err := svc.ApplyPatchPlan(ctx, plan, params.Summary, params.Commit)
	if err != nil {
		return designError(err), nil
	}
	fmt.Fprintf(&b, "\n%d addition(s), %d removal(s)\n", totalAdd, totalDel)
	if version > 0 {
		fmt.Fprintf(&b, "recorded version %d\n", version)
	}
	b.WriteString("\nNext: design_render to refresh the index, then look at the result with design_screenshot.")
	return WithResponseMetadata(NewTextResponse(b.String()), plan), nil
}

// --- design_screenshot ---

type DesignScreenshotParams struct {
	ArtifactID string `json:"artifact_id"`
	Selector   string `json:"selector,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
	Slide      int    `json:"slide,omitempty"`
	FullPage   bool   `json:"full_page,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	WaitMS     int    `json:"wait_ms,omitempty"`
}

type designScreenshotTool struct{ designTool }

// NewDesignScreenshotTool returns the visual-feedback tool.
func NewDesignScreenshotTool() BaseTool { return &designScreenshotTool{} }

func (t *designScreenshotTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignScreenshotToolName,
		Description: `Capture a design artifact as an image and return it to you directly.

WHEN TO USE THIS TOOL:
- After every render, to actually see what you built. Layout problems, contrast problems and spacing problems are obvious in the image and invisible in the source.

WHAT YOU CAN CAPTURE:
- The viewport (default), the whole document (full_page), one element (selector or node_id), or one deck slide (slide).

NOTES:
- Use image_crop on a saved screenshot when you need to read fine detail at native resolution.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to capture"},
			"selector":    map[string]any{"type": "string", "description": "Capture a single element"},
			"node_id":     map[string]any{"type": "string", "description": "Capture the element behind a node id from design_inspect"},
			"slide":       map[string]any{"type": "integer", "description": "Capture one deck slide (1-based)"},
			"full_page":   map[string]any{"type": "boolean", "description": "Capture the whole document instead of the viewport"},
			"width":       map[string]any{"type": "integer", "description": "Viewport width in CSS pixels"},
			"height":      map[string]any{"type": "integer", "description": "Viewport height in CSS pixels"},
			"wait_ms":     map[string]any{"type": "integer", "description": "Extra settle time after load, in milliseconds"},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designScreenshotTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignScreenshotParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}
	artifact, err := svc.Get(ctx, params.ArtifactID)
	if err != nil {
		return designError(err), nil
	}
	renderer := svc.Renderer()
	if renderer == nil || !renderer.Available() {
		return designError(design.ErrNoBrowser), nil
	}

	selector := params.Selector
	if selector == "" && params.NodeID != "" {
		node, err := svc.Node(ctx, params.ArtifactID, 0, params.NodeID)
		if err != nil {
			return designError(err), nil
		}
		selector = node.Selector
	}
	slide := params.Slide
	if slide == 0 {
		slide = -1
	}

	shot, err := renderer.Screenshot(ctx, artifact, design.ScreenshotOptions{
		RenderOptions: design.RenderOptions{
			Viewport: design.Viewport{W: params.Width, H: params.Height},
			Wait:     time.Duration(params.WaitMS) * time.Millisecond,
		},
		Selector: selector,
		Slide:    slide,
		FullPage: params.FullPage,
	})
	if err != nil {
		return designError(err), nil
	}
	return ToolResponse{
		Type:    ToolResponseTypeImage,
		Content: base64.StdEncoding.EncodeToString(shot),
	}, nil
}

// --- design_export ---

type DesignExportParams struct {
	ArtifactID string `json:"artifact_id"`
	Format     string `json:"format,omitempty"`
	Dest       string `json:"dest,omitempty"`
	Slide      int    `json:"slide,omitempty"`
	FullPage   bool   `json:"full_page,omitempty"`
	Landscape  bool   `json:"landscape,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

type designExportTool struct{ designTool }

// NewDesignExportTool returns the export tool.
func NewDesignExportTool(permissions permission.Service) BaseTool {
	return &designExportTool{designTool{permissions: permissions}}
}

func (t *designExportTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignExportToolName,
		Description: `Export a design artifact as a self-contained HTML file, a PNG or a PDF.

FORMATS:
- "html": a single file with local stylesheets, scripts and images inlined, so it can be sent or committed on its own. Remote references are left alone.
- "png": a screenshot; use full_page for the whole document or slide for one deck slide.
- "pdf": printed under print emulation, honouring the artifact's own @page rules. A deck whose slides carry break-after: page becomes one page per slide, and the tool tells you when they do not.

OUTPUT:
- dest is a path relative to the working directory; omit it to write into the artifact's exports/ directory.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to export"},
			"format":      map[string]any{"type": "string", "enum": []string{"html", "png", "pdf"}, "description": "Export format (default \"html\")"},
			"dest":        map[string]any{"type": "string", "description": "Output path; defaults to the artifact's exports/ directory"},
			"slide":       map[string]any{"type": "integer", "description": "Export one deck slide (PNG only, 1-based)"},
			"full_page":   map[string]any{"type": "boolean", "description": "Capture the whole document for PNG exports"},
			"landscape":   map[string]any{"type": "boolean", "description": "Landscape orientation for PDFs without their own @page size"},
			"width":       map[string]any{"type": "integer", "description": "Viewport width in CSS pixels"},
			"height":      map[string]any{"type": "integer", "description": "Viewport height in CSS pixels"},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designExportTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignExportParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}
	artifact, err := svc.Get(ctx, params.ArtifactID)
	if err != nil {
		return designError(err), nil
	}

	format := strings.ToLower(strings.TrimSpace(params.Format))
	if format == "" {
		format = design.ExportHTML
	}
	dest := params.Dest
	if dest == "" {
		dest = filepath.ToSlash(filepath.Join(artifact.Dir, "exports", artifact.Slug+"."+format))
	}
	if err := t.requireWrite(ctx, DesignExportToolName, "write", dest,
		fmt.Sprintf("Export %s as %s to %s", artifact.Title, format, dest),
		map[string]any{"artifact_id": artifact.ID, "format": format, "dest": dest}); err != nil {
		return ToolResponse{}, err
	}

	slide := params.Slide
	if slide == 0 {
		slide = -1
	}
	result, err := svc.Export(ctx, params.ArtifactID, design.ExportOptions{
		Format:    format,
		Dest:      params.Dest,
		Slide:     slide,
		FullPage:  params.FullPage,
		Landscape: params.Landscape,
		Viewport:  design.Viewport{W: params.Width, H: params.Height},
	})
	if err != nil {
		return designError(err), nil
	}
	text := fmt.Sprintf("Exported %s as %s to %s (%d bytes)", artifact.Title, result.Format, result.Path, result.Bytes)
	if result.Note != "" {
		text += "\n\nnote: " + result.Note
	}
	return WithResponseMetadata(NewTextResponse(text), result), nil
}

// --- design_canvas ---

type DesignCanvasParams struct {
	HTML   string `json:"html"`
	Dest   string `json:"dest"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	WaitMS int    `json:"wait_ms,omitempty"`
}

type designCanvasTool struct{ designTool }

// NewDesignCanvasTool returns the canvas rasterisation tool.
func NewDesignCanvasTool(permissions permission.Service) BaseTool {
	return &designCanvasTool{designTool{permissions: permissions}}
}

func (t *designCanvasTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignCanvasToolName,
		Description: `Generate an image by rendering a self-contained HTML/canvas/SVG document in the browser and saving the result as a PNG.

WHEN TO USE THIS TOOL:
- To produce the imagery a design needs — a gradient mesh background, a generated pattern, a chart, an icon, an SVG rasterised at a specific size — without any external image service. You write the drawing code, the browser executes it, the PNG lands in the working tree.

HOW TO USE:
- Pass a complete HTML document that draws at the given size (a <canvas> you paint with script, an inline <svg>, or plain styled markup).
- dest is where the PNG is written, relative to the working directory. Reference it from the artifact afterwards.

NOTES:
- The document is rendered in isolation: it cannot see the artifact's stylesheets. Inline everything it needs.`,
		Parameters: map[string]any{
			"html":    map[string]any{"type": "string", "description": "Complete, self-contained HTML document to render"},
			"dest":    map[string]any{"type": "string", "description": "Output PNG path, relative to the working directory"},
			"width":   map[string]any{"type": "integer", "description": "Image width in pixels (default 1024)"},
			"height":  map[string]any{"type": "integer", "description": "Image height in pixels (default 1024)"},
			"wait_ms": map[string]any{"type": "integer", "description": "Settle time before capture, in milliseconds"},
		},
		Required: []string{"html", "dest"},
	}
}

func (t *designCanvasTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignCanvasParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.HTML) == "" {
		return NewTextErrorResponse("html is required"), nil
	}
	if strings.TrimSpace(params.Dest) == "" {
		return NewTextErrorResponse("dest is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}
	renderer := svc.Renderer()
	if renderer == nil || !renderer.Available() {
		return designError(design.ErrNoBrowser), nil
	}
	width, height := params.Width, params.Height
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 1024
	}
	if err := t.requireWrite(ctx, DesignCanvasToolName, "write", params.Dest,
		fmt.Sprintf("Generate a %dx%d image at %s", width, height, params.Dest),
		map[string]any{"dest": params.Dest, "width": width, "height": height}); err != nil {
		return ToolResponse{}, err
	}

	png, err := renderer.Rasterize(ctx, params.HTML, width, height, time.Duration(params.WaitMS)*time.Millisecond)
	if err != nil {
		return designError(err), nil
	}
	path, err := svc.WriteWorkspaceFile(params.Dest, png)
	if err != nil {
		return designError(err), nil
	}
	return WithResponseMetadata(
		NewTextResponse(fmt.Sprintf("Wrote a %dx%d PNG to %s (%d bytes)", width, height, path, len(png))),
		map[string]any{"path": path, "width": width, "height": height, "bytes": len(png)},
	), nil
}

// --- design_present ---

type DesignPresentParams struct {
	ArtifactID string `json:"artifact_id"`
	Slide      int    `json:"slide,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
	Open       bool   `json:"open,omitempty"`
}

type designPresentTool struct{ designTool }

// NewDesignPresentTool returns the tool that surfaces an artifact to the user.
func NewDesignPresentTool() BaseTool { return &designPresentTool{} }

func (t *designPresentTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignPresentToolName,
		Description: `Show a design artifact to the user: return the address to open it at, optionally focused on one slide or one node.

WHEN TO USE THIS TOOL:
- When the work is ready to look at, or when you want the user to review a specific part of it. Call it at the end of an iteration rather than after every edit.

RETURNS:
- The artifact address, the entry document path and, for decks, the slide count. A node id is returned as a design://<node_id> selection reference the interface resolves to that element.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to present"},
			"slide":       map[string]any{"type": "integer", "description": "Open the deck at this slide (1-based)"},
			"node_id":     map[string]any{"type": "string", "description": "Focus this node from design_inspect"},
			"open":        map[string]any{"type": "boolean", "description": "Also open the address in the user's default browser. Use it when the user asked to see the work now, not on every iteration."},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designPresentTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignPresentParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}
	// Presenting is the moment a URL has to exist, so it is where a process
	// without an API server starts its own loopback preview. Rendering and
	// exporting deliberately do not: they must stay socket-free.
	if _, err := design.EnsurePreviewServer(); err != nil {
		logging.Warn("design: preview server unavailable, falling back to file://", "error", err)
	}

	presentation, err := svc.Presentation(ctx, params.ArtifactID, params.Slide, params.NodeID)
	if err != nil {
		return designError(err), nil
	}

	opened := ""
	if params.Open {
		if err := auth.OpenBrowser(presentation.URL); err != nil {
			opened = fmt.Sprintf("could not open a browser automatically (%v); open the address above manually", err)
		} else {
			opened = "opened in the default browser"
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, v%d)\n", presentation.Title, presentation.Kind, presentation.Version)
	fmt.Fprintf(&b, "open: %s\n", presentation.URL)
	fmt.Fprintf(&b, "entry: %s\n", presentation.Entry)
	if presentation.Slides > 0 {
		fmt.Fprintf(&b, "slides: %d\n", presentation.Slides)
	}
	if presentation.Selection != "" {
		fmt.Fprintf(&b, "selection: %s\n", presentation.Selection)
	}
	if opened != "" {
		fmt.Fprintf(&b, "%s\n", opened)
	}
	return WithResponseMetadata(NewTextResponse(b.String()), presentation), nil
}
