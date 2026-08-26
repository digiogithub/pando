package design

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Extraction turns something that already looks the way the user wants — a
// codebase, a live page, a screenshot, a written style guide — into the token
// set the designer is then constrained by. Every source funnels into the same
// harvest: tally raw CSS values, rank them, name them. Only the harvesting step
// differs.
//
// An extractor never invents a token. What it cannot measure it reports in
// Notes and leaves to the default system, because a plausible-looking colour
// that nobody chose is worse than an obvious placeholder.

// ExtractSource names where a design system is read from.
type ExtractSource string

const (
	// SourceCode scans stylesheets and component files in a directory.
	SourceCode ExtractSource = "code"
	// SourceURL renders a page and reads its computed styles.
	SourceURL ExtractSource = "url"
	// SourceImage quantises a bitmap into a palette.
	SourceImage ExtractSource = "image"
	// SourceText reads a written style guide, including the bundled examples.
	SourceText ExtractSource = "text"
)

// ExtractOptions configures one extraction.
type ExtractOptions struct {
	// Source selects the extractor. Empty defaults to SourceCode.
	Source ExtractSource
	// Target is a directory (code), a URL, an image path or a markdown path.
	// Empty means the project root for code, and is an error otherwise.
	Target string
	// Name overrides the name given to the extracted system.
	Name string
	// MaxFiles bounds a code scan. Zero uses defaultExtractMaxFiles.
	MaxFiles int
}

// ExtractResult is an extracted system plus an account of how it was obtained,
// so a caller can show what was looked at before committing anything to disk.
type ExtractResult struct {
	System  DesignSystem  `json:"system"`
	Source  ExtractSource `json:"source"`
	Target  string        `json:"target"`
	Scanned []string      `json:"scanned,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

// defaultExtractMaxFiles bounds a code scan. A design system is visible in the
// first handful of stylesheets; walking a whole monorepo to confirm it is a
// waste of a tool call.
const defaultExtractMaxFiles = 200

// maxExtractFileBytes skips generated bundles, which are megabytes of minified
// CSS whose colour frequencies describe the framework, not the project.
const maxExtractFileBytes = 512 << 10

// ExtractSystem runs the extractor named by opts and returns the resulting
// system without writing anything. Persisting is a separate, explicit step.
func (s *Service) ExtractSystem(ctx context.Context, opts ExtractOptions) (ExtractResult, error) {
	if opts.Source == "" {
		opts.Source = SourceCode
	}
	var (
		res ExtractResult
		err error
	)
	switch opts.Source {
	case SourceCode:
		res, err = s.extractFromCode(opts)
	case SourceURL:
		res, err = s.extractFromURL(ctx, opts)
	case SourceImage:
		res, err = extractFromImage(opts)
	case SourceText:
		res, err = extractFromText(opts)
	default:
		return ExtractResult{}, fmt.Errorf("design: unknown extraction source %q (want code, url, image or text)", opts.Source)
	}
	if err != nil {
		return ExtractResult{}, err
	}
	res.Source = opts.Source
	res.Target = opts.Target
	if opts.Name != "" {
		res.System.Name = opts.Name
	}
	if res.System.Name == "" {
		res.System.Name = "extracted"
	}
	return res, nil
}

// harvest accumulates raw values from any source before they are named.
type harvest struct {
	colors *tally
	// Colours are also tallied by the role the source used them in; see
	// assignRoles for why that beats raw frequency.
	backgrounds *tally
	foregrounds *tally
	borders     *tally
	fonts       *tally
	sizes       *tally
	radii       *tally
	spacing     *tally
	// declared holds custom properties found verbatim, which outrank anything
	// inferred: a project that already wrote --color-accent has told us its
	// answer and we should not overrule it with a frequency count.
	declared map[string]map[string]string
	notes    []string
}

func newHarvest() *harvest {
	return &harvest{
		colors:      newTally(),
		backgrounds: newTally(),
		foregrounds: newTally(),
		borders:     newTally(),
		fonts:       newTally(),
		sizes:       newTally(),
		radii:       newTally(),
		spacing:     newTally(),
		declared:    map[string]map[string]string{},
	}
}

func (h *harvest) note(format string, args ...any) {
	h.notes = append(h.notes, fmt.Sprintf(format, args...))
}

// addRoleColor files a colour under the role the declaration it came from
// implies. A property we do not recognise contributes to the palette but to no
// specific role, which is what keeps a guess out of the token set.
func (h *harvest) addRoleColor(property, value string, weight int) {
	switch property {
	case "background", "background-color":
		h.backgrounds.add(value, weight)
	case "color", "fill":
		h.foregrounds.add(value, weight)
	case "border-color":
		h.borders.add(value, weight)
	}
}

// dropRoleHints discards the by-property colour tallies. Prose sources need it:
// a style guide quotes CSS as illustration, and a snippet showing `background:
// white` in an example is not a statement that the system's background is
// white. Frequency across the whole document is the better signal there.
func (h *harvest) dropRoleHints() {
	h.backgrounds, h.foregrounds, h.borders = newTally(), newTally(), newTally()
}

// addDeclared records a custom property under the group its name implies.
func (h *harvest) addDeclared(name, value string) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "--")
	value = strings.TrimSpace(value)
	if name == "" || value == "" || strings.Contains(value, "var(") {
		return
	}
	group, key := splitTokenName(name)
	if group == "" {
		return
	}
	if h.declared[group] == nil {
		h.declared[group] = map[string]string{}
	}
	if _, exists := h.declared[group][key]; !exists {
		h.declared[group][key] = value
	}
}

