package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/logging"
)

// Configuration overlays.
//
// An overlay is a configuration document produced at run time by something
// other than a file or the environment, merged on top of what the loader read.
// The mechanism is generic: the config package knows only that some provider
// hands it a document plus a list of paths that must not be edited locally. It
// never learns where the document came from, and it never persists it.
//
// Two halves:
//
//   - Merge. applyOverlayProviders runs inside Load, after the global and
//     local files have been merged into viper and before viper.Unmarshal, so
//     the overlay participates in exactly the same decode, migration and
//     decryption path as a file-sourced value. Reload therefore reapplies the
//     overlay for free.
//
//   - Lock. Each overlay may declare paths that the write path refuses to
//     change (see updateCfgFile). This is what makes an overlay authoritative
//     rather than merely a default: without it a settings screen would happily
//     write the overridden value back to the file and the next load would
//     silently fight the provider.
//
// Locking and merging are independent. A path may be locked without being set
// by the overlay ("freeze what the file says"), and set without being locked
// ("a better default the user may still change").

// overlayCallTimeout bounds a single provider call. Providers are contractually
// required to answer from a cache, so this is a boundary between "slow" and
// "wedged", not a tuning knob.
const overlayCallTimeout = 30 * time.Second

// OverlaySource is the value reported in ConfigChangeEvent.Source for a change
// that came from an overlay rather than from a user surface or the file.
const OverlaySource = "overlay"

// EventOverlayApplied is the ConfigChangeEvent.Event value published on Bus
// after a load in which at least one overlay was merged.
const EventOverlayApplied = "overlay_applied"

// Overlay is one configuration document imposed by a provider.
//
// It mirrors extension.ConfigOverlay. The duplication is deliberate: this
// package must not depend on the extension contract, and the extension
// contract must not depend on internal packages, so the host adapter in
// internal/extensions translates between the two.
type Overlay struct {
	// Source is a short label for where the document came from, used in log
	// lines and in the change event.
	Source string
	// Values is the overlay document, shaped like the configuration file.
	Values map[string]any
	// Locked lists dotted paths the write path must refuse to change.
	Locked []string
	// Additive lists dotted paths of lists that should be unioned with the
	// loaded list rather than replacing it.
	Additive []string
}

// OverlayProvider produces an Overlay on demand. Load calls it; an error means
// "nothing to say right now" and is logged, never fatal.
type OverlayProvider interface {
	ConfigOverlay(ctx context.Context) (Overlay, error)
}

// OverlayProviderFunc adapts a function to OverlayProvider.
type OverlayProviderFunc func(ctx context.Context) (Overlay, error)

// ConfigOverlay implements OverlayProvider.
func (f OverlayProviderFunc) ConfigOverlay(ctx context.Context) (Overlay, error) {
	return f(ctx)
}

var (
	overlayMu        sync.RWMutex
	overlayProviders []OverlayProvider
	lockedKeys       []string
	lastOverlayKeys  []string
	overlayApplyMu   sync.Mutex
	// overlayCtx carries the caller's context into the Load triggered by
	// ApplyOverlays, so a cancelled reapply does not sit on a provider call.
	overlayCtx context.Context
)

// ErrKeyLocked is the sentinel every locked-key refusal wraps. Callers report
// the key as managed rather than as a failed save:
//
//	if errors.Is(err, config.ErrKeyLocked) { ... }
var ErrKeyLocked = errors.New("configuration key is managed by an extension")

// LockedKeyError names the locked path a write tried to change.
type LockedKeyError struct {
	// Key is the dotted configuration path that is locked.
	Key string
}

func (e *LockedKeyError) Error() string {
	if e == nil || e.Key == "" {
		return ErrKeyLocked.Error()
	}
	return fmt.Sprintf("%q is managed by an extension and cannot be changed here", e.Key)
}

// Unwrap makes errors.Is(err, ErrKeyLocked) true for every locked-key refusal.
func (e *LockedKeyError) Unwrap() error { return ErrKeyLocked }

// RegisterOverlayProvider adds p to the set consulted on every configuration
// load. Providers are consulted in registration order and later documents win
// over earlier ones, so the caller controls precedence by ordering.
//
// Registering does not by itself apply the overlay: the caller runs
// ApplyOverlays once it has registered everything it means to.
func RegisterOverlayProvider(p OverlayProvider) {
	if p == nil {
		return
	}
	overlayMu.Lock()
	defer overlayMu.Unlock()
	overlayProviders = append(overlayProviders, p)
}

// ClearOverlayProviders removes every registered provider and forgets the lock
// list. It exists for tests and for hosts that tear a manager down.
func ClearOverlayProviders() {
	overlayMu.Lock()
	defer overlayMu.Unlock()
	overlayProviders = nil
	lockedKeys = nil
	lastOverlayKeys = nil
}

