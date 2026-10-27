package app

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/digiogithub/pando/internal/nativeui/shell"
)

var (
	authToastMu   sync.Mutex
	authToastLast time.Time
)

func (a *Context) authToastAllowed() bool {
	authToastMu.Lock()
	defer authToastMu.Unlock()
	if time.Since(authToastLast) < time.Minute {
		return false
	}
	authToastLast = time.Now()
	return true
}

func isAuthErr(err error) bool     { return client.IsAuthError(err) }
func describeErr(err error) string { return client.DescribeError(err) }

// legacyFamilies maps v1 (brand-named) theme families to their v2
// replacement, as the Web UI's parseThemeId.
var legacyFamilies = map[string]kit.Family{"claude": "paper", "clay": "paper", "starbucks": "forest"}

// ParseThemeID parses a backend theme id ("family-mode", the config field
// shared with the TUI). ok is false for ids the desktop does not know
// (TUI-only themes such as "pando-nobg").
func ParseThemeID(id string) (kit.Family, kit.Mode, bool) {
	i := strings.LastIndex(id, "-")
	if i <= 0 {
		return "", "", false
	}
	fam, mode := kit.Family(id[:i]), kit.Mode(id[i+1:])
	if mode != kit.ModeLight && mode != kit.ModeDark && mode != kit.ModeSystem {
		return "", "", false
	}
	if f, ok := legacyFamilies[string(fam)]; ok {
		fam = f
	}
	if !slices.Contains(kit.Families(), fam) {
		return "", "", false
	}
	return fam, mode, true
}

// ApplyPrefs applies persisted UI preferences (theme, language, sidebar) to
// the live state. UI thread.
func (a *Context) ApplyPrefs(p client.UIPrefs) {
	t := a.Theme
	if f := kit.Family(p.Theme); slices.Contains(kit.Families(), f) {
		t.Family = f
	}
	switch m := kit.Mode(p.ThemeMode); m {
	case kit.ModeSystem, kit.ModeLight, kit.ModeDark:
		t.Mode = m
	}
	if p.ThemeAccent != "" && slices.Contains(kit.Accents(), kit.Accent(p.ThemeAccent)) {
		t.Accent = kit.Accent(p.ThemeAccent)
	}
	a.Theme = t
	if p.Language != "" {
		i18n.SetLanguage(p.Language)
	}
	a.localizeTray()
	if p.SidebarCollapsed != nil {
		a.Shell().SidebarCollapsed = *p.SidebarCollapsed
	}
	a.Invalidate()
}

// TrayLabels returns the tray menu labels in the UI language (Web UI keys
// where they exist; English defaults otherwise).
func (a *Context) TrayLabels() shell.TrayLabels {
	return shell.TrayLabels{Show: a.T("shell.restoreWindow"), Settings: a.T("nav.settings"), Projects: a.T("nav.projects")}
}

// localizeTray relabels the tray menu after a language change.
func (a *Context) localizeTray() {
	if h, ok := a.Shell().Host.(interface{ SetTrayLabels(shell.TrayLabels) }); ok {
		h.SetTrayLabels(a.TrayLabels())
	}
}

// SavePrefs persists a preferences patch in the background (the server
// merges it). A failure is logged, and a 401 is surfaced.
func (a *Context) SavePrefs(patch client.UIPrefs) {
	if a.Client == nil {
		return
	}
	a.Go(func(ctx context.Context) {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if _, err := a.Client.PutUIPrefs(cctx, patch); err != nil {
			if isAuthErr(err) {
				a.ReportError("Could not save preferences", err)
				return
			}
			a.Log.Warn("saving UI preferences failed", "error", err)
		}
	})
}

// SetTheme selects and persists the theme. UI thread.
func (a *Context) SetTheme(s kit.Settings) {
	a.Theme = s
	a.Invalidate()
	a.SavePrefs(client.UIPrefs{Theme: string(s.Family), ThemeMode: string(s.Mode), ThemeAccent: string(s.Accent)})
}

// CycleThemeMode cycles system -> light -> dark and persists it.
func (a *Context) CycleThemeMode() {
	t := a.Theme
	switch t.Mode {
	case kit.ModeLight:
		t.Mode = kit.ModeDark
	case kit.ModeDark:
		t.Mode = kit.ModeSystem
	default:
		t.Mode = kit.ModeLight
	}
	a.SetTheme(t)
}

// SetLanguage selects and persists the UI language. UI thread.
func (a *Context) SetLanguage(lang string) {
	lang = i18n.SetLanguage(strings.TrimSpace(lang))
	a.localizeTray()
	a.Invalidate()
	a.SavePrefs(client.UIPrefs{Language: lang})
}

// SetSidebarCollapsed collapses or expands the sidebar and persists it.
func (a *Context) SetSidebarCollapsed(v bool) {
	a.Shell().SidebarCollapsed = v
	a.SavePrefs(client.UIPrefs{SidebarCollapsed: &v})
}

// ToggleSimpleChat switches between the full and the simple chat and
// remembers the choice (chatMode).
func (a *Context) ToggleSimpleChat() {
	if a.Router.Path() == "/chat/simple" {
		a.Router.Push("/chat")
		a.SavePrefs(client.UIPrefs{ChatMode: "advanced"})
		return
	}
	a.Router.Push("/chat/simple")
	a.SavePrefs(client.UIPrefs{ChatMode: "simple"})
}

// homeRe matches a home directory prefix of another user or machine.
var homeRe = regexp.MustCompile(`^(/home/[^/]+|/Users/[^/]+)`)

// ShortenPath replaces the home directory with ~ (the Web UI's
// shortenPath), also for paths under another home (remote instances).
func ShortenPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return homeRe.ReplaceAllString(p, "~")
}
