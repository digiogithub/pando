package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/llm/models"
)

// resetRuntimeState clears the runtime-override and reload globals around a
// test, for the same reason resetOverlayState exists: the configuration layer
// is a process-wide singleton.
func resetRuntimeState(t *testing.T) {
	t.Helper()
	ClearRuntimeOverrides()
	t.Cleanup(func() {
		ClearRuntimeOverrides()
		reloadMu.Lock()
		reloadPending = nil
		reloadMu.Unlock()
	})
}

// shortReloadDebounce makes the coalescing window small enough for a test to
// wait on it without making the test flaky: it is still long enough that the
// requests a test fires in a loop land inside one window.
func shortReloadDebounce(t *testing.T, d time.Duration) {
	t.Helper()
	prev := reloadDebounce
	reloadDebounce = d
	t.Cleanup(func() { reloadDebounce = prev })
}

func TestReloadPreservesOverrides(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)

	// Two real models, so validation does not revert the agent to a provider
	// default and make this test assert something other than what it means to.
	fileModel := registerTestModel(t, "copilot.reload-file-model")
	flagModel := registerTestModel(t, "copilot.reload-flag-model")

	dir := writeProjectConfig(t, map[string]any{
		"tui":       map[string]any{"theme": "dark"},
		"providers": map[string]any{"copilot": map[string]any{"apiKey": "test-key"}},
		"agents":    map[string]any{"coder": map[string]any{"model": string(fileModel)}},
	})

	logPath := t.TempDir() + "/pando.log"
	loaded, err := Load(dir, false, logPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LogFile != logPath {
		t.Fatalf("logFile = %q, want the flag value", loaded.LogFile)
	}

	// This is what --model does: an in-memory selection that is never written
	// to the file, recorded so the next load reapplies it.
	if err := OverrideAgentModel(AgentCoder, flagModel); err != nil {
		t.Fatalf("OverrideAgentModel: %v", err)
	}

	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := Get().LogFile; got != logPath {
		t.Fatalf("after reload logFile = %q, want %q: the flag override was dropped", got, logPath)
	}
	if got := Get().Agents[AgentCoder].Model; got != flagModel {
		t.Fatalf("after reload agents.coder.model = %q, want %q: the runtime override was dropped", got, flagModel)
	}
	// The file is untouched: an override is not a save.
	if got := viper.GetString("agents.coder.model"); got != string(flagModel) {
		t.Fatalf("viper value = %q, want the override to be the top layer", got)
	}
	if got := fileValue(t, dir, "agents", "coder", "model"); got != string(fileModel) {
		t.Fatalf("config file says %q, want %q: a runtime override must not be persisted", got, fileModel)
	}

	// Clearing it puts the file value back on the next load.
	ClearRuntimeOverride("agents.coder.model")
	if err := Reload(); err != nil {
		t.Fatalf("Reload after clearing: %v", err)
	}
	if got := Get().Agents[AgentCoder].Model; got != fileModel {
		t.Fatalf("after clearing the override, model = %q, want %q", got, fileModel)
	}
}

// registerTestModel adds a model to the registry for the duration of a test,
// so an agent configured with it survives validation.
func registerTestModel(t *testing.T, id string) models.ModelID {
	t.Helper()
	modelID := models.ModelID(id)
	models.RegisterDynamicModel(models.Model{
		ID:               modelID,
		Name:             id,
		Provider:         models.ProviderCopilot,
		APIModel:         id,
		ContextWindow:    128_000,
		DefaultMaxTokens: 4096,
	})
	t.Cleanup(func() { models.DeleteSupportedModels(modelID) })
	return modelID
}

// fileValue reads a dotted path out of the project config file, which is how a
// test tells "applied in memory" apart from "written to disk".
func fileValue(t *testing.T, dir string, path ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".pando.json"))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse config file: %v", err)
	}
	var cur any = doc
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[seg]
	}
	s, _ := cur.(string)
	return s
}

func TestRuntimeOverrideNeverBeatsALockedKey(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{
			Source: "test",
			Values: map[string]any{"tui": map[string]any{"theme": "corporate"}},
			Locked: []string{"tui.theme"},
		}, nil
	}))

	// A locked key stays the overlay's; an unlocked one takes the override.
	SetRuntimeOverride("tui.theme", "flag-theme")
	SetRuntimeOverride("logFile", "/tmp/pando-runtime-test.log")

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want the locked overlay value", loaded.TUI.Theme)
	}
	if loaded.LogFile != "/tmp/pando-runtime-test.log" {
		t.Fatalf("logFile = %q, want the runtime override", loaded.LogFile)
	}
}

