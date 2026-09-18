package sandbox

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox/portguard"
)

// fullCap is an enforced backend with every guarantee.
var fullCap = Capability{Backend: BackendBwrapLandlock, Version: "6", Enforced: true,
	ProtectsNestedPaths: true, BlocksPorts: true}

type capWrapper struct{ c Capability }

func (w capWrapper) Capability() Capability     { return w.c }
func (capWrapper) Wrap(*exec.Cmd, Policy) error { return nil }

func TestResolveGuardedPorts(t *testing.T) {
	opts := ResolveOptions{GOOS: "linux", HomeDir: "/home/u", GuardedPorts: []int{20001, 8765, 0, 8765}}
	p := ResolveConfig(config.SandboxConfig{}, "/ws", opts)
	if !reflect.DeepEqual(p.DenyConnectPorts, []int{8765, 20001}) {
		t.Fatalf("workspace-write DenyConnectPorts = %v", p.DenyConnectPorts)
	}
	// A restricted network already refuses every TCP connection.
	r := ResolveConfig(config.SandboxConfig{Network: "restricted"}, "/ws", opts)
	if r.DenyConnectPorts != nil {
		t.Fatalf("restricted DenyConnectPorts = %v", r.DenyConnectPorts)
	}
	off := ResolveConfig(config.SandboxConfig{Disabled: true}, "/ws", opts)
	if off.DenyConnectPorts != nil {
		t.Fatalf("off DenyConnectPorts = %v", off.DenyConnectPorts)
	}
}

// TestGuardedPortChangesHash: registering a listener changes the policy hash,
// which is what makes the persistent shell re-spawn with the new port denied.
func TestGuardedPortChangesHash(t *testing.T) {
	portguard.ResetForTests()
	t.Cleanup(portguard.ResetForTests)
	t.Cleanup(portguard.SetSharedDirForTests(""))
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { config.SetForTests(prev) })
	t.Setenv(EnvVar, "")

	before := CurrentPolicyHash()
	unregister := RegisterGuardedPort(18765, "test-api")
	if got := Current().DenyConnectPorts; !reflect.DeepEqual(got, []int{18765}) {
		t.Fatalf("DenyConnectPorts = %v", got)
	}
	during := CurrentPolicyHash()
	if during == before {
		t.Fatal("registering a guarded port did not change the policy hash")
	}
	unregister()
	if after := CurrentPolicyHash(); after != before {
		t.Fatal("unregistering did not restore the policy hash")
	}

	// Order and duplicates do not matter.
	a := Policy{Mode: ModeWorkspaceWrite, DenyConnectPorts: []int{2, 1, 2}}
	b := Policy{Mode: ModeWorkspaceWrite, DenyConnectPorts: []int{1, 2}}
	if a.Hash() != b.Hash() {
		t.Fatal("hash depends on port order/duplicates")
	}
}

