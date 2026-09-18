package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/digiogithub/pando/internal/config"
)

// Wrapper confines a command to a Policy. Implementations are per OS (see the
// package doc) and must be safe for concurrent use.
type Wrapper interface {
	// Capability reports the backend and whether it is enforced on this
	// machine. It is cheap: probes run once and are cached.
	Capability() Capability
	// Wrap rewrites cmd (Path, Args, ExtraFiles, SysProcAttr, ...) so that,
	// once started, it runs under p. It must be called before cmd.Start.
	//
	// Contract:
	//   - A disabled policy (p.Enabled() == false) leaves cmd untouched and
	//     returns nil.
	//   - When the backend is not enforced (Capability().Enforced == false)
	//     Wrap leaves cmd untouched and returns nil: the sandbox fails open,
	//     and callers surface that through Capability/Active.
	//   - An error means enforcement was available but could not be set up
	//     for this command (e.g. a path that cannot be encoded). cmd may be
	//     partially modified and must not be started; wrap such errors with
	//     ErrWrapFailed.
	//
	// Wrap does not touch cmd.Env; use ScrubEnv (or WrapCmd, which does both).
	Wrap(cmd *exec.Cmd, p Policy) error
}

// ErrWrapFailed marks a Wrap error: the command must not be started as is.
var ErrWrapFailed = errors.New("sandbox: cannot apply policy to command")

var (
	defaultMu       sync.RWMutex
	defaultOnce     sync.Once
	defaultWrapper  Wrapper
	defaultOverride Wrapper
)

// Default returns the wrapper for this OS (built by platformWrapper in the
// wrapper_<goos>.go file), created once.
func Default() Wrapper {
	defaultMu.RLock()
	override := defaultOverride
	defaultMu.RUnlock()
	if override != nil {
		return override
	}
	defaultOnce.Do(func() { defaultWrapper = platformWrapper() })
	return defaultWrapper
}

// SetDefaultForTests replaces the wrapper Default returns until the returned
// restore function is called. For tests in other packages (e.g. a fake
// enforced wrapper to exercise auto-allow).
func SetDefaultForTests(w Wrapper) (restore func()) {
	defaultMu.Lock()
	prev := defaultOverride
	defaultOverride = w
	defaultMu.Unlock()
	return func() {
		defaultMu.Lock()
		defaultOverride = prev
		defaultMu.Unlock()
	}
}

// Current resolves the policy for the live configuration (config.Get()) and
// its working directory. It re-resolves on every call, so a settings change,
// an overlay reload or a PANDO_SANDBOX change is always reflected; it is cheap
// (no probes, a few stat calls to find the project config file).
func Current() Policy {
	return Resolve(config.Get(), "")
}

// CurrentPolicyHash is Current().Hash(). A long-lived sandboxed child (the
// persistent shell) stores it at spawn and is re-spawned when it changes.
func CurrentPolicyHash() string {
	return Current().Hash()
}

// Active reports whether commands are really confined right now: the policy
// is enabled AND the backend is enforced on this OS.
func Active() bool {
	return Current().Enabled() && Default().Capability().Enforced
}

// AutoAllowBash reports whether the bash permission prompt may be skipped:
// only while the sandbox is Active, gives its full guarantees (Guarantees:
// protected paths and Pando's own ports enforced) and auto-allow is not
// disabled. Callers still keep their own floor (dangerous commands, explicit
// approvals).
func AutoAllowBash() bool {
	p := Current()
	if !p.Enabled() || !p.AutoAllowBash {
		return false
	}
	full, _ := Guarantees(p, Default().Capability())
	return full
}

// WrapCmd is the one-call helper for spawn sites: when the current policy
// covers purpose it scrubs cmd.Env (inheriting os.Environ() when nil) and
// wraps cmd with Default(). It returns the policy and capability it applied so
// the caller can record them (hash, backend) and warn when not enforced.
// When the policy does not cover purpose cmd is left untouched.
func WrapCmd(cmd *exec.Cmd, purpose Purpose) (Policy, Capability, error) {
	p := Current()
	w := Default()
	c := w.Capability()
	if !p.Covers(purpose) {
		return p, c, nil
	}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = ScrubEnv(cmd.Env, p)
	return p, c, w.Wrap(cmd, p)
}
