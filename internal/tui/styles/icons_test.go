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
