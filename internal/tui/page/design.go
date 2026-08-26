package page

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/snapshot"
	tuiimage "github.com/digiogithub/pando/internal/tui/image"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

// DesignPageModel is the public interface for the design page.
type DesignPageModel interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

// The TUI design page is deliberately read-and-open, not an editor: artifacts
// are authored by asking the agent in the chat tab. What a terminal adds over
// the WebUI is that it is already open — so this page answers "what exists,
// what changed, and give me a URL" without leaving it.
type designPage struct {
	sessionID     string
	width, height int

	artifacts []design.Artifact
	versions  []design.Version
	diff      []snapshot.DiffEntry
	shot      image.Image

	selected int
	loading  bool
	busy     string
	err      error

	// systemView swaps the detail column for the shared design system. The
	// system is a project-wide setting, not a property of the selected
	// artifact, so it gets the panel rather than a line in the detail list.
	systemView   bool
	system       *design.DesignSystem
	systemExists bool

	// galleryView swaps the detail column for the bundled design templates.
	// It is read-only: starting an artifact from a template needs a brief, and
	// a brief is typed to the agent, not picked from a list.
	galleryView bool
	templates   []design.Template

	// issuesView expands the findings of the selected version's last critic
	// pass. The critique already travels with the version list, so this costs
	// nothing to show and needs no browser — unlike running a new pass, which
	// belongs to the agent, not to a key in a list.
	issuesView bool
}

type designSystemMsg struct {
	system design.DesignSystem
	exists bool
	err    error
}

type designArtifactsMsg struct {
	artifacts []design.Artifact
	err       error
}

type designDetailMsg struct {
	artifactID string
	versions   []design.Version
	err        error
}

type designShotMsg struct {
	artifactID string
	img        image.Image
	err        error
}

type designDiffMsg struct {
	artifactID string
	entries    []snapshot.DiffEntry
	err        error
}

type designOpenedMsg struct {
	url string
	err error
}

// NewDesignPage creates the design page. The service is resolved per command
// rather than captured: the design provider is installed during application
// start-up, which can happen after the TUI model is constructed.
func NewDesignPage(sessionID string) DesignPageModel {
	return &designPage{sessionID: sessionID, loading: true}
}

func (p *designPage) service() (*design.Service, error) {
	return design.ServiceFor(p.sessionID)
}

func (p *designPage) Init() tea.Cmd { return p.loadArtifacts() }

// Refresh re-reads the artifact list every time the page is opened. Artifacts
// are created by the agent while the user is on the chat page, so a list built
// once at Init would be wrong exactly when the user comes here to look at what
// was just made.
func (p *designPage) Refresh() tea.Cmd {
	p.clearDetail()
	return p.loadArtifacts()
}

func (p *designPage) loadArtifacts() tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designArtifactsMsg{err: err}
		}
		artifacts, err := svc.List(context.Background(), false)
		return designArtifactsMsg{artifacts: artifacts, err: err}
	}
}

// loadSystem reads the shared design system. It is cheap — one file — so it is
// re-read every time the panel is opened rather than cached: the agent edits
// tokens while the user is on another page.
func (p *designPage) loadSystem() tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designSystemMsg{err: err}
		}
		ds, exists, err := svc.LoadSystem()
		return designSystemMsg{system: ds, exists: exists, err: err}
	}
}

// adoptExample replaces the project design system with one extracted from a
// bundled style guide. This is the TUI's "pick the active design system": the
// guides are the only systems that exist without a project to extract from.
func (p *designPage) adoptExample(name string) tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designSystemMsg{err: err}
		}
		result, err := svc.ExtractSystem(context.Background(), design.ExtractOptions{
			Source: design.SourceText,
			Target: name,
		})
		if err != nil {
			return designSystemMsg{err: err}
		}
		if _, _, err := svc.SaveSystem(result.System); err != nil {
			return designSystemMsg{err: err}
		}
		// The knowledge-base mirror is a convenience; failing to publish must
		// not undo a system that is already written to disk.
		_, _ = svc.MirrorSystem(context.Background(), result.System, result.Source, result.Target)
		return designSystemMsg{system: result.System, exists: true}
	}
}

