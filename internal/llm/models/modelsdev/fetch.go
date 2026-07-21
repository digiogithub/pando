package modelsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
)

const (
	// catalogURL is the only endpoint models.dev publishes: a single JSON
	// document with every provider and model. There is no per-provider route,
	// so the whole payload (~3 MB) is fetched once and indexed in memory.
	catalogURL = "https://models.dev/api.json"

	cacheFileName = ".pando_modelsdev.json"
	// cacheTTL bounds how long the on-disk copy is served without refetching.
	// Pricing changes rarely, and a stale copy is still infinitely better than
	// no cost data at all.
	cacheTTL = 24 * time.Hour
	// cacheSchemaVersion invalidates on-disk copies written by an older layout.
	cacheSchemaVersion = 1

	fetchTimeout = 20 * time.Second
	// maxPayloadBytes guards against an unbounded read if the endpoint ever
	// starts serving something other than the catalog.
	maxPayloadBytes = 64 << 20
)

// disabled turns the catalog off process-wide. It is a live switch: the
// settings UI can toggle it while provider refreshes are running, so it is kept
// out of the memoized state and read atomically on every call.
var disabled atomic.Bool

// SetDisabled turns the catalog off (true) or back on (false). Turning it on
// after startup is enough for the next Get to load the catalog; nothing needs to
// be reset.
func SetDisabled(v bool) { disabled.Store(v) }

// IsDisabled reports the current switch position.
func IsDisabled() bool { return disabled.Load() }

var errDisabled = errors.New("models.dev catalog disabled by configuration")

type diskCache struct {
	Version   int             `json:"version"`
	FetchedAt time.Time       `json:"fetched_at"`
	Payload   json.RawMessage `json:"payload"`
}

var (
	mu        sync.Mutex
	attempted bool
	loaded    Catalog
	loadNo    error
)

// Get returns the models.dev catalog, loading it at most once per process. The
// first caller pays the ~3 MB download; everyone else gets the memoized value,
// including the memoized failure — a provider refresh must never retry that
// download once per model.
//
// Resolution order: fresh on-disk copy, then network, then a stale on-disk
// copy. An error is only returned when all three fail, and callers are
// expected to treat it as "no enrichment available" rather than a hard error.
func Get(ctx context.Context) (Catalog, error) {
	// Checked outside the memoized state so a runtime toggle takes effect
	// immediately in both directions.
	if disabled.Load() {
		return Catalog{}, errDisabled
	}

	mu.Lock()
	defer mu.Unlock()
	if attempted {
		return loaded, loadNo
	}
	attempted = true
	loaded, loadNo = load(ctx)
	if loadNo != nil {
		logging.Debug("models.dev catalog unavailable, continuing without cost metadata", "error", loadNo)
	} else {
		logging.Debug("models.dev catalog loaded", "providers", len(loaded.providers))
	}
	return loaded, loadNo
}

// Reset drops the memoized catalog so the next Get loads it again. Intended for
// tests; production code toggles the catalog with SetDisabled instead.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	attempted = false
	loaded = Catalog{}
	loadNo = nil
}

func load(ctx context.Context) (Catalog, error) {
	cached, cachedErr := readDiskCache()
	if cachedErr == nil && time.Since(cached.FetchedAt) < cacheTTL {
		if catalog, err := decode(cached.Payload); err == nil {
			return catalog, nil
		}
	}

	payload, fetchErr := fetch(ctx)
	if fetchErr == nil {
		catalog, err := decode(payload)
		if err == nil {
			writeDiskCache(payload)
			return catalog, nil
		}
		fetchErr = err
	}

	// Network failed: a stale cache still beats losing pricing entirely.
	if cachedErr == nil {
		if catalog, err := decode(cached.Payload); err == nil {
			logging.Debug("models.dev fetch failed, using stale cache", "error", fetchErr)
			return catalog, nil
		}
	}
	return Catalog{}, fetchErr
}

func decode(payload []byte) (Catalog, error) {
	var providers map[string]Provider
	if err := json.Unmarshal(payload, &providers); err != nil {
		return Catalog{}, fmt.Errorf("decode models.dev catalog: %w", err)
	}
	if len(providers) == 0 {
		return Catalog{}, errors.New("models.dev catalog is empty")
	}
	return newCatalog(providers), nil
}

func fetch(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pando/"+version.Version)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models.dev catalog: unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxPayloadBytes))
}

func cacheFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, cacheFileName), nil
}

func readDiskCache() (diskCache, error) {
	path, err := cacheFilePath()
	if err != nil {
		return diskCache{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return diskCache{}, err
	}
	var cached diskCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return diskCache{}, err
	}
	if cached.Version != cacheSchemaVersion || len(cached.Payload) == 0 {
		return diskCache{}, errors.New("models.dev cache schema mismatch")
	}
	return cached, nil
}

func writeDiskCache(payload []byte) {
	path, err := cacheFilePath()
	if err != nil {
		return
	}
	data, err := json.Marshal(diskCache{
		Version:   cacheSchemaVersion,
		FetchedAt: time.Now(),
		Payload:   payload,
	})
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		logging.Debug("failed to persist models.dev cache", "error", err)
	}
}
