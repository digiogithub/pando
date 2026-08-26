package design

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/digiogithub/pando/internal/browser"
)

// DefaultSlideSelector matches the slide containers a deck is expected to use.
const DefaultSlideSelector = "[data-slide], .slide, section.slide"

// DefaultStyleProps is the computed-style subset carried in the node index. It
// is deliberately short: the index is fed to a model, so every extra property
// costs tokens on every node.
var DefaultStyleProps = []string{
	"display", "position", "color", "background-color",
	"font-family", "font-size", "font-weight", "line-height",
	"margin", "padding", "border-radius", "text-align",
}

// defaultMaxNodes bounds the index of a single render.
const defaultMaxNodes = 400

// defaultMaxDepth bounds how deep the walk descends.
const defaultMaxDepth = 14

// RenderOptions configures one render.
type RenderOptions struct {
	// URL overrides the document to load. Empty renders the artifact's entry
	// document from disk (file://); the preview server takes over in P3.
	URL string
	// Viewport overrides the artifact manifest viewport.
	Viewport Viewport
	// Wait is an extra settle delay after the load event, for pages that build
	// themselves in script.
	Wait time.Duration
	// MaxNodes and MaxDepth bound the structure index.
	MaxNodes int
	MaxDepth int
	// StyleProps overrides the computed-style subset.
	StyleProps []string
	// SlideSelector overrides the deck slide selector.
	SlideSelector string
	// PrintMedia renders under print emulation instead of screen.
	PrintMedia bool
}

// RenderResult is what one render reports back to the agent.
type RenderResult struct {
	URL       string           `json:"url"`
	Title     string           `json:"title"`
	Viewport  Viewport         `json:"viewport"`
	Slides    int              `json:"slides"`
	Nodes     []Node           `json:"nodes"`
	Truncated bool             `json:"truncated,omitempty"`
	Console   []ConsoleEntry   `json:"console,omitempty"`
	Failures  []NetworkFailure `json:"failures,omitempty"`
	Height    float64          `json:"height"`
	// Width is the document scroll width, which is how a horizontal overflow
	// is detected: a page wider than its own viewport.
	Width float64 `json:"width,omitempty"`
	// Facts carries the per-node accessibility and typography detail the
	// quality audit runs on. It is never persisted and never travels to a
	// model as part of the structure index: it exists for the lifetime of one
	// render, so the index itself stays as small as the inspector needs.
	Facts []NodeFacts `json:"-"`
}

// NodeFacts is what the audit needs to know about one rendered element beyond
// what the node index stores.
type NodeFacts struct {
	NodeID string `json:"node_id"`
	Tag    string `json:"tag"`
	// Name is an approximation of the accessible name: enough to tell a named
	// control from an unnamed one.
	Name string `json:"name"`
	// AltPresent distinguishes an image with alt="" — decorative on purpose —
	// from one that simply has no alt attribute at all.
	AltPresent   bool    `json:"alt_present"`
	HeadingLevel int     `json:"heading_level"`
	Interactive  bool    `json:"interactive"`
	AriaHidden   bool    `json:"aria_hidden"`
	HasText      bool    `json:"has_text"`
	Color        string  `json:"color"`
	Background   string  `json:"background"`
	FontSize     float64 `json:"font_size"`
	FontWeight   int     `json:"font_weight"`
	// Slide and Box are copied from the matching node so a rule can report a
	// finding without a second lookup.
	Slide int  `json:"slide"`
	Box   Rect `json:"box"`
}

// SlideBreak reports how one slide behaves under print emulation.
type SlideBreak struct {
	Index      int     `json:"index"`
	BreakAfter string  `json:"break_after"`
	Height     float64 `json:"height"`
}

// Renderer drives a headless browser over design artifacts: render + index,
// screenshots, PDF printing and canvas rasterization.
type Renderer struct {
	layout  Layout
	session *browserSession
}

// NewRenderer builds a renderer. The browser is started lazily on first use, so
// constructing one is free on machines with no Chromium.
func NewRenderer(layout Layout, opts BrowserOptions) *Renderer {
	return &Renderer{layout: layout, session: newBrowserSession(opts)}
}

// Close releases the browser.
func (r *Renderer) Close() { r.session.Close() }

// Available reports whether a usable browser can be resolved, without starting
// one. Callers use it to degrade instead of failing a whole design flow.
func (r *Renderer) Available() bool {
	install, ok := browser.ResolveBrowserInstall(r.session.opts.Type, r.session.opts.Executable)
	return ok && !browser.IsRemoteBrowserType(install.Type)
}