func (p *designPage) loadDetail(artifactID string) tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designDetailMsg{artifactID: artifactID, err: err}
		}
		versions, err := svc.Versions(context.Background(), artifactID)
		return designDetailMsg{artifactID: artifactID, versions: versions, err: err}
	}
}

// loadScreenshot is bound to a key rather than run on selection: a screenshot
// costs a headless browser render, and arrowing through a list of artifacts
// would fire one per keystroke.
func (p *designPage) loadScreenshot(artifact design.Artifact) tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designShotMsg{artifactID: artifact.ID, err: err}
		}
		renderer := svc.Renderer()
		if renderer == nil {
			return designShotMsg{artifactID: artifact.ID, err: design.ErrNoBrowser}
		}
		png, err := renderer.Screenshot(context.Background(), artifact, design.ScreenshotOptions{})
		if err != nil {
			return designShotMsg{artifactID: artifact.ID, err: err}
		}
		img, _, err := image.Decode(bytes.NewReader(png))
		return designShotMsg{artifactID: artifact.ID, img: img, err: err}
	}
}

func (p *designPage) loadDiff(artifact design.Artifact) tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designDiffMsg{artifactID: artifact.ID, err: err}
		}
		if artifact.CurrentVersion < 2 {
			return designDiffMsg{artifactID: artifact.ID, err: fmt.Errorf("design: v1 has nothing to diff against")}
		}
		entries, err := svc.Diff(context.Background(), artifact.ID, artifact.CurrentVersion-1, artifact.CurrentVersion)
		return designDiffMsg{artifactID: artifact.ID, entries: entries, err: err}
	}
}

// openArtifact goes through LiveURL, so pressing `o` in a TUI with no API
// server starts the loopback preview instead of handing the browser a file://
// address whose relative assets and bridge do not work.
func (p *designPage) openArtifact(artifact design.Artifact) tea.Cmd {
	return func() tea.Msg {
		svc, err := p.service()
		if err != nil {
			return designOpenedMsg{err: err}
		}
		presentation, err := svc.LiveURL(context.Background(), artifact.ID, 0)
		if err != nil {
			return designOpenedMsg{err: err}
		}
		if err := auth.OpenBrowser(presentation.URL); err != nil {
			return designOpenedMsg{url: presentation.URL, err: err}
		}
		return designOpenedMsg{url: presentation.URL}
	}
}

func (p *designPage) current() (design.Artifact, bool) {
	if p.selected < 0 || p.selected >= len(p.artifacts) {
		return design.Artifact{}, false
	}
	return p.artifacts[p.selected], true
}

func (p *designPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil

	case designArtifactsMsg:
		p.loading = false
		p.err = msg.err
		p.artifacts = msg.artifacts
		if p.selected >= len(p.artifacts) {
			p.selected = 0
		}
		if artifact, ok := p.current(); ok {
			return p, p.loadDetail(artifact.ID)
		}
		return p, nil

	case designDetailMsg:
		if artifact, ok := p.current(); !ok || artifact.ID != msg.artifactID {
			return p, nil // A stale reply for an artifact no longer selected.
		}
		p.err = msg.err
		p.versions = msg.versions
		return p, nil

	case designShotMsg:
		p.busy = ""
		if artifact, ok := p.current(); !ok || artifact.ID != msg.artifactID {
			return p, nil
		}
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		p.shot = msg.img
		return p, nil

	case designDiffMsg:
		p.busy = ""
		if artifact, ok := p.current(); !ok || artifact.ID != msg.artifactID {
			return p, nil
		}
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		p.diff = msg.entries
		return p, nil

	case designOpenedMsg:
		p.busy = ""
		if msg.err != nil {
			if msg.url != "" {
				return p, util.ReportInfo("Could not open a browser. URL: " + msg.url)
			}
			return p, util.ReportError(msg.err)
		}
		return p, util.ReportInfo("Opened " + msg.url)

	case designSystemMsg:
		p.busy = ""
		if msg.err != nil {
			p.err = msg.err
			return p, util.ReportError(msg.err)
		}
		system := msg.system
		p.system, p.systemExists = &system, msg.exists
		return p, nil

	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	return p, nil
}

