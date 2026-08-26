package design

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- colour arithmetic ---

func TestParseColorAcceptsTheFormsStylesheetsActuallyUse(t *testing.T) {
	cases := map[string]string{
		"#fff":              "#ffffff",
		"#C96442":           "#c96442",
		"#2f6febff":         "#2f6feb",
		"rgb(47, 111, 235)": "#2f6feb",
		"rgba(0,0,0,0.5)":   "#000000",
		"white":             "#ffffff",
	}
	for input, want := range cases {
		got, ok := parseColor(input)
		if !ok {
			t.Errorf("parseColor(%q) rejected a valid colour", input)
			continue
		}
		if got.hex() != want {
			t.Errorf("parseColor(%q) = %s, want %s", input, got.hex(), want)
		}
	}
	// A fully transparent colour never shows, so treating it as a palette
	// member would put an invisible value into the token set.
	for _, bad := range []string{"transparent", "rgba(255,0,0,0)", "#00000000", "currentColor", "linear-gradient(red, blue)", ""} {
		if _, ok := parseColor(bad); ok {
			t.Errorf("parseColor(%q) should have been rejected", bad)
		}
	}
}

func TestAssignColorRolesUsesMeasuredProperties(t *testing.T) {
	// Frequency order deliberately puts the accent last: roles must come from
	// luminance and saturation, not from where a colour sits in the list.
	p := palette{
		colors: []rgb{
			{255, 255, 255}, // background
			{20, 20, 19},    // text
			{240, 238, 230}, // surface
			{135, 134, 127}, // muted
			{201, 100, 66},  // accent
		},
		counts: []int{100, 60, 30, 12, 5},
	}
	roles := assignColorRoles(p)
	if roles["bg"] != "#ffffff" {
		t.Errorf("bg = %q, want #ffffff", roles["bg"])
	}
	if roles["text"] != "#141413" {
		t.Errorf("text = %q, want #141413", roles["text"])
	}
	if roles["accent"] != "#c96442" {
		t.Errorf("accent = %q, want #c96442 (the most saturated colour, not the most frequent)", roles["accent"])
	}
	if roles["bg"] == roles["text"] {
		t.Error("bg and text must never be the same colour")
	}
}

func TestAssignColorRolesSkipsRolesItCannotJustify(t *testing.T) {
	// One colour cannot produce a text role: nothing contrasts with it.
	roles := assignColorRoles(palette{colors: []rgb{{255, 255, 255}}, counts: []int{10}})
	if _, ok := roles["text"]; ok {
		t.Error("a single-colour palette must not invent a text colour")
	}
	if roles["bg"] != "#ffffff" {
		t.Errorf("bg = %q, want #ffffff", roles["bg"])
	}
}

// --- extraction ---

func TestExtractFromCodePrefersDeclaredCustomProperties(t *testing.T) {
	svc, project := newTestService(t)
	writeFile(t, filepath.Join(project, "src", "app.css"), `
:root {
  --color-accent: #c96442;
  --space-md: 18px;
}
body { background: #f5f4ed; color: #141413; font-family: "Iowan Old Style", serif; }
.card { background: #f0eee6; border-radius: 12px; padding: 18px; }
.cta { background: #c96442; color: #ffffff; border-radius: 12px; padding: 8px; }
`)
	result, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: SourceCode})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := result.System.Tokens["color"]["accent"]; got != "#c96442" {
		t.Errorf("accent = %q, want the declared --color-accent #c96442", got)
	}
	if got := result.System.Tokens["space"]["md"]; got != "18px" {
		t.Errorf("space-md = %q, want the declared 18px", got)
	}
	if len(result.Scanned) == 0 {
		t.Error("extraction reported no scanned files")
	}
}

func TestExtractFromCodeSkipsDependenciesAndItsOwnOutput(t *testing.T) {
	svc, project := newTestService(t)
	writeFile(t, filepath.Join(project, "app.css"), "body { color: #123456; background: #ffffff; }")
	// Both of these would otherwise dominate the harvest.
	writeFile(t, filepath.Join(project, "node_modules", "framework", "theme.css"),
		strings.Repeat("a { color: #ff00ff; }\n", 50))
	writeFile(t, filepath.Join(project, "designer", "_system", "system.css"),
		strings.Repeat("b { color: #00ff00; }\n", 50))

	result, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: SourceCode})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, f := range result.Scanned {
		if strings.Contains(f, "node_modules") {
			t.Errorf("scanned a dependency: %s", f)
		}
		if strings.Contains(f, "designer/") {
			t.Errorf("re-read its own output: %s", f)
		}
	}
}

