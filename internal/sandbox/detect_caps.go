package sandbox

import (
	"fmt"
	"strings"
)

// Backend names reported in Capability.Backend.
const (
	// BackendNone: nothing is enforced (unsupported OS, old kernel, backend
	// not implemented yet).
	BackendNone = "none"
	// BackendLandlock: Linux Landlock + seccomp via the re-exec helper.
	BackendLandlock = "landlock+seccomp"
	// BackendBwrapLandlock: Linux bubblewrap for protected paths plus Landlock.
	BackendBwrapLandlock = "bwrap+landlock"
	// BackendSeatbelt: macOS sandbox-exec with an SBPL profile.
	BackendSeatbelt = "seatbelt"
	// BackendJobObject: Windows Job Object containment (not a sandbox).
	BackendJobObject = "job-object"
)

// Capability describes the sandbox backend available on this machine.
type Capability struct {
	// Backend is one of the Backend* constants.
	Backend string `json:"backend"`
	// Version is backend specific, e.g. the Landlock ABI ("5") or the bwrap
	// version. Empty when unknown.
	Version string `json:"version,omitempty"`
	// Enforced is true only when Wrap really confines commands.
	Enforced bool `json:"enforced"`
	// Reason explains a degraded or unavailable backend, for the UI and logs.
	Reason string `json:"reason,omitempty"`
	// ProtectsNestedPaths: protected and deny paths inside writable roots
	// are enforced (read-only, deny paths hidden), however deep. False for
	// Linux without bubblewrap.
	ProtectsNestedPaths bool `json:"protectsNestedPaths"`
	// BlocksPorts: connections to individual TCP ports
	// (Policy.DenyConnectPorts) can be refused while the network is allowed.
	// False for Linux with a Landlock ABI below 4.
	BlocksPorts bool `json:"blocksPorts"`
}

// String renders the capability for logs and badges, e.g.
// "landlock+seccomp v5" or "none (windows: not supported)".
func (c Capability) String() string {
	s := c.Backend
	if s == "" {
		s = BackendNone
	}
	if c.Version != "" {
		s += " v" + c.Version
	}
	if c.Reason != "" {
		s += " (" + c.Reason + ")"
	}
	return s
}

// Status is the combined view a status badge or settings page needs.
type Status struct {
	Policy     Policy     `json:"policy"`
	Capability Capability `json:"capability"`
	// Active: policy enabled and backend enforced.
	Active bool `json:"active"`
	// Hash is Policy.Hash().
	Hash string `json:"hash"`
	// Full: Active and without gaps (see Guarantees). Only then may bash
	// skip its permission prompt.
	Full bool `json:"full"`
	// Gaps lists what an Active sandbox does not guarantee here (Guarantees).
	Gaps []string `json:"gaps,omitempty"`
}

// CurrentStatus returns the Status for the live configuration.
func CurrentStatus() Status {
	return NewStatus(Current(), Default().Capability())
}

// NewStatus builds the Status of policy p under backend c.
func NewStatus(p Policy, c Capability) Status {
	s := Status{Policy: p, Capability: c, Active: p.Enabled() && c.Enforced, Hash: p.Hash()}
	if s.Active {
		s.Full, s.Gaps = Guarantees(p, c)
	}
	return s
}

// Label renders the status as a short badge, e.g.
// "workspace-write (landlock+seccomp v5)", "workspace-write (not enforced:
// windows: not supported)" or "off".
func (s Status) Label() string {
	if !s.Policy.Enabled() {
		return string(ModeOff)
	}
	if !s.Capability.Enforced {
		reason := s.Capability.Reason
		if reason == "" {
			reason = "unavailable"
		}
		return fmt.Sprintf("%s (not enforced: %s)", s.Policy.Mode, reason)
	}
	c := s.Capability
	c.Reason = ""
	if len(s.Gaps) > 0 {
		return fmt.Sprintf("%s (%s; partial: %s)", s.Policy.Mode, c.String(), strings.Join(s.Gaps, "; "))
	}
	return fmt.Sprintf("%s (%s)", s.Policy.Mode, c.String())
}