// HasOverlayProviders reports whether anything would be merged on the next
// load.
func HasOverlayProviders() bool {
	overlayMu.RLock()
	defer overlayMu.RUnlock()
	return len(overlayProviders) > 0
}

// ApplyOverlays re-runs configuration load so that every registered provider
// is asked for its current document. It is the one call a provider makes when
// its document changes: the provider never pushes values, it only says that
// the answer is different now.
//
// Calls are serialised. The whole load path runs, so files and environment are
// re-read as well and the result is exactly what a fresh start would produce.
func ApplyOverlays(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	overlayApplyMu.Lock()
	overlayMu.Lock()
	overlayCtx = ctx
	overlayMu.Unlock()
	defer func() {
		overlayMu.Lock()
		overlayCtx = nil
		overlayMu.Unlock()
		overlayApplyMu.Unlock()
	}()

	return Reload()
}

// OverlayChangedKeys returns the dotted paths the overlays changed on the last
// load, sorted and deduplicated. Surfaces use it to refresh only what moved.
func OverlayChangedKeys() []string {
	overlayMu.RLock()
	defer overlayMu.RUnlock()
	return append([]string(nil), lastOverlayKeys...)
}

// LockedKeys returns the paths locked by the overlays applied on the last
// load, sorted and deduplicated.
func LockedKeys() []string {
	overlayMu.RLock()
	defer overlayMu.RUnlock()
	return append([]string(nil), lockedKeys...)
}

// IsKeyLocked reports whether path is covered by the current lock list.
//
// Coverage runs in both directions, segment by segment and case-insensitively:
// a locked "mcpServers" covers a write to "mcpServers.github.command", and a
// locked "agents.coder.model" covers a write that replaces the whole "agents"
// map, because that write would change the locked leaf too. Whether it
// *actually* changes it is decided by the diff in the write path; this
// function answers the coarser question a UI asks when it decides how to draw
// a field.
func IsKeyLocked(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	overlayMu.RLock()
	defer overlayMu.RUnlock()
	for _, locked := range lockedKeys {
		if pathsOverlap(locked, path) {
			return true
		}
	}
	return false
}

// ErrIfLocked returns a *LockedKeyError for the first locked path among paths,
// and nil when none of them is locked. Mutators call it before touching the
// in-memory configuration so a refusal leaves no half-applied change behind.
func ErrIfLocked(paths ...string) error {
	for _, p := range paths {
		if IsKeyLocked(p) {
			return &LockedKeyError{Key: p}
		}
	}
	return nil
}

// lockedKeyCovering returns the locked path that covers path, or "" when none
// does. Unlike IsKeyLocked it only matches a locked path that is path itself or
// an ancestor of it, which is the right question for a concrete changed leaf.
func lockedKeyCovering(path string) string {
	overlayMu.RLock()
	defer overlayMu.RUnlock()
	for _, locked := range lockedKeys {
		if pathHasPrefix(path, locked) {
			return locked
		}
	}
	return ""
}

// pathHasPrefix reports whether path is prefix itself or lives under it,
// comparing whole segments case-insensitively so that "TUI.Theme" and
// "tui.theme" are the same path.
func pathHasPrefix(path, prefix string) bool {
	p := splitPath(path)
	q := splitPath(prefix)
	if len(q) == 0 || len(q) > len(p) {
		return false
	}
	for i, seg := range q {
		if !strings.EqualFold(seg, p[i]) {
			return false
		}
	}
	return true
}

// pathsOverlap reports whether either path covers the other.
func pathsOverlap(a, b string) bool {
	return pathHasPrefix(a, b) || pathHasPrefix(b, a)
}