func TestFailedReloadKeepsThePreviousConfiguration(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	broken := false
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		if !broken {
			return Overlay{Values: map[string]any{"tui": map[string]any{"theme": "corporate"}}}, nil
		}
		// A document the loader cannot decode: debug is a bool.
		return Overlay{Values: map[string]any{"debug": map[string]any{"nonsense": true}}}, nil
	}))

	loaded, err := Load(dir, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want corporate", loaded.TUI.Theme)
	}

	broken = true
	if err := Reload(); err == nil {
		t.Fatal("Reload succeeded on an undecodable overlay, want an error")
	}
	if Get() != loaded {
		t.Fatal("a failed reload replaced the configuration pointer callers hold")
	}
	if Get().TUI.Theme != "corporate" {
		t.Fatalf("after a failed reload tui.theme = %q, want the previous value", Get().TUI.Theme)
	}

	// Recovery: the next reload works and the error was not sticky.
	broken = false
	if err := Reload(); err != nil {
		t.Fatalf("Reload after recovery: %v", err)
	}
	if Get().TUI.Theme != "corporate" {
		t.Fatalf("after recovery tui.theme = %q, want corporate", Get().TUI.Theme)
	}
}

func TestRequestReloadCoalescesAStorm(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)
	shortReloadDebounce(t, 30*time.Millisecond)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	var calls atomic.Int64
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		calls.Add(1)
		return Overlay{Values: map[string]any{"tui": map[string]any{"theme": "corporate"}}}, nil
	}))

	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}
	calls.Store(0)

	const requests = 50
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RequestReload(context.Background(), "storm"); err != nil {
				t.Errorf("RequestReload: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got == 0 || got >= requests {
		t.Fatalf("provider consulted %d times for %d requests, want a coalesced handful", got, requests)
	}
	if Get().TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want the overlay value after the reload", Get().TUI.Theme)
	}
}

func TestRequestReloadReportsTheReloadError(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)
	shortReloadDebounce(t, 10*time.Millisecond)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	broken := false
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		if broken {
			return Overlay{Values: map[string]any{"debug": map[string]any{"nonsense": true}}}, nil
		}
		return Overlay{Values: map[string]any{"tui": map[string]any{"theme": "corporate"}}}, nil
	}))

	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	broken = true
	err := RequestReload(context.Background(), "bad document")
	if err == nil {
		t.Fatal("RequestReload returned nil, want the reload error reported back to the caller")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("error = %v, want the load failure", err)
	}
	if Get().TUI.Theme != "corporate" {
		t.Fatalf("tui.theme = %q, want the previous value after a failed reload", Get().TUI.Theme)
	}
}

func TestRequestReloadHonoursACancelledContext(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)
	shortReloadDebounce(t, time.Second)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := RequestReload(ctx, "impatient"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RequestReload = %v, want the context error", err)
	}

	// The reload the caller abandoned still runs, so the configuration is
	// consistent for everyone else.
	if err := RequestReload(context.Background(), "patient"); err != nil {
		t.Fatalf("RequestReload: %v", err)
	}
}

func TestReloadPublishesConfigReloadedWithChangedKeys(t *testing.T) {
	isolateGlobalConfig(t)
	resetOverlayState(t)
	resetRuntimeState(t)

	dir := writeProjectConfig(t, map[string]any{"tui": map[string]any{"theme": "dark"}})

	theme := "dark"
	RegisterOverlayProvider(OverlayProviderFunc(func(ctx context.Context) (Overlay, error) {
		return Overlay{Values: map[string]any{"tui": map[string]any{"theme": theme}}}, nil
	}))
	if _, err := Load(dir, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	events := make(chan ConfigChangeEvent, 8)
	Bus.Subscribe(events)
	t.Cleanup(func() { Bus.Unsubscribe(events) })

	theme = "corporate"
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	var reloaded *ConfigChangeEvent
	for {
		select {
		case ev := <-events:
			if ev.Event == EventConfigReloaded {
				copy := ev
				reloaded = &copy
			}
			continue
		default:
		}
		break
	}
	if reloaded == nil {
		t.Fatal("no config_reloaded event was published")
	}
	if reloaded.Source != ReloadSource {
		t.Fatalf("source = %q, want %q", reloaded.Source, ReloadSource)
	}
	if !contains(reloaded.ChangedKeys, "tui.theme") {
		t.Fatalf("changed keys = %v, want tui.theme among them", reloaded.ChangedKeys)
	}
	for _, key := range reloaded.ChangedKeys {
		if key == "tui.nerdFonts" {
			t.Fatal("a key whose value did not change was reported as changed")
		}
	}
}