// EntryURL returns the file:// URL of an artifact's entry document.
func (r *Renderer) EntryURL(a Artifact) (string, error) {
	absDir, err := r.layout.AbsDir(a.Dir)
	if err != nil {
		return "", err
	}
	manifest, err := ReadManifest(absDir)
	entry := "index.html"
	if err == nil {
		entry = manifest.Entry
	} else if !os.IsNotExist(err) {
		return "", err
	}
	target := filepath.Join(absDir, filepath.FromSlash(entry))
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("design: entry document %s: %w", entry, err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(target)}).String(), nil
}

// Render loads an artifact, stamps data-pando-id on its elements and returns
// the structure index together with whatever the page logged or failed to load.
func (r *Renderer) Render(ctx context.Context, a Artifact, opts RenderOptions) (RenderResult, error) {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()

	browserCtx, err := r.session.context()
	if err != nil {
		return RenderResult{}, err
	}

	target := opts.URL
	if target == "" {
		if target, err = r.EntryURL(a); err != nil {
			return RenderResult{}, err
		}
	}
	viewport := r.viewportFor(a, opts)
	opts = normalizeRenderOptions(opts, a)

	r.session.resetEvents()

	actions := []chromedp.Action{
		emulation.SetDeviceMetricsOverride(int64(viewport.W), int64(viewport.H), 1, false),
	}
	if opts.PrintMedia {
		actions = append(actions, emulation.SetEmulatedMedia().WithMedia("print"))
	}
	actions = append(actions,
		chromedp.Navigate(target),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
	if opts.Wait > 0 {
		actions = append(actions, chromedp.Sleep(opts.Wait))
	}

	var raw indexPayload
	slideSelector := ""
	if a.Kind == KindDeck {
		slideSelector = opts.SlideSelector
	}
	script, err := buildIndexScript(opts, slideSelector)
	if err != nil {
		return RenderResult{}, err
	}
	actions = append(actions, chromedp.Evaluate(script, &raw))

	runCtx, cancel := runContext(ctx, browserCtx)
	defer cancel()
	if err := chromedp.Run(runCtx, actions...); err != nil {
		return RenderResult{}, fmt.Errorf("design: render %s: %w", target, err)
	}

	console, failures := r.session.takeEvents()
	result := RenderResult{
		URL:       target,
		Title:     raw.Title,
		Viewport:  viewport,
		Slides:    raw.Slides,
		Truncated: raw.Truncated,
		Console:   console,
		Failures:  failures,
		Height:    raw.ScrollHeight,
		Width:     raw.ScrollWidth,
		Nodes:     make([]Node, 0, len(raw.Nodes)),
	}
	for _, n := range raw.Nodes {
		result.Nodes = append(result.Nodes, Node{
			ArtifactID: a.ID,
			Version:    a.CurrentVersion,
			NodeID:     n.NodeID,
			ParentID:   n.ParentID,
			Selector:   n.Selector,
			Role:       n.Role,
			Text:       n.Text,
			Slide:      n.Slide,
			Box:        Rect{X: round2(n.Box.X), Y: round2(n.Box.Y), W: round2(n.Box.W), H: round2(n.Box.H)},
			Styles:     n.Styles,
		})
	}

	boxes := make(map[string]Node, len(result.Nodes))
	for _, n := range result.Nodes {
		boxes[n.NodeID] = n
	}
	result.Facts = make([]NodeFacts, 0, len(raw.Facts))
	for _, f := range raw.Facts {
		node := boxes[f.NodeID]
		result.Facts = append(result.Facts, NodeFacts{
			NodeID:       f.NodeID,
			Tag:          f.Tag,
			Name:         f.Name,
			AltPresent:   f.AltPresent,
			HeadingLevel: f.HeadingLevel,
			Interactive:  f.Interactive,
			AriaHidden:   f.AriaHidden,
			HasText:      f.HasText,
			Color:        f.Color,
			Background:   f.Background,
			FontSize:     f.FontSize,
			FontWeight:   f.FontWeight,
			Slide:        node.Slide,
			Box:          node.Box,
		})
	}
	return result, nil
}

// ScreenshotOptions selects what a screenshot captures.
type ScreenshotOptions struct {
	RenderOptions
	// Selector captures a single element instead of the page.
	Selector string
	// Slide captures one deck slide; -1 for the whole document.
	Slide int
	// FullPage captures beyond the viewport.
	FullPage bool
	// Quality is the PNG/JPEG quality passed to the browser (0 keeps the
	// browser default).
	Quality int
}

// Screenshot renders the artifact and captures a PNG.
func (r *Renderer) Screenshot(ctx context.Context, a Artifact, opts ScreenshotOptions) ([]byte, error) {
	if _, err := r.Render(ctx, a, opts.RenderOptions); err != nil {
		return nil, err
	}

	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	browserCtx, err := r.session.context()
	if err != nil {
		return nil, err
	}

	runCtx, cancel := runContext(ctx, browserCtx)
	defer cancel()

	var buf []byte
	switch {
	case opts.Selector != "":
		if err := chromedp.Run(runCtx, chromedp.Screenshot(opts.Selector, &buf, chromedp.ByQuery)); err != nil {
			return nil, fmt.Errorf("design: screenshot %q: %w", opts.Selector, err)
		}
	case opts.Slide >= 0 && a.Kind == KindDeck:
		selector := opts.SlideSelector
		if selector == "" {
			selector = DefaultSlideSelector
		}
		clip, err := r.slideClip(runCtx, selector, opts.Slide)
		if err != nil {
			return nil, err
		}
		if err := chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			data, err := page.CaptureScreenshot().WithClip(clip).WithCaptureBeyondViewport(true).Do(ctx)
			buf = data
			return err
		})); err != nil {
			return nil, fmt.Errorf("design: screenshot slide %d: %w", opts.Slide, err)
		}
	case opts.FullPage:
		quality := opts.Quality
		if quality <= 0 {
			quality = 90
		}
		if err := chromedp.Run(runCtx, chromedp.FullScreenshot(&buf, quality)); err != nil {
			return nil, fmt.Errorf("design: full-page screenshot: %w", err)
		}
	default:
		if err := chromedp.Run(runCtx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return nil, fmt.Errorf("design: screenshot: %w", err)
		}
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("design: screenshot produced no image")
	}
	return buf, nil
}