func (p *designPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if p.selected > 0 {
			p.selected--
			p.clearDetail()
			if artifact, ok := p.current(); ok {
				return p, p.loadDetail(artifact.ID)
			}
		}
	case "down", "j":
		if p.selected < len(p.artifacts)-1 {
			p.selected++
			p.clearDetail()
			if artifact, ok := p.current(); ok {
				return p, p.loadDetail(artifact.ID)
			}
		}
	case "r":
		p.loading = true
		p.clearDetail()
		return p, p.loadArtifacts()
	case "o":
		if artifact, ok := p.current(); ok {
			p.busy = "Opening preview..."
			return p, p.openArtifact(artifact)
		}
	case "s":
		if artifact, ok := p.current(); ok {
			p.busy = "Rendering screenshot..."
			return p, p.loadScreenshot(artifact)
		}
	case "d":
		if artifact, ok := p.current(); ok {
			p.busy = "Diffing versions..."
			return p, p.loadDiff(artifact)
		}
	case "y":
		p.systemView = !p.systemView
		if p.systemView {
			p.galleryView = false
			p.issuesView = false
			return p, p.loadSystem()
		}
	case "c":
		p.issuesView = !p.issuesView
		if p.issuesView {
			p.systemView = false
			p.galleryView = false
		}
	case "g":
		p.galleryView = !p.galleryView
		if p.galleryView {
			p.systemView = false
			p.issuesView = false
			// The bundles are embedded in the binary, so there is nothing to
			// load asynchronously and nothing that can fail at this point.
			if templates, err := design.BundledTemplates(); err == nil {
				p.templates = templates
			}
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Digits only mean anything while the system panel is open, so they
		// stay available to whatever the rest of the page wants them for.
		if !p.systemView {
			return p, nil
		}
		names := design.ExampleSystemNames()
		idx := int(msg.String()[0] - '1')
		if idx < len(names) {
			p.busy = "Adopting " + names[idx] + "..."
			return p, p.adoptExample(names[idx])
		}
	}
	return p, nil
}

func (p *designPage) clearDetail() {
	p.versions = nil
	p.diff = nil
	p.shot = nil
}

func (p *designPage) BindingKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select artifact")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open preview in browser")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "render screenshot")),
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "design system")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "last critique findings")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff last two versions")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
	}
}

func (p *designPage) GetSize() (int, int) { return p.width, p.height }

func (p *designPage) SetSize(width, height int) tea.Cmd {
	p.width, p.height = width, height
	return nil
}

func (p *designPage) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle().Width(p.width).Height(p.height)

	if p.loading {
		return base.Render(lipgloss.NewStyle().
			Foreground(t.TextMuted()).Background(t.Background()).Italic(true).Padding(1, 2).
			Render("Loading design artifacts..."))
	}
	if p.err != nil {
		return base.Render(lipgloss.NewStyle().
			Foreground(t.Error()).Background(t.Background()).Padding(1, 2).
			Render(p.designErrorText()))
	}
	if len(p.artifacts) == 0 {
		return base.Render(lipgloss.NewStyle().
			Foreground(t.TextMuted()).Background(t.Background()).Padding(1, 2).
			Render("No design artifacts yet.\n\nAsk the agent in the chat tab to design something;\nartifacts appear here as soon as they are created."))
	}

	listWidth := p.width / 3
	if listWidth < 24 {
		listWidth = 24
	}
	if listWidth > 48 {
		listWidth = 48
	}
	detailWidth := p.width - listWidth - 4

	border := func(focused bool) lipgloss.Style {
		colour := t.BorderNormal()
		if focused {
			colour = t.BorderFocused()
		}
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colour).
			BorderBackground(t.Background()).
			Background(t.Background())
	}

	bodyHeight := p.height - lipgloss.Height(p.helpBar()) - 2
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	panels := lipgloss.JoinHorizontal(lipgloss.Top,
		border(true).Width(listWidth).Height(bodyHeight).Render(p.renderList(listWidth)),
		border(false).Width(detailWidth).Height(bodyHeight).Render(p.renderDetail(detailWidth)),
	)
	return base.Render(lipgloss.JoinVertical(lipgloss.Left, panels, p.helpBar()))
}

