package sandbox

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/sandbox/helper"
)

func TestLinuxCapabilityGuaranteeFlags(t *testing.T) {
	w := fakeLinuxWrapper(t, 3, nil, bwrapProbe{ok: true, path: "/usr/bin/bwrap"})
	c := w.Capability()
	if !c.Enforced || !c.ProtectsNestedPaths || c.BlocksPorts || !strings.Contains(c.Reason, "ABI 3") {
		t.Fatalf("ABI 3 with bwrap: %+v", c)
	}
	w = fakeLinuxWrapper(t, 6, nil, bwrapProbe{reason: "bubblewrap (bwrap) not found in PATH"})
	c = w.Capability()
	if !c.Enforced || c.ProtectsNestedPaths || !c.BlocksPorts {
		t.Fatalf("ABI 6 without bwrap: %+v", c)
	}
	full, gaps := Guarantees(testPolicy("/ws"), c)
	if full || !slices.Contains(gaps, GapProtectedPaths) {
		t.Fatalf("Landlock-only guarantees: full=%v gaps=%q", full, gaps)
	}
}

func TestBuildSpecGuardedPorts(t *testing.T) {
	p := testPolicy(t.TempDir())
	p.DenyConnectPorts = []int{20001, 8765, 8765}
	s := buildSpec(p, nil, false)
	if !reflect.DeepEqual(s.DenyConnectPorts, []int{8765, 20001}) {
		t.Fatalf("DenyConnectPorts = %v", s.DenyConnectPorts)
	}
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := helper.DecodeSpec(data)
	if err != nil || !reflect.DeepEqual(back.DenyConnectPorts, s.DenyConnectPorts) {
		t.Fatalf("round trip: %+v, %v", back, err)
	}

	p.Network = NetworkRestricted
	if s := buildSpec(p, nil, false); s.DenyConnectPorts != nil {
		t.Fatalf("restricted network carries guarded ports: %v", s.DenyConnectPorts)
	}
}

