package page

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAdvanceLogoIdleRestoresPandoIcon(t *testing.T) {
	b := newMainTabBar()
	b.AdvanceLogo(true)
	if b.logoFrame < 0 {
		t.Fatalf("expected an animation frame while busy, got %d", b.logoFrame)
	}
	b.AdvanceLogo(false)
	if b.logoFrame != -1 {
		t.Fatalf("expected idle frame when not busy, got %d", b.logoFrame)
	}
}

func TestAdvanceLogoNeverRepeatsFrame(t *testing.T) {
	b := newMainTabBar()
	prev := -1
	seen := map[int]bool{}
	for range 200 {
		b.AdvanceLogo(true)
		if b.logoFrame == prev {
			t.Fatalf("frame %d repeated back to back", b.logoFrame)
		}
		if b.logoFrame < 0 || b.logoFrame >= len(logoAnimFrames) {
			t.Fatalf("frame %d out of range", b.logoFrame)
		}
		seen[b.logoFrame] = true
		prev = b.logoFrame
	}
	if len(seen) != len(logoAnimFrames) {
		t.Fatalf("expected every frame to be reachable, saw %d of %d", len(seen), len(logoAnimFrames))
	}
}

func TestLogoGlyphWidthIsStable(t *testing.T) {
	b := newMainTabBar()
	want := lipgloss.Width(b.logoGlyph())
	if want != logoGlyphWidth {
		t.Fatalf("idle glyph width = %d, want %d", want, logoGlyphWidth)
	}
	for i := range logoAnimFrames {
		b.logoFrame = i
		if got := lipgloss.Width(b.logoGlyph()); got != logoGlyphWidth {
			t.Fatalf("frame %q width = %d, want %d", logoAnimFrames[i], got, logoGlyphWidth)
		}
		if strings.TrimSpace(b.logoGlyph()) != logoAnimFrames[i] {
			t.Fatalf("frame %d glyph = %q, want %q", i, b.logoGlyph(), logoAnimFrames[i])
		}
	}
}
