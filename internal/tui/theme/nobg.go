package theme

import "github.com/charmbracelet/lipgloss"

// transparent is an empty adaptive color that tells lipgloss not to set any background.
var transparent = lipgloss.AdaptiveColor{Dark: "", Light: ""}

// NoBackgroundWrapper wraps any Theme and disables all background colors,
// letting the terminal's own background show through.
type NoBackgroundWrapper struct {
	Theme
}

func (w *NoBackgroundWrapper) HasBackground() bool                       { return false }
func (w *NoBackgroundWrapper) Background() lipgloss.AdaptiveColor        { return transparent }
func (w *NoBackgroundWrapper) BackgroundSecondary() lipgloss.AdaptiveColor { return transparent }
func (w *NoBackgroundWrapper) BackgroundDarker() lipgloss.AdaptiveColor  { return transparent }

// WrapNoBackground returns a copy of t with all background colors removed.
func WrapNoBackground(t Theme) Theme {
	return &NoBackgroundWrapper{Theme: t}
}