func splitPath(path string) []string {
	path = strings.Trim(strings.TrimSpace(path), ".")
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// applyOverlayProviders collects every provider's document, merges them into
// viper and records the resulting lock list. It runs inside Load, between the
// file merge and viper.Unmarshal.
//
// It returns the dotted paths whose value the overlays changed, so the caller
// can report them once the load has succeeded.
func applyOverlayProviders() []string {
	overlayMu.RLock()
	providers := append([]OverlayProvider(nil), overlayProviders...)
	ctx := overlayCtx
	overlayMu.RUnlock()

	// Reset the lock list even when nothing is registered: a provider that
	// went away must not leave keys frozen behind it.
	if len(providers) == 0 {
		overlayMu.Lock()
		lockedKeys = nil
		lastOverlayKeys = nil
		overlayMu.Unlock()
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var (
		locked  []string
		changed []string
		applied int
	)

	for _, p := range providers {
		ov, err := callOverlayProvider(ctx, p)
		if err != nil {
			logging.Warn("Configuration overlay provider failed, keeping the loaded configuration", "error", err)
			continue
		}
		locked = append(locked, ov.Locked...)
		if len(ov.Values) == 0 {
			if len(ov.Locked) > 0 {
				applied++
			}
			continue
		}

		base := viper.AllSettings()
		patch, keys := buildOverlayPatch(base, ov.Values, ov.Additive, nil)
		if len(patch) > 0 {
			if err := viper.MergeConfigMap(patch); err != nil {
				logging.Warn("Failed to merge configuration overlay", "source", ov.Source, "error", err)
				continue
			}
		}
		changed = append(changed, keys...)
		applied++
	}

	locked = normalizePaths(locked)
	changed = normalizePaths(changed)

	overlayMu.Lock()
	lockedKeys = locked
	lastOverlayKeys = changed
	overlayMu.Unlock()

	if applied == 0 {
		return nil
	}
	return changed
}

// callOverlayProvider runs one provider under a deadline, converting a panic
// into an error: a provider lives outside this package and must not be able to
// stop configuration from loading.
func callOverlayProvider(parent context.Context, p OverlayProvider) (ov Overlay, err error) {
	defer func() {
		if r := recover(); r != nil {
			ov = Overlay{}
			err = fmt.Errorf("configuration overlay provider panicked: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(parent, overlayCallTimeout)
	defer cancel()
	return p.ConfigOverlay(ctx)
}

// publishOverlayApplied announces a load in which an overlay was merged.
func publishOverlayApplied(changed []string) {
	if len(changed) == 0 {
		return
	}
	Bus.Publish(ConfigChangeEvent{
		Section:     OverlaySource,
		Event:       EventOverlayApplied,
		ChangedKeys: changed,
		Source:      OverlaySource,
		Timestamp:   time.Now(),
	})
}

// buildOverlayPatch walks the overlay document against the loaded settings and
// returns the subtree to merge into viper plus the dotted paths whose value it
// changes. Only keys the overlay mentions appear in the patch, so a merge
// leaves everything else exactly as loaded.
func buildOverlayPatch(base map[string]any, overlay map[string]any, additive []string, prefix []string) (map[string]any, []string) {
	if len(overlay) == 0 {
		return nil, nil
	}
	patch := make(map[string]any, len(overlay))
	var changed []string

	for key, value := range overlay {
		path := append(append([]string(nil), prefix...), key)
		dotted := strings.Join(path, ".")
		current, has := lookupInsensitive(base, key)

		switch typed := value.(type) {
		case map[string]any:
			currentMap, _ := current.(map[string]any)
			sub, subChanged := buildOverlayPatch(currentMap, typed, additive, path)
			if len(sub) > 0 {
				patch[key] = sub
			}
			changed = append(changed, subChanged...)
		case []any:
			resolved := mergeOverlayList(current, typed, isAdditivePath(additive, dotted))
			patch[key] = resolved
			if !has || !equalValues(current, resolved) {
				changed = append(changed, dotted)
			}
		default:
			patch[key] = value
			if !has || !equalValues(current, value) {
				changed = append(changed, dotted)
			}
		}
	}

	return patch, changed
}

// mergeOverlayList applies the list semantics: merge by id when both sides are
// lists of objects carrying one, union when the path is additive, replace
// otherwise.
func mergeOverlayList(current any, overlay []any, additive bool) []any {
	currentList, _ := current.([]any)

	if len(currentList) > 0 && isIdentifiedList(currentList) && isIdentifiedList(overlay) {
		return mergeListByID(currentList, overlay)
	}

	if !additive {
		return append([]any(nil), overlay...)
	}

	out := append([]any(nil), currentList...)
	for _, item := range overlay {
		if containsValue(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// isIdentifiedList reports whether every element is an object with a non-empty
// "id". Such a list is a keyed collection written as a list, and merging it by
// id is what lets an overlay add one entry without restating the others.
func isIdentifiedList(list []any) bool {
	if len(list) == 0 {
		return false
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return false
		}
		if entryID(m) == "" {
			return false
		}
	}
	return true
}

func entryID(m map[string]any) string {
	for k, v := range m {
		if strings.EqualFold(k, "id") {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// mergeListByID puts the overlay entries first, then the loaded entries whose
// id the overlay did not redefine. Order matters in some lists, and putting the
// authoritative entries first is the same rule the local/global provider
// account merge already uses.
func mergeListByID(current, overlay []any) []any {
	out := make([]any, 0, len(current)+len(overlay))
	seen := make(map[string]bool, len(overlay))
	for _, item := range overlay {
		out = append(out, item)
		if m, ok := item.(map[string]any); ok {
			if id := entryID(m); id != "" {
				seen[strings.ToLower(id)] = true
			}
		}
	}
	for _, item := range current {
		m, ok := item.(map[string]any)
		if ok && seen[strings.ToLower(entryID(m))] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func isAdditivePath(additive []string, path string) bool {
	for _, a := range additive {
		if pathHasPrefix(path, a) {
			return true
		}
	}
	return false
}

// lookupInsensitive finds key in m ignoring case, because viper lowercases the
// keys it stores while an overlay document is written the way a config file
// is.
func lookupInsensitive(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// equalValues compares two decoded configuration values structurally. It is
// deliberately tolerant about numbers, since TOML yields int64 and JSON
// float64 for the same written value.
func equalValues(a, b any) bool {
	if an, aok := toFloat(a); aok {
		if bn, bok := toFloat(b); bok {
			return an == bn
		}
		return false
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, has := lookupInsensitive(bv, k)
			if !has || !equalValues(v, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalValues(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func containsValue(list []any, want any) bool {
	for _, item := range list {
		if equalValues(item, want) {
			return true
		}
	}
	return false
}

// normalizePaths trims, drops empties, deduplicates case-insensitively and
// sorts.
func normalizePaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.Trim(strings.TrimSpace(p), ".")
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// Locked-key enforcement on the write path.
//
// Every configuration mutator funnels through updateCfgFile, which applies a
// caller-supplied closure to the parsed file and writes the result back. That
// funnel is where the lock is enforced, and it is enforced by *diffing* rather
// than by asking each of the fifty-odd mutators to declare what it touches:
// a declaration can drift out of step with the code under it, a diff cannot.
// The rule is exact — a write is refused only when it would actually change a
// locked path, so a mutator that rewrites an unrelated section still works
// while an overlay is in force.

// configTree renders a configuration value as a generic tree, using the same
// JSON shape ConfigView.Lookup exposes, so that paths mean the same thing on
// the read side and the write side.
func configTree(c *Config) (map[string]any, error) {
	if c == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return tree, nil
}

// changedPaths lists the dotted paths whose value differs between two trees.
// A map is walked key by key; anything else is compared whole, so a list that
// gained an element reports as one changed path rather than as an index.
func changedPaths(before, after map[string]any, prefix []string) []string {
	var out []string
	seen := make(map[string]bool, len(before)+len(after))

	walk := func(key string) {
		lower := strings.ToLower(key)
		if seen[lower] {
			return
		}
		seen[lower] = true

		path := append(append([]string(nil), prefix...), key)
		oldVal, hadOld := lookupInsensitive(before, key)
		newVal, hadNew := lookupInsensitive(after, key)

		switch {
		case hadOld && hadNew:
			oldMap, oldIsMap := oldVal.(map[string]any)
			newMap, newIsMap := newVal.(map[string]any)
			if oldIsMap && newIsMap {
				out = append(out, changedPaths(oldMap, newMap, path)...)
				return
			}
			if !equalValues(oldVal, newVal) {
				out = append(out, strings.Join(path, "."))
			}
		default:
			// Present on one side only. An absent value and an explicit zero
			// are the same configuration, so compare against the zero rather
			// than reporting every omitempty field as a change.
			present := oldVal
			if hadNew {
				present = newVal
			}
			if m, ok := present.(map[string]any); ok {
				// Report the leaves that appeared or vanished, not the whole
				// subtree: a lock names a path, and "agents" changing is a
				// different statement from "agents.task.model" changing.
				if hadNew {
					out = append(out, changedPaths(nil, m, path)...)
				} else {
					out = append(out, changedPaths(m, nil, path)...)
				}
				return
			}
			if !isZeroValue(present) {
				out = append(out, strings.Join(path, "."))
			}
		}
	}

	for key := range before {
		walk(key)
	}
	for key := range after {
		walk(key)
	}
	return out
}

// isZeroValue reports whether v is what an absent configuration value decodes
// to: nil, false, zero, an empty string, or an empty collection.
func isZeroValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	default:
		if n, ok := toFloat(v); ok {
			return n == 0
		}
		return false
	}
}

// ensureNoLockedChange refuses a write that would change a locked path. It
// returns a *LockedKeyError naming the locked path, not the leaf under it, so
// the message a user sees matches what the lock list says.
func ensureNoLockedChange(beforeTree map[string]any, after *Config) error {
	if len(LockedKeys()) == 0 {
		return nil
	}
	afterTree, err := configTree(after)
	if err != nil {
		return fmt.Errorf("failed to inspect configuration after write: %w", err)
	}
	for _, path := range changedPaths(beforeTree, afterTree, nil) {
		if locked := lockedKeyCovering(path); locked != "" {
			return &LockedKeyError{Key: locked}
		}
	}
	return nil
}
