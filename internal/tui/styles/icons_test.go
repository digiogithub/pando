package styles

import "testing"

func TestFileIconForGoFilesUsesSpecificIcon(t *testing.T) {
	if got := FileIconFor("main.go"); got == "" || got == DocumentIcon {
		t.Fatalf("expected a dedicated Go icon, got %q", got)
	}
}

func TestFolderIconsAreConfigured(t *testing.T) {
	if FolderIcon == "" || FolderOpenIcon == "" {
		t.Fatalf("expected folder icons to be configured, got closed=%q open=%q", FolderIcon, FolderOpenIcon)
	}
}

func TestSetNerdFontsSwapsIconSet(t *testing.T) {
	t.Cleanup(func() { SetNerdFonts(true) })

	SetNerdFonts(true)
	if !NerdFontsEnabled() {
		t.Fatal("expected Nerd Fonts to be enabled")
	}
	nerdCheck := CheckIcon

	SetNerdFonts(false)
	if NerdFontsEnabled() {
		t.Fatal("expected Nerd Fonts to be disabled")
	}
	if CheckIcon == nerdCheck {
		t.Fatalf("expected fallback CheckIcon to differ from Nerd Font glyph %q", nerdCheck)
	}
	if CheckIcon == "" || SpinnerIcon == "" || len(SpinnerFrames) == 0 {
		t.Fatalf("fallback icons must be non-empty: check=%q spinner=%q frames=%d", CheckIcon, SpinnerIcon, len(SpinnerFrames))
	}
}

func TestFileIconForFallsBackWithoutNerdFonts(t *testing.T) {
	t.Cleanup(func() { SetNerdFonts(true) })

	SetNerdFonts(false)
	// In fallback mode every file resolves to the generic document glyph
	// rather than an unrenderable per-type Nerd Font glyph.
	if got := FileIconFor("main.go"); got != DocumentIcon {
		t.Fatalf("expected fallback FileIconFor to return DocumentIcon %q, got %q", DocumentIcon, got)
	}

	SetNerdFonts(true)
	if got := FileIconFor("main.go"); got == "" || got == DocumentIcon {
		t.Fatalf("expected a dedicated Go icon in Nerd Font mode, got %q", got)
	}
}
