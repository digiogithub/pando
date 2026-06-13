package theme

import "github.com/charmbracelet/lipgloss"

// LightTheme is designed for terminals with a light (white/cream) background.
// All AdaptiveColor values use the same color for both Dark and Light variants
// so the palette is consistent regardless of terminal background detection.
type LightTheme struct {
	BaseTheme
}

func NewLightTheme() *LightTheme {
	bg := "#f5f5f5"
	bgAlt := "#e8e8e8"
	// bgCode is used as both a background (code blocks) and as foreground text on dark
	// badge backgrounds (help widget, tokens badge). Pure white maximises contrast
	// on those dark badges while remaining a subtle code-block background.
	bgCode := "#ffffff"
	fg := "#1a1a1a"
	fgMuted := "#666666"
	fgEmph := "#8b5e00"
	primary := "#2563eb"
	secondary := "#7c3aed"
	accent := "#b45309"
	red := "#b91c1c"
	// Darker amber so that light foreground text (#e8e8e8) on warning bg passes WCAG AA.
	// #d97706 (bright orange) only gives 2.96:1; #b45309 gives 5.1:1.
	orange := "#b45309"
	green := "#15803d"
	cyan := "#0e7490"
	border := "#c4c4c4"
	selection := "#bfdbfe"
	selFg := "#1e3a5f"

	c := func(v string) lipgloss.AdaptiveColor {
		return lipgloss.AdaptiveColor{Dark: v, Light: v}
	}

	t := &LightTheme{}
	t.PrimaryColor = c(primary)
	t.SecondaryColor = c(secondary)
	t.AccentColor = c(accent)
	t.ErrorColor = c(red)
	t.WarningColor = c(orange)
	t.SuccessColor = c(green)
	t.InfoColor = c(cyan)
	t.TextColor = c(fg)
	t.TextMutedColor = c(fgMuted)
	t.TextEmphasizedColor = c(fgEmph)
	t.BackgroundColor = c(bg)
	t.BackgroundSecondaryColor = c(bgAlt)
	t.BackgroundDarkerColor = c(bgCode)
	// Light theme uses dark badge-backgrounds (Text, TextMuted, Primary…),
	// so badge text must be white for legibility.
	t.BadgeTextColor = c("#ffffff")
	t.SelectionBackgroundColor = c(selection)
	t.SelectionForegroundColor = c(selFg)
	t.BorderNormalColor = c(border)
	t.BorderFocusedColor = c(primary)
	t.BorderDimColor = c(bgCode)

	t.DiffAddedColor = c("#166534")
	t.DiffRemovedColor = c("#991b1b")
	t.DiffContextColor = c("#6b7280")
	t.DiffHunkHeaderColor = c("#4b5563")
	t.DiffHighlightAddedColor = c("#bbf7d0")
	t.DiffHighlightRemovedColor = c("#fecaca")
	t.DiffAddedBgColor = c("#dcfce7")
	t.DiffRemovedBgColor = c("#fee2e2")
	t.DiffContextBgColor = c(bg)
	t.DiffLineNumberColor = c("#9ca3af")
	t.DiffAddedLineNumberBgColor = c("#bbf7d0")
	t.DiffRemovedLineNumberBgColor = c("#fecaca")

	t.MarkdownTextColor = c(fg)
	t.MarkdownHeadingColor = c(primary)
	t.MarkdownLinkColor = c(secondary)
	t.MarkdownLinkTextColor = c(cyan)
	t.MarkdownCodeColor = c(green)
	t.MarkdownBlockQuoteColor = c(fgEmph)
	t.MarkdownEmphColor = c(fgEmph)
	t.MarkdownStrongColor = c(secondary)
	t.MarkdownHorizontalRuleColor = c(fgMuted)
	t.MarkdownListItemColor = c(primary)
	t.MarkdownListEnumerationColor = c(cyan)
	t.MarkdownImageColor = c(primary)
	t.MarkdownImageTextColor = c(cyan)
	t.MarkdownCodeBlockColor = c(fg)

	t.SyntaxCommentColor = c(fgMuted)
	t.SyntaxKeywordColor = c(secondary)
	t.SyntaxFunctionColor = c(primary)
	t.SyntaxVariableColor = c(red)
	t.SyntaxStringColor = c(green)
	t.SyntaxNumberColor = c(accent)
	t.SyntaxTypeColor = c(fgEmph)
	t.SyntaxOperatorColor = c(cyan)
	t.SyntaxPunctuationColor = c(fg)

	return t
}

func init() {
	RegisterTheme("light", NewLightTheme())
}
