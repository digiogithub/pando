package sandbox

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/sandbox/helper"
)

// End-to-end tests: real commands under the real helper. The test binary is
// both the helper (the helper package dispatches `__sandbox-exec` from its
// init) and the sandboxed program (TestMain runs e2eChildEnv actions), so no
// external tools are needed except for the `unshare` check.

const (
	e2eChildEnv = "PANDO_SANDBOX_E2E_CHILD"
	e2eArgEnv   = "PANDO_SANDBOX_E2E_ARG"
)

func TestMain(m *testing.M) {
	if action := os.Getenv(e2eChildEnv); action != "" {
		os.Exit(e2eChild(action, os.Getenv(e2eArgEnv)))
	}
	os.Exit(m.Run())
}

// e2eChild runs inside the sandbox; exit 0 means the operation succeeded.
func e2eChild(action, arg string) int {
	var err error
	switch action {
	case "write":
		err = os.WriteFile(arg, []byte("x"), 0o644)
	case "read":
		_, err = os.ReadFile(arg)
	case "tcp":
		var c net.Conn
		if c, err = net.DialTimeout("tcp", arg, 2*time.Second); err == nil {
			c.Close()
		}
	case "unix":
		var c net.Conn
		if c, err = net.DialTimeout("unix", arg, 2*time.Second); err == nil {
			c.Close()
		}
	case "true":
	default:
		err = fmt.Errorf("unknown action %q", action)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// requireLandlock skips unless the kernel enforces Landlock and the arch has
// a seccomp table, and returns a real (unfaked) Linux wrapper.
func requireLandlock(t *testing.T) *linuxWrapper {
	t.Helper()
	w := newLinuxWrapper()
	if c := w.Capability(); !c.Enforced {
		t.Skipf("sandbox not enforceable here: %s", c.Reason)
	}
	return w
}

type e2eEnv struct {
	ws, outside string
	w           *linuxWrapper
}

// newE2E creates a workspace with protected entries and an "outside"
// directory standing in for $HOME (both under t.TempDir; the policy's only
// writable root is the workspace).
func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	w := requireLandlock(t)
	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "home")
	for _, d := range []string{ws, outside, filepath.Join(ws, ".pando"), filepath.Join(ws, "src"),
		filepath.Join(ws, ".git", "hooks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{".pando.toml", "README.md", ".git/config", ".pando/pando.db", "secret.env"} {
		if err := os.WriteFile(filepath.Join(ws, f), []byte("orig"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "profile"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &e2eEnv{ws: ws, outside: outside, w: w}
}

func (e *e2eEnv) policy(bw BwrapPolicy, network Network) Policy {
	p := testPolicy(e.ws)
	p.UseBwrap = bw
	p.Network = network
	return p
}

// run executes the test binary's child action under p.
func (e *e2eEnv) run(t *testing.T, p Policy, action, arg string) error {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Dir = e.ws
	cmd.Env = append(os.Environ(), e2eChildEnv+"="+action, e2eArgEnv+"="+arg)
	if err := e.w.Wrap(cmd, p); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), helper.Arg) {
		t.Fatalf("command was not wrapped: %q", cmd.Args)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func expectOK(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: expected success, got %v", what, err)
	}
}

func expectDenied(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected the sandbox to deny it, but it succeeded", what)
		return
	}
	t.Logf("%s: denied: %v", what, err)
}

// variants returns the protected-path strategies runnable here.
func variants(t *testing.T, w *linuxWrapper) []BwrapPolicy {
	out := []BwrapPolicy{BwrapNever}
	if b := w.bwrapState(); b.ok {
		out = append(out, BwrapAlways)
	} else {
		t.Logf("bubblewrap variant skipped: %s", b.reason)
	}
	return out
}

func TestE2EFilesystem(t *testing.T) {
	e := newE2E(t)
	for _, bw := range variants(t, e.w) {
		t.Run(string(bw), func(t *testing.T) {
			p := e.policy(bw, NetworkAllowed)
			expectOK(t, "write workspace subdir", e.run(t, p, "write", filepath.Join(e.ws, "src", "x.go")))
			expectOK(t, "write existing top-level file", e.run(t, p, "write", filepath.Join(e.ws, "README.md")))
			expectOK(t, "read outside", e.run(t, p, "read", filepath.Join(e.outside, "profile")))
			expectDenied(t, "write outside ($HOME stand-in)", e.run(t, p, "write", filepath.Join(e.outside, "x")))
			expectDenied(t, "write .pando.toml", e.run(t, p, "write", filepath.Join(e.ws, ".pando.toml")))
			expectDenied(t, "write .pando/pando.db", e.run(t, p, "write", filepath.Join(e.ws, ".pando", "pando.db")))
			expectDenied(t, "create in .pando", e.run(t, p, "write", filepath.Join(e.ws, ".pando", "new")))

			newTop := e.run(t, p, "write", filepath.Join(e.ws, "new-top-level.txt"))
			hook := e.run(t, p, "write", filepath.Join(e.ws, ".git", "hooks", "pre-commit"))
			gitCfg := e.run(t, p, "write", filepath.Join(e.ws, ".git", "config"))
			gitOther := e.run(t, p, "write", filepath.Join(e.ws, ".git", "index.lock"))
			expectOK(t, ".git/index.lock (git must keep working)", gitOther)
			if bw == BwrapNever {
				// Documented Landlock-only trade-offs (splitWritable).
				expectDenied(t, "new top-level entry (Landlock-only split)", newTop)
				if hook != nil || gitCfg != nil {
					t.Errorf("nested protected paths unexpectedly enforced without bwrap: %v %v", hook, gitCfg)
				}
			} else {
				expectOK(t, "new top-level entry", newTop)
				expectDenied(t, "write .git/hooks/pre-commit", hook)
				expectDenied(t, "write .git/config", gitCfg)
			}
		})
	}
}

func TestE2EDenyPathsBwrap(t *testing.T) {
	e := newE2E(t)
	if b := e.w.bwrapState(); !b.ok {
		t.Skipf("bubblewrap unavailable: %s", b.reason)
	}
	keys := filepath.Join(e.ws, "keys")
	if err := os.Mkdir(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keys, "id"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := e.policy(BwrapAlways, NetworkAllowed)
	p.DenyPaths = []string{filepath.Join(e.ws, "*.env"), keys}
	expectDenied(t, "read denied file", e.run(t, p, "read", filepath.Join(e.ws, "secret.env")))
	expectDenied(t, "read file in denied dir", e.run(t, p, "read", filepath.Join(keys, "id")))
	expectDenied(t, "write denied file", e.run(t, p, "write", filepath.Join(e.ws, "secret.env")))
	expectOK(t, "read other file", e.run(t, p, "read", filepath.Join(e.ws, "README.md")))
	if data, _ := os.ReadFile(filepath.Join(e.ws, "secret.env")); string(data) != "orig" {
		t.Errorf("denied file changed: %q", data)
	}
}

func TestE2ENamespaceLockdown(t *testing.T) {
	e := newE2E(t)
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare not installed")
	}
	// Sanity: unprivileged user namespaces must work unsandboxed, or the
	// denial below proves nothing.
	if err := exec.Command(unshare, "-Ur", "true").Run(); err != nil {
		t.Skipf("unshare -Ur fails even unsandboxed: %v", err)
	}
	for _, bw := range variants(t, e.w) {
		t.Run(string(bw), func(t *testing.T) {
			cmd := exec.Command(unshare, "-Ur", "true")
			if err := e.w.Wrap(cmd, e.policy(bw, NetworkAllowed)); err != nil {
				t.Fatal(err)
			}
			out, err := cmd.CombinedOutput()
			expectDenied(t, "unshare -Ur true", err)
			t.Logf("unshare output: %s", strings.TrimSpace(string(out)))
		})
	}
}

func TestE2ENetwork(t *testing.T) {
	e := newE2E(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()
	go acceptAll(ln)

	sock := filepath.Join(t.TempDir(), "s.sock")
	uln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer uln.Close()
	go acceptAll(uln)

	for _, bw := range variants(t, e.w) {
		t.Run(string(bw), func(t *testing.T) {
			expectOK(t, "tcp connect (allowed)", e.run(t, e.policy(bw, NetworkAllowed), "tcp", ln.Addr().String()))
			expectOK(t, "unix connect (allowed)", e.run(t, e.policy(bw, NetworkAllowed), "unix", sock))

			restricted := e.policy(bw, NetworkRestricted)
			expectDenied(t, "tcp connect (restricted)", e.run(t, restricted, "tcp", ln.Addr().String()))
			uerr := e.run(t, restricted, "unix", sock)
			if bw == BwrapNever {
				expectDenied(t, "unix connect (restricted, Landlock-only)", uerr)
			} else {
				expectOK(t, "unix connect (restricted, bwrap)", uerr)
			}
		})
	}
}

func acceptAll(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close()
	}
}

// TestE2EHelperStartup checks the helper adds little latency: it dispatches
// before the rest of the binary initialises and loads no config or DB. The
// bound is generous for loaded CI machines; the best of several runs is used.
func TestE2EHelperStartup(t *testing.T) {
	e := newE2E(t)
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not found")
	}
	p := e.policy(BwrapNever, NetworkRestricted)
	best := time.Hour
	for i := 0; i < 7; i++ {
		cmd := exec.Command(truePath)
		if err := e.w.Wrap(cmd, p); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("wrapped true: %v: %s", err, out)
		}
		if d := time.Since(start); d < best {
			best = d
		}
	}
	t.Logf("best wrapped `true`: %v", best)
	if best > 20*time.Millisecond {
		t.Errorf("helper startup too slow: %v (want < 20ms)", best)
	}
}

func TestE2ESetupFailureExitCode(t *testing.T) {
	requireLandlock(t)
	// A bogus policy fd must fail closed with the helper's setup exit code,
	// without running the command.
	cmd := exec.Command(selfExe, helper.Arg, "--policy-fd", "9", "--", "true")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != helper.ExitSetupFailed {
		t.Fatalf("err = %v, want exit %d", err, helper.ExitSetupFailed)
	}
}
