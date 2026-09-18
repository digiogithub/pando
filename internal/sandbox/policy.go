package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/digiogithub/pando/internal/config"
)

// Mode is the sandbox profile.
type Mode string

const (
	// ModeWorkspaceWrite (the default): write the workspace, temp dirs,
	// dependency caches and extra WritableRoots; read everything; network as
	// configured (allowed by default).
	ModeWorkspaceWrite Mode = config.SandboxModeWorkspaceWrite
	// ModeReadOnly: write temp dirs only; read everything; network restricted.
	ModeReadOnly Mode = config.SandboxModeReadOnly
	// ModeStrict: write the workspace, temp dirs and extra WritableRoots; read
	// only ReadableRoots (workspace, system dirs, toolchains); network
	// restricted.
	ModeStrict Mode = config.SandboxModeStrict
	// ModeOff: no confinement.
	ModeOff Mode = config.SandboxModeOff
)

// ParseMode parses a mode name case-insensitively. "" is the default
// (workspace-write); an unknown name returns ok=false.
func ParseMode(s string) (Mode, bool) {
	switch normalize(s) {
	case "", config.SandboxModeWorkspaceWrite:
		return ModeWorkspaceWrite, true
	case config.SandboxModeReadOnly:
		return ModeReadOnly, true
	case config.SandboxModeStrict:
		return ModeStrict, true
	case config.SandboxModeOff:
		return ModeOff, true
	}
	return "", false
}

// Network is the child network policy.
type Network string

const (
	// NetworkAllowed leaves networking untouched (the default).
	NetworkAllowed Network = config.SandboxNetworkAllowed
	// NetworkRestricted blocks outbound/inbound network for the child (the
	// backend decides the exact mechanism; AF_UNIX may stay allowed).
	NetworkRestricted Network = config.SandboxNetworkRestricted
)

// BwrapPolicy selects whether the Linux backend uses bubblewrap to enforce
// protected paths inside writable roots.
type BwrapPolicy string

const (
	BwrapAuto   BwrapPolicy = config.SandboxBwrapAuto
	BwrapAlways BwrapPolicy = config.SandboxBwrapAlways
	BwrapNever  BwrapPolicy = config.SandboxBwrapNever
)

// EnvInherit selects the base set of variables a sandboxed child inherits.
type EnvInherit string

const (
	// EnvInheritAll passes every variable except scrubbed ones (the default).
	EnvInheritAll EnvInherit = config.SandboxEnvInheritAll
	// EnvInheritCore passes only the core allowlist (PATH, HOME, TERM, ...)
	// plus EnvPolicy.Keep.
	EnvInheritCore EnvInherit = config.SandboxEnvInheritCore
	// EnvInheritNone passes only EnvPolicy.Keep.
	EnvInheritNone EnvInherit = config.SandboxEnvInheritNone
)

// Purpose names a spawn site, for Policy.Covers.
type Purpose string

const (
	// PurposeBash is the bash tool's shell; it is always covered.
	PurposeBash         Purpose = "bash"
	PurposeACPTerminals Purpose = config.SandboxExtendACPTerminals
	PurposeSkills       Purpose = config.SandboxExtendSkills
	PurposeMCP          Purpose = config.SandboxExtendMCP
	PurposeSubagents    Purpose = config.SandboxExtendSubagents
)

// DefaultExtendTo is the set of extra spawn sites always wrapped besides bash.
var DefaultExtendTo = []Purpose{PurposeACPTerminals, PurposeSkills}

// Source says which layer decided the policy's mode (for status displays).
type Source string

const (
	SourceDefault Source = "default"
	SourceConfig  Source = "config"
	SourceEnv     Source = "env"
	SourceLock    Source = "lock"
)

// EnvPolicy controls ScrubEnv.
type EnvPolicy struct {
	Inherit EnvInherit `json:"inherit"`
	// ScrubSecrets drops credential-looking variables (default true).
	ScrubSecrets bool `json:"scrubSecrets"`
	// Exclude are extra glob patterns (case-insensitive, '*' and '?') of
	// variable names to drop. Exclude wins over everything.
	Exclude []string `json:"exclude,omitempty"`
	// Keep are glob patterns of variable names always passed through, even
	// when they look like secrets or are outside the core set.
	Keep []string `json:"keep,omitempty"`
}

