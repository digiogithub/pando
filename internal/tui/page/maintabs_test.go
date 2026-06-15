package page

import (
	"strings"
	"testing"
)

func TestMainTabIndexFor(t *testing.T) {
	cases := []struct {
		mode ChatLayoutMode
		want int
	}{
		{ChatOnly, 0},
		{SidebarChat, 0},
		{SidebarEditor, 1},
		{EditorChatSplit, 2},
		{EditorChatTab, 2},
	}
	for _, c := range cases {
		if got := mainTabIndexFor(c.mode); got != c.want {
			t.Errorf("mainTabIndexFor(%v) = %d, want %d", c.mode, got, c.want)
		}
	}
}

func TestMainTabModeFor(t *testing.T) {
	if mode, ok := mainTabModeFor(0); !ok || mode != ChatOnly {
		t.Errorf("tab 0 = (%v, %v), want (ChatOnly, true)", mode, ok)
	}
	if mode, ok := mainTabModeFor(1); !ok || mode != SidebarEditor {
		t.Errorf("tab 1 = (%v, %v), want (SidebarEditor, true)", mode, ok)
	}
	if mode, ok := mainTabModeFor(2); !ok || mode != EditorChatSplit {
		t.Errorf("tab 2 = (%v, %v), want (EditorChatSplit, true)", mode, ok)
	}
	if _, ok := mainTabModeFor(99); ok {
		t.Error("out-of-range tab index should report not ok")
	}
}

func TestMainTabBarViewRendersLabels(t *testing.T) {
	bar := newMainTabBar()
	if got := bar.View(ChatOnly); got != "" {
		t.Errorf("zero-width bar should render empty, got %q", got)
	}

	bar.SetWidth(80)
	view := bar.View(ChatOnly)
	for _, label := range []string{"Chat", "Editor", "Editor+Chat"} {
		if !strings.Contains(view, label) {
			t.Errorf("rendered tab bar missing label %q", label)
		}
	}
}