// tokenGroupAliases maps the prefixes real projects use onto our group names.
var tokenGroupAliases = map[string]string{
	"color": "color", "colour": "color", "c": "color", "brand": "color",
	"space": "space", "spacing": "space", "gap": "space", "size": "space",
	"font": "font", "type": "font", "text": "font",
	"radius": "radius", "rounded": "radius", "br": "radius",
	"shadow": "shadow", "elevation": "shadow",
}

// splitTokenName maps a custom-property name onto a (group, key) pair. A name
// whose prefix names no group we model is dropped: importing every stray
// variable would bury the six tokens that matter.
func splitTokenName(name string) (string, string) {
	parts := strings.Split(strings.ToLower(name), "-")
	if len(parts) < 2 {
		return "", ""
	}
	group, ok := tokenGroupAliases[parts[0]]
	if !ok {
		return "", ""
	}
	key := strings.Join(parts[1:], "-")
	if key == "" {
		return "", ""
	}
	return group, key
}

// compile builds the design system from everything harvested.
func (h *harvest) compile(name string) DesignSystem {
	ds := DefaultDesignSystem()
	ds.Name = name

	roles := assignRoles(
		buildPalette(h.colors, 12, 24),
		buildPalette(h.backgrounds, 6, 24),
		buildPalette(h.foregrounds, 6, 24),
		buildPalette(h.borders, 4, 24),
	)
	if len(roles) > 0 {
		for role, value := range roles {
			ds.Tokens["color"][role] = value
		}
	}
	if fonts := h.fonts.ranked(); len(fonts) > 0 {
		for _, f := range fonts {
			if isMonoFont(f) {
				ds.Tokens["font"]["mono"] = f
				break
			}
		}
		for _, f := range fonts {
			if !isMonoFont(f) {
				ds.Tokens["font"]["sans"] = f
				break
			}
		}
	}
	if scale := deriveScale(h.sizes.ranked()); scale != "" {
		ds.Tokens["font"]["scale"] = scale
	}
	if steps := deriveLengthSteps(h.radii.ranked(), []string{"sm", "md", "lg"}); len(steps) > 0 {
		for k, v := range steps {
			ds.Tokens["radius"][k] = v
		}
	}
	if steps := deriveLengthSteps(h.spacing.ranked(), []string{"xs", "sm", "md", "lg", "xl"}); len(steps) > 0 {
		for k, v := range steps {
			ds.Tokens["space"][k] = v
		}
	}
	// Declared custom properties win over everything inferred.
	for group, values := range h.declared {
		if ds.Tokens[group] == nil {
			ds.Tokens[group] = map[string]string{}
		}
		for key, value := range values {
			ds.Tokens[group][key] = value
		}
	}
	return ds
}

