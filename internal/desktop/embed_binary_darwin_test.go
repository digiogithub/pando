//go:build darwin

package desktop

import (
	"io/fs"
	"testing"
)

// A Pando.app embedded without Contents/_CodeSignature extracts to a bundle
// with a broken code signature, which macOS refuses to register as an app.
func TestEmbeddedAppBundleKeepsCodeSignature(t *testing.T) {
	if !hasUsableEmbeddedAppBundle() {
		t.Skip("no Pando.app embedded (run make desktop-embed)")
	}
	if _, err := fs.Stat(DesktopBundle, "bin/Pando.app/Contents/_CodeSignature/CodeResources"); err != nil {
		t.Fatalf("embedded Pando.app lost its code signature: %v", err)
	}
}
