package client

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
)

// Design Studio API (web-ui/packages/pando-client stores/designStore.ts,
// internal/api/handlers_design.go).

// DesignArtifact is one design artifact (a web prototype or a deck).
type DesignArtifact struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id,omitempty"`
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Dir            string `json:"dir"`
	Kind           string `json:"kind"` // "web" | "deck"
	CurrentVersion int    `json:"current_version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	// URL is the served preview address, or a file:// address when no
	// preview server runs.
	URL string `json:"url,omitempty"`
	// BridgeURL is the preview with the selection bridge enabled.
	BridgeURL string `json:"bridge_url,omitempty"`
	FileURL   string `json:"file_url,omitempty"`
	Slides    int    `json:"slides,omitempty"`
	Entry     string `json:"entry,omitempty"`
}

// DesignCritiqueScore is the score a version carries in the timeline.
type DesignCritiqueScore struct {
	Score   float64 `json:"score"`
	Summary string  `json:"summary,omitempty"`
}

// DesignVersion is one accepted iteration of an artifact.
type DesignVersion struct {
	ArtifactID string               `json:"artifact_id"`
	Number     int                  `json:"number"`
	SnapshotID string               `json:"snapshot_id"`
	Summary    string               `json:"summary"`
	CreatedAt  string               `json:"created_at"`
	Critique   *DesignCritiqueScore `json:"critique,omitempty"`
}

// DesignBox is the rectangle of a node in the render.
type DesignBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// DesignNode is one entry of the structure index of a render.
type DesignNode struct {
	NodeID   string            `json:"node_id"`
	ParentID string            `json:"parent_id,omitempty"`
	Selector string            `json:"selector,omitempty"`
	Role     string            `json:"role,omitempty"`
	Text     string            `json:"text,omitempty"`
	Slide    int               `json:"slide,omitempty"`
	Box      DesignBox         `json:"box"`
	Styles   map[string]string `json:"styles,omitempty"`
}

// DesignStatus tells whether the Design Studio is available.
type DesignStatus struct {
	Enabled       bool     `json:"enabled"`
	Preview       bool     `json:"preview"`
	PreviewReason string   `json:"preview_reason,omitempty"`
	Renderer      bool     `json:"renderer"`
	Kinds         []string `json:"kinds"`
	OutputDir     string   `json:"output_dir,omitempty"`
}

// DesignTemplate is one gallery entry: a design skill.
type DesignTemplate struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Category        string   `json:"category"`
	Scenario        string   `json:"scenario,omitempty"`
	ExamplePrompt   string   `json:"example_prompt,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	RequiresSystem  bool     `json:"requires_system"`
	CritiquePolicy  string   `json:"critique_policy,omitempty"`
	Startable       bool     `json:"startable"`
	Source          string   `json:"source"` // "bundled" | "installed"
	Installed       bool     `json:"installed"`
	SourcePath      string   `json:"source_path,omitempty"`
	Preview         string   `json:"preview,omitempty"`
	CraftReferences []string `json:"craft,omitempty"`
}

// DesignIssue is one finding of the audit or the critic.
type DesignIssue struct {
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity"` // info | warning | error | blocking
	NodeID   string `json:"node_id,omitempty"`
	Slide    int    `json:"slide,omitempty"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// DesignCritique is one recorded critic pass over a version.
type DesignCritique struct {
	ID         string        `json:"id"`
	ArtifactID string        `json:"artifact_id"`
	Version    int           `json:"version"`
	Score      float64       `json:"score"`
	Summary    string        `json:"summary,omitempty"`
	Issues     []DesignIssue `json:"issues"`
	CreatedAt  string        `json:"created_at"`
}

// DesignGateDecision is the quality gate's verdict.
type DesignGateDecision struct {
	Pass      bool    `json:"pass"`
	Iterate   bool    `json:"iterate"`
	Reason    string  `json:"reason"`
	Round     int     `json:"round"`
	MaxRounds int     `json:"max_rounds"`
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold"`
	Policy    string  `json:"policy"`
	Blocking  int     `json:"blocking"`
}

