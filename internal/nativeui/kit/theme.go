// Package kit is the Pando component library of the native desktop: the Web
// UI design tokens (web-ui/src/styles/tokens.css) ported onto MyGo's
// ui.Theme, an embedded lucide icon subset, and small components built on
// MyGo's widget bases. Views import kit for their look; only kit, shell and
// app/... import MyGo.
package kit

import (
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Family is a theme family of the Web UI (data-theme-name).
type Family string

// Families.
const (
	FamilyPando  Family = "pando"
	FamilyPaper  Family = "paper"
	FamilySlate  Family = "slate"
	FamilyForest Family = "forest"
)

// Mode is the appearance preference: follow the system, or force one.
type Mode string

// Modes.
const (
	ModeSystem Mode = "system"
	ModeLight  Mode = "light"
	ModeDark   Mode = "dark"
)

// Accent is an accent preset of the Web UI (data-accent); "" keeps the
// family's own.
type Accent string

// palette holds the colour literals of one family in one mode, exactly as
// tokens.css writes them.
type palette struct {
	bg, bgShell, bgRaised, bgCard, bgInput, bgOverlay string
	fg, fgMuted, fgFaint                              string
	border, borderStrong                              string
	accent, accentFg                                  string
}

var families = map[Family][2]palette{
	FamilyPando: {
		{"#fdfcf8", "#f4f1e8", "#ece6d4", "#ffffff", "#ffffff", "#ffffff", "#10251c", "#4b564e", "#707a71", "#e9e3d2", "#d9d0b8", "#8f6310", "#ffffff"},
		{"#0c1f18", "#0a1a14", "#16332a", "#0e2419", "#0d221a", "#132d22", "#f4f1e8", "#b6b2a2", "#8b887c", "#1c352b", "#2a4238", "#e9b949", "#0f2a20"},
	},
	FamilyPaper: {
		{"#faf9f5", "#f3f1ea", "#ece9df", "#ffffff", "#ffffff", "#ffffff", "#1f1e1d", "#5c5b56", "#75736c", "#e6e3d8", "#d4d0c3", "#b0512e", "#ffffff"},
		{"#262624", "#1f1e1d", "#34342f", "#2b2b28", "#30302d", "#30302d", "#f4f2ec", "#bdbab0", "#9a978d", "#3a3935", "#4a4944", "#d97757", "#1f1e1d"},
	},
	FamilySlate: {
		{"#fbfcfd", "#f1f4f8", "#e9edf3", "#ffffff", "#ffffff", "#ffffff", "#0f172a", "#475569", "#64748b", "#e2e8f0", "#cbd5e1", "#2563eb", "#ffffff"},
		{"#0f141b", "#141a22", "#212a36", "#151c25", "#161d27", "#1a222d", "#e6ebf2", "#a0acbc", "#808da0", "#222b37", "#313c4a", "#6ea8fe", "#0b1016"},
	},
	FamilyForest: {
		{"#fbfbf9", "#f2f3ef", "#eaece6", "#ffffff", "#ffffff", "#ffffff", "#1a1d1a", "#4f554e", "#686e67", "#e2e4de", "#cfd3ca", "#1f6f4a", "#ffffff"},
		{"#121513", "#171b18", "#242a25", "#181c19", "#1a1f1b", "#1d221e", "#e8ece8", "#a5aea6", "#838d84", "#252b26", "#353c36", "#5cb88a", "#0b1a12"},
	},
}

// accents are the accent presets: [light accent, light fg, dark accent, dark fg].
var accents = map[Accent][4]string{
	"gold":       {"#8f6310", "#ffffff", "#e9b949", "#0f2a20"},
	"terracotta": {"#b0512e", "#ffffff", "#d97757", "#1f1e1d"},
	"violet":     {"#5b43e8", "#ffffff", "#8b7cf6", "#0e0b1f"},
	"blue":       {"#2563eb", "#ffffff", "#6ea8fe", "#0b1016"},
	"green":      {"#1f6f4a", "#ffffff", "#5cb88a", "#0b1a12"},
	"rose":       {"#be185d", "#ffffff", "#f472b6", "#2a0a18"},
	"graphite":   {"#27272a", "#ffffff", "#e4e4e7", "#18181b"},
}

// Families lists the theme families, in the Web UI's order.
func Families() []Family { return []Family{FamilyPando, FamilyPaper, FamilySlate, FamilyForest} }

// Accents lists the accent presets.
func Accents() []Accent {
	return []Accent{"gold", "terracotta", "violet", "blue", "green", "rose", "graphite"}
}

// Tokens are the resolved design tokens of one theme: the semantic names of
// tokens.css. Use them for what ui.Theme has no slot for (shell and card
// backgrounds, faint text, soft status fills).
type Tokens struct {
	Family Family
	Dark   bool

	Bg, BgShell, BgRaised, BgCard, BgInput, BgOverlay, BgHover ui.Color
	Fg, FgMuted, FgFaint                                       ui.Color
	Border, BorderStrong                                       ui.Color
	Accent, AccentFg, AccentSoft, AccentHover, FocusRing       ui.Color
	Danger, Warning, Success, Info                             ui.Color
	DangerSoft, WarningSoft, SuccessSoft, InfoSoft             ui.Color
	Scrim                                                      ui.Color

	// Radii (DIPs): --radius-xs .. --radius-xl.
	RadiusXS, RadiusSM, RadiusMD, RadiusLG, RadiusXL float32
	// Text sizes (DIPs): --text-xs .. --text-2xl.
	TextXS, TextSM, TextBase, TextMD, TextLG, TextXL, Text2XL float32
	// ControlH and ControlHSm are the control heights.
	ControlH, ControlHSm float32
	// MonoFont is the code font family.
	MonoFont string

	accent  Accent
	uiTheme *ui.Theme
	mdTheme *markdown.Theme
}

// mix is CSS color-mix(in srgb, a p%, b).
func mix(a, b ui.Color, p float32) ui.Color { return b.Mix(a, p) }

// Resolve returns the tokens of a family in light or dark, with an optional
// accent preset. Unknown families resolve to Pando.
func Resolve(f Family, dark bool, accent Accent) *Tokens {
	pals, ok := families[f]
	if !ok {
		f, pals = FamilyPando, families[FamilyPando]
	}
	p := pals[0]
	if dark {
		p = pals[1]
	}
	if a, ok := accents[accent]; ok {
		if dark {
			p.accent, p.accentFg = a[2], a[3]
		} else {
			p.accent, p.accentFg = a[0], a[1]
		}
	}
	t := &Tokens{
		Family: f, Dark: dark, accent: accent,
		Bg: ui.Hex(p.bg), BgShell: ui.Hex(p.bgShell), BgRaised: ui.Hex(p.bgRaised),
		BgCard: ui.Hex(p.bgCard), BgInput: ui.Hex(p.bgInput), BgOverlay: ui.Hex(p.bgOverlay),
		Fg: ui.Hex(p.fg), FgMuted: ui.Hex(p.fgMuted), FgFaint: ui.Hex(p.fgFaint),
		Border: ui.Hex(p.border), BorderStrong: ui.Hex(p.borderStrong),
		Accent: ui.Hex(p.accent), AccentFg: ui.Hex(p.accentFg),
		RadiusXS: 4, RadiusSM: 6, RadiusMD: 10, RadiusLG: 14, RadiusXL: 20,
		TextXS: 12, TextSM: 13, TextBase: 14, TextMD: 15, TextLG: 17, TextXL: 20, Text2XL: 26,
		ControlH: 32, ControlHSm: 28,
		MonoFont: "JetBrains Mono",
	}
	soft := float32(0.12)
	if dark {
		t.Danger, t.Warning, t.Success, t.Info = ui.Hex("#f87171"), ui.Hex("#eab308"), ui.Hex("#34d399"), ui.Hex("#60a5fa")
		t.Scrim = ui.RGBA(0, 0, 0, 0.55)
		t.AccentHover = mix(t.Accent, ui.Hex("#ffffff"), 0.88)
		soft = 0.16
	} else {
		t.Danger, t.Warning, t.Success, t.Info = ui.Hex("#dc2626"), ui.Hex("#a16207"), ui.Hex("#15803d"), ui.Hex("#2563eb")
		t.Scrim = ui.RGBA(24, 24, 27, 0.32)
		t.AccentHover = mix(t.Accent, t.Fg, 0.88)
	}
	t.AccentSoft = t.Accent.Alpha(soft)
	t.FocusRing = t.Accent.Alpha(0.70)
	t.DangerSoft = t.Danger.Alpha(soft)
	t.WarningSoft = t.Warning.Alpha(soft + 0.02)
	t.SuccessSoft = t.Success.Alpha(soft)
	t.InfoSoft = t.Info.Alpha(soft)
	t.BgHover = t.BgRaised.Alpha(0.7)
	return t
}

// UI maps the tokens onto MyGo's theme, so MyGo's own widgets (lists,
// inputs, menus, scroll bars, tooltips) look like the rest of Pando.
func (t *Tokens) UI() *ui.Theme {
	if t.uiTheme == nil {
		t.uiTheme = t.buildUI()
	}
	return t.uiTheme
}

func (t *Tokens) buildUI() *ui.Theme {
	th := ui.LightTheme()
	if t.Dark {
		th = ui.DarkTheme()
	}
	th.Dark = t.Dark
	th.Background = t.Bg
	th.Surface = t.BgRaised
	th.SurfaceHover = mix(t.BgRaised, t.Fg, 0.92)
	th.SurfacePressed = mix(t.BgRaised, t.Fg, 0.85)
	th.Border = t.BorderStrong
	th.Text = t.Fg
	th.TextMuted = t.FgMuted
	th.Accent = t.Accent
	th.AccentHover = t.AccentHover
	th.AccentPressed = mix(t.AccentHover, t.Fg, 0.85)
	th.AccentText = t.AccentFg
	th.Danger, th.Warning, th.Success = t.Danger, t.Warning, t.Success
	th.Selection = t.Accent.Alpha(0.25)
	th.Focus = t.FocusRing
	th.Inverse, th.InverseText = t.Fg, t.Bg
	if t.Dark {
		th.Scrollbar = ui.RGBA(255, 255, 255, 0.28)
	} else {
		th.Scrollbar = ui.RGBA(0, 0, 0, 0.28)
	}
	th.Radius = t.RadiusSM
	th.Spacing = 4
	return th
}

// Markdown returns the markdown theme matching the tokens.
func (t *Tokens) Markdown() *markdown.Theme {
	if t.mdTheme != nil {
		return t.mdTheme
	}
	t.mdTheme = &markdown.Theme{
		Text:                t.Fg,
		Muted:               t.FgMuted,
		Link:                t.Accent,
		Code:                t.Fg,
		CodeBackground:      t.BgRaised,
		CodeBlockBackground: t.BgShell,
		CodeBlockBorder:     t.Border,
		CodeBlockRadius:     0.6,
		QuoteBar:            t.BorderStrong,
		QuoteText:           t.FgMuted,
		Rule:                t.Border,
	}
	return t.mdTheme
}

// Settings selects the theme of the app: a family, an appearance mode and
// an optional accent preset (the Web UI's appearance settings).
type Settings struct {
	Family Family
	Mode   Mode
	Accent Accent
}

// DefaultSettings is Pando, following the system.
var DefaultSettings = Settings{Family: FamilyPando, Mode: ModeSystem}

// current holds the tokens of the frame being built (UI thread only, but
// atomic so tests and goroutines reading colours never race).
var current atomic.Pointer[Tokens]

func init() { current.Store(Resolve(FamilyPando, false, "")) }

// Apply resolves the theme for this frame and installs it: call it first in
// the window's view. systemDark comes from c.Theme().Dark before any
// SetTheme, which follows the system appearance.
func Apply(c *ui.Context, s Settings) *Tokens {
	dark := c.Theme().Dark
	switch s.Mode {
	case ModeLight:
		dark = false
	case ModeDark:
		dark = true
	}
	t := current.Load()
	if t == nil || t.Family != orPando(s.Family) || t.Dark != dark || t.accent != s.Accent {
		t = Resolve(s.Family, dark, s.Accent)
		current.Store(t)
	}
	c.SetTheme(t.UI())
	return t
}

func orPando(f Family) Family {
	if _, ok := families[f]; ok {
		return f
	}
	return FamilyPando
}

// Current returns the tokens installed by the last Apply.
func Current() *Tokens { return current.Load() }
