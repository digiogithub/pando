package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
)

// withTelemetryCLIConfig isolates $HOME/$XDG_CONFIG_HOME to an empty temp dir,
// chdirs into a second throwaway "project" directory, and forces a fresh
// config load. Telemetry is a GLOBAL-only setting (internal/config/telemetry.go):
// every mutating action persists through config.updateGlobalCfgFile, which
// falls back to the real user's $HOME when not isolated.
//
// config.Load is a process-wide singleton loader: once the package-level
// config is non-nil, Load(...) just returns it unchanged and ignores its
// arguments entirely (see internal/config/config.go, "if cfg != nil { return
// cfg, nil }"). config.ResetForTests() is what clears that cached config (plus
// viper's accumulated state), so the Load call below actually re-reads
// instead of being a no-op that silently keeps whatever a previous test in
// this same test binary left behind — including, on a machine where telemetry
// was ever enabled for real, that real enabled state and debug id.
//
// Every runTelemetry* helper calls loadTelemetryConfig(), which resolves its
// project directory via os.Getwd() — exactly like a real "pando telemetry ..."
// invocation does from whatever directory the user runs it in. Because Load
// is a singleton, those later calls just return the config loaded below
// without re-resolving os.Getwd() at all; the chdir here only keeps things
// consistent in case that ever changes.
func withTelemetryCLIConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	projectDir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("os.Chdir(%q): %v", projectDir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore cwd %q: %v", origWD, err)
		}
	})

	config.ResetForTests()
	t.Cleanup(config.ResetForTests)

	if _, err := config.Load(projectDir, false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
}

// newTelemetryTestCmd returns a bare cobra.Command wired to capture stdout,
// suitable for calling the runTelemetry* helpers directly (bypassing
// cobra.Execute and flag parsing, which the package-level telemetryJSON var
// already covers separately).
func newTelemetryTestCmd() (*cobra.Command, *bytes.Buffer) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	return cmd, &buf
}

