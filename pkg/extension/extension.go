package extension

import (
	"context"
	"fmt"
	"strings"
)

// ID uniquely identifies an extension. IDs are namespaced with dots, most
// general segment first, so that a subsystem can ask for everything under a
// prefix without knowing concrete types:
//
//	tools.acme.jira
//	api.acme.audit
//	memory.sink.corp
//	ui.acme.dashboard
type ID string

// Namespace returns everything before the last dot, or "" when the ID has no
// dot in it.
func (id ID) Namespace() string {
	i := strings.LastIndex(string(id), ".")
	if i < 0 {
		return ""
	}
	return string(id)[:i]
}

// Valid reports whether the ID is well formed: non-empty, dot-separated
// segments of lowercase letters, digits, '_' or '-', with no empty segment.
func (id ID) Valid() bool {
	if id == "" {
		return false
	}
	for seg := range strings.SplitSeq(string(id), ".") {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= '0' && r <= '9':
			case r == '_' || r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// License identifies the licensing regime an extension ships under. It is
// informational: it drives reporting (`pando extensions list`) and, later,
// entitlement checks. It is not an enforcement mechanism by itself.
type License string

const (
	// LicenseMIT is the license of the open-source core and of any extension
	// bundled with it.
	LicenseMIT License = "MIT"
	// LicenseEnterprise marks a closed-source extension shipped only in
	// enterprise builds.
	LicenseEnterprise License = "Enterprise"
)

// Info is the metadata every extension declares. The zero value is invalid:
// ID and New are required.
type Info struct {
	// ID is the namespaced identifier. Required, must be Valid.
	ID ID
	// Name is a short human-readable name.
	Name string
	// Description is one line explaining what the extension does.
	Description string
	// Version is the extension's own version, independent of the core.
	Version string
	// Author identifies who ships it.
	Author string
	// License is informational. Defaults to LicenseMIT when empty.
	License License
	// RequiresCore is an optional semver constraint on the Pando core version
	// (for example ">= 0.647.0"). Empty means no constraint. The manager only
	// records it today; enforcement arrives with the licensing work.
	RequiresCore string
	// RequiresExtensions lists other extension IDs that must be loaded before
	// this one. Load order is derived from it.
	RequiresExtensions []ID
	// New builds a fresh instance. Required. The registry stores the factory,
	// never a live instance, so each Manager gets its own instances.
	New func() Extension
}

// Extension is the base interface. Everything an extension can do beyond
// announcing itself comes from the optional interfaces below and from the
// capability interfaces in the rest of this package.
type Extension interface {
	ExtensionInfo() Info
}

// Provisioner is implemented by extensions that need setup once their
// configuration and the host services are available. Returning an error aborts
// loading of that extension.
type Provisioner interface {
	Provision(ctx context.Context, host HostServices) error
}

// Validator is implemented by extensions that can reject their own
// configuration. It runs after Provision; a failure triggers Cleanup.
type Validator interface {
	Validate() error
}

// CleanerUpper releases resources when the extension is unloaded or the
// process shuts down.
type CleanerUpper interface {
	Cleanup() error
}

// Status describes what happened to one extension in a Manager.
type Status struct {
	Info Info
	// Loaded is true once Provision and Validate have both succeeded.
	Loaded bool
	// Disabled is true when configuration switched the extension off; Err is
	// nil in that case.
	Disabled bool
	// Unlicensed is true when the license gate refused the extension. Err then
	// holds the reason. It is kept apart from a plain load failure because the
	// two need different answers: a load failure is a bug report, an
	// unlicensed extension is a question for whoever owns the contract.
	Unlicensed bool
	// Err holds the reason an extension failed to load.
	Err error
}

// String renders a one-line status, used by `pando extensions list`.
func (s Status) String() string {
	state := "registered"
	switch {
	case s.Unlicensed:
		state = "unlicensed: " + s.Err.Error()
	case s.Err != nil:
		state = "error: " + s.Err.Error()
	case s.Disabled:
		state = "disabled"
	case s.Loaded:
		state = "loaded"
	}
	lic := s.Info.License
	if lic == "" {
		lic = LicenseMIT
	}
	return fmt.Sprintf("%s [%s] %s — %s (%s)", s.Info.ID, lic, s.Info.Version, s.Info.Name, state)
}