func TestExtractFromTextReadsABundledStyleGuide(t *testing.T) {
	svc, _ := newTestService(t)
	result, err := svc.ExtractSystem(context.Background(), ExtractOptions{
		Source: SourceText,
		Target: "claude",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// The guide names #f5f4ed as its canvas and #c96442 as its brand colour, so
	// a harvest that read the prose must contain them somewhere.
	joined := strings.ToLower(strings.Join(flattenTokens(result.System), " "))
	for _, want := range []string{"#f5f4ed", "#c96442"} {
		if !strings.Contains(joined, want) {
			t.Errorf("extracted system has no %s; got %s", want, joined)
		}
	}
	if len(result.Notes) == 0 {
		t.Error("reading prose must report that the roles are a guess")
	}
}

func TestExtractFromTextRejectsAnUnknownTarget(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: SourceText, Target: "nope"})
	if err == nil {
		t.Fatal("expected an error for an unknown style guide")
	}
	// The message must list what is available, or the user has to guess twice.
	for _, name := range ExampleSystemNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention the bundled guide %q", err, name)
		}
	}
}

func TestExtractFromImageReportsWhatAnImageCannotTell(t *testing.T) {
	svc, project := newTestService(t)
	path := filepath.Join(project, "shot.png")
	writePNG(t, path, []color.RGBA{
		{0xff, 0xff, 0xff, 0xff}, // dominant background
		{0x14, 0x14, 0x13, 0xff},
		{0xc9, 0x64, 0x42, 0xff},
	})
	result, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: SourceImage, Target: path})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(result.Notes) == 0 {
		t.Fatal("an image extraction must say that typography and spacing are not in it")
	}
	// Typography stays at the default rather than being invented from pixels.
	if result.System.Tokens["font"]["sans"] != DefaultDesignSystem().Tokens["font"]["sans"] {
		t.Error("an image must not change the typography tokens")
	}
}

func TestExtractRejectsAnUnknownSource(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: "vibes"}); err == nil {
		t.Fatal("expected an error for an unknown source")
	}
}

