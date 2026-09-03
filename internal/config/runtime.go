package config

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/logging"
)

// Runtime overrides and reload requests.
//
// Two related problems, both created by the fact that a reload re-runs the
// whole load path:
//
//   - A value set for this process only (`--model`, `--log-file`) lives in the
//     loaded configuration and nowhere else. Re-reading the files throws it
//     away, so the run silently reverts to the saved model halfway through.
//     Runtime overrides fix that: the value is recorded here and reapplied on
//     every load, as a layer *above* files and overlays.
//
//   - Anything that can ask for a reload can ask for a thousand. A reload
//     re-reads files, re-runs every overlay provider and republishes on the
//     bus, so a caller in a loop would wedge the process. RequestReload
//     coalesces: requests arriving inside one window share a single reload and
//     all learn its outcome.
//
// A locked key beats a runtime override. Locking is the mechanism that makes an
// overlay authoritative, and a command-line flag is still local editing.

// EventConfigReloaded is the ConfigChangeEvent.Event value published after a
// successful Reload, whether or not any overlay took part in it.
const EventConfigReloaded = "config_reloaded"

// ReloadSource is the ConfigChangeEvent.Source value for a reload the process
// performed on itself, as opposed to a change a user surface made.
const ReloadSource = "reload"

var (
	runtimeMu        sync.RWMutex
	runtimeOverrides = map[string]any{}
)

// SetRuntimeOverride records a configuration value that applies to this
// process only and is reapplied on every load, so a reload does not discard
// it. path is a dotted configuration path in the same form the overlay and
// lock lists use ("logFile", "agents.coder.model").
//
// Nothing is written to disk, and no reload happens: the caller has usually
// just applied the value in memory and only wants it to survive the next one.
func SetRuntimeOverride(path string, value any) {
	path = strings.Trim(strings.TrimSpace(path), ".")
	if path == "" {
		return
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeOverrides[strings.ToLower(path)] = value
}

// ClearRuntimeOverride forgets one override, so the next load takes the value
// from the files and overlays again.
func ClearRuntimeOverride(path string) {
	path = strings.Trim(strings.TrimSpace(path), ".")
	if path == "" {
		return
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	delete(runtimeOverrides, strings.ToLower(path))
}

// ClearRuntimeOverrides forgets every override. Tests and hosts that restart
// the configuration layer call it.
func ClearRuntimeOverrides() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeOverrides = map[string]any{}
}

// RuntimeOverrides returns a copy of the recorded overrides, keyed by lowercase
// dotted path.
func RuntimeOverrides() map[string]any {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	out := make(map[string]any, len(runtimeOverrides))
	for k, v := range runtimeOverrides {
		out[k] = v
	}
	return out
}

// applyRuntimeOverrides merges the recorded overrides into viper. It runs
// inside Load, after the overlay merge and before viper.Unmarshal, which is
// what makes a runtime override the top layer: files, then overlays, then this.
//
// An override of a locked path is skipped, not applied: an overlay that locks a
// key is stating that nothing local may change it, and a flag is local.
func applyRuntimeOverrides() {
	overrides := RuntimeOverrides()
	if len(overrides) == 0 {
		return
	}
	// Sort for a deterministic merge order; nested paths must not depend on map
	// iteration order.
	paths := make([]string, 0, len(overrides))
	for path := range overrides {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if IsKeyLocked(path) {
			logging.Warn("Runtime configuration override ignored, the key is managed by an extension",
				"key", path)
			continue
		}
		patch := nestPath(splitPath(path), overrides[path])
		if len(patch) == 0 {
			continue
		}
		if err := viper.MergeConfigMap(patch); err != nil {
			logging.Warn("Failed to apply runtime configuration override", "key", path, "error", err)
		}
	}
}

// nestPath turns a dotted path and a value into the nested map viper merges.
// Segments are lowercased because viper stores its keys that way.
func nestPath(segments []string, value any) map[string]any {
	if len(segments) == 0 {
		return nil
	}
	out := value
	for i := len(segments) - 1; i >= 0; i-- {
		out = map[string]any{strings.ToLower(segments[i]): out}
	}
	m, _ := out.(map[string]any)
	return m
}

// Reload coalescing.

// reloadDebounce is how long RequestReload waits before running, so a burst of
// requests becomes one reload. It is a variable only so tests can shorten it.
var reloadDebounce = 250 * time.Millisecond

// reloadBatch is one coalesced reload: the requests that joined it all wait on
// done and read the same err.
type reloadBatch struct {
	done    chan struct{}
	reasons []string
	err     error
}

var (
	reloadMu      sync.Mutex
	reloadPending *reloadBatch
)

// RequestReload asks for a configuration reload and returns its outcome.
//
// It is the call an extension makes when something it owns changed and the
// configuration must be built again: it re-reads the files, asks every
// registered overlay provider for its current document and reapplies the
// runtime overrides, exactly as a fresh start would.
//
// Requests are coalesced. A call made while another is waiting joins it, and
// both return the same error, so a caller that fires on every message from a
// server cannot make Pando reload once per message. Reloads never run
// concurrently.
//
// ctx bounds the *wait*, not the reload: cancelling it returns ctx.Err() to
// this caller and leaves the reload, which other callers may be waiting on,
// running. A failed reload leaves the previous configuration in effect.
func RequestReload(ctx context.Context, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	reloadMu.Lock()
	batch := reloadPending
	if batch == nil {
		batch = &reloadBatch{done: make(chan struct{})}
		reloadPending = batch
		go runReloadBatch(batch)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		batch.reasons = append(batch.reasons, reason)
	}
	reloadMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-batch.done:
		return batch.err
	}
}

// runReloadBatch waits out the debounce window, detaches the batch so later
// requests start a new one, and runs the reload.
func runReloadBatch(batch *reloadBatch) {
	timer := time.NewTimer(reloadDebounce)
	<-timer.C

	reloadMu.Lock()
	if reloadPending == batch {
		reloadPending = nil
	}
	reasons := append([]string(nil), batch.reasons...)
	reloadMu.Unlock()

	logging.Debug("Configuration reload requested", "requests", len(reasons), "reasons", strings.Join(reasons, ", "))

	// The reload runs under a background context on purpose: it is shared, so a
	// single impatient caller must not cancel work the others are waiting for.
	batch.err = ApplyOverlays(context.Background())
	if batch.err != nil {
		logging.Warn("Configuration reload failed, keeping the previous configuration", "error", batch.err)
	}
	close(batch.done)
}