// DesignCritiqueSettings are the gate settings.
type DesignCritiqueSettings struct {
	Enabled   bool    `json:"enabled"`
	MaxRounds int     `json:"max_rounds"`
	Threshold float64 `json:"threshold"`
	Policy    string  `json:"policy"`
}

// DesignCritiqueState is what the critique panel shows.
type DesignCritiqueState struct {
	Exists   bool                   `json:"exists"`
	Critique *DesignCritique        `json:"critique,omitempty"`
	Settings DesignCritiqueSettings `json:"settings"`
	Decision *DesignGateDecision    `json:"decision,omitempty"`
}

// DesignCritiqueReport is the answer of a full critic pass.
type DesignCritiqueReport struct {
	Rendered    bool                   `json:"rendered"`
	RenderError string                 `json:"render_error,omitempty"`
	Critique    DesignCritique         `json:"critique"`
	Decision    DesignGateDecision     `json:"decision"`
	Settings    DesignCritiqueSettings `json:"settings"`
}

// DesignEvent is a lifecycle event of /design/events.
type DesignEvent struct {
	Kind       string `json:"kind"`
	ArtifactID string `json:"artifact_id"`
	Title      string `json:"title,omitempty"`
}

// Design event kinds.
const (
	DesignEventCreated  = "design.created"
	DesignEventVersion  = "design.version"
	DesignEventRender   = "design.render"
	DesignEventCritique = "design.critique"
)

// DesignExport is the result of an export request.
type DesignExport struct {
	Path        string
	Bytes       int64
	Note        string
	DownloadURL string // path below the server root, with ?...
}

// DesignStatus reports whether the Design Studio is on. Old servers without
// the route are reported as disabled.
func (c *Client) DesignStatus(ctx context.Context) (DesignStatus, error) {
	var out DesignStatus
	if err := c.Get(ctx, "/api/v1/design/status", &out); err != nil {
		if IsStatus(err, 404) {
			return DesignStatus{}, nil
		}
		return out, err
	}
	return out, nil
}

// DesignArtifacts lists the artifacts of the project.
func (c *Client) DesignArtifacts(ctx context.Context) ([]DesignArtifact, error) {
	var out struct {
		Artifacts []DesignArtifact `json:"artifacts"`
	}
	err := c.Get(ctx, "/api/v1/design/artifacts", &out)
	return out.Artifacts, err
}

// DesignArtifact fetches one artifact.
func (c *Client) DesignArtifact(ctx context.Context, id string) (DesignArtifact, error) {
	var out DesignArtifact
	err := c.Get(ctx, "/api/v1/design/artifacts/"+pathEscape(id), &out)
	return out, err
}

