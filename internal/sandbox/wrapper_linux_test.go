package sandbox

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/digiogithub/pando/internal/sandbox/helper"
)

// fakeLinuxWrapper returns a wrapper whose probes report Landlock ABI abi (or
// abiErr) and the given bubblewrap state, without touching the machine.
func fakeLinuxWrapper(t *testing.T, abi int, abiErr error, bw bwrapProbe) *linuxWrapper {
	t.Helper()
	prevABI, prevBwrap := landlockABIProbe, bwrapProbeFunc
	landlockABIProbe = func() (int, error) { return abi, abiErr }
	bwrapProbeFunc = func() bwrapProbe { return bw }
	t.Cleanup(func() { landlockABIProbe, bwrapProbeFunc = prevABI, prevBwrap })
	return newLinuxWrapper()
}

func testPolicy(ws string) Policy {
	return Policy{
		Mode:          ModeWorkspaceWrite,
		Network:       NetworkAllowed,
		Workspace:     ws,
		WritableRoots: []string{ws},
		ProtectedPaths: []string{
			filepath.Join(ws, ".git", "config"),
			filepath.Join(ws, ".git", "hooks"),
			filepath.Join(ws, ".pando"),
			filepath.Join(ws, ".pando.toml"),
		},
		UseBwrap: BwrapNever,
	}
}

// readSpecFile decodes the policy memfd Wrap attached.
func readSpecFile(t *testing.T, f *os.File) helper.Spec {
	t.Helper()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	s, err := helper.DecodeSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLinuxCapabilityNoLandlock(t *testing.T) {
	w := fakeLinuxWrapper(t, 0, syscall.ENOSYS, bwrapProbe{ok: true, path: "/usr/bin/bwrap"})
	c := w.Capability()
	if c.Enforced || c.Backend != BackendNone || !strings.Contains(c.Reason, "5.13") {
		t.Fatalf("Capability = %+v", c)
	}
	cmd := exec.Command("/bin/true")
	before := *cmd
	if err := w.Wrap(cmd, testPolicy(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if cmd.Path != before.Path || !reflect.DeepEqual(cmd.Args, before.Args) || cmd.ExtraFiles != nil || cmd.SysProcAttr != nil {
		t.Fatalf("Wrap modified cmd without Landlock: %+v", cmd)
	}

	w = fakeLinuxWrapper(t, 0, syscall.EOPNOTSUPP, bwrapProbe{})
	if c := w.Capability(); c.Enforced || !strings.Contains(c.Reason, "disabled") {
		t.Fatalf("Capability(EOPNOTSUPP) = %+v", c)
	}
}

func TestLinuxCapabilityBackends(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{ok: true, path: "/usr/bin/bwrap"})
	if c := w.Capability(); !c.Enforced || c.Backend != BackendBwrapLandlock || c.Version != "5" || c.Reason != "" {
		t.Fatalf("with bwrap: %+v", c)
	}
	w = fakeLinuxWrapper(t, 3, nil, bwrapProbe{reason: "bubblewrap (bwrap) not found in PATH"})
	c := w.Capability()
	if !c.Enforced || c.Backend != BackendLandlock || c.Version != "3" || !strings.Contains(c.Reason, "best effort") {
		t.Fatalf("without bwrap: %+v", c)
	}
}

func TestLinuxWrapDisabledPolicy(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{})
	cmd := exec.Command("/bin/true")
	if err := w.Wrap(cmd, Policy{Mode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/bin/true" || cmd.SysProcAttr != nil {
		t.Fatalf("disabled policy modified cmd: %+v", cmd)
	}
}

func TestLinuxWrapRewritesDirect(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{reason: "none"})
	ws := t.TempDir()
	existing, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer existing.Close()

	cmd := exec.Command("/bin/echo", "hello", "--", "world")
	cmd.Dir = ws
	cmd.Env = []string{"A=1"}
	cmd.ExtraFiles = []*os.File{existing}
	p := testPolicy(ws)
	p.Network = NetworkRestricted
	if err := w.Wrap(cmd, p); err != nil {
		t.Fatal(err)
	}

	want := []string{selfExe, helper.Arg, "--policy-fd", "4", "--", "/bin/echo", "hello", "--", "world"}
	if cmd.Path != selfExe || !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("Path=%q Args=%q", cmd.Path, cmd.Args)
	}
	if cmd.Dir != ws || !reflect.DeepEqual(cmd.Env, []string{"A=1"}) {
		t.Fatalf("Dir/Env not kept: %q %q", cmd.Dir, cmd.Env)
	}
	if len(cmd.ExtraFiles) != 2 || cmd.ExtraFiles[0] != existing {
		t.Fatalf("ExtraFiles = %v", cmd.ExtraFiles)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr = %+v", cmd.SysProcAttr)
	}
	s := readSpecFile(t, cmd.ExtraFiles[1])
	if s.Path != "/bin/echo" || s.Network != helper.NetworkRestricted || s.AllowUnixSockets {
		t.Fatalf("spec = %+v", s)
	}
	if !reflect.DeepEqual(s.ReadDirs, []string{"/"}) || s.PolicyHash != p.Hash() {
		t.Fatalf("spec = %+v", s)
	}

	// Idempotent.
	args := slices.Clone(cmd.Args)
	if err := w.Wrap(cmd, p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.Args, args) || len(cmd.ExtraFiles) != 2 {
		t.Fatalf("second Wrap changed cmd: %q", cmd.Args)
	}
}

