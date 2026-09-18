package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// sandboxExecPath is Apple's Seatbelt launcher. Deprecated by Apple but still
// shipped (macOS 15+) and used by Codex, Claude Code and Gemini CLI.
const sandboxExecPath = "/usr/bin/sandbox-exec"

// platformWrapper returns the macOS backend: /usr/bin/sandbox-exec with a
// generated SBPL profile (sbpl.go).
func platformWrapper() Wrapper {
	return &seatbeltWrapper{}
}

// seatbeltWrapper wraps commands with sandbox-exec. The capability probe runs
// once, on first use.
type seatbeltWrapper struct {
	once sync.Once
	cap  Capability
}

func (w *seatbeltWrapper) Capability() Capability {
	w.once.Do(func() { w.cap = probeSeatbelt() })
	return w.cap
}

// Wrap rewrites cmd to run [sandbox-exec -p <profile> -D K=V ... -- orig...].
// A disabled policy or an unavailable sandbox-exec leaves cmd untouched (fail
// open, per the Wrapper contract).
func (w *seatbeltWrapper) Wrap(cmd *exec.Cmd, p Policy) error {
	if !p.Enabled() || !w.Capability().Enforced {
		return nil
	}
	if cmd.Err != nil {
		// exec.Command could not resolve the program; Start reports it.
		return nil
	}
	if cmd.Path == sandboxExecPath && len(cmd.Args) > 0 && cmd.Args[0] == "sandbox-exec" {
		// Already wrapped; nesting sandbox-exec fails at sandbox_apply.
		return nil
	}
	profile, params, err := GenerateSBPLWith(p, OSSBPLOptions())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrapFailed, err)
	}
	argv := append([]string{cmd.Path}, argsTail(cmd.Args)...)
	cmd.Args = SandboxExecArgs(profile, params, argv)
	cmd.Path = sandboxExecPath
	return nil
}

// argsTail returns args[1:], nil-safe.
func argsTail(args []string) []string {
	if len(args) <= 1 {
		return nil
	}
	return args[1:]
}

// probeSeatbelt checks that sandbox-exec exists and can apply a trivial
// profile, then that it accepts profiles generated for representative
// policies (so an SBPL construct a macOS release rejects shows up here as a
// not-enforced backend instead of every command failing).
func probeSeatbelt() Capability {
	c := Capability{Backend: BackendSeatbelt, Version: macOSVersion()}
	if st, err := os.Stat(sandboxExecPath); err != nil || st.IsDir() {
		c.Backend = BackendNone
		c.Reason = "darwin: " + sandboxExecPath + " not found"
		return c
	}
	if out, err := runSeatbeltProbe("(version 1)(allow default)", nil); err != nil {
		c.Reason = "darwin: sandbox-exec unusable (already sandboxed?): " + describeProbeError(err, out)
		return c
	}
	for _, p := range seatbeltProbePolicies() {
		profile, params, err := GenerateSBPLWith(p, OSSBPLOptions())
		if err == nil {
			var out []byte
			out, err = runSeatbeltProbe(profile, params)
			if err != nil {
				err = fmt.Errorf("%s", describeProbeError(err, out))
			}
		}
		if err != nil {
			c.Reason = fmt.Sprintf("darwin: generated %s profile rejected: %v", p.Mode, err)
			return c
		}
	}
	c.Enforced = true
	// The profile denies writes under protected paths at any depth and
	// connections to guarded ports (see GenerateSBPL).
	c.ProtectsNestedPaths = true
	c.BlocksPorts = true
	return c
}

// seatbeltProbePolicies are the policies whose generated profiles the probe
// compiles: workspace-write with open network, and strict (restricted
// network, readable-roots list) with protected paths, deny paths and a glob.
func seatbeltProbePolicies() []Policy {
	tmp := os.TempDir()
	home, _ := os.UserHomeDir()
	if home == "" {
		home = tmp
	}
	base := Policy{
		Workspace:      tmp,
		WritableRoots:  []string{tmp, "/tmp", "/private/var/folders"},
		ProtectedPaths: []string{tmp + "/.pando", tmp + "/.git/hooks"},
		DenyPaths:      []string{home + "/.ssh", tmp + "/*.pando-probe-secret"},
	}
	ws := base
	ws.Mode, ws.Network = ModeWorkspaceWrite, NetworkAllowed
	// Compile the guarded-port denies too, so a macOS release that rejects
	// them reports "not enforced" instead of failing every command.
	ws.DenyConnectPorts = []int{65534}
	strict := base
	strict.Mode, strict.Network = ModeStrict, NetworkRestricted
	strict.ReadableRoots = []string{"/usr", "/bin", "/System", "/Library", "/private", "/dev", "/etc", "/var", tmp}
	return []Policy{ws, strict}
}

// runSeatbeltProbe runs /usr/bin/true under profile.
func runSeatbeltProbe(profile string, params []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := SandboxExecArgs(profile, params, []string{"/usr/bin/true"})
	cmd := exec.CommandContext(ctx, sandboxExecPath, args[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func describeProbeError(err error, out []byte) string {
	msg := strings.TrimSpace(string(out))
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		return err.Error()
	}
	return err.Error() + ": " + msg
}

// macOSVersion returns the product version (e.g. "15.3"), "" when unknown.
func macOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