func (p *designPage) designErrorText() string {
	if strings.Contains(p.err.Error(), design.ErrNoProvider.Error()) {
		return "The design subsystem is not available in this process."
	}
	return p.err.Error()
}

func (p *designPage) renderList(width int) string {
	t := theme.CurrentTheme()
	selected := lipgloss.NewStyle().Foreground(t.Background()).Background(t.Primary()).Bold(true)
	normal := lipgloss.NewStyle().Foreground(t.Text()).Background(t.Background())
	muted := lipgloss.NewStyle().Foreground(t.TextMuted()).Background(t.Background())

	lines := []string{muted.Render("ARTIFACTS"), ""}
	for i, a := range p.artifacts {
		label := fmt.Sprintf(" %-*s v%d ", width-8, truncate(a.Slug, width-8), a.CurrentVersion)
		if i == p.selected {
			lines = append(lines, selected.Render(label))
		} else {
			lines = append(lines, normal.Render(label))
		}
		lines = append(lines, muted.Render(fmt.Sprintf("   %s", a.Kind)))
	}
	return strings.Join(lines, "\n")
}

func (p *designPage) renderDetail(width int) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted()).Background(t.Background())
	normal := lipgloss.NewStyle().Foreground(t.Text()).Background(t.Background())

	if p.systemView {
		return p.renderSystem(width)
	}
	if p.galleryView {
		return p.renderGallery(width)
	}

	artifact, ok := p.current()
	if !ok {
		return ""
	}

	sections := []string{
		normal.Bold(true).Render(artifact.Title),
		muted.Render(fmt.Sprintf("%s · %s · v%d · %s", artifact.Kind, artifact.Slug, artifact.CurrentVersion, artifact.Dir)),
		"",
	}
	if p.busy != "" {
		sections = append(sections, muted.Italic(true).Render(p.busy), "")
	}

	if p.shot != nil {
		previewWidth := width - 4
		if previewWidth > 72 {
			previewWidth = 72
		}
		if previewWidth > 8 {
			sections = append(sections, tuiimage.ToString(previewWidth, p.shot), "")
		}
	}

	sections = append(sections, muted.Render("VERSIONS"))
	if len(p.versions) == 0 {
		sections = append(sections, muted.Render("  (none)"))
	}
	for _, v := range p.versions {
		marker := " "
		if v.Number == artifact.CurrentVersion {
			marker = "*"
		}
		summary := v.Summary
		if summary == "" {
			summary = "(no summary)"
		}
		score := ""
		if v.Critique != nil {
			score = fmt.Sprintf(" %.1f/10", v.Critique.Score)
		}
		sections = append(sections, normal.Render(fmt.Sprintf(" %s v%-3d%s %s",
			marker, v.Number, score, truncate(summary, width-20))))
	}

	if p.issuesView {
		sections = append(sections, "", p.renderIssues(width, artifact))
	}

	if p.diff != nil {
		sections = append(sections, "", muted.Render(fmt.Sprintf("CHANGES v%d → v%d", artifact.CurrentVersion-1, artifact.CurrentVersion)))
		if len(p.diff) == 0 {
			sections = append(sections, muted.Render("  (identical)"))
		}
		for _, entry := range p.diff {
			sections = append(sections, normal.Render(fmt.Sprintf("  %-8s %s", entry.Type, truncate(entry.Path, width-14))))
		}
	}
	return strings.Join(sections, "\n")
}