// DesignVersions lists the versions of an artifact (server order).
func (c *Client) DesignVersions(ctx context.Context, id string) ([]DesignVersion, error) {
	var out struct {
		Versions []DesignVersion `json:"versions"`
	}
	err := c.Get(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/versions", &out)
	return out.Versions, err
}

// DesignNodesQuery filters the structure index.
type DesignNodesQuery struct {
	NodeID string
	Text   string
	Slide  *int
	Styles bool
	Limit  int
}

// DesignNodes reads the structure index of the last render.
func (c *Client) DesignNodes(ctx context.Context, id string, q DesignNodesQuery) ([]DesignNode, int, error) {
	v := url.Values{}
	if q.NodeID != "" {
		v.Set("node_id", q.NodeID)
	}
	if q.Text != "" {
		v.Set("text", q.Text)
	}
	if q.Slide != nil {
		v.Set("slide", strconv.Itoa(*q.Slide))
	}
	if q.Styles {
		v.Set("styles", "1")
	}
	if q.Limit <= 0 {
		q.Limit = 300
	}
	v.Set("limit", strconv.Itoa(q.Limit))
	var out struct {
		Nodes []DesignNode `json:"nodes"`
		Total int          `json:"total"`
	}
	err := c.Get(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/nodes?"+v.Encode(), &out)
	return out.Nodes, out.Total, err
}

// DesignRender re-renders an artifact and rebuilds its structure index.
func (c *Client) DesignRender(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/render", map[string]any{}, nil)
}

// DesignCheckout restores the artifact directory to a version.
func (c *Client) DesignCheckout(ctx context.Context, id string, version int) (DesignArtifact, error) {
	var out DesignArtifact
	err := c.Post(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/checkout", map[string]int{"version": version}, &out)
	return out, err
}

// DesignExportArtifact writes an export (html, png or pdf) on the server.
// slide selects the deck slide of a PNG (0: whole document).
func (c *Client) DesignExportArtifact(ctx context.Context, id, format string, slide int) (DesignExport, error) {
	var out struct {
		Export struct {
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
			Note  string `json:"note"`
		} `json:"export"`
		DownloadURL string `json:"download_url"`
	}
	err := c.Post(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/export",
		map[string]any{"format": format, "slide": slide}, &out)
	return DesignExport{Path: out.Export.Path, Bytes: out.Export.Bytes, Note: out.Export.Note, DownloadURL: out.DownloadURL}, err
}

// DesignDownload fetches the bytes behind a download_url path.
func (c *Client) DesignDownload(ctx context.Context, downloadPath string) ([]byte, error) {
	return c.getBytes(ctx, downloadPath)
}

// DesignScreenshot fetches the live screenshot of an artifact (PNG). A server
// without a headless browser answers 503.
func (c *Client) DesignScreenshot(ctx context.Context, id string) ([]byte, error) {
	return c.getBytes(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/screenshot")
}

func (c *Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.send(ctx, "GET", path, nil, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// DesignCanvasURL returns the address of the read-only canvas of every
// artifact, and how many artboards it holds.
func (c *Client) DesignCanvasURL(ctx context.Context) (string, int, error) {
	var out struct {
		URL       string `json:"url"`
		Artboards int    `json:"artboards"`
	}
	err := c.Get(ctx, "/api/v1/design/canvas", &out)
	return out.URL, out.Artboards, err
}

// DesignTemplates lists the design skills and the craft references.
func (c *Client) DesignTemplates(ctx context.Context) ([]DesignTemplate, []string, error) {
	var out struct {
		Skills []DesignTemplate `json:"skills"`
		Craft  []string         `json:"craft"`
	}
	err := c.Get(ctx, "/api/v1/design/skills", &out)
	return out.Skills, out.Craft, err
}

// DesignInstallTemplate copies a template into the skills directory.
func (c *Client) DesignInstallTemplate(ctx context.Context, name, scope string) error {
	if scope == "" {
		scope = "project"
	}
	return c.Post(ctx, "/api/v1/design/skills/"+pathEscape(name)+"/install", map[string]string{"scope": scope}, nil)
}

// DesignCritique reads the latest critique of an artifact.
func (c *Client) DesignCritique(ctx context.Context, id string) (DesignCritiqueState, error) {
	var out DesignCritiqueState
	err := c.Get(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/critique", &out)
	return out, err
}

// DesignRunCritique runs a critic pass.
func (c *Client) DesignRunCritique(ctx context.Context, id, policy string) (DesignCritiqueReport, error) {
	var out DesignCritiqueReport
	err := c.Post(ctx, "/api/v1/design/artifacts/"+pathEscape(id)+"/critique", map[string]string{"policy": policy}, &out)
	return out, err
}

// SubscribeDesignEvents follows /design/events until ctx ends (reconnecting).
func (c *Client) SubscribeDesignEvents(ctx context.Context, fn func(DesignEvent)) error {
	return c.Subscribe(ctx, "/api/v1/design/events", func(f SSEFrame) {
		if f.Event == "connected" || f.Data == "" {
			return
		}
		var ev DesignEvent
		if json.Unmarshal([]byte(f.Data), &ev) != nil {
			return
		}
		if ev.Kind == "" {
			ev.Kind = f.Event
		}
		fn(ev)
	})
}

// DesignTokenURL returns an absolute URL for path carrying the API token as a
// query parameter, for pages that cannot send headers (downloads, embedded
// web views).
func (c *Client) DesignTokenURL(ctx context.Context, path string) (string, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return "", err
	}
	sep := "?"
	if u, err := url.Parse(path); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	return c.URL(path) + sep + "token=" + url.QueryEscape(tok), nil
}
