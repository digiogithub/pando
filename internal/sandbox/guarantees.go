package sandbox

import (
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/sandbox/portguard"
)

// RegisterGuardedPort records a TCP port Pando itself listens on, so that
// sandboxed commands cannot connect to it (Policy.DenyConnectPorts). Every
// listener bind site calls it (or portguard.Guard, which also unregisters
// when the listener closes). The returned function removes the registration.
func RegisterGuardedPort(port int, owner string) (unregister func()) {
	return portguard.Register(port, owner)
}

// GuardedPorts returns the ports of this process's listeners plus those
// published by other live Pando processes on this machine.
func GuardedPorts() []int {
	return portguard.Ports()
}

// Gap descriptions returned by Guarantees. They are short: they end up in the
// status badge ("partial: ...").
const (
	GapNotEnforced       = "not enforced"
	GapProtectedPaths    = "protected paths inside the workspace not enforced (install bubblewrap)"
	GapProtectedPathsAny = "protected paths inside the workspace not enforced"
	GapBwrapNever        = "protected paths inside the workspace not enforced (UseBwrap = never)"
	GapGuardedPorts      = "Pando's own ports reachable (needs Landlock ABI 4, Linux 6.7+)"
	GapGuardedPortsAny   = "Pando's own ports reachable"
)

// Guarantees reports whether the sandbox fully protects Pando from the
// commands it confines under p with backend c, and lists what is missing
// otherwise. Protection is only complete when
//
//   - the backend is enforced;
//   - protected and deny paths inside writable roots are enforced (Linux
//     without bubblewrap cannot: .git/hooks, .git/config and deny paths stay
//     reachable);
//   - Pando's own TCP ports (p.DenyConnectPorts) are blocked while the
//     network is allowed (Linux needs Landlock ABI 4).
//
// A command confined with gaps can still reach one of those escape hatches,
// so the bash tool must not skip its permission prompt (AutoAllowBash).
func Guarantees(p Policy, c Capability) (full bool, gaps []string) {
	if !p.Enabled() {
		return false, nil
	}
	if !c.Enforced {
		return false, []string{GapNotEnforced}
	}
	// UseBwrap = "never" makes the Linux backend skip bubblewrap even when
	// the probe found it, so nested paths are then left unenforced.
	bwrapSkipped := c.Backend == BackendBwrapLandlock && p.UseBwrap == BwrapNever
	if (!c.ProtectsNestedPaths || bwrapSkipped) && needsNestedProtection(p) {
		switch {
		case bwrapSkipped:
			gaps = append(gaps, GapBwrapNever)
		case c.Backend == BackendLandlock:
			gaps = append(gaps, GapProtectedPaths)
		default:
			gaps = append(gaps, GapProtectedPathsAny)
		}
	}
	if !c.BlocksPorts && !p.RestrictsNetwork() && len(p.DenyConnectPorts) > 0 {
		if c.Backend == BackendLandlock || c.Backend == BackendBwrapLandlock {
			gaps = append(gaps, GapGuardedPorts)
		} else {
			gaps = append(gaps, GapGuardedPortsAny)
		}
	}
	return len(gaps) == 0, gaps
}

// needsNestedProtection reports whether p has deny paths, or protected paths
// more than one level below a writable root: what a backend without
// ProtectsNestedPaths leaves unenforced (the Landlock-only split handles a
// root's direct children).
func needsNestedProtection(p Policy) bool {
	if len(p.DenyPaths) > 0 {
		return true
	}
	for _, path := range p.ProtectedPaths {
		for _, root := range p.WritableRoots {
			rel, err := filepath.Rel(root, path)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if strings.Contains(rel, string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}
