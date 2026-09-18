package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/digiogithub/pando/internal/sandbox/helper"
)

// platformWrapper returns the Linux backend: Landlock + seccomp applied by
// the `pando __sandbox-exec` re-exec helper (internal/sandbox/helper),
// optionally under bubblewrap for the protected paths.
func platformWrapper() Wrapper {
	return newLinuxWrapper()
}

// selfExe is how the child re-executes the running binary: at execve time
// /proc/self/exe names the forked child's image, which is Pando even when the
// binary on disk was replaced or deleted since start.
const selfExe = "/proc/self/exe"

// deviceAllowlist are the device nodes and directories a sandboxed command
// may read, write and ioctl (Grok Build paths.rs DEVICE_FILES/DEVICE_DIRS,
// plus /dev/full and /dev/shm for POSIX shared memory). Missing ones are
// skipped by the helper.
var deviceAllowlist = []string{
	"/dev/null", "/dev/zero", "/dev/full", "/dev/random", "/dev/urandom",
	"/dev/tty", "/dev/ptmx", "/dev/pts", "/dev/shm", "/dev/fd",
}

// Probe hooks, replaced by tests.
var (
	landlockABIProbe = helper.LandlockABI
	bwrapProbeFunc   = probeBwrap
)

// linuxWrapper is the Linux Wrapper. Probes run once, lazily.
type linuxWrapper struct {
	capOnce sync.Once
	abi     int
	reason  string // non-empty when not enforced

	bwrapOnce sync.Once
	bwrap     bwrapProbe
	exe       string // os.Executable(), for bwrap (which cannot use /proc/self/exe)
}

func newLinuxWrapper() *linuxWrapper {
	return &linuxWrapper{}
}

func (w *linuxWrapper) probe() {
	w.capOnce.Do(func() {
		if _, err := helper.NativeArch(); err != nil {
			w.reason = err.Error()
			return
		}
		abi, err := landlockABIProbe()
		if err != nil {
			w.reason = helper.LandlockUnavailableReason(err)
			return
		}
		if abi < 1 {
			w.reason = fmt.Sprintf("unexpected Landlock ABI %d", abi)
			return
		}
		w.abi = abi
	})
}

func (w *linuxWrapper) enforced() bool {
	w.probe()
	return w.reason == ""
}

func (w *linuxWrapper) bwrapState() bwrapProbe {
	w.bwrapOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			w.bwrap = bwrapProbe{reason: "cannot resolve the Pando executable for bubblewrap: " + err.Error()}
			return
		}
		w.exe = exe
		w.bwrap = bwrapProbeFunc()
	})
	return w.bwrap
}

// Capability reports Landlock availability. With a working bubblewrap the
// backend is "bwrap+landlock" (protected paths enforced; this is what
// useBwrap "auto" and "always" use); otherwise "landlock+seccomp" with a
// reason saying protected paths are best effort. Blocking Pando's own TCP
// ports needs Landlock network rules (ABI 4, Linux 6.7).
func (w *linuxWrapper) Capability() Capability {
	if !w.enforced() {
		return Capability{Backend: BackendNone, Enforced: false, Reason: w.reason}
	}
	c := Capability{Backend: BackendLandlock, Version: strconv.Itoa(w.abi), Enforced: true,
		BlocksPorts: w.abi >= helper.LandlockNetABI}
	var reasons []string
	if b := w.bwrapState(); b.ok {
		c.Backend = BackendBwrapLandlock
		c.ProtectsNestedPaths = true
	} else {
		reasons = append(reasons, "protected paths inside writable roots are best effort ("+b.reason+")")
	}
	if !c.BlocksPorts {
		reasons = append(reasons, fmt.Sprintf("Landlock ABI %d cannot block Pando's own ports (ABI %d, Linux 6.7+, needed)",
			w.abi, helper.LandlockNetABI))
	}
	c.Reason = strings.Join(reasons, "; ")
	return c
}

// Wrap rewrites cmd to
//
//	/proc/self/exe __sandbox-exec --policy-fd N -- <argv...>
//
// or, when bubblewrap is used,
//
//	bwrap <binds...> -- <pando> __sandbox-exec --policy-fd N -- <argv...>
//
// where fd N (3 + the caller's own ExtraFiles) is a memfd holding the helper
// Spec as JSON. cmd.Dir and cmd.Env are kept; the original cmd.Path is the
// exec target and cmd.Args its argv. The child gets its own process group
// unless the caller configured SysProcAttr's session/group itself.
func (w *linuxWrapper) Wrap(cmd *exec.Cmd, p Policy) error {
	if !p.Enabled() || !w.enforced() {
		return nil
	}
	if cmd.Err != nil {
		// exec.Command could not resolve the program; Start reports it.
		return nil
	}
	if isWrapped(cmd) {
		return nil
	}

	useBwrap := false
	switch p.UseBwrap {
	case BwrapNever:
	case BwrapAlways:
		b := w.bwrapState()
		if !b.ok {
			return bwrapUnavailable(b)
		}
		useBwrap = true
	default:
		useBwrap = w.bwrapState().ok
	}

	target := cmd.Path
	if target == "" {
		return fmt.Errorf("%w: command has no path", ErrWrapFailed)
	}
	argv := cmd.Args
	if len(argv) == 0 {
		argv = []string{target}
	}

	deny := expandDenyPaths(p.DenyPaths)
	spec := buildSpec(p, deny, useBwrap)
	spec.Path = target
	data, err := spec.Encode()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrapFailed, err)
	}
	policyFile, err := policyMemfd(data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrapFailed, err)
	}

	// ExtraFiles[i] becomes fd 3+i in the child. The parent's copy of the
	// memfd is closed by the os.File finalizer once cmd is released.
	fd := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, policyFile)
	helperArgs := helper.Args(fd, argv)

	if useBwrap {
		home, _ := os.UserHomeDir()
		args := append([]string{"bwrap"}, bwrapArgs(p, deny, home)...)
		args = append(args, "--", w.exe)
		cmd.Path = w.bwrap.path
		cmd.Args = append(args, helperArgs...)
	} else {
		cmd.Path = selfExe
		cmd.Args = append([]string{selfExe}, helperArgs...)
	}

	switch attr := cmd.SysProcAttr; {
	case attr == nil:
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	case !attr.Setsid && !attr.Setpgid && !attr.Foreground:
		attr.Setpgid = true
	}
	return nil
}