// Policy is the resolved, platform-neutral sandbox policy. Paths are absolute
// and cleaned; lists are sorted and deduplicated. Roots may not exist on disk:
// backends must skip missing ones rather than fail.
type Policy struct {
	Mode    Mode    `json:"mode"`
	Network Network `json:"network"`
	// Workspace is the project directory the policy was resolved for.
	Workspace string `json:"workspace"`
	// WritableRoots may be written (recursively), except under
	// ProtectedPaths and DenyPaths.
	WritableRoots []string `json:"writableRoots,omitempty"`
	// ReadableRoots, when non-empty (strict mode), is the complete set of
	// readable directories. Empty means "read everything".
	ReadableRoots []string `json:"readableRoots,omitempty"`
	// ProtectedPaths stay read-only even inside WritableRoots: Pando's own
	// config and data (anti self-disable) and git code-exec vectors.
	ProtectedPaths []string `json:"protectedPaths,omitempty"`
	// DenyPaths are denied for both read and write. Entries may be globs.
	DenyPaths []string `json:"denyPaths,omitempty"`
	// Env controls environment scrubbing.
	Env EnvPolicy `json:"env"`
	// AutoAllowBash is the configured intent to skip the bash permission
	// prompt. It only applies while the sandbox is enforced; use the package
	// AutoAllowBash() helper, which checks both.
	AutoAllowBash bool `json:"autoAllowBash"`
	// UseBwrap is the Linux bubblewrap preference.
	UseBwrap BwrapPolicy `json:"useBwrap"`
	// ExtendTo lists the extra spawn sites wrapped besides bash.
	ExtendTo []Purpose `json:"extendTo,omitempty"`
	// AllowAutoEscalation lets a denied command be re-run unsandboxed without
	// an explicit approval.
	AllowAutoEscalation bool `json:"allowAutoEscalation"`
	// DenyConnectPorts are TCP ports a child must not connect to even though
	// the network is allowed: the listeners of Pando itself (API, AG-UI, MCP
	// HTTP, LLM proxy, IPC bus, ...) and of other live Pando processes, see
	// GuardedPorts. Reaching one of them would let a command change the
	// configuration or run code outside the sandbox. Filled by Resolve only
	// when the network is allowed (a restricted network already refuses all
	// TCP); part of Hash, so a new listener re-spawns the persistent shell.
	DenyConnectPorts []int `json:"denyConnectPorts,omitempty"`
	// Source is the layer that decided Mode. Informational; not hashed.
	Source Source `json:"-"`
}

// Enabled reports whether the policy asks for confinement at all. Whether the
// confinement is actually enforced depends on the Wrapper's Capability.
func (p Policy) Enabled() bool {
	return p.Mode != "" && p.Mode != ModeOff
}

// RestrictsNetwork reports whether the child's network must be blocked.
func (p Policy) RestrictsNetwork() bool {
	return p.Enabled() && p.Network == NetworkRestricted
}

// Covers reports whether a spawn site of the given purpose must be wrapped.
func (p Policy) Covers(purpose Purpose) bool {
	if !p.Enabled() {
		return false
	}
	if purpose == PurposeBash {
		return true
	}
	return slices.Contains(p.ExtendTo, purpose)
}

// Hash returns a stable hex digest of everything that affects enforcement
// (all fields except Source). Two policies with the same Hash confine a child
// identically, so a spawn site stores it and re-spawns when it changes.
func (p Policy) Hash() string {
	c := p
	c.Source = ""
	c.WritableRoots = sortedCopy(p.WritableRoots)
	c.ReadableRoots = sortedCopy(p.ReadableRoots)
	c.ProtectedPaths = sortedCopy(p.ProtectedPaths)
	c.DenyPaths = sortedCopy(p.DenyPaths)
	c.Env.Exclude = sortedCopy(p.Env.Exclude)
	c.Env.Keep = sortedCopy(p.Env.Keep)
	c.ExtendTo = slices.Clone(p.ExtendTo)
	slices.Sort(c.ExtendTo)
	c.ExtendTo = slices.Compact(c.ExtendTo)
	c.DenyConnectPorts = cleanPorts(p.DenyConnectPorts)

	data, err := json.Marshal(c)
	if err != nil {
		// Cannot happen for this plain struct; keep the signature simple.
		panic("sandbox: marshal policy: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// cleanPorts returns the valid (1-65535) ports of in, sorted and
// deduplicated; nil when there are none.
func cleanPorts(in []int) []int {
	var out []int
	for _, port := range in {
		if port > 0 && port <= 65535 {
			out = append(out, port)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}
