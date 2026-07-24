// Package runtime discovers the JavaScript toolchain installed on the host and
// provisions npm-distributed language servers on demand.
//
// Pando prefers bun over Node.js, unless the LSPRunner setting pins a specific
// package manager. When no usable toolchain remains, no npm package can be
// installed or executed, and callers must fall back to whatever binary is
// already on PATH (or tell the user how to install it).
package runtime

import (
	"os/exec"
	"sync"

	"github.com/digiogithub/pando/internal/config"
)

// Manager identifies the package manager used to install and run npm packages.
type Manager string

const (
	// ManagerNone means no usable JavaScript runtime was found on the host.
	ManagerNone Manager = ""
	// ManagerBun uses bun/bunx.
	ManagerBun Manager = "bun"
	// ManagerNpm uses npm/npx from a Node.js installation.
	ManagerNpm Manager = "npm"
)

// NodeRuntime describes the JavaScript toolchain available on the host.
type NodeRuntime struct {
	// Manager is the package manager family that was detected.
	Manager Manager
	// Install is the absolute path of the binary that installs packages into a
	// staging directory ("bun" or "npm"). Empty when only an ephemeral runner
	// is available.
	Install string
	// Exec is the absolute path of the binary that runs a package without
	// installing it ("bunx" or "npx"). Empty when only the installer is
	// available.
	Exec string
	// ExecArgs are the fixed arguments Exec needs before anything else. It is
	// ["x"] when bunx is missing and `bun x` is used instead.
	ExecArgs []string
}

// Available reports whether any npm package can be provisioned at all.
func (r NodeRuntime) Available() bool {
	return r.Manager != ManagerNone && (r.Install != "" || r.Exec != "")
}

// lspLookPath resolves a binary on PATH. It is a package variable so tests can
// stub it without depending on the developer's machine.
var lookPath = exec.LookPath

// hostRuntimes holds every toolchain found on PATH. Both are probed, because
// which one is used depends on the LSPRunner setting, which the user can change
// while Pando is running.
type hostRuntimes struct {
	bun NodeRuntime
	npm NodeRuntime
}

var (
	stateMu  sync.Mutex
	probed   *hostRuntimes
	override *NodeRuntime
)

// Detect returns the JavaScript toolchain Pando should use, honoring the
// LSPRunner setting. PATH is probed once per process — it is not expected to
// change while Pando runs, and this is called on every on-demand activation —
// but the preference is re-read on every call.
func Detect() NodeRuntime {
	return DetectFor(config.Get().LSPRunnerMode())
}

// DetectFor returns the toolchain for an explicit runner preference
// ("auto", "bun", "npm" or "off"), regardless of the current configuration.
func DetectFor(preference string) NodeRuntime {
	stateMu.Lock()
	defer stateMu.Unlock()

	if override != nil {
		// A pinned runtime describes the whole host: honor "off", but do not let
		// a preference invent a toolchain the test did not pin.
		if preference == config.LSPRunnerOff {
			return NodeRuntime{}
		}
		return *override
	}
	if probed == nil {
		h := probeRuntimes()
		probed = &h
	}

	switch preference {
	case config.LSPRunnerOff:
		return NodeRuntime{}
	case config.LSPRunnerBun:
		return probed.bun
	case config.LSPRunnerNpm:
		return probed.npm
	default:
		// bun is preferred because it both installs and runs packages an order
		// of magnitude faster than npm.
		if probed.bun.Available() {
			return probed.bun
		}
		return probed.npm
	}
}

// probeRuntimes performs the actual PATH probing for every supported manager.
func probeRuntimes() hostRuntimes {
	return hostRuntimes{bun: probeBun(), npm: probeNpm()}
}

func probeBun() NodeRuntime {
	bun, err := lookPath("bun")
	if err != nil {
		return NodeRuntime{}
	}
	rt := NodeRuntime{Manager: ManagerBun, Install: bun}
	if bunx, err := lookPath("bunx"); err == nil {
		rt.Exec = bunx
	} else {
		// Every bun ships the `x` subcommand even when the bunx shim is not
		// linked into PATH.
		rt.Exec = bun
		rt.ExecArgs = []string{"x"}
	}
	return rt
}

func probeNpm() NodeRuntime {
	// Node.js is only usable through npm/npx; some distributions ship node
	// without either, in which case there is nothing to provision with.
	if _, err := lookPath("node"); err != nil {
		return NodeRuntime{}
	}
	var rt NodeRuntime
	if npm, err := lookPath("npm"); err == nil {
		rt.Manager = ManagerNpm
		rt.Install = npm
	}
	if npx, err := lookPath("npx"); err == nil {
		rt.Manager = ManagerNpm
		rt.Exec = npx
	}
	return rt
}

// resetDetection clears the memoized probe and any pinned runtime. Tests only.
func resetDetection() {
	stateMu.Lock()
	defer stateMu.Unlock()
	probed = nil
	override = nil
}

// SetForTests pins the detected runtime, so tests in other packages do not
// depend on what is installed on the machine running them. It returns a
// function that restores the previous state, following config.SetForTests.
func SetForTests(rt NodeRuntime) func() {
	stateMu.Lock()
	override = &rt
	probed = nil
	stateMu.Unlock()
	return func() { resetDetection() }
}