// slideClip resolves the document-space box of one slide.
func (r *Renderer) slideClip(ctx context.Context, selector string, index int) (*page.Viewport, error) {
	encoded, err := json.Marshal(selector)
	if err != nil {
		return nil, fmt.Errorf("design: encode slide selector: %w", err)
	}
	var rect *rawRect
	script := fmt.Sprintf(slideRectScript, encoded, index)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &rect)); err != nil {
		return nil, fmt.Errorf("design: locate slide %d: %w", index, err)
	}
	if rect == nil || rect.W <= 0 || rect.H <= 0 {
		return nil, fmt.Errorf("design: slide %d not found (selector %q)", index, selector)
	}
	return &page.Viewport{X: rect.X, Y: rect.Y, Width: rect.W, Height: rect.H, Scale: 1}, nil
}

// PrintOptions configures PDF export.
type PrintOptions struct {
	RenderOptions
	// Landscape prints in landscape orientation.
	Landscape bool
	// PaperWidth and PaperHeight are in inches; zero uses the page's own CSS
	// size, which is what makes one-slide-per-page decks work.
	PaperWidth  float64
	PaperHeight float64
	// NoBackground omits background graphics. They print by default: a design
	// without its backgrounds is not the design.
	NoBackground bool
}

// PrintPDF renders the artifact under print emulation and prints it to PDF.
// Deck exports rely on the artifact's own @page rules, so preferCSSPageSize is
// always on.
func (r *Renderer) PrintPDF(ctx context.Context, a Artifact, opts PrintOptions) ([]byte, error) {
	renderOpts := opts.RenderOptions
	renderOpts.PrintMedia = true
	if _, err := r.Render(ctx, a, renderOpts); err != nil {
		return nil, err
	}

	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	browserCtx, err := r.session.context()
	if err != nil {
		return nil, err
	}

	runCtx, cancel := runContext(ctx, browserCtx)
	defer cancel()

	var buf []byte
	err = chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		params := page.PrintToPDF().
			WithPrintBackground(!opts.NoBackground).
			WithPreferCSSPageSize(true).
			WithLandscape(opts.Landscape)
		if opts.PaperWidth > 0 && opts.PaperHeight > 0 {
			params = params.WithPaperWidth(opts.PaperWidth).WithPaperHeight(opts.PaperHeight)
		}
		data, _, err := params.Do(ctx)
		buf = data
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("design: print pdf: %w", err)
	}
	return buf, nil
}

// SlideBreaks reports how each slide behaves under print emulation. A deck
// whose slides do not break after each other will not export one slide per
// page.
func (r *Renderer) SlideBreaks(ctx context.Context, a Artifact, opts RenderOptions) ([]SlideBreak, error) {
	opts.PrintMedia = true
	if _, err := r.Render(ctx, a, opts); err != nil {
		return nil, err
	}

	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	browserCtx, err := r.session.context()
	if err != nil {
		return nil, err
	}

	selector := opts.SlideSelector
	if selector == "" {
		selector = DefaultSlideSelector
	}
	encoded, err := json.Marshal(selector)
	if err != nil {
		return nil, fmt.Errorf("design: encode slide selector: %w", err)
	}
	runCtx, cancel := runContext(ctx, browserCtx)
	defer cancel()

	var breaks []SlideBreak
	if err := chromedp.Run(runCtx, chromedp.Evaluate(fmt.Sprintf(printBreakScript, encoded), &breaks)); err != nil {
		return nil, fmt.Errorf("design: inspect slide breaks: %w", err)
	}
	return breaks, nil
}