// isWrapped reports whether cmd was already rewritten by Wrap.
func isWrapped(cmd *exec.Cmd) bool {
	return len(cmd.Args) > 1 && slices.Contains(cmd.Args, helper.Arg) &&
		(cmd.Path == selfExe || filepath.Base(cmd.Path) == "bwrap")
}

// policyMemfd returns an anonymous in-memory file holding data. Unlike a
// pipe it has no capacity limit (large strict-mode policies) and needs no
// writer goroutine.
func policyMemfd(data []byte) (*os.File, error) {
	fd, err := unix.MemfdCreate("pando-sandbox-policy", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("memfd_create: %w", err)
	}
	f := os.NewFile(uintptr(fd), "pando-sandbox-policy")
	if _, err := f.Write(data); err != nil {
		f.Close()
		return nil, fmt.Errorf("write policy: %w", err)
	}
	return f, nil
}

// buildSpec turns a policy into the helper Spec.
//
// Read scope: strict mode reads only ReadableRoots; the other modes read
// everything ("/"). Write scope: WritableRoots.
//
// Protected and deny paths inside a writable root:
//   - with bubblewrap they are bound read-only / hidden (bwrapArgs), so the
//     writable roots are granted whole;
//   - without it (Landlock cannot carve an exception out of a granted tree) a
//     writable root that directly contains a protected or deny path is
//     granted entry by entry, skipping those paths (see splitWritable).
//
// Network: restricted uses the seccomp network filter; AF_UNIX stays usable
// only with bubblewrap, which masks the container-runtime and D-Bus sockets.
// Allowed passes the guarded ports (Pando's own listeners), which the helper
// blocks with Landlock network rules when the kernel has them.
func buildSpec(p Policy, deny []string, useBwrap bool) helper.Spec {
	s := helper.Spec{
		Network:    helper.NetworkAllowed,
		Devices:    slices.Clone(deviceAllowlist),
		PolicyHash: p.Hash(),
	}
	if p.RestrictsNetwork() {
		s.Network = helper.NetworkRestricted
		s.AllowUnixSockets = useBwrap
	} else {
		s.DenyConnectPorts = cleanPorts(p.DenyConnectPorts)
	}
	if len(p.ReadableRoots) > 0 {
		s.ReadDirs = slices.Clone(p.ReadableRoots)
	} else {
		s.ReadDirs = []string{"/"}
	}
	if useBwrap {
		s.WriteDirs = slices.Clone(p.WritableRoots)
	} else {
		excluded := append(slices.Clone(p.ProtectedPaths), deny...)
		s.WriteDirs, s.WriteFiles = splitWritable(p.WritableRoots, excluded)
	}
	return s
}

// splitWritable is the Landlock-only fallback for protected paths (Grok
// Build's devbox trick, profiles.rs:421-441). For each writable root:
//
//   - a root that is itself excluded is not granted;
//   - a root with no excluded direct child is granted whole;
//   - otherwise every existing entry of the root is granted on its own
//     (directories recursively, files individually), except the excluded
//     ones and symlinks (a rule would follow the link out of the root).
//
// Trade-off (documented as "best effort" in Capability.Reason): in a split
// root, new top-level entries cannot be created and top-level files cannot be
// replaced by rename, because Landlock rights granted on the root directory
// would extend to the protected entries beneath it. Excluded paths deeper than
// a direct child (e.g. <ws>/.git/hooks) are not enforced: splitting .git
// would stop git from writing .git/index.lock and friends.
func splitWritable(roots, excluded []string) (dirs, files []string) {
	for _, root := range roots {
		if slices.Contains(excluded, root) {
			continue
		}
		var children []string
		for _, x := range excluded {
			if filepath.Dir(x) == root && x != root {
				if _, err := os.Lstat(x); err == nil {
					children = append(children, x)
				}
			}
		}
		if len(children) == 0 {
			dirs = append(dirs, root)
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			// Unreadable root: grant nothing rather than too much.
			continue
		}
		for _, e := range entries {
			full := filepath.Join(root, e.Name())
			if slices.Contains(children, full) {
				continue
			}
			switch mode := e.Type(); {
			case mode&os.ModeSymlink != 0:
				continue
			case mode.IsDir():
				dirs = append(dirs, full)
			default:
				files = append(files, full)
			}
		}
	}
	return dirs, files
}

// expandDenyPaths expands glob patterns in deny paths to the existing
// matches; plain paths are kept as they are.
func expandDenyPaths(in []string) []string {
	var out []string
	for _, p := range in {
		if !hasGlobMeta(p) {
			out = append(out, p)
			continue
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			continue
		}
		out = append(out, matches...)
	}
	return sortedCopy(out)
}

func hasGlobMeta(p string) bool {
	for _, c := range p {
		switch c {
		case '*', '?', '[':
			return true
		}
	}
	return false
}