func TestLinuxWrapSysProcAttrMerge(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{})
	ws := t.TempDir()

	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGTERM}
	if err := w.Wrap(cmd, testPolicy(ws)); err != nil {
		t.Fatal(err)
	}
	if a := cmd.SysProcAttr; a.Setpgid || !a.Setsid || a.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Setsid caller: %+v", a)
	}

	cmd = exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := w.Wrap(cmd, testPolicy(ws)); err != nil {
		t.Fatal(err)
	}
	if a := cmd.SysProcAttr; !a.Setpgid || a.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("plain caller: %+v", a)
	}
}

func TestLinuxWrapCmdErrLeftAlone(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{})
	cmd := exec.Command("definitely-not-a-command-pando-sandbox")
	if cmd.Err == nil {
		t.Skip("lookup unexpectedly succeeded")
	}
	if err := w.Wrap(cmd, testPolicy(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if cmd.ExtraFiles != nil || slices.Contains(cmd.Args, helper.Arg) {
		t.Fatalf("cmd with Err was wrapped: %q", cmd.Args)
	}
}

func TestLinuxWrapBwrap(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".pando.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(ws, "secret.env")
	if err := os.WriteFile(secret, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(ws, "keys")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}

	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{ok: true, path: "/usr/bin/bwrap"})
	p := testPolicy(ws)
	p.UseBwrap = BwrapAuto
	p.Network = NetworkRestricted
	p.DenyPaths = []string{filepath.Join(ws, "*.env"), secretDir}
	cmd := exec.Command("/bin/sh", "-c", "true")
	if err := w.Wrap(cmd, p); err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/usr/bin/bwrap" || cmd.Args[0] != "bwrap" {
		t.Fatalf("Path=%q Args=%q", cmd.Path, cmd.Args)
	}
	joined := strings.Join(cmd.Args, " ")
	exe, _ := os.Executable()
	realWS, _ := filepath.EvalSymlinks(ws)
	for _, want := range []string{
		"--die-with-parent --cap-drop ALL --bind / /",
		"--ro-bind " + filepath.Join(realWS, ".pando.toml") + " " + filepath.Join(realWS, ".pando.toml"),
		"--ro-bind /dev/null " + filepath.Join(realWS, "secret.env"),
		"--tmpfs " + filepath.Join(realWS, "keys") + " --remount-ro " + filepath.Join(realWS, "keys"),
		"--dev-bind /dev /dev --proc /proc -- " + exe + " " + helper.Arg + " --policy-fd 3 -- /bin/sh -c true",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("bwrap args lack %q:\n%s", want, joined)
		}
	}
	// Missing protected paths are not bound (bwrap would create them).
	if strings.Contains(joined, filepath.Join(realWS, ".pando ")) {
		t.Errorf("missing .pando was bound: %s", joined)
	}
	s := readSpecFile(t, cmd.ExtraFiles[0])
	if !s.AllowUnixSockets || !reflect.DeepEqual(s.WriteDirs, []string{ws}) || len(s.WriteFiles) != 0 {
		t.Fatalf("spec = %+v", s)
	}
}

