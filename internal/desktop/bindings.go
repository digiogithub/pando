package desktop

import (
	"github.com/digiogithub/pando/internal/version"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetVersion returns the current Pando version string.
func (a *App) GetVersion() string {
	return version.Version
}

// SelectDirectory opens a native OS directory picker dialog.
// Returns the selected path or empty string if cancelled.
func (a *App) SelectDirectory() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Directory",
	})
	if err != nil {
		return ""
	}
	return dir
}

// OpenFileDialog opens a native OS file picker dialog.
func (a *App) OpenFileDialog(title string) string {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return file
}

// SaveFileDialog opens a native OS save dialog.
func (a *App) SaveFileDialog(title, defaultFilename string) string {
	file, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
	})
	if err != nil {
		return ""
	}
	return file
}

// WindowMinimise minimises the desktop window.
func (a *App) WindowMinimise() {
	runtime.WindowMinimise(a.ctx)
}

// WindowMaximise maximises the desktop window.
func (a *App) WindowMaximise() {
	runtime.WindowMaximise(a.ctx)
}

// WindowToggleMaximise toggles between maximised and normal window state.
func (a *App) WindowToggleMaximise() {
	runtime.WindowToggleMaximise(a.ctx)
}

// WindowSetTitle sets the desktop window title bar text.
func (a *App) WindowSetTitle(title string) {
	runtime.WindowSetTitle(a.ctx, title)
}

// OpenInBrowser opens the given URL in the system default browser.
func (a *App) OpenInBrowser(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

// ShowMessageDialog shows a native OS message dialog and returns the selected button.
func (a *App) ShowMessageDialog(title, message string) string {
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
	if err != nil {
		return ""
	}
	return result
}
