package theme

import "github.com/charmbracelet/lipgloss"

// transparent is an empty adaptive color — lipgloss emits no background ANSI
// sequence when a style's background is set to this.
var transparent = lipgloss.AdaptiveColor{Dark: "", Light: ""}

// NoBackgroundWrapper wraps any Theme and suppresses its background colors so
// the terminal's own background shows through.
//
// Background(), BackgroundSecondary(), and BackgroundDarker() all return an
// empty AdaptiveColor so that lipgloss does not emit a background sequence.
// BadgeText() is intentionally NOT suppressed: it must remain a real color so
// that text rendered ON colored badge surfaces (status bar, cursor) is still
// legible.
type NoBackgroundWrapper struct {
	Theme
}

func (w *NoBackgroundWrapper) HasBackground() bool                         { return false }
func (w *NoBackgroundWrapper) Background() lipgloss.AdaptiveColor          { return transparent }
func (w *NoBackgroundWrapper) BackgroundSecondary() lipgloss.AdaptiveColor { return transparent }
func (w *NoBackgroundWrapper) BackgroundDarker() lipgloss.AdaptiveColor    { return transparent }

// WrapNoBackground returns a view of t with all background colors suppressed.
func WrapNoBackground(t Theme) Theme {
	return &NoBackgroundWrapper{Theme: t}
}
