package config

import "testing"

// TestNormalizeInternalToolsDefaultsReplacesZeros covers the case a config
// rewrite creates: the desktop and browser sizing knobs stamped into the file
// as an explicit 0, which shadows their viper defaults on every later load.
func TestNormalizeInternalToolsDefaultsReplacesZeros(t *testing.T) {
	isolateGlobalConfig(t)
	previous := cfg
	t.Cleanup(func() { cfg = previous })

	cfg = &Config{InternalTools: InternalToolsConfig{
		DesktopEnabled: true,
		// Everything else deliberately left at its zero value.
	}}

	normalizeInternalToolsDefaults()

	it := cfg.InternalTools
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"FetchMaxSizeMB", it.FetchMaxSizeMB, defaultFetchMaxSizeMB},
		{"BrowserTimeout", it.BrowserTimeout, defaultBrowserTimeoutSeconds},
		{"BrowserMaxSessions", it.BrowserMaxSessions, defaultBrowserMaxSessions},
		{"DesktopMaxNodes", it.DesktopMaxNodes, defaultDesktopMaxNodes},
		{"DesktopDefaultDepth", it.DesktopDefaultDepth, defaultDesktopDepth},
		{"DesktopActionTimeout", it.DesktopActionTimeout, defaultDesktopActionTimeoutS},
		{"DesktopSnapshotTTL", it.DesktopSnapshotTTL, defaultDesktopSnapshotTTLSecs},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if it.DesktopScreenshotScale != defaultDesktopScreenshotScale {
		t.Errorf("DesktopScreenshotScale = %v, want %v", it.DesktopScreenshotScale, defaultDesktopScreenshotScale)
	}
	if it.DesktopBackend != defaultDesktopBackend {
		t.Errorf("DesktopBackend = %q, want %q", it.DesktopBackend, defaultDesktopBackend)
	}
}

// TestNormalizeInternalToolsDefaultsKeepsExplicitValues makes sure the
// normalization only fills in non-positive knobs and never overrides a value
// the user actually chose.
func TestNormalizeInternalToolsDefaultsKeepsExplicitValues(t *testing.T) {
	isolateGlobalConfig(t)
	previous := cfg
	t.Cleanup(func() { cfg = previous })

	cfg = &Config{InternalTools: InternalToolsConfig{
		FetchMaxSizeMB:         25,
		BrowserTimeout:         5,
		BrowserMaxSessions:     1,
		DesktopBackend:         "atspi",
		DesktopMaxNodes:        50,
		DesktopDefaultDepth:    1,
		DesktopActionTimeout:   2,
		DesktopSnapshotTTL:     5,
		DesktopScreenshotScale: 0.5,
	}}

	normalizeInternalToolsDefaults()

	it := cfg.InternalTools
	if it.FetchMaxSizeMB != 25 || it.BrowserTimeout != 5 || it.BrowserMaxSessions != 1 {
		t.Fatalf("fetch/browser knobs were overwritten: %+v", it)
	}
	if it.DesktopBackend != "atspi" || it.DesktopMaxNodes != 50 || it.DesktopDefaultDepth != 1 {
		t.Fatalf("desktop knobs were overwritten: %+v", it)
	}
	if it.DesktopActionTimeout != 2 || it.DesktopSnapshotTTL != 5 || it.DesktopScreenshotScale != 0.5 {
		t.Fatalf("desktop timing knobs were overwritten: %+v", it)
	}
}