func TestGuarantees(t *testing.T) {
	ws := "/ws"
	p := Policy{Mode: ModeWorkspaceWrite, Network: NetworkAllowed, Workspace: ws,
		WritableRoots:    []string{ws, "/tmp"},
		ProtectedPaths:   []string{ws + "/.git/hooks", ws + "/.pando"},
		DenyConnectPorts: []int{8765}}

	if full, gaps := Guarantees(p, fullCap); !full || len(gaps) != 0 {
		t.Fatalf("full backend: full=%v gaps=%q", full, gaps)
	}
	if full, gaps := Guarantees(p, Capability{Backend: BackendNone}); full || !slices.Equal(gaps, []string{GapNotEnforced}) {
		t.Fatalf("not enforced: full=%v gaps=%q", full, gaps)
	}
	if full, _ := Guarantees(Policy{Mode: ModeOff}, fullCap); full {
		t.Fatal("off policy reported full")
	}

	noBwrap := fullCap
	noBwrap.Backend, noBwrap.ProtectsNestedPaths = BackendLandlock, false
	if full, gaps := Guarantees(p, noBwrap); full || !slices.Equal(gaps, []string{GapProtectedPaths}) {
		t.Fatalf("Landlock-only: full=%v gaps=%q", full, gaps)
	}
	// Only top-level protected paths and no deny paths: the split enforces them.
	top := p
	top.ProtectedPaths = []string{ws + "/.pando", ws + "/.pando.toml"}
	if full, gaps := Guarantees(top, noBwrap); !full {
		t.Fatalf("top-level protected paths only: gaps=%q", gaps)
	}
	top.DenyPaths = []string{"/home/u/.ssh"}
	if full, _ := Guarantees(top, noBwrap); full {
		t.Fatal("deny paths are not read-protected without bwrap")
	}

	never := p
	never.UseBwrap = BwrapNever
	if full, gaps := Guarantees(never, fullCap); full || !slices.Equal(gaps, []string{GapBwrapNever}) {
		t.Fatalf("UseBwrap never: full=%v gaps=%q", full, gaps)
	}

	oldKernel := fullCap
	oldKernel.BlocksPorts = false
	if full, gaps := Guarantees(p, oldKernel); full || !slices.Equal(gaps, []string{GapGuardedPorts}) {
		t.Fatalf("ABI < 4: full=%v gaps=%q", full, gaps)
	}
	restricted := p
	restricted.Network = NetworkRestricted
	if full, _ := Guarantees(restricted, oldKernel); !full {
		t.Fatal("a restricted network needs no port guard")
	}
	noPorts := p
	noPorts.DenyConnectPorts = nil
	if full, _ := Guarantees(noPorts, oldKernel); !full {
		t.Fatal("no guarded ports, nothing to block")
	}
}

func TestStatusLabelPartial(t *testing.T) {
	p := Policy{Mode: ModeWorkspaceWrite, Network: NetworkAllowed, DenyConnectPorts: []int{8765}}
	c := fullCap
	c.BlocksPorts = false
	c.Reason = "Landlock ABI 3 cannot block Pando's own ports"
	s := NewStatus(p, c)
	if !s.Active || s.Full || len(s.Gaps) != 1 {
		t.Fatalf("status = %+v", s)
	}
	if l := s.Label(); !strings.Contains(l, "partial: "+GapGuardedPorts) || strings.Contains(l, "cannot block") {
		t.Fatalf("Label = %q", l)
	}
	if l := NewStatus(p, fullCap).Label(); strings.Contains(l, "partial") {
		t.Fatalf("full Label = %q", l)
	}
}

// TestAutoAllowBashNeedsFullGuarantees: an enforced but partial sandbox keeps
// the bash permission prompt.
func TestAutoAllowBashNeedsFullGuarantees(t *testing.T) {
	portguard.ResetForTests()
	t.Cleanup(portguard.ResetForTests)
	t.Cleanup(portguard.SetSharedDirForTests(""))
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { config.SetForTests(prev) })
	t.Setenv(EnvVar, "")

	restore := SetDefaultForTests(capWrapper{fullCap})
	defer restore()
	if !AutoAllowBash() {
		t.Fatal("full sandbox should auto-allow")
	}

	noBwrap := fullCap
	noBwrap.Backend, noBwrap.ProtectsNestedPaths = BackendLandlock, false
	SetDefaultForTests(capWrapper{noBwrap})
	if AutoAllowBash() {
		t.Fatal("Landlock-only (protected .git/hooks unenforced) must keep the prompt")
	}

	oldKernel := fullCap
	oldKernel.BlocksPorts = false
	SetDefaultForTests(capWrapper{oldKernel})
	if !AutoAllowBash() {
		t.Fatal("no Pando listener registered: nothing to block, should auto-allow")
	}
	unregister := RegisterGuardedPort(18765, "test-api")
	defer unregister()
	if AutoAllowBash() {
		t.Fatal("a guarded port the backend cannot block must keep the prompt")
	}
}