func TestRunTelemetryStatusUnavailableByDefault(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetryCLIConfig(t)

	cmd, buf := newTelemetryTestCmd()
	if err := runTelemetryStatus(cmd); err != nil {
		t.Fatalf("runTelemetryStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"available:  no", "enabled:    no", "min level:  info", "Unavailable in this build"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestRunTelemetryStatusJSON(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	withTelemetryCLIConfig(t)

	telemetryJSON = true
	t.Cleanup(func() { telemetryJSON = false })

	cmd, buf := newTelemetryTestCmd()
	if err := runTelemetryStatus(cmd); err != nil {
		t.Fatalf("runTelemetryStatus: %v", err)
	}

	var view telemetryStatusView
	if err := json.Unmarshal(buf.Bytes(), &view); err != nil {
		t.Fatalf("status --json did not produce valid JSON: %v\n%s", err, buf.String())
	}
	if !view.Available {
		t.Error("Available should be true with PANDO_TELEMETRY_TOKEN set")
	}
	if view.Enabled {
		t.Error("Enabled should default to false")
	}
	if view.MinLevel != config.TelemetryLevelInfo {
		t.Errorf("MinLevel = %q, want %q", view.MinLevel, config.TelemetryLevelInfo)
	}
}

func TestRunTelemetryEnableRefusedWithoutToken(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "")
	withTelemetryCLIConfig(t)

	cmd, _ := newTelemetryTestCmd()
	err := runTelemetryEnable(cmd)
	if err == nil {
		t.Fatal("enable without a token should fail")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error should explain telemetry is unavailable: %v", err)
	}
	if config.Get().Telemetry.Enabled {
		t.Fatal("Enabled must stay false when the enable is refused")
	}
}

func TestRunTelemetryEnableIDDisableRegenerateFlow(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_TOKEN", "test")
	withTelemetryCLIConfig(t)

	// enable prints a 19-char grouped debug id.
	enableCmd, enableBuf := newTelemetryTestCmd()
	if err := runTelemetryEnable(enableCmd); err != nil {
		t.Fatalf("enable: %v", err)
	}
	firstID := config.Get().Telemetry.DebugID
	if len(firstID) != 16 {
		t.Fatalf("DebugID = %q, want 16 digits", firstID)
	}
	grouped := config.TelemetryDebugIDDisplay()
	if len(grouped) != 19 {
		t.Fatalf("grouped debug id = %q (len %d), want len 19", grouped, len(grouped))
	}
	if !strings.Contains(enableBuf.String(), grouped) {
		t.Fatalf("enable output missing the grouped debug id:\n%s", enableBuf.String())
	}

	// id prints exactly the same grouped id, script-friendly (single line).
	idCmd, idBuf := newTelemetryTestCmd()
	if err := runTelemetryID(idCmd); err != nil {
		t.Fatalf("id: %v", err)
	}
	if got := strings.TrimSpace(idBuf.String()); got != grouped {
		t.Fatalf("id output = %q, want %q", got, grouped)
	}

	// disable keeps the debug id.
	disableCmd, _ := newTelemetryTestCmd()
	if err := runTelemetryDisable(disableCmd); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if config.Get().Telemetry.Enabled {
		t.Fatal("Enabled should be false after disable")
	}
	if config.Get().Telemetry.DebugID != firstID {
		t.Fatalf("disable changed the debug id: got %q, want %q", config.Get().Telemetry.DebugID, firstID)
	}

	// re-enable reuses the same id.
	reenableCmd, _ := newTelemetryTestCmd()
	if err := runTelemetryEnable(reenableCmd); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if config.Get().Telemetry.DebugID != firstID {
		t.Fatalf("re-enable changed the debug id: got %q, want %q", config.Get().Telemetry.DebugID, firstID)
	}

	// regenerate changes it.
	regenCmd, regenBuf := newTelemetryTestCmd()
	if err := runTelemetryRegenerate(regenCmd); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	newID := config.Get().Telemetry.DebugID
	if newID == firstID {
		t.Fatal("regenerate should replace the debug id")
	}
	if len(newID) != 16 {
		t.Fatalf("regenerated DebugID = %q, want 16 digits", newID)
	}
	if !strings.Contains(regenBuf.String(), config.TelemetryDebugIDDisplay()) {
		t.Fatalf("regenerate output missing the new grouped debug id:\n%s", regenBuf.String())
	}
}

func TestRunTelemetryIDFailsWithoutOne(t *testing.T) {
	withTelemetryCLIConfig(t)

	cmd, buf := newTelemetryTestCmd()
	err := runTelemetryID(cmd)
	if err == nil {
		t.Fatal("id should fail (non-zero exit) when no debug id has ever been generated")
	}
	if buf.Len() != 0 {
		t.Fatalf("id should print nothing to stdout on failure, got: %q", buf.String())
	}
}

func TestRunTelemetryLevel(t *testing.T) {
	withTelemetryCLIConfig(t)

	cmd, _ := newTelemetryTestCmd()
	if err := runTelemetryLevel(cmd, "warn"); err != nil {
		t.Fatalf("level warn: %v", err)
	}
	if config.Get().Telemetry.MinLevel != "warn" {
		t.Fatalf("MinLevel = %q, want warn", config.Get().Telemetry.MinLevel)
	}

	badCmd, _ := newTelemetryTestCmd()
	if err := runTelemetryLevel(badCmd, "verbose"); err == nil {
		t.Fatal("an unrecognized level should fail")
	}
	if config.Get().Telemetry.MinLevel != "warn" {
		t.Fatalf("a rejected level must not change the persisted one, got %q", config.Get().Telemetry.MinLevel)
	}
}

func TestTelemetryEndpointHostNeverLeaksScheme(t *testing.T) {
	t.Setenv("PANDO_TELEMETRY_ENDPOINT", "http://127.0.0.1:54321")
	if got := telemetryEndpointHost(); got != "127.0.0.1:54321" {
		t.Fatalf("telemetryEndpointHost() = %q, want host:port only", got)
	}
}
