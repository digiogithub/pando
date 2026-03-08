package core

import (
	"strings"
	"testing"
)

func TestHelpWidgetUsesCtrlHShortcut(t *testing.T) {
	widget := getHelpWidget()
	if !strings.Contains(widget, "ctrl+h help") {
		t.Fatalf("expected help widget to mention ctrl+h, got %q", widget)
	}
}