// monoFontHints are the substrings that identify a monospace stack without
// having to resolve the font itself.
var monoFontHints = []string{"mono", "consolas", "menlo", "courier", "code"}

func isMonoFont(stack string) bool {
	lower := strings.ToLower(stack)
	for _, hint := range monoFontHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// deriveScale infers the typographic ratio from the two most common font sizes
// that differ, which is closer to what the design actually does than any
// canonical scale we could pick for it.
func deriveScale(sizes []string) string {
	var px []float64
	for _, raw := range sizes {
		if v, ok := parseLengthPx(raw); ok && v >= 8 && v <= 128 {
			px = append(px, v)
		}
		if len(px) >= 6 {
			break
		}
	}
	if len(px) < 2 {
		return ""
	}
	sort.Float64s(px)
	base, top := px[0], px[len(px)-1]
	if base <= 0 || top <= base {
		return ""
	}
	steps := float64(len(px) - 1)
	ratio := math.Pow(top/base, 1/steps)
	if ratio < 1.05 || ratio > 2 {
		return ""
	}
	return strconv.FormatFloat(round2(ratio), 'f', -1, 64)
}

// deriveLengthSteps picks len(names) representative lengths from a ranked list,
// smallest first, so the resulting scale is ordered even though the input was
// ranked by frequency.
func deriveLengthSteps(values []string, names []string) map[string]string {
	seen := map[float64]string{}
	var px []float64
	for _, raw := range values {
		v, ok := parseLengthPx(raw)
		if !ok || v < 0 || v > 256 {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = raw
		px = append(px, v)
		if len(px) >= len(names)*3 {
			break
		}
	}
	if len(px) < len(names) {
		return nil
	}
	sort.Float64s(px)
	out := map[string]string{}
	for i, name := range names {
		// Spread the picks across the observed range instead of taking the
		// first N, which would collapse the scale into its smallest values.
		idx := i * (len(px) - 1) / (len(names) - 1)
		out[name] = seen[px[idx]]
	}
	return out
}

var lengthPattern = regexp.MustCompile(`^(-?[0-9]*\.?[0-9]+)(px|rem|em|pt)?$`)

// parseLengthPx converts a CSS length to pixels, treating rem and em as 16px
// because that is the browser default and we only need the values to be
// comparable to each other.
func parseLengthPx(raw string) (float64, bool) {
	m := lengthPattern.FindStringSubmatch(strings.TrimSpace(strings.ToLower(raw)))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	switch m[2] {
	case "rem", "em":
		v *= 16
	case "pt":
		v *= 4.0 / 3.0
	}
	return v, true
}

// --- code ---

var (
	customPropPattern = regexp.MustCompile(`(--[a-zA-Z0-9_-]+)\s*:\s*([^;{}\n]+)`)
	declPattern       = regexp.MustCompile(`(?i)\b(color|background-color|background|border-color|fill|font-family|font-size|border-radius|padding|margin|gap)\s*:\s*([^;{}\n]+)`)
	colorLiteral      = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)`)
	keyedHexPattern   = regexp.MustCompile(`(?i)['"]?([a-z][a-z0-9 _-]{1,28})['"]?\s*[:=]\s*['"](#[0-9a-fA-F]{3,8})['"]`)
)

// codeScanExtensions are the file types worth reading. Everything else in a
// repository either has no styling in it or has so much noise that the harvest
// stops describing the design.
var codeScanExtensions = map[string]bool{
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".html": true, ".vue": true, ".svelte": true,
	".jsx": true, ".tsx": true,
}

// codeScanSkipDirs are never descended into: their contents are dependencies or
// build output, and their colours are the framework's, not the project's.
var codeScanSkipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".jj": true, "dist": true, "build": true,
	"vendor": true, ".pando": true, "coverage": true, ".next": true, "out": true,
	"target": true, ".venv": true, "__pycache__": true,
}

func (s *Service) extractFromCode(opts ExtractOptions) (ExtractResult, error) {
	root := opts.Target
	if root == "" {
		root = s.layout.WorkingDir
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(s.layout.WorkingDir, root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("design: scan %s: %w", root, err)
	}
	if !info.IsDir() {
		return ExtractResult{}, fmt.Errorf("design: %s is not a directory; use source \"text\" or \"image\" for a file", root)
	}

	limit := opts.MaxFiles
	if limit <= 0 {
		limit = defaultExtractMaxFiles
	}
	h := newHarvest()
	var scanned []string
	// The design output directory is skipped so re-extracting does not read
	// back the system we generated last time and mistake it for evidence.
	designRoot := s.layout.Root()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (codeScanSkipDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			if sameOrUnder(path, designRoot) {
				return fs.SkipDir
			}
			return nil
		}
		if len(scanned) >= limit {
			return fs.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(path))
		isTailwind := strings.HasPrefix(strings.ToLower(d.Name()), "tailwind.config.")
		if !codeScanExtensions[ext] && !isTailwind {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxExtractFileBytes {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		harvestSource(h, string(raw), isTailwind)
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		scanned = append(scanned, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return ExtractResult{}, fmt.Errorf("design: scan %s: %w", root, walkErr)
	}
	if len(scanned) == 0 {
		return ExtractResult{}, fmt.Errorf("design: no stylesheet or component files found under %s", root)
	}
	if len(scanned) >= limit {
		h.note("stopped after %d files; pass a narrower directory for a more focused system", limit)
	}
	return ExtractResult{System: h.compile("extracted"), Scanned: scanned, Notes: h.notes}, nil
}

// harvestSource pulls values out of one stylesheet or component file.
func harvestSource(h *harvest, src string, keyedLiterals bool) {
	for _, m := range customPropPattern.FindAllStringSubmatch(src, -1) {
		// A custom property is a definition, not a usage. Counting its value in
		// the frequency palette as well would make a declared accent outrank
		// the background it is used against, and the roles would swap.
		h.addDeclared(m[1], m[2])
	}
	for _, m := range declPattern.FindAllStringSubmatch(src, -1) {
		property, value := strings.ToLower(m[1]), strings.TrimSpace(m[2])
		if strings.Contains(value, "var(") {
			continue
		}
		switch property {
		case "font-family":
			h.fonts.add(normalizeFontStack(value), 1)
		case "font-size":
			h.sizes.add(firstToken(value), 1)
		case "border-radius":
			for _, part := range strings.Fields(value) {
				h.radii.add(part, 1)
			}
		case "padding", "margin", "gap":
			for _, part := range strings.Fields(value) {
				h.spacing.add(part, 1)
			}
		default:
			for _, c := range colorLiteral.FindAllString(value, -1) {
				h.colors.add(c, 1)
				h.addRoleColor(property, c, 1)
			}
		}
	}
	if keyedLiterals {
		// A theme config states its palette as name/value pairs, so each entry
		// counts once no matter how often the colour is used downstream.
		for _, m := range keyedHexPattern.FindAllStringSubmatch(src, -1) {
			h.colors.add(m[2], 2)
		}
	}
}

// normalizeFontStack trims the quoting noise so the same stack written two ways
// tallies as one value.
func normalizeFontStack(value string) string {
	parts := strings.Split(value, ",")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ", ")
}

func firstToken(value string) string {
	if i := strings.IndexAny(value, " \t"); i > 0 {
		return value[:i]
	}
	return value
}

// sameOrUnder reports whether path is dir or lives inside it.
func sameOrUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// --- url ---

// urlStyleProps is a wider computed-style subset than the node index uses: this
// render is never shown to a model, so the extra properties cost nothing.
// border-width is read only to decide whether border-color means anything: a
// browser reports the text colour as the border colour of every element that
// has no border, so tallying it unconditionally would make the border token the
// text colour on every page.
var urlStyleProps = []string{
	"color", "background-color", "border-color", "border-width", "border-radius",
	"font-family", "font-size", "padding", "margin", "gap",
}

func (s *Service) extractFromURL(ctx context.Context, opts ExtractOptions) (ExtractResult, error) {
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		return ExtractResult{}, fmt.Errorf("design: extracting from a URL needs a target")
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return ExtractResult{}, fmt.Errorf("design: %q is not an http(s) URL", target)
	}
	if s.renderer == nil {
		return ExtractResult{}, fmt.Errorf("%w: no renderer attached", ErrNoBrowser)
	}
	result, err := s.renderer.Render(ctx, Artifact{Kind: KindWeb}, RenderOptions{
		URL:        target,
		StyleProps: urlStyleProps,
		MaxNodes:   400,
	})
	if err != nil {
		return ExtractResult{}, err
	}
	h := newHarvest()
	for _, node := range result.Nodes {
		// Weight by area: a colour covering the hero says more about the design
		// than the same colour on a footnote.
		weight := 1
		if area := node.Box.W * node.Box.H; area > 40000 {
			weight = 3
		}
		for prop, value := range node.Styles {
			switch prop {
			case "color", "background-color", "border-color":
				if prop == "border-color" && !hasVisibleBorder(node.Styles["border-width"]) {
					continue
				}
				if _, ok := parseColor(value); ok {
					h.colors.add(value, weight)
					h.addRoleColor(prop, value, weight)
				}
			case "font-family":
				h.fonts.add(normalizeFontStack(value), weight)
			case "font-size":
				h.sizes.add(firstToken(value), 1)
			case "border-radius":
				h.radii.add(firstToken(value), 1)
			case "padding", "margin", "gap":
				for _, part := range strings.Fields(value) {
					h.spacing.add(part, 1)
				}
			}
		}
	}
	if len(result.Nodes) == 0 {
		return ExtractResult{}, fmt.Errorf("design: %s rendered no elements to read styles from", target)
	}
	h.note("computed styles only: a page cannot tell us the names its authors gave these tokens")
	name := "extracted"
	if result.Title != "" {
		name = Slugify(result.Title)
	}
	return ExtractResult{
		System:  h.compile(name),
		Scanned: []string{target},
		Notes:   h.notes,
	}, nil
}

// hasVisibleBorder reports whether a computed border-width draws anything.
func hasVisibleBorder(width string) bool {
	for _, part := range strings.Fields(width) {
		if v, ok := parseLengthPx(part); ok && v > 0 {
			return true
		}
	}
	return false
}

// --- image ---

// imageSampleStride keeps a large screenshot cheap: sampling every Nth pixel
// changes which colours dominate not at all, and a full walk of a 4K capture
// is 8M iterations for the same answer.
const imageSampleStride = 4

// imageQuantizeStep buckets each channel so anti-aliasing and JPEG artefacts
// collapse onto the colour they are noise around.
const imageQuantizeStep = 16

func extractFromImage(opts ExtractOptions) (ExtractResult, error) {
	path := strings.TrimSpace(opts.Target)
	if path == "" {
		return ExtractResult{}, fmt.Errorf("design: extracting from an image needs a target file")
	}
	f, err := os.Open(path)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("design: open %s: %w", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("design: decode %s: %w", path, err)
	}
	h := newHarvest()
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y += imageSampleStride {
		for x := bounds.Min.X; x < bounds.Max.X; x += imageSampleStride {
			r, g, b, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			c := rgb{
				quantizeChannel(uint8(r >> 8)),
				quantizeChannel(uint8(g >> 8)),
				quantizeChannel(uint8(b >> 8)),
			}
			h.colors.add(c.hex(), 1)
		}
	}
	if len(h.colors.order) == 0 {
		return ExtractResult{}, fmt.Errorf("design: %s has no opaque pixels to sample", path)
	}
	// An image carries no type, spacing or radius information. Saying so is the
	// point: the caller keeps the default typography rather than believing the
	// extracted system is complete.
	h.note("colours only: an image cannot tell us typography, spacing or radii — those stay at their defaults")
	return ExtractResult{
		System:  h.compile(Slugify(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))),
		Scanned: []string{path},
		Notes:   h.notes,
	}, nil
}

func quantizeChannel(v uint8) uint8 {
	q := int(v)/imageQuantizeStep*imageQuantizeStep + imageQuantizeStep/2
	if q > 255 {
		q = 255
	}
	return uint8(q)
}

// --- text ---

var (
	// boldNamedHex matches the "**Terracotta Brand** (`#c96442`)" shape the
	// bundled style guides use, which is also how most written design systems
	// name a colour.
	boldNamedHex = regexp.MustCompile("(?i)\\*\\*([^*]{1,40})\\*\\*[^`\\n]{0,20}`(#[0-9a-fA-F]{3,8})`")
	// backtickHex catches the rest of the hexes in prose.
	backtickHex = regexp.MustCompile("`(#[0-9a-fA-F]{3,8})`")
	// backtickFont matches a quoted font stack in prose.
	backtickFont = regexp.MustCompile("(?i)`([A-Za-z][A-Za-z0-9 '\"-]{2,60}(?:serif|sans-serif|mono|monospace|system-ui))`")
)

func extractFromText(opts ExtractOptions) (ExtractResult, error) {
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		return ExtractResult{}, fmt.Errorf("design: extracting from text needs a target file or a bundled example name")
	}
	raw, scannedName, err := readTextSource(target)
	if err != nil {
		return ExtractResult{}, err
	}
	h := newHarvest()
	// Named colours come first and count heaviest: the guide told us these are
	// the palette, so they must outrank a hex mentioned once in an example.
	for _, m := range boldNamedHex.FindAllStringSubmatch(raw, -1) {
		h.colors.add(strings.ToLower(m[2]), 6)
	}
	for _, m := range backtickHex.FindAllStringSubmatch(raw, -1) {
		h.colors.add(strings.ToLower(m[1]), 1)
	}
	for _, m := range backtickFont.FindAllStringSubmatch(raw, -1) {
		h.fonts.add(normalizeFontStack(m[1]), 1)
	}
	harvestSource(h, raw, false)
	h.dropRoleHints()
	if len(h.colors.order) == 0 {
		return ExtractResult{}, fmt.Errorf("design: %s contains no colour literals to extract", target)
	}
	h.note("read from prose: verify the roles, a written guide states colours in an order we can only guess the meaning of")
	name := Slugify(strings.TrimSuffix(filepath.Base(scannedName), filepath.Ext(scannedName)))
	return ExtractResult{System: h.compile(name), Scanned: []string{scannedName}, Notes: h.notes}, nil
}

// readTextSource resolves a target that is either a bundled example name or a
// path on disk, preferring the bundled name so `extract --from claude` works
// from any directory.
func readTextSource(target string) (string, string, error) {
	if body, ok := ExampleSystem(target); ok {
		return body, target + ".md", nil
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("design: %q is neither a bundled example (%s) nor a readable file",
				target, strings.Join(ExampleSystemNames(), ", "))
		}
		return "", "", fmt.Errorf("design: read %s: %w", target, err)
	}
	return string(raw), target, nil
}