func TestExtractFromURLRejectsANonHTTPTarget(t *testing.T) {
	svc, _ := newTestService(t)
	// Rejected before any browser is needed: a file:// or javascript: target
	// would otherwise be handed straight to the renderer.
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", ""} {
		if _, err := svc.ExtractSystem(context.Background(), ExtractOptions{Source: SourceURL, Target: bad}); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

// --- contract ---

func TestSaveSystemWritesTheContractAndPreservesItsProse(t *testing.T) {
	svc, _ := newTestService(t)
	if _, _, err := svc.SaveSystem(DefaultDesignSystem()); err != nil {
		t.Fatalf("save: %v", err)
	}
	contract := svc.ContractPath()
	body := readFile(t, contract)
	if !strings.Contains(body, contractTokensBegin) || !strings.Contains(body, contractTokensEnd) {
		t.Fatal("DESIGN.md must fence the generated token table")
	}

	// A person writes a rule into the contract; regenerating must keep it.
	edited := strings.Replace(body, "## Voice", "## Voice\n\nAlways use sentence case.", 1)
	if err := os.WriteFile(contract, []byte(edited), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
	if _, err := svc.SetSystemTokens("", map[string]map[string]string{"color": {"accent": "#c96442"}}); err != nil {
		t.Fatalf("set tokens: %v", err)
	}
	after := readFile(t, contract)
	if !strings.Contains(after, "Always use sentence case.") {
		t.Error("regenerating the token table destroyed the prose around it")
	}
	if !strings.Contains(after, "#c96442") {
		t.Error("the regenerated token table does not carry the new value")
	}
}

func TestConstraintBlockNamesTheStylesheetAndForbidsInvention(t *testing.T) {
	block := DefaultDesignSystem().ConstraintBlock("designer/_system/system.css", "designer/_system/DESIGN.md")
	for _, want := range []string{"designer/_system/system.css", "designer/_system/DESIGN.md", "Never invent", "--color-accent"} {
		if !strings.Contains(block, want) {
			t.Errorf("constraint block is missing %q:\n%s", want, block)
		}
	}
}

// --- applier ---

func TestApplySystemLinksTheStylesheetOnceAndAuditsLiterals(t *testing.T) {
	svc, _ := newTestService(t)
	if _, _, err := svc.SaveSystem(DefaultDesignSystem()); err != nil {
		t.Fatalf("save system: %v", err)
	}
	artifact, err := svc.Create(context.Background(), CreateParams{Title: "Landing", Kind: KindWeb})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	absDir, err := svc.AbsDir(artifact)
	if err != nil {
		t.Fatalf("abs dir: %v", err)
	}
	// A hardcoded accent, and a line that already does the right thing.
	writeFile(t, filepath.Join(absDir, "style.css"), strings.Join([]string{
		".cta { background: #2f6feb; }",
		".ok { background: var(--color-accent); }",
	}, "\n"))

	result, err := svc.ApplySystem(context.Background(), artifact.ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Linked {
		t.Error("the first apply must add the stylesheet link")
	}
	entry := readFile(t, filepath.Join(absDir, result.Entry))
	if !strings.Contains(entry, result.Stylesheet) {
		t.Errorf("entry document does not link %s", result.Stylesheet)
	}

	// A second apply is a no-op, not a duplicate link.
	again, err := svc.ApplySystem(context.Background(), artifact.ID)
	if err != nil {
		t.Fatalf("apply twice: %v", err)
	}
	if again.Linked {
		t.Error("applying twice must not add a second link")
	}
	if strings.Count(readFile(t, filepath.Join(absDir, result.Entry)), result.Stylesheet) != 1 {
		t.Error("the stylesheet is linked more than once")
	}

	var found *SystemFinding
	for i := range result.Findings {
		if result.Findings[i].File == "style.css" && result.Findings[i].Value == "#2f6feb" {
			found = &result.Findings[i]
		}
	}
	if found == nil {
		t.Fatalf("the hardcoded accent was not reported: %+v", result.Findings)
	}
	if found.Token != "--color-accent" {
		t.Errorf("finding suggests %q, want --color-accent", found.Token)
	}
	for _, f := range result.Findings {
		if strings.Contains(f.File, "system.css") {
			t.Error("the generated stylesheet must not be audited against itself")
		}
	}
}

func TestApplySystemRefusesWithoutACommittedSystem(t *testing.T) {
	svc, _ := newTestService(t)
	artifact, err := svc.Create(context.Background(), CreateParams{Title: "Landing", Kind: KindWeb})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.ApplySystem(context.Background(), artifact.ID)
	if err == nil {
		t.Fatal("applying a system that was never committed must fail loudly")
	}
	if !strings.Contains(err.Error(), "init") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

// --- mirror ---

type recordingMirror struct {
	path     string
	content  string
	metadata map[string]interface{}
	err      error
}

func (m *recordingMirror) AddDocument(_ context.Context, filePath, content string, metadata map[string]interface{}) error {
	m.path, m.content, m.metadata = filePath, content, metadata
	return m.err
}

func TestMirrorSystemPublishesTokensAndCSS(t *testing.T) {
	svc, _ := newTestService(t)
	mirror := &recordingMirror{}
	svc = svc.WithMirror(mirror)

	ds := DefaultDesignSystem()
	ds.Name = "Warm Paper"
	path, err := svc.MirrorSystem(context.Background(), ds, SourceText, "claude")
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	if path != "pando/design-systems/warm-paper.md" {
		t.Errorf("mirror path = %q", path)
	}
	if !strings.Contains(mirror.content, "--color-accent") || !strings.Contains(mirror.content, ":root {") {
		t.Error("the mirrored document must carry both the tokens and the stylesheet")
	}
}

func TestMirrorSystemIsANoOpWithoutAKnowledgeBase(t *testing.T) {
	svc, _ := newTestService(t)
	path, err := svc.MirrorSystem(context.Background(), DefaultDesignSystem(), SourceCode, "")
	if err != nil {
		t.Fatalf("mirroring without a knowledge base must not fail: %v", err)
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

// --- bundled examples ---

func TestBundledExamplesAreReadable(t *testing.T) {
	names := ExampleSystemNames()
	if len(names) == 0 {
		t.Fatal("no bundled style guides are embedded")
	}
	for _, name := range names {
		body, ok := ExampleSystem(name)
		if !ok || len(body) < 100 {
			t.Errorf("bundled guide %q is missing or empty", name)
		}
		if ExampleSystemTitle(name) == "" {
			t.Errorf("bundled guide %q has no title", name)
		}
	}
	// A name is a file lookup, so traversal must not resolve.
	for _, bad := range []string{"../secrets", "claude.md", "a/b", ""} {
		if _, ok := ExampleSystem(bad); ok {
			t.Errorf("ExampleSystem(%q) resolved and should not have", bad)
		}
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// writePNG paints horizontal bands, the first one covering half the image so it
// is unambiguously the dominant colour.
func writePNG(t *testing.T, path string, colors []color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		c := colors[0]
		if y >= 20 && len(colors) > 1 {
			c = colors[1+(y-20)*(len(colors)-1)/20]
		}
		for x := 0; x < 40; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	writeFile(t, path, buf.String())
}

func flattenTokens(ds DesignSystem) []string {
	var out []string
	for _, group := range SortedTokenGroups(ds.Tokens) {
		for _, name := range SortedTokenNames(ds.Tokens[group]) {
			out = append(out, ds.Tokens[group][name])
		}
	}
	return out
}