func TestLinuxWrapBwrapAlwaysUnavailable(t *testing.T) {
	w := fakeLinuxWrapper(t, 5, nil, bwrapProbe{reason: "bubblewrap (bwrap) not found in PATH"})
	p := testPolicy(t.TempDir())
	p.UseBwrap = BwrapAlways
	err := w.Wrap(exec.Command("/bin/true"), p)
	if !errors.Is(err, ErrWrapFailed) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
	// auto falls back to Landlock only.
	p.UseBwrap = BwrapAuto
	cmd := exec.Command("/bin/true")
	if err := w.Wrap(cmd, p); err != nil || cmd.Path != selfExe {
		t.Fatalf("auto: err=%v path=%q", err, cmd.Path)
	}
}

func TestBuildSpecSplit(t *testing.T) {
	ws := t.TempDir()
	other := t.TempDir()
	for _, d := range []string{".pando", "src", ".git/hooks"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{".pando.toml", "README.md", ".git/config"} {
		if err := os.WriteFile(filepath.Join(ws, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc", filepath.Join(ws, "etc-link")); err != nil {
		t.Fatal(err)
	}

	p := testPolicy(ws)
	p.WritableRoots = []string{ws, other, filepath.Join(ws, ".pando")}
	s := buildSpec(p, nil, false)
	wantDirs := []string{filepath.Join(ws, ".git"), filepath.Join(ws, "src"), other}
	wantFiles := []string{filepath.Join(ws, "README.md")}
	slices.Sort(s.WriteDirs)
	slices.Sort(wantDirs)
	if !reflect.DeepEqual(s.WriteDirs, wantDirs) || !reflect.DeepEqual(s.WriteFiles, wantFiles) {
		t.Fatalf("split:\n dirs  %q\n files %q", s.WriteDirs, s.WriteFiles)
	}

	// Without an existing protected child the root is granted whole.
	p2 := testPolicy(other)
	if s := buildSpec(p2, nil, false); !reflect.DeepEqual(s.WriteDirs, []string{other}) || s.WriteFiles != nil {
		t.Fatalf("unsplit: %+v", s)
	}

	// Deny paths split too.
	p3 := testPolicy(other)
	if err := os.WriteFile(filepath.Join(other, "a.key"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "b.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s3 := buildSpec(p3, expandDenyPaths([]string{filepath.Join(other, "*.key")}), false)
	if !reflect.DeepEqual(s3.WriteFiles, []string{filepath.Join(other, "b.txt")}) || len(s3.WriteDirs) != 0 {
		t.Fatalf("deny split: %+v", s3)
	}

	// Strict mode reads only the readable roots; restricted network without
	// bwrap does not keep AF_UNIX.
	p4 := testPolicy(ws)
	p4.Mode = ModeStrict
	p4.Network = NetworkRestricted
	p4.ReadableRoots = []string{"/usr", ws}
	s4 := buildSpec(p4, nil, false)
	if !reflect.DeepEqual(s4.ReadDirs, []string{"/usr", ws}) || s4.AllowUnixSockets || !s4.RestrictsNetwork() {
		t.Fatalf("strict: %+v", s4)
	}
	if !slices.Contains(s4.Devices, "/dev/null") || !slices.Contains(s4.Devices, "/dev/tty") {
		t.Fatalf("devices: %q", s4.Devices)
	}
}
