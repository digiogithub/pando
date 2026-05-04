package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type BrowserInstall struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Executable  string `json:"executable"`
	UserDataDir string `json:"userDataDir,omitempty"`
	ProfileDir  string `json:"profileDir,omitempty"`
}

type browserCandidate struct {
	Type      string
	Label     string
	ExecNames []string
	Paths     []string
	Profiles  []string
}

var supportedBrowserCandidates = []browserCandidate{
	{
		Type:      "chrome",
		Label:     "Google Chrome",
		ExecNames: []string{"google-chrome", "google-chrome-stable", "chrome", "chrome.exe"},
		Paths: []string{
			`C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe`,
			`C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe`,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/snap/bin/chromium",
		},
		Profiles: []string{
			"~/.config/google-chrome/Default",
			"~/Library/Application Support/Google/Chrome",
			`%LOCALAPPDATA%\\Google\\Chrome\\User Data\\Default`,
		},
	},
	{
		Type:      "msedge",
		Label:     "Microsoft Edge",
		ExecNames: []string{"microsoft-edge", "microsoft-edge-stable", "msedge", "msedge.exe"},
		Paths: []string{
			`C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe`,
			`C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe`,
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
		},
		Profiles: []string{
			"~/.config/microsoft-edge/Default",
			"~/Library/Application Support/Microsoft Edge",
			`%LOCALAPPDATA%\\Microsoft\\Edge\\User Data\\Default`,
		},
	},
	{
		Type:      "chromium",
		Label:     "Chromium",
		ExecNames: []string{"chromium", "chromium-browser", "chromium.exe"},
		Paths: []string{
			`C:\\Program Files\\Chromium\\Application\\chrome.exe`,
			`C:\\Program Files (x86)\\Chromium\\Application\\chrome.exe`,
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		},
		Profiles: []string{
			"~/.config/chromium/Default",
			"~/Library/Application Support/Chromium",
			`%LOCALAPPDATA%\\Chromium\\User Data\\Default`,
		},
	},
	{
		Type:      "opera",
		Label:     "Opera",
		ExecNames: []string{"opera", "opera-stable", "launcher.exe"},
		Paths: []string{
			`C:\\Users\\%USERNAME%\\AppData\\Local\\Programs\\Opera\\launcher.exe`,
			`C:\\Program Files\\Opera\\launcher.exe`,
			"/Applications/Opera.app/Contents/MacOS/Opera",
			"/usr/bin/opera",
			"/usr/bin/opera-stable",
		},
		Profiles: []string{
			"~/.config/opera/Default",
			"~/Library/Application Support/com.operasoftware.Opera",
			`%APPDATA%\\Opera Software\\Opera Stable\\Default`,
		},
	},
}

func DetectInstalledBrowsers() []BrowserInstall {
	installs := make([]BrowserInstall, 0, len(supportedBrowserCandidates))
	seen := make(map[string]struct{}, len(supportedBrowserCandidates))
	for _, candidate := range supportedBrowserCandidates {
		install, ok := detectBrowserInstall(candidate)
		if !ok {
			continue
		}
		if _, exists := seen[install.Type]; exists {
			continue
		}
		seen[install.Type] = struct{}{}
		installs = append(installs, install)
	}
	return installs
}

func ResolveBrowserInstall(browserType, executable string) (BrowserInstall, bool) {
	browserType = NormalizeBrowserType(browserType)
	if strings.TrimSpace(executable) != "" {
		resolved := expandBrowserPath(executable)
		if fileExists(resolved) {
			userDataDir, profileDir := detectBrowserProfile(browserType)
			return BrowserInstall{Type: browserType, Label: browserLabel(browserType), Executable: resolved, UserDataDir: userDataDir, ProfileDir: profileDir}, true
		}
		if lookedUp, err := exec.LookPath(executable); err == nil {
			userDataDir, profileDir := detectBrowserProfile(browserType)
			return BrowserInstall{Type: browserType, Label: browserLabel(browserType), Executable: lookedUp, UserDataDir: userDataDir, ProfileDir: profileDir}, true
		}
	}
	for _, install := range DetectInstalledBrowsers() {
		if install.Type == browserType {
			return install, true
		}
	}
	return BrowserInstall{}, false
}

func NormalizeBrowserType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "chrome", "google-chrome":
		return "chrome"
	case "msedge", "edge", "microsoft-edge":
		return "msedge"
	case "chromium", "chromium-browser":
		return "chromium"
	case "opera", "opera-stable":
		return "opera"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func browserLabel(browserType string) string {
	for _, candidate := range supportedBrowserCandidates {
		if candidate.Type == NormalizeBrowserType(browserType) {
			return candidate.Label
		}
	}
	return browserType
}

func detectBrowserInstall(candidate browserCandidate) (BrowserInstall, bool) {
	userDataDir, profileDir := detectBrowserProfile(candidate.Type)
	for _, name := range candidate.ExecNames {
		if lookedUp, err := exec.LookPath(name); err == nil {
			return BrowserInstall{Type: candidate.Type, Label: candidate.Label, Executable: lookedUp, UserDataDir: userDataDir, ProfileDir: profileDir}, true
		}
	}
	for _, path := range candidate.Paths {
		expanded := expandBrowserPath(path)
		if fileExists(expanded) {
			return BrowserInstall{Type: candidate.Type, Label: candidate.Label, Executable: expanded, UserDataDir: userDataDir, ProfileDir: profileDir}, true
		}
	}
	return BrowserInstall{}, false
}

func detectBrowserProfile(browserType string) (string, string) {
	for _, candidate := range supportedBrowserCandidates {
		if candidate.Type != NormalizeBrowserType(browserType) {
			continue
		}
		for _, profile := range candidate.Profiles {
			expanded := expandBrowserPath(profile)
			if dirExists(expanded) {
				return inferBrowserUserDataDir(expanded), inferBrowserProfileDir(expanded)
			}
		}
	}
	return "", ""
}

func inferBrowserUserDataDir(path string) string {
	cleaned := filepath.Clean(path)
	base := filepath.Base(cleaned)
	if strings.EqualFold(base, "Default") || strings.HasPrefix(strings.ToLower(base), "profile ") {
		return filepath.Dir(cleaned)
	}
	return cleaned
}

func inferBrowserProfileDir(path string) string {
	cleaned := filepath.Clean(path)
	base := filepath.Base(cleaned)
	if strings.EqualFold(base, "Default") || strings.HasPrefix(strings.ToLower(base), "profile ") {
		return base
	}
	return ""
}

func expandBrowserPath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if runtime.GOOS == "windows" {
		path = os.Expand(path, func(key string) string {
			return os.Getenv(key)
		})
	}
	return filepath.Clean(path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
