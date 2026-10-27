package i18n

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestLocalesInSyncWithWebUI fails when the Web UI catalogs change without
// the copies here: run `cp web-ui/src/i18n/locales/*.json
// internal/nativeui/i18n/locales/`.
func TestLocalesInSyncWithWebUI(t *testing.T) {
	web := filepath.Join("..", "..", "..", "web-ui", "src", "i18n", "locales")
	files, err := filepath.Glob(filepath.Join(web, "*.json"))
	if err != nil || len(files) == 0 {
		t.Skip("web-ui locales not available")
	}
	for _, f := range files {
		want, _ := os.ReadFile(f)
		got, err := localeFS.ReadFile("locales/" + filepath.Base(f))
		if err != nil {
			t.Errorf("%s missing from internal/nativeui/i18n/locales", filepath.Base(f))
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s differs from the Web UI copy", filepath.Base(f))
		}
	}
}

func TestTranslate(t *testing.T) {
	defer SetLanguage("en")
	if got := T("nav.chat"); got != "Chat" {
		t.Errorf("nav.chat = %q", got)
	}
	if got := T("chat.files.changed", "count", 1); got != "1 file changed" {
		t.Errorf("plural one = %q", got)
	}
	if got := T("chat.files.changed", "count", 3); got != "3 files changed" {
		t.Errorf("plural other = %q", got)
	}
	if SetLanguage("es-ES") != "es" {
		t.Error("region fallback")
	}
	if got := T("nav.settings"); got == "Settings" || got == "nav.settings" {
		t.Errorf("es nav.settings = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key = %q", got)
	}
	if SetLanguage("xx") != "en" {
		t.Error("unknown language should select English")
	}
}
