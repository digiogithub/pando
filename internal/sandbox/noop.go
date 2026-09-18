package sandbox

import "os/exec"

// noopWrapper is the not-enforced fallback: it never modifies a command and
// reports Backend "none" with the reason it cannot sandbox.
type noopWrapper struct {
	reason string
}

// newNoopWrapper returns a wrapper that enforces nothing, reporting reason.
func newNoopWrapper(reason string) Wrapper {
	return noopWrapper{reason: reason}
}

func (w noopWrapper) Capability() Capability {
	return Capability{Backend: BackendNone, Enforced: false, Reason: w.reason}
}

func (noopWrapper) Wrap(*exec.Cmd, Policy) error {
	return nil
}
