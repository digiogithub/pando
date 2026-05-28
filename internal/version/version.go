package version

import (
	"runtime/debug"
	"strings"

	"github.com/blang/semver"
)

// Build-time parameters set via -ldflags
var Version = "unknown"

// A user may install pug using `go install github.com/digiogithub/pando@latest`.
// without -ldflags, in which case the version above is unset. As a workaround
// we use the embedded build version that *is* set when using `go install` (and
// is only set for `go install` and not for `go build`).
func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		// < go v1.18
		return
	}
	mainVersion := info.Main.Version
	if mainVersion == "" || mainVersion == "(devel)" {
		// bin not built using `go install`
		return
	}
	// bin built using `go install`
	Version = mainVersion
}

// Canonical returns the version with a leading v when the current value is a semver.
func Canonical() string {
	if normalized, ok := normalize(Version); ok {
		return normalized
	}
	return Version
}

// Semver returns the current version parsed as semantic version.
func Semver() (semver.Version, bool) {
	return Parse(Version)
}

// Parse parses a version string that may be prefixed with 'v'.
func Parse(v string) (semver.Version, bool) {
	normalized, ok := normalize(v)
	if !ok {
		return semver.Version{}, false
	}
	parsed, err := semver.Parse(strings.TrimPrefix(normalized, "v"))
	if err != nil {
		return semver.Version{}, false
	}
	return parsed, true
}

func normalize(v string) (string, bool) {
	trimmed := strings.TrimSpace(v)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" || trimmed == "unknown" || trimmed == "(devel)" {
		return "", false
	}
	return "v" + trimmed, true
}
