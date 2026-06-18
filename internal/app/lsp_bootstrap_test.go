package app

import "testing"

func TestBootstrapShouldExcludeDir(t *testing.T) {
	cases := map[string]bool{
		"/repo/src":          false,
		"/repo/internal/app": false,
		"/repo/.git":         true,
		"/repo/.idea":        true,
		"/repo/node_modules": true,
		"/repo/vendor":       true,
		"/repo/dist":         true,
		"/repo/target":       true,
	}
	for path, want := range cases {
		if got := bootstrapShouldExcludeDir(path); got != want {
			t.Errorf("bootstrapShouldExcludeDir(%q) = %v, want %v", path, got, want)
		}
	}
}
