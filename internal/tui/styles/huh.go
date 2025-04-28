package styles

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/opencode-ai/opencode/internal/tui/theme"
)

// returns a huh.Theme configured with the current app theme colors
func HuhTheme() *huh.Theme {
	t := huh.ThemeBase()
	currentTheme := theme.CurrentTheme()

	// Base theme elements
	bgColor := currentTheme.Background()
	bgSecondaryColor := currentTheme.BackgroundSecondary()
	textColor := currentTheme.Text()
	textMutedColor := currentTheme.TextMuted()
	primaryColor := currentTheme.Primary()
	secondaryColor := currentTheme.Secondary()
	// accentColor := currentTheme.Accent()
	errorColor := currentTheme.Error()
	successColor := currentTheme.Success()
	// warningColor := currentTheme.Warning()
	// infoColor := currentTheme.Info()
	borderColor := currentTheme.BorderNormal()
	borderFocusedColor := currentTheme.BorderFocused()

	// Focused styles
	t.Focused.Base = t.Focused.Base.
		BorderStyle(lipgloss.HiddenBorder()).
		Background(bgColor).
		BorderForeground(borderColor)

	t.Focused.Title = t.Focused.Title.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.NoteTitle = t.Focused.NoteTitle.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.Directory = t.Focused.Directory.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.Description = t.Focused.Description.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.
		Foreground(errorColor).
		Background(bgColor)

	t.Focused.ErrorMessage = t.Focused.ErrorMessage.
		Foreground(errorColor).
		Background(bgColor)

	t.Focused.SelectSelector = t.Focused.SelectSelector.
		Foreground(primaryColor).
		Background(bgColor)

	t.Focused.NextIndicator = t.Focused.NextIndicator.
		Foreground(primaryColor).
		Background(bgColor)

	t.Focused.PrevIndicator = t.Focused.PrevIndicator.
		Foreground(primaryColor).
		Background(bgColor)

	t.Focused.Option = t.Focused.Option.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.
		Foreground(primaryColor).
		Background(bgColor)

	t.Focused.SelectedOption = t.Focused.SelectedOption.
		Foreground(successColor).
		Background(bgColor)

	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.
		Foreground(successColor).
		Background(bgColor)

	t.Focused.UnselectedPrefix = t.Focused.UnselectedPrefix.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.UnselectedOption = t.Focused.UnselectedOption.
		Foreground(textColor).
		Background(bgColor)

	t.Focused.FocusedButton = t.Focused.FocusedButton.
		Foreground(bgColor).
		Background(primaryColor).
		BorderForeground(borderFocusedColor)

	t.Focused.BlurredButton = t.Focused.BlurredButton.
		Foreground(textColor).
		Background(bgSecondaryColor).
		BorderForeground(borderColor)

	// Text input styles
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.
		Foreground(secondaryColor).
		Background(bgColor)

	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.
		Foreground(primaryColor).
		Background(bgColor)

	t.Focused.TextInput.Text = t.Focused.TextInput.Text.
		Foreground(textColor).
		Background(bgColor)

	// Blur and focus states should be similar
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.
		BorderStyle(lipgloss.HiddenBorder()).
		Background(bgColor)

	// Help styles
	t.Help.Ellipsis = t.Help.Ellipsis.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Help.ShortKey = t.Help.ShortKey.
		Foreground(primaryColor).
		Background(bgColor)

	t.Help.ShortDesc = t.Help.ShortDesc.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Help.ShortSeparator = t.Help.ShortSeparator.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Help.FullKey = t.Help.FullKey.
		Foreground(primaryColor).
		Background(bgColor)

	t.Help.FullDesc = t.Help.FullDesc.
		Foreground(textMutedColor).
		Background(bgColor)

	t.Help.FullSeparator = t.Help.FullSeparator.
		Foreground(textMutedColor).
		Background(bgColor)

	return t
}