// TestBwrapPinsAncestors checks that the directories between a writable root
// and a protected path become mount points before the read-only binds.
func TestBwrapPinsAncestors(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "ws")
	for _, d := range []string{".git/hooks", ".pando/data", "src"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, ".git", "config"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := testPolicy(ws)
	p.WritableRoots = []string{base, ws}
	p.ProtectedPaths = append(p.ProtectedPaths, filepath.Join(ws, ".pando", "data"))
	args := bwrapArgs(p, nil, "")
	joined := strings.Join(args, " ")

	gitDir := filepath.Join(ws, ".git")
	for _, want := range []string{
		"--bind " + ws + " " + ws, // the workspace is inside the writable base
		"--bind " + gitDir + " " + gitDir,
		"--bind " + filepath.Join(ws, ".pando") + " " + filepath.Join(ws, ".pando"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("bwrap args lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--bind "+base+" ") {
		t.Errorf("a writable root that is not inside another one was pinned:\n%s", joined)
	}
	if strings.Contains(joined, "--bind "+filepath.Join(ws, "src")) {
		t.Errorf("an unrelated directory was pinned:\n%s", joined)
	}
	// Parent binds come before every read-only bind, shallow first.
	idx := func(s string) int { return strings.Index(joined, s) }
	if !(idx("--bind "+ws+" ") < idx("--bind "+gitDir+" ") &&
		idx("--bind "+gitDir+" ") < idx("--ro-bind "+filepath.Join(gitDir, "hooks"))) {
		t.Errorf("bind order wrong:\n%s", joined)
	}
}

// TestE2EGuardedPorts: a guarded port is refused while the network is
// allowed; any other port, and the same port unguarded, still connects.
func TestE2EGuardedPorts(t *testing.T) {
	e := newE2E(t)
	if abi, err := helper.LandlockABI(); err != nil || abi < helper.LandlockNetABI {
		t.Skipf("Landlock ABI %d: guarding ports needs ABI %d", abi, helper.LandlockNetABI)
	}
	guarded := listenTCP(t)
	open := listenTCP(t)
	for _, bw := range variants(t, e.w) {
		t.Run(string(bw), func(t *testing.T) {
			p := e.policy(bw, NetworkAllowed)
			expectOK(t, "guarded port without a guard", e.run(t, p, "tcp", guarded))

			p.DenyConnectPorts = []int{portOf(t, guarded)}
			start := time.Now()
			expectDenied(t, "connect to the guarded port", e.run(t, p, "tcp", guarded))
			t.Logf("sandboxed run with port rules: %v", time.Since(start))
			expectOK(t, "connect to another port", e.run(t, p, "tcp", open))
			localhost := "localhost:" + strings.Split(guarded, ":")[1]
			expectDenied(t, "connect to the guarded port via localhost", e.run(t, p, "tcp", localhost))
		})
	}
}

// TestE2EGitDirSwapBwrap reproduces the parent-rename attack: move .git away,
// rebuild it without the read-only binds and plant a hook. With bubblewrap
// the move must fail, and ordinary git operations must keep working.
func TestE2EGitDirSwapBwrap(t *testing.T) {
	e := newE2E(t)
	if b := e.w.bwrapState(); !b.ok {
		t.Skipf("bubblewrap unavailable: %s", b.reason)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	// A real repository (newE2E's .git is a stub).
	if err := os.RemoveAll(filepath.Join(e.ws, ".git")); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(gitPath, "-C", e.ws, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	hook := filepath.Join(e.ws, ".git", "hooks", "pre-commit")

	shell := func(p Policy, script string) (string, error) {
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.Dir = e.ws
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+e.outside)
		if err := e.w.Wrap(cmd, p); err != nil {
			t.Fatalf("Wrap: %v", err)
		}
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	for _, nested := range []bool{false, true} {
		name := "workspace-root"
		if nested {
			name = "workspace-inside-writable-root"
		}
		t.Run(name, func(t *testing.T) {
			p := e.policy(BwrapAlways, NetworkAllowed)
			if nested {
				// A workspace under a writable temp dir: the workspace itself
				// could be renamed away too.
				p.WritableRoots = []string{filepath.Dir(e.ws), e.ws}
			}
			out, err := shell(p, "mv .git .g2 && mkdir .git && cp -a .g2/. .git/ && echo evil > .git/hooks/pre-commit")
			expectDenied(t, "mv .git aside and plant a hook", err)
			t.Logf("attack output: %s", out)
			if _, statErr := os.Stat(hook); statErr == nil {
				t.Fatalf("hook planted on the host: %s", hook)
			}
			if _, statErr := os.Stat(filepath.Join(e.ws, ".g2")); statErr == nil {
				t.Fatalf(".git was moved on the host")
			}
			if nested {
				out, err := shell(p, "cd .. && mv ws ws2 && mkdir -p ws/.git/hooks && echo evil > ws/.git/hooks/pre-commit")
				expectDenied(t, "mv the workspace aside", err)
				t.Logf("workspace attack output: %s", out)
				if _, statErr := os.Stat(filepath.Join(filepath.Dir(e.ws), "ws2")); statErr == nil {
					t.Fatalf("the workspace was moved on the host")
				}
			}
			// Normal git work keeps functioning under the pinned binds.
			script := "echo one > file.txt && git add file.txt && git commit -q -m one && " +
				"echo two >> file.txt && git commit -q -am two && git log --oneline | wc -l"
			out, err = shell(p, script)
			if err != nil {
				t.Fatalf("git add/commit under bwrap: %v: %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(e.ws, ".git", "index.lock")); err == nil {
				t.Fatalf("stale index.lock left behind")
			}
			_ = os.RemoveAll(filepath.Join(e.ws, "file.txt"))
		})
	}
}

func listenTCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go acceptAll(ln)
	return ln.Addr().String()
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
