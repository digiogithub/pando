package design

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// This file holds the colour arithmetic and the frequency bookkeeping shared by
// every extractor. The extractors differ only in where they get raw CSS values
// from — a stylesheet, a rendered page, a bitmap — so the part that turns a pile
// of values into a named token set lives here once.

// rgb is a colour in sRGB, 0-255 per channel.
type rgb struct{ R, G, B uint8 }

// hex renders the colour as a lowercase #rrggbb literal.
func (c rgb) hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// luminance is the WCAG relative luminance, 0 (black) to 1 (white). It decides
// which harvested colour is the page background and which is the text.
func (c rgb) luminance() float64 {
	channel := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// saturation is the HSL saturation, 0 (grey) to 1. The accent of a design
// system is almost always its most saturated recurring colour.
func (c rgb) saturation() float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	if max == min {
		return 0
	}
	l := (max + min) / 2
	if l > 0.5 {
		return (max - min) / (2 - max - min)
	}
	return (max - min) / (max + min)
}

// distance is a plain Euclidean distance in RGB. It is not perceptually
// uniform, but it only has to answer "is this the same colour again?", and a
// cheap answer keeps the harvest from degenerating into a palette of near
// duplicates.
func (c rgb) distance(o rgb) float64 {
	dr := float64(c.R) - float64(o.R)
	dg := float64(c.G) - float64(o.G)
	db := float64(c.B) - float64(o.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// parseColor accepts the CSS colour forms that actually show up in stylesheets
// and in computed styles: #rgb, #rrggbb, #rrggbbaa, rgb()/rgba() and the few
// keywords worth knowing. Anything else, including gradients and colour
// functions we cannot evaluate, is rejected rather than guessed at.
func parseColor(raw string) (rgb, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "white":
		return rgb{255, 255, 255}, true
	case "black":
		return rgb{0, 0, 0}, true
	case "transparent", "inherit", "currentcolor", "none", "initial", "unset":
		return rgb{}, false
	}
	if strings.HasPrefix(s, "#") {
		digits := s[1:]
		switch len(digits) {
		case 3, 4:
			expanded := make([]byte, 0, 6)
			for i := 0; i < 3; i++ {
				expanded = append(expanded, digits[i], digits[i])
			}
			digits = string(expanded)
		case 6:
		case 8:
			// An alpha channel of zero means the colour never shows.
			if alpha, err := strconv.ParseUint(digits[6:8], 16, 8); err == nil && alpha == 0 {
				return rgb{}, false
			}
			digits = digits[:6]
		default:
			return rgb{}, false
		}
		v, err := strconv.ParseUint(digits, 16, 32)
		if err != nil {
			return rgb{}, false
		}
		return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	if strings.HasPrefix(s, "rgb") {
		open := strings.IndexByte(s, '(')
		close := strings.LastIndexByte(s, ')')
		if open < 0 || close < open {
			return rgb{}, false
		}
		fields := strings.FieldsFunc(s[open+1:close], func(r rune) bool {
			return r == ',' || r == '/' || r == ' '
		})
		if len(fields) < 3 {
			return rgb{}, false
		}
		if len(fields) >= 4 {
			if alpha, err := strconv.ParseFloat(strings.TrimSuffix(fields[3], "%"), 64); err == nil && alpha == 0 {
				return rgb{}, false
			}
		}
		var out [3]uint8
		for i := 0; i < 3; i++ {
			f, err := strconv.ParseFloat(strings.TrimSuffix(fields[i], "%"), 64)
			if err != nil {
				return rgb{}, false
			}
			if strings.HasSuffix(fields[i], "%") {
				f = f * 255 / 100
			}
			out[i] = uint8(math.Max(0, math.Min(255, f)))
		}
		return rgb{out[0], out[1], out[2]}, true
	}
	return rgb{}, false
}

// tally counts how often a raw value was seen, keeping the order of first
// sighting so equal counts break deterministically instead of by map order.
type tally struct {
	counts map[string]int
	order  []string
}

func newTally() *tally { return &tally{counts: map[string]int{}} }

func (t *tally) add(value string, weight int) {
	value = strings.TrimSpace(value)
	if value == "" || weight <= 0 {
		return
	}
	if _, seen := t.counts[value]; !seen {
		t.order = append(t.order, value)
	}
	t.counts[value] += weight
}

// ranked returns the values sorted by count, then by first sighting.
func (t *tally) ranked() []string {
	index := make(map[string]int, len(t.order))
	for i, v := range t.order {
		index[v] = i
	}
	out := append([]string(nil), t.order...)
	sort.SliceStable(out, func(i, j int) bool {
		if t.counts[out[i]] != t.counts[out[j]] {
			return t.counts[out[i]] > t.counts[out[j]]
		}
		return index[out[i]] < index[out[j]]
	})
	return out
}

func (t *tally) count(value string) int { return t.counts[value] }

// palette is the ranked, de-duplicated colour list a harvest produces.
type palette struct {
	colors []rgb
	counts []int
}

// buildPalette ranks the tallied colours and drops near duplicates, keeping the
// more frequent of any two colours closer together than minDistance.
func buildPalette(t *tally, limit int, minDistance float64) palette {
	var out palette
	for _, raw := range t.ranked() {
		c, ok := parseColor(raw)
		if !ok {
			continue
		}
		duplicate := false
		for i, kept := range out.colors {
			if kept.distance(c) < minDistance {
				out.counts[i] += t.count(raw)
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		out.colors = append(out.colors, c)
		out.counts = append(out.counts, t.count(raw))
		if limit > 0 && len(out.colors) >= limit {
			break
		}
	}
	// Merging near duplicates moves counts between entries, so the list has to
	// be re-ranked: the caller reads position 0 as "the most common colour".
	order := make([]int, len(out.colors))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return out.counts[order[i]] > out.counts[order[j]] })
	ranked := palette{colors: make([]rgb, len(order)), counts: make([]int, len(order))}
	for i, idx := range order {
		ranked.colors[i], ranked.counts[i] = out.colors[idx], out.counts[idx]
	}
	return ranked
}

// isAccentCandidate rejects the near-whites and near-blacks that HSL reports as
// saturated. A parchment #f5f4ed has a saturation of 0.29 and is nobody's brand
// colour; requiring a mid luminance keeps the page background out of the role.
func isAccentCandidate(c rgb) bool {
	if c.saturation() <= 0.25 {
		return false
	}
	l := c.luminance()
	return l > 0.05 && l < 0.85
}

// assignColorRoles maps a harvested palette onto the role names the rest of the
// design system uses. Roles are assigned by measurable property — luminance for
// the background/text pair, saturation for the accent — never by position in
// the source, because the order colours appear in a stylesheet says nothing
// about what they are for.
//
// A role with no plausible candidate is left out entirely: the caller merges the
// result over the default system, so an absent role keeps its accessible
// default instead of being filled with a colour that happens to be nearby.
func assignColorRoles(p palette) map[string]string {
	if len(p.colors) == 0 {
		return nil
	}
	roles := map[string]string{}
	used := make([]bool, len(p.colors))

	take := func(i int, role string) {
		if i < 0 || used[i] {
			return
		}
		used[i] = true
		roles[role] = p.colors[i].hex()
	}

	// Background: the most frequent colour is the surface the page sits on far
	// more often than not, whether the design is light or dark.
	take(0, "bg")
	dark := p.colors[0].luminance() < 0.5

	// Text: the most frequent colour whose contrast against the background is
	// high enough to actually be readable on it.
	best, bestCount := -1, -1
	for i, c := range p.colors {
		if used[i] || contrastRatio(c, p.colors[0]) < 4.5 {
			continue
		}
		if p.counts[i] > bestCount {
			best, bestCount = i, p.counts[i]
		}
	}
	take(best, "text")

	// Accent: the recurring saturated colour. Saturation alone is the wrong
	// test — a palette usually contains one very saturated colour used once,
	// for a focus ring or an error state, and picking it would make the brand
	// colour of the whole system a state colour.
	best = -1
	bestScore := 0
	bestSat := 0.0
	for i, c := range p.colors {
		if used[i] || !isAccentCandidate(c) {
			continue
		}
		if p.counts[i] > bestScore || (p.counts[i] == bestScore && c.saturation() > bestSat) {
			best, bestScore, bestSat = i, p.counts[i], c.saturation()
		}
	}
	take(best, "accent")

	// Surface sits next to the background, so it is the unused colour closest to
	// it that is not the background itself.
	best, bestDist := -1, math.MaxFloat64
	for i, c := range p.colors {
		if used[i] {
			continue
		}
		if d := c.distance(p.colors[0]); d > 4 && d < bestDist {
			best, bestDist = i, d
		}
	}
	take(best, "surface")

	// Muted text: a remaining low-saturation colour between the background and
	// the text in luminance.
	lo, hi := 0.15, 0.6
	if dark {
		lo, hi = 0.2, 0.7
	}
	for i, c := range p.colors {
		if used[i] || c.saturation() > 0.4 {
			continue
		}
		if l := c.luminance(); l >= lo && l <= hi {
			take(i, "muted")
			break
		}
	}

	// Border: whatever grey is left, closest to the background.
	best, bestDist = -1, math.MaxFloat64
	for i, c := range p.colors {
		if used[i] || c.saturation() > 0.35 {
			continue
		}
		if d := c.distance(p.colors[0]); d < bestDist {
			best, bestDist = i, d
		}
	}
	take(best, "border")

	return roles
}

// assignRoles maps colours onto roles using what the source said each colour
// was for, falling back to assignColorRoles for anything it cannot place.
//
// The property a colour appeared in is far better evidence than its frequency:
// `color` is inherited, so on a rendered page the text colour appears on every
// node and would win a raw popularity contest for "background" every time.
func assignRoles(all, backgrounds, foregrounds, borders palette) map[string]string {
	roles := map[string]string{}
	if len(backgrounds.colors) > 0 {
		bg := backgrounds.colors[0]
		roles["bg"] = bg.hex()
		// A surface sits next to the background, not against it. Without that
		// test the second most common background is usually a button, and the
		// whole page would be painted in the call-to-action colour.
		for _, c := range backgrounds.colors[1:] {
			if c.distance(bg) < 96 || c.saturation() < 0.25 {
				roles["surface"] = c.hex()
				break
			}
		}
	}
	if len(foregrounds.colors) > 0 {
		text := foregrounds.colors[0]
		roles["text"] = text.hex()
		// Muted text is quieter than the text colour: it must sit between the
		// text and the background, or it is another accent rather than a
		// secondary one.
		if bg, ok := roles["bg"]; ok {
			if bgColor, parsed := parseColor(bg); parsed {
				for _, c := range foregrounds.colors[1:] {
					if c.saturation() > 0.4 {
						continue
					}
					if between(c.luminance(), bgColor.luminance(), text.luminance()) {
						roles["muted"] = c.hex()
						break
					}
				}
			}
		}
	}
	if len(borders.colors) > 0 {
		roles["border"] = borders.colors[0].hex()
	}
	// A page whose text colour equals its background is telling us the split
	// failed; drop both and let the generic pass decide.
	if roles["bg"] != "" && roles["bg"] == roles["text"] {
		delete(roles, "bg")
		delete(roles, "text")
	}

	taken := map[string]bool{}
	for _, hex := range roles {
		taken[hex] = true
	}
	best, bestCount, bestSat := -1, 0, 0.0
	for i, c := range all.colors {
		if taken[c.hex()] || !isAccentCandidate(c) {
			continue
		}
		if all.counts[i] > bestCount || (all.counts[i] == bestCount && c.saturation() > bestSat) {
			best, bestCount, bestSat = i, all.counts[i], c.saturation()
		}
	}
	if best >= 0 {
		roles["accent"] = all.colors[best].hex()
	}

	for role, hex := range assignColorRoles(all) {
		if _, ok := roles[role]; !ok {
			roles[role] = hex
		}
	}
	return roles
}

// between reports whether v lies strictly inside the range bounded by a and b,
// in either order.
func between(v, a, b float64) bool {
	if a > b {
		a, b = b, a
	}
	return v > a && v < b
}

// contrastRatio is the WCAG contrast ratio between two colours, 1 to 21.
func contrastRatio(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
