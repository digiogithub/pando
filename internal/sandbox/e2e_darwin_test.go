//go:build darwin

package sandbox

// End-to-end tests of the Seatbelt backend: real commands under
// /usr/bin/sandbox-exec. They self-skip when sandbox-exec is missing or
// unusable (e.g. the test process is itself sandboxed), following Grok
// Build's deny_paths_e2e pattern. Policies are built by hand so the only
// writable places are the ones each test names (t.TempDir lives under
// /private/var/folders, which a resolved policy would make writable).

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sbE2EWrapper(t *testing.T) Wrapper {
	t.Helper()
	if _, err := os.Stat(sandboxExecPath); err != nil {
		t.Skipf("%s not available: %v", sandboxExecPath, err)
	}
	w := &seatbeltWrapper{}
	if c := w.Capability(); !c.Enforced {
		t.Skipf("seatbelt not enforced here: %s", c.Reason)
	}
	return w
}

// sbE2ERealDir returns a fresh temp dir with symlinks resolved.
func sbE2ERealDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sbE2ERun runs `/bin/sh -c script` under p and returns its combined
// output and error.
func sbE2ERun(t *testing.T, w Wrapper, p Policy, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	if err := w.Wrap(cmd, p); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if cmd.Path != sandboxExecPath {
		t.Fatalf("command not wrapped: %s %q", cmd.Path, cmd.Args)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sbE2EMustRun(t *testing.T, w Wrapper, p Policy, script string) {
	t.Helper()
	if out, err := sbE2ERun(t, w, p, script); err != nil {
		t.Errorf("%q should succeed: %v\n%s", script, err, out)
	}
}

func sbE2EMustFail(t *testing.T, w Wrapper, p Policy, script string) {
	t.Helper()
	if out, err := sbE2ERun(t, w, p, script); err == nil {
		t.Errorf("%q should be denied, it succeeded:\n%s", script, out)
	}
}

func sbE2EWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sbE2EPolicy(mode Mode, network Network, ws string) Policy {
	p := Policy{
		Mode:      mode,
		Network:   network,
		Workspace: ws,
		ProtectedPaths: []string{
			filepath.Join(ws, ".pando"),
			filepath.Join(ws, ".pando.toml"),
			filepath.Join(ws, ".git", "hooks"),
			filepath.Join(ws, ".git", "config"),
		},
	}
	if mode != ModeReadOnly {
		p.WritableRoots = []string{ws}
	}
	return p
}

func TestSeatbeltCapability(t *testing.T) {
	w := sbE2EWrapper(t)
	c := w.Capability()
	if c.Backend != BackendSeatbelt || !c.Enforced {
		t.Fatalf("capability = %+v", c)
	}
	if Default().Capability().Backend != BackendSeatbelt {
		t.Errorf("Default() backend = %s", Default().Capability().Backend)
	}
}

func TestSeatbeltWrapLeavesDisabledPolicyAlone(t *testing.T) {
	w := sbE2EWrapper(t)
	cmd := exec.Command("/usr/bin/true")
	if err := w.Wrap(cmd, Policy{Mode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/usr/bin/true" {
		t.Errorf("disabled policy wrapped the command: %s", cmd.Path)
	}
}

func TestSeatbeltWorkspaceWrite(t *testing.T) {
	w := sbE2EWrapper(t)
	ws, outside := sbE2ERealDir(t), sbE2ERealDir(t)
	sbE2EWriteFile(t, filepath.Join(ws, ".pando.toml"), "x=1\n")
	sbE2EWriteFile(t, filepath.Join(ws, ".pando", "data", "pando.db"), "db")
	sbE2EWriteFile(t, filepath.Join(ws, ".git", "hooks", "pre-commit.sample"), "#!/bin/sh\n")
	sbE2EWriteFile(t, filepath.Join(ws, ".git", "config"), "[core]\n")
	sbE2EWriteFile(t, filepath.Join(ws, ".git", "HEAD"), "ref: refs/heads/main\n")
	p := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, ws)

	sbE2EMustRun(t, w, p, fmt.Sprintf("touch %q && mkdir -p %q", ws+"/x", ws+"/sub/dir"))
	sbE2EMustRun(t, w, p, fmt.Sprintf("echo ok > %q", ws+"/.git/HEAD.new")) // rest of .git stays writable
	sbE2EMustRun(t, w, p, fmt.Sprintf("cat %q >/dev/null", ws+"/.pando.toml"))
	sbE2EMustRun(t, w, p, "echo hi >/dev/null")
	sbE2EMustFail(t, w, p, fmt.Sprintf("touch %q", outside+"/x"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo y=2 >> %q", ws+"/.pando.toml"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("rm %q", ws+"/.pando.toml"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("mv %q %q", ws+"/.pando.toml", ws+"/moved.toml"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("touch %q", ws+"/.pando/new"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo x > %q", ws+"/.pando/data/pando.db"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo '#!/bin/sh' > %q", ws+"/.git/hooks/pre-commit"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo '[x]' >> %q", ws+"/.git/config"))
	// Renaming a protected path's parent away (to edit the copy and move it
	// back) is blocked by the ancestor guards.
	sbE2EMustFail(t, w, p, fmt.Sprintf("mv %q %q", ws+"/.git", ws+"/.git-moved"))
	if _, err := os.Stat(filepath.Join(ws, ".git", "hooks")); err != nil {
		t.Errorf(".git was moved: %v", err)
	}
}

func TestSeatbeltGitInitWithoutExistingGitDir(t *testing.T) {
	w := sbE2EWrapper(t)
	ws := sbE2ERealDir(t)
	p := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, ws)
	// .git does not exist yet: creating it must work (only existing parents
	// are guarded).
	sbE2EMustRun(t, w, p, fmt.Sprintf("mkdir %q && touch %q", ws+"/.git", ws+"/.git/HEAD"))
}

func TestSeatbeltReadOnly(t *testing.T) {
	w := sbE2EWrapper(t)
	ws := sbE2ERealDir(t)
	sbE2EWriteFile(t, filepath.Join(ws, "file.txt"), "hello")
	p := sbE2EPolicy(ModeReadOnly, NetworkRestricted, ws)
	sbE2EMustRun(t, w, p, fmt.Sprintf("cat %q", ws+"/file.txt"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("touch %q", ws+"/x"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo x >> %q", ws+"/file.txt"))
}

func TestSeatbeltStrictReads(t *testing.T) {
	w := sbE2EWrapper(t)
	ws, outside := sbE2ERealDir(t), sbE2ERealDir(t)
	sbE2EWriteFile(t, filepath.Join(outside, "secret"), "s3cret")
	p := sbE2EPolicy(ModeStrict, NetworkRestricted, ws)
	p.ReadableRoots = []string{"/usr", "/bin", "/sbin", "/System", "/Library", "/dev",
		"/private/etc", "/private/var/db", ws}
	sbE2EMustRun(t, w, p, fmt.Sprintf("echo ok > %q && cat %q", ws+"/f", ws+"/f"))
	sbE2EMustRun(t, w, p, fmt.Sprintf("test -e %q", outside+"/secret")) // metadata stays visible
	sbE2EMustFail(t, w, p, fmt.Sprintf("cat %q", outside+"/secret"))
}

func TestSeatbeltDenyPaths(t *testing.T) {
	w := sbE2EWrapper(t)
	ws := sbE2ERealDir(t)
	sbE2EWriteFile(t, filepath.Join(ws, "secrets", "token"), "t")
	sbE2EWriteFile(t, filepath.Join(ws, "a.key"), "k")
	sbE2EWriteFile(t, filepath.Join(ws, "a.txt"), "ok")
	sbE2EWriteFile(t, filepath.Join(ws, "nested", "b.key"), "k")
	p := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, ws)
	p.DenyPaths = []string{filepath.Join(ws, "secrets"), filepath.Join(ws, "*.key")}

	sbE2EMustRun(t, w, p, fmt.Sprintf("cat %q", ws+"/a.txt"))
	sbE2EMustRun(t, w, p, fmt.Sprintf("cat %q", ws+"/nested/b.key")) // "*" stays in one directory
	sbE2EMustFail(t, w, p, fmt.Sprintf("cat %q", ws+"/secrets/token"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo x > %q", ws+"/secrets/new"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("cat %q", ws+"/a.key"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("touch %q", ws+"/new.key"))
	// The denied directory itself cannot be renamed out of the deny.
	sbE2EMustFail(t, w, p, fmt.Sprintf("mv %q %q", ws+"/secrets", ws+"/public"))
}

// TestSeatbeltPrivateTmpAliases: rules given with /tmp must hold through
// /private/tmp and vice versa (Grok macos_deny_aliases).
func TestSeatbeltPrivateTmpAliases(t *testing.T) {
	w := sbE2EWrapper(t)
	base, err := os.MkdirTemp("/tmp", "pando-sbpl-e2e-")
	if err != nil {
		t.Skipf("cannot create a dir under /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	name := filepath.Base(base)
	viaTmp := "/tmp/" + name
	viaPrivate := "/private/tmp/" + name
	sbE2EWriteFile(t, filepath.Join(viaTmp, "secret"), "s")
	sbE2EWriteFile(t, filepath.Join(viaTmp, ".pando.toml"), "x=1\n")

	// Everything given in the /tmp form.
	p := Policy{
		Mode: ModeWorkspaceWrite, Network: NetworkAllowed, Workspace: viaTmp,
		WritableRoots:  []string{viaTmp},
		ProtectedPaths: []string{viaTmp + "/.pando.toml"},
		DenyPaths:      []string{viaTmp + "/secret"},
	}
	sbE2EMustRun(t, w, p, fmt.Sprintf("touch %q", viaPrivate+"/ok1"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("cat %q", viaPrivate+"/secret"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo y >> %q", viaPrivate+"/.pando.toml"))

	// Everything given in the /private/tmp form.
	p.Workspace = viaPrivate
	p.WritableRoots = []string{viaPrivate}
	p.ProtectedPaths = []string{viaPrivate + "/.pando.toml"}
	p.DenyPaths = []string{viaPrivate + "/secret"}
	sbE2EMustRun(t, w, p, fmt.Sprintf("touch %q", viaTmp+"/ok2"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("cat %q", viaTmp+"/secret"))
	sbE2EMustFail(t, w, p, fmt.Sprintf("echo y >> %q", viaTmp+"/.pando.toml"))
}

func TestSeatbeltNetwork(t *testing.T) {
	w := sbE2EWrapper(t)
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc not available")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen: %v", err)
	}
	defer ln.Close()
	go sbE2EAcceptAll(ln)
	port := ln.Addr().(*net.TCPAddr).Port

	ws := sbE2ERealDir(t)
	sock := filepath.Join(ws, "s.sock")
	uln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix listen: %v", err)
	}
	defer uln.Close()
	go sbE2EAcceptAll(uln)

	tcp := fmt.Sprintf("nc -z -w 3 127.0.0.1 %d", port)
	unix := fmt.Sprintf("nc -U -z %q", sock)

	allowed := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, ws)
	sbE2EMustRun(t, w, allowed, tcp)
	sbE2EMustRun(t, w, allowed, unix)

	restricted := sbE2EPolicy(ModeWorkspaceWrite, NetworkRestricted, ws)
	sbE2EMustFail(t, w, restricted, tcp)
	sbE2EMustRun(t, w, restricted, unix) // unix-domain sockets stay allowed
}

func sbE2EAcceptAll(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}

func TestSeatbeltRejectsBadPathWithWrapError(t *testing.T) {
	w := sbE2EWrapper(t)
	p := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, sbE2ERealDir(t))
	p.DenyPaths = []string{"/tmp/bad\npath"}
	cmd := exec.Command("/usr/bin/true")
	err := w.Wrap(cmd, p)
	if !errors.Is(err, ErrWrapFailed) || !strings.Contains(err.Error(), "Seatbelt") {
		t.Fatalf("Wrap error = %v, want an ErrWrapFailed/ErrSBPLPath error", err)
	}
}

// TestSeatbeltGuardedPorts: with the network allowed, a guarded port is
// refused (by address and by localhost) while another port still connects.
func TestSeatbeltGuardedPorts(t *testing.T) {
	w := sbE2EWrapper(t)
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("nc not available")
	}
	listen := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("listen: %v", err)
		}
		t.Cleanup(func() { ln.Close() })
		go sbE2EAcceptAll(ln)
		return ln.Addr().(*net.TCPAddr).Port
	}
	guarded, open := listen(), listen()
	ws := sbE2ERealDir(t)
	p := sbE2EPolicy(ModeWorkspaceWrite, NetworkAllowed, ws)
	p.DenyConnectPorts = []int{guarded}
	sbE2EMustFail(t, w, p, fmt.Sprintf("nc -z -w 3 127.0.0.1 %d", guarded))
	sbE2EMustFail(t, w, p, fmt.Sprintf("nc -z -w 3 localhost %d", guarded))
	sbE2EMustRun(t, w, p, fmt.Sprintf("nc -z -w 3 127.0.0.1 %d", open))
}
