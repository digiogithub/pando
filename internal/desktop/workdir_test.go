package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLoginPathIgnoresShellNoise(t *testing.T) {
	out := []byte("welcome banner\n" + loginPathMarker + "/opt/homebrew/bin:/usr/bin\nbye\n")
	if got := parseLoginPath(out); got != "/opt/homebrew/bin:/usr/bin" {
		t.Fatalf("parseLoginPath = %q", got)
	}
	if got := parseLoginPath([]byte("no marker")); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestMergePathListsKeepsOrderWithoutDuplicates(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := mergePathLists("/a"+sep+"/b", "/b"+sep+"/c"+sep+"")
	if want := "/a" + sep + "/b" + sep + "/c"; got != want {
		t.Fatalf("mergePathLists = %q, want %q", got, want)
	}
}

func TestDefaultWorkingDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(LaunchedFromAppEnv, "1")
	got, err := DefaultWorkingDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(home) {
		t.Fatalf("DefaultWorkingDir = %q, want home %q", got, home)
	}

	t.Setenv(LaunchedFromAppEnv, "")
	wd, _ := os.Getwd()
	if got, _ := DefaultWorkingDir(); got != wd {
		t.Fatalf("terminal launch: DefaultWorkingDir = %q, want %q", got, wd)
	}
}
