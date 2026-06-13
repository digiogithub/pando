package theme

// NoBackgroundWrapper wraps any Theme and signals that the terminal's own
// background should show through instead of the theme's background colors.
//
// It does NOT modify the color values returned by Background(),
// BackgroundSecondary(), or BackgroundDarker(). Those colors are still needed
// when used as foreground text on colored badges (e.g., status bar widgets).
// The places that must skip setting backgrounds already check HasBackground().
type NoBackgroundWrapper struct {
	Theme
}

func (w *NoBackgroundWrapper) HasBackground() bool { return false }

// WrapNoBackground returns a view of t that reports HasBackground() == false.
func WrapNoBackground(t Theme) Theme {
	return &NoBackgroundWrapper{Theme: t}
}