// Rasterize renders an HTML document that draws into a canvas (or any markup)
// and captures the result as a PNG. This is Pando's image-generation path: the
// browser is the renderer, so no image-model provider is involved.
func (r *Renderer) Rasterize(ctx context.Context, html string, width, height int, wait time.Duration) ([]byte, error) {
	if strings.TrimSpace(html) == "" {
		return nil, fmt.Errorf("design: rasterize needs a document")
	}
	if width <= 0 {
		width = DefaultViewport.W
	}
	if height <= 0 {
		height = DefaultViewport.H
	}

	dir, err := os.MkdirTemp("", "pando-design-canvas-*")
	if err != nil {
		return nil, fmt.Errorf("design: rasterize temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "canvas.html")
	if err := os.WriteFile(target, []byte(html), 0o600); err != nil {
		return nil, fmt.Errorf("design: rasterize write document: %w", err)
	}

	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	browserCtx, err := r.session.context()
	if err != nil {
		return nil, err
	}

	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(target)}).String()
	actions := []chromedp.Action{
		emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false),
		chromedp.Navigate(fileURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	}
	if wait > 0 {
		actions = append(actions, chromedp.Sleep(wait))
	}
	runCtx, cancel := runContext(ctx, browserCtx)
	defer cancel()

	var buf []byte
	actions = append(actions, chromedp.CaptureScreenshot(&buf))
	if err := chromedp.Run(runCtx, actions...); err != nil {
		return nil, fmt.Errorf("design: rasterize: %w", err)
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("design: rasterize produced no image")
	}
	return buf, nil
}

// --- internals ---

type rawRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type rawNode struct {
	NodeID   string            `json:"node_id"`
	ParentID string            `json:"parent_id"`
	Selector string            `json:"selector"`
	Role     string            `json:"role"`
	Text     string            `json:"text"`
	Slide    int               `json:"slide"`
	Box      rawRect           `json:"box"`
	Styles   map[string]string `json:"styles"`
}

type rawFacts struct {
	NodeID       string  `json:"node_id"`
	Tag          string  `json:"tag"`
	Name         string  `json:"name"`
	AltPresent   bool    `json:"alt_present"`
	HeadingLevel int     `json:"heading_level"`
	Interactive  bool    `json:"interactive"`
	AriaHidden   bool    `json:"aria_hidden"`
	HasText      bool    `json:"has_text"`
	Color        string  `json:"color"`
	Background   string  `json:"background"`
	FontSize     float64 `json:"font_size"`
	FontWeight   int     `json:"font_weight"`
}

type indexPayload struct {
	Title        string     `json:"title"`
	Slides       int        `json:"slides"`
	Truncated    bool       `json:"truncated"`
	ScrollHeight float64    `json:"scroll_height"`
	ScrollWidth  float64    `json:"scroll_width"`
	Nodes        []rawNode  `json:"nodes"`
	Facts        []rawFacts `json:"facts"`
}

// buildIndexScript injects the render options into the walker script.
func buildIndexScript(opts RenderOptions, slideSelector string) (string, error) {
	payload := map[string]any{
		"maxNodes":      opts.MaxNodes,
		"maxDepth":      opts.MaxDepth,
		"styleProps":    opts.StyleProps,
		"slideSelector": slideSelector,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("design: encode index options: %w", err)
	}
	return fmt.Sprintf(indexScript, encoded), nil
}

// normalizeRenderOptions fills the bounds and selectors a render needs.
func normalizeRenderOptions(opts RenderOptions, a Artifact) RenderOptions {
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = defaultMaxNodes
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if len(opts.StyleProps) == 0 {
		opts.StyleProps = DefaultStyleProps
	}
	if opts.SlideSelector == "" && a.Kind == KindDeck {
		opts.SlideSelector = DefaultSlideSelector
	}
	return opts
}

// viewportFor resolves the render size: explicit option, then the artifact
// manifest, then the package default.
func (r *Renderer) viewportFor(a Artifact, opts RenderOptions) Viewport {
	if opts.Viewport.W > 0 && opts.Viewport.H > 0 {
		return opts.Viewport
	}
	if absDir, err := r.layout.AbsDir(a.Dir); err == nil {
		if manifest, err := ReadManifest(absDir); err == nil {
			return manifest.Preview.Viewport
		}
	}
	return DefaultViewport
}

// runContext ties a chromedp browser context to the caller's context, so a
// cancelled tool call or an expired deadline actually aborts the page work
// instead of leaving the browser busy.
func runContext(caller, browserCtx context.Context) (context.Context, context.CancelFunc) {
	runCtx, cancel := context.WithCancel(browserCtx)
	done := make(chan struct{})
	go func() {
		select {
		case <-caller.Done():
			cancel()
		case <-done:
		}
	}()
	return runCtx, func() {
		close(done)
		cancel()
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
