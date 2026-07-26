package mcpauth

import (
	"os"
	"testing"
)

// TestMain isolates every test in this package from the real user home
// directory before any test runs. The AGE-at-rest encryption wired into
// Store (see crypto.go, store.go) transparently loads/generates an AGE
// identity under $HOME/.config/pando/keys on every encrypt/decrypt call;
// without this, running `go test ./internal/mcpauth/...` would read and
// write real key material into the developer's actual home directory.
// Redirecting HOME here, once, gives every test in the package (existing and
// new) a private, disposable home for free.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "pando-mcpauth-test-home-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", "")

	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
