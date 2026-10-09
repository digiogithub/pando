package app

import "testing"

func TestBootstrapExcluder(t *testing.T) {
	root := t.TempDir()
	e := newWatchExcluder(root)
	for _, n := range []string{".git", ".idea", "node_modules", "vendor", "dist", "target", "DerivedData", "Pods"} {
		if !e.ShouldSkipDir(root + "/" + n) {
			t.Errorf("%s should be excluded", n)
		}
	}
	for _, n := range []string{"src", "internal/app"} {
		if e.ShouldSkipDir(root + "/" + n) {
			t.Errorf("%s should not be excluded", n)
		}
	}
}
