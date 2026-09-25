package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// PandoTheme implements the Theme interface with Pando brand colors.
// It provides both dark and light variants.
type PandoTheme struct {
	BaseTheme
}

// NewPandoTheme creates a new instance of the Pando theme.
//
// Colors are derived from the Pando v1 brand palette (assets/pando-brand-v1/README.md):
//
//	Bosque       #0F2A20  brand background / ink
//	Marfil       #F4F1E8  stroke on dark
//	Álamo        #E9B949  accent / nodes on dark
//	Álamo oscuro #C68A17  accent on light
//
// Status colors (error/warning/success/info) are left as generic semantic
// hues since the brief only calls for the accent, text, background, and
// border families to follow the brand.
func NewPandoTheme() *PandoTheme {
	// Pando color palette
	// Dark mode colors
	darkBackground := "#0F2A20"  // Bosque
	darkCurrentLine := "#16352A" // Bosque, one step lighter (elevated surface)
	darkSelection := "#1E4A38"   // Bosque, two steps lighter (selection / dim border)
	darkForeground := "#F4F1E8"  // Marfil
	darkComment := "#5C7568"     // muted Bosque green-gray (de-emphasized: comments, hr)
	darkPrimary := "#E9B949"     // Álamo
	darkSecondary := "#5EAE86"   // Bosque-family green (secondary accent)
	darkAccent := "#F0C868"      // Álamo, lighter tint (strong text, numbers)
	darkRed := "#e06c75"         // Error red
	darkOrange := "#f5a742"      // Warning orange
	darkGreen := "#7fd88f"       // Success green
	darkCyan := "#56b6c2"        // Info cyan
	darkYellow := "#D9A73E"      // Álamo, deeper tint (emphasized text, blockquote)
	darkBorder := "#2E5A46"      // Bosque-family mid green

	// Light mode colors
	lightBackground := "#FAF8F3"  // Marfil tint (near-white ivory)
	lightCurrentLine := "#F2EEE3" // Marfil tint, muted (elevated surface)
	lightSelection := "#EAE5D6"   // Marfil tint, muted deeper
	lightForeground := "#1C2B23"  // Bosque-tinted near-black (brand ink as text)
	lightComment := "#8C8879"     // muted warm gray (Marfil family, de-emphasized)
	lightPrimary := "#A8730E"     // Álamo oscuro, darkened for AA text contrast
	lightSecondary := "#1F6B4A"   // Bosque-family deep green (secondary accent)
	lightAccent := "#8F6210"      // Álamo oscuro, darker tint (strong text, numbers)
	lightRed := "#d1383d"         // Error red
	lightOrange := "#d68c27"      // Warning orange
	lightGreen := "#3d9a57"       // Success green
	lightCyan := "#318795"        // Info cyan
	lightYellow := "#B37D12"      // Álamo oscuro, mid tint (emphasized text, blockquote)
	lightBorder := "#D9D3C4"      // Marfil-muted (warm gray-tan)

	theme := &PandoTheme{}

	// Base colors
	theme.PrimaryColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.SecondaryColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.AccentColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}

	// Status colors
	theme.ErrorColor = lipgloss.AdaptiveColor{
		Dark:  darkRed,
		Light: lightRed,
	}
	theme.WarningColor = lipgloss.AdaptiveColor{
		Dark:  darkOrange,
		Light: lightOrange,
	}
	theme.SuccessColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.InfoColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}

	// Text colors
	theme.TextColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}
	theme.TextMutedColor = lipgloss.AdaptiveColor{
		Dark:  "#B9B6A9", // muted Marfil, brighter than darkComment for readability on dark bg
		Light: lightComment,
	}
	theme.TextEmphasizedColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}

	// Background colors
	theme.BackgroundColor = lipgloss.AdaptiveColor{
		Dark:  darkBackground,
		Light: lightBackground,
	}
	theme.BackgroundSecondaryColor = lipgloss.AdaptiveColor{
		Dark:  darkCurrentLine,
		Light: lightCurrentLine,
	}
	theme.BackgroundDarkerColor = lipgloss.AdaptiveColor{
		Dark:  "#0A1D16", // Bosque, darker than background
		Light: "#EFEBDE", // Marfil tint, deeper than background
	}

	// Selection colors
	theme.SelectionBackgroundColor = lipgloss.AdaptiveColor{
		Dark:  darkSelection,  // #303030 - good contrast with light text
		Light: lightSelection, // #e5e5e6 - good contrast with dark text
	}
	theme.SelectionForegroundColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,  // Marfil, bright text on dark selection
		Light: lightForeground, // brand ink
	}

	// Border colors
	theme.BorderNormalColor = lipgloss.AdaptiveColor{
		Dark:  darkBorder,
		Light: lightBorder,
	}
	theme.BorderFocusedColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.BorderDimColor = lipgloss.AdaptiveColor{
		Dark:  darkSelection,
		Light: lightSelection,
	}

	// Diff view colors
	theme.DiffAddedColor = lipgloss.AdaptiveColor{
		Dark:  "#478247",
		Light: "#2E7D32",
	}
	theme.DiffRemovedColor = lipgloss.AdaptiveColor{
		Dark:  "#7C4444",
		Light: "#C62828",
	}
	theme.DiffContextColor = lipgloss.AdaptiveColor{
		Dark:  "#a0a0a0",
		Light: "#757575",
	}
	theme.DiffHunkHeaderColor = lipgloss.AdaptiveColor{
		Dark:  "#a0a0a0",
		Light: "#757575",
	}
	theme.DiffHighlightAddedColor = lipgloss.AdaptiveColor{
		Dark:  "#DAFADA",
		Light: "#A5D6A7",
	}
	theme.DiffHighlightRemovedColor = lipgloss.AdaptiveColor{
		Dark:  "#FADADD",
		Light: "#EF9A9A",
	}
	theme.DiffAddedBgColor = lipgloss.AdaptiveColor{
		Dark:  "#303A30",
		Light: "#E8F5E9",
	}
	theme.DiffRemovedBgColor = lipgloss.AdaptiveColor{
		Dark:  "#3A3030",
		Light: "#FFEBEE",
	}
	theme.DiffContextBgColor = lipgloss.AdaptiveColor{
		Dark:  darkBackground,
		Light: lightBackground,
	}
	theme.DiffLineNumberColor = lipgloss.AdaptiveColor{
		Dark:  "#888888",
		Light: "#9E9E9E",
	}
	theme.DiffAddedLineNumberBgColor = lipgloss.AdaptiveColor{
		Dark:  "#293229",
		Light: "#C8E6C9",
	}
	theme.DiffRemovedLineNumberBgColor = lipgloss.AdaptiveColor{
		Dark:  "#332929",
		Light: "#FFCDD2",
	}

	// Markdown colors
	theme.MarkdownTextColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}
	theme.MarkdownHeadingColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.MarkdownLinkColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownLinkTextColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownCodeColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.MarkdownBlockQuoteColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.MarkdownEmphColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.MarkdownStrongColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}
	theme.MarkdownHorizontalRuleColor = lipgloss.AdaptiveColor{
		Dark:  darkComment,
		Light: lightComment,
	}
	theme.MarkdownListItemColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownListEnumerationColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownImageColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.MarkdownImageTextColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.MarkdownCodeBlockColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}

	// Syntax highlighting colors
	theme.SyntaxCommentColor = lipgloss.AdaptiveColor{
		Dark:  darkComment,
		Light: lightComment,
	}
	theme.SyntaxKeywordColor = lipgloss.AdaptiveColor{
		Dark:  darkSecondary,
		Light: lightSecondary,
	}
	theme.SyntaxFunctionColor = lipgloss.AdaptiveColor{
		Dark:  darkPrimary,
		Light: lightPrimary,
	}
	theme.SyntaxVariableColor = lipgloss.AdaptiveColor{
		Dark:  darkRed,
		Light: lightRed,
	}
	theme.SyntaxStringColor = lipgloss.AdaptiveColor{
		Dark:  darkGreen,
		Light: lightGreen,
	}
	theme.SyntaxNumberColor = lipgloss.AdaptiveColor{
		Dark:  darkAccent,
		Light: lightAccent,
	}
	theme.SyntaxTypeColor = lipgloss.AdaptiveColor{
		Dark:  darkYellow,
		Light: lightYellow,
	}
	theme.SyntaxOperatorColor = lipgloss.AdaptiveColor{
		Dark:  darkCyan,
		Light: lightCyan,
	}
	theme.SyntaxPunctuationColor = lipgloss.AdaptiveColor{
		Dark:  darkForeground,
		Light: lightForeground,
	}

	return theme
}

func init() {
	// Register the Pando theme with the theme manager
	RegisterTheme("pando", NewPandoTheme())
}
