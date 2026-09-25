package updatecheck

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
)

const (
	// successTTL is how long a completed lookup is reused before GitHub is asked again.
	successTTL = 6 * time.Hour
	// failureTTL throttles retries after a failed lookup (offline, rate limited).
	failureTTL = 15 * time.Minute
	// lookupTimeout bounds a single GitHub lookup.
	lookupTimeout = 10 * time.Second
)

// Status describes the running version and whether a newer release exists.
type Status struct {
	// Version is the running build, with a leading "v" when it is a semver.
	Version string `json:"version"`
	// Latest is the newest release for this platform ("" when unknown).
	Latest string `json:"latest,omitempty"`
	// UpdateAvailable is true when Latest is newer than Version.
	UpdateAvailable bool `json:"update_available"`
	// UpdateCommand is the CLI command that installs the update.
	UpdateCommand string `json:"update_command,omitempty"`
	// ReleaseURL links to the release notes of Latest.
	ReleaseURL string `json:"release_url,omitempty"`
	// Checkable is false for development builds without a semantic version,
	// which cannot be compared against releases.
	Checkable bool `json:"checkable"`
}

var (
	cacheMu   sync.Mutex
	cached    *Status
	cachedAt  time.Time
	cachedTTL time.Duration
)

// CurrentStatus returns the running version and, for released builds, whether
// a newer release is published. GitHub lookups are cached so callers can poll it.
func CurrentStatus(ctx context.Context) Status {
	base := Status{Version: version.Canonical()}
	current, ok := version.Semver()
	if !ok {
		return base
	}
	base.Checkable = true

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cached != nil && time.Since(cachedAt) < cachedTTL {
		return *cached
	}

	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	result, err := DetectLatest(lookupCtx)
	if err != nil {
		logging.Debug("update status check failed", "error", err)
		store(base, failureTTL)
		return base
	}
	if result.Found {
		base.Latest = "v" + result.Release.Version.String()
		base.ReleaseURL = result.Release.URL
		if result.Release.Version.GT(current) {
			base.UpdateAvailable = true
			base.UpdateCommand = "pando update"
		}
	}
	store(base, successTTL)
	return base
}

func store(s Status, ttl time.Duration) {
	cached = &s
	cachedAt = time.Now()
	cachedTTL = ttl
}