// renderIssues lists the findings of the last critic pass over the current
// version. It reads what is already on record rather than running a pass: a
// pass renders a browser, and a list view is not where that belongs.
func (p *designPage) renderIssues(width int, artifact design.Artifact) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted()).Background(t.Background())
	normal := lipgloss.NewStyle().Foreground(t.Text()).Background(t.Background())

	var critique *design.Critique
	for _, v := range p.versions {
		if v.Number == artifact.CurrentVersion && v.Critique != nil {
			critique = v.Critique
			break
		}
	}
	if critique == nil {
		return muted.Render("FINDINGS\n  v" + fmt.Sprint(artifact.CurrentVersion) +
			" has not been critiqued yet — ask the agent to run design_critique on it")
	}

	lines := []string{muted.Render(fmt.Sprintf("FINDINGS · v%d scored %.1f/10",
		critique.Version, critique.Score))}
	if critique.Summary != "" {
		lines = append(lines, muted.Render("  "+truncate(critique.Summary, width-4)))
	}
	if len(critique.Issues) == 0 {
		lines = append(lines, normal.Render("  (none)"))
		return strings.Join(lines, "\n")
	}
	for _, issue := range critique.Issues {
		anchor := issue.NodeID
		if anchor == "" {
			anchor = "-"
		}
		lines = append(lines, normal.Render(fmt.Sprintf("  %-8s %-8s %s",
			issue.Severity, anchor, truncate(issue.Message, width-22))))
	}
	return strings.Join(lines, "\n")
}

// renderSystem shows the project design system and the bundled guides that can
// replace it. The guides are numbered because adopting one is the only write
// this page performs, and a number is a deliberate keystroke in a way that a
// cursor landing on a row is not.
func (p *designPage) renderSystem(width int) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted()).Background(t.Background())
	normal := lipgloss.NewStyle().Foreground(t.Text()).Background(t.Background())

	if p.system == nil {
		return muted.Render("Loading the design system...")
	}
	lines := []string{muted.Render("DESIGN SYSTEM"), ""}
	lines = append(lines, normal.Render("  "+p.system.Name))
	if !p.systemExists {
		lines = append(lines, muted.Render("  not committed yet — these are the defaults"))
	}
	lines = append(lines, "")
	for _, group := range design.SortedTokenGroups(p.system.Tokens) {
		for _, name := range design.SortedTokenNames(p.system.Tokens[group]) {
			token := fmt.Sprintf("--%s-%s", group, name)
			lines = append(lines, normal.Render(fmt.Sprintf("  %-18s %s",
				token, truncate(p.system.Tokens[group][name], width-22))))
		}
	}
	names := design.ExampleSystemNames()
	if len(names) > 0 {
		lines = append(lines, "", muted.Render("REPLACE WITH A BUNDLED GUIDE"))
		for i, name := range names {
			if i >= 9 {
				break
			}
			lines = append(lines, normal.Render(fmt.Sprintf("  %d  %-12s %s",
				i+1, name, truncate(design.ExampleSystemTitle(name), width-20))))
		}
	}
	return strings.Join(lines, "\n")
}

// renderGallery lists the bundled design templates. Each entry shows the brief
// it expects, because the template is only half of the input: the other half is
// what the user says, and showing an example is the fastest way to say so.
func (p *designPage) renderGallery(width int) string {
	t := theme.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(t.TextMuted()).Background(t.Background())
	normal := lipgloss.NewStyle().Foreground(t.Text()).Background(t.Background())

	if len(p.templates) == 0 {
		return muted.Render("No design templates are bundled with this build.")
	}
	lines := []string{muted.Render("DESIGN TEMPLATES"), ""}
	for _, tpl := range p.templates {
		label := tpl.Name
		if tpl.Startable {
			label = fmt.Sprintf("%s [%s]", tpl.Name, tpl.Kind)
		}
		lines = append(lines, normal.Render("  "+label))
		lines = append(lines, muted.Render("  "+truncate(tpl.Description, width-4)))
		if tpl.RequiresSystem {
			lines = append(lines, muted.Render("  needs a committed design system"))
		}
		if tpl.ExamplePrompt != "" {
			lines = append(lines, muted.Render("  try: "+truncate(tpl.ExamplePrompt, width-8)))
		}
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render("Ask the agent for one by name; it scaffolds the artifact."))
	return strings.Join(lines, "\n")
}

func (p *designPage) helpBar() string {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Foreground(t.TextMuted()).Background(t.Background()).Padding(0, 1).
		Render("↑/↓ select · o open · s screenshot · c findings · d diff · y system · g templates · r reload")
}

func truncate(s string, width int) string {
	if width < 4 || len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}
