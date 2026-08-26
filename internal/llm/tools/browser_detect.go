package tools

import "github.com/digiogithub/pando/internal/browser"

// Browser detection lives in internal/browser so the design renderer can reuse
// it without importing this package. These aliases keep the existing tool, API
// and TUI call sites unchanged.

// BrowserInstall describes a detected browser installation.
type BrowserInstall = browser.BrowserInstall

// DetectInstalledBrowsers lists the browsers found on this machine.
func DetectInstalledBrowsers() []BrowserInstall { return browser.DetectInstalledBrowsers() }

// ResolveBrowserInstall resolves a configured browser type/executable pair to a
// concrete installation.
func ResolveBrowserInstall(browserType, executable string) (BrowserInstall, bool) {
	return browser.ResolveBrowserInstall(browserType, executable)
}

// NormalizeBrowserType canonicalises a configured browser type string.
func NormalizeBrowserType(value string) string { return browser.NormalizeBrowserType(value) }

// IsRemoteBrowserType reports whether a browser type is driven over a remote
// CDP endpoint instead of a locally launched process.
func IsRemoteBrowserType(browserType string) bool { return browser.IsRemoteBrowserType(browserType) }
