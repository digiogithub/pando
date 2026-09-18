package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// bubblewrap enforces what Landlock cannot: read-only (and hidden) subpaths
// inside a writable root. The helper runs inside bwrap, so Landlock and
// seccomp still apply; bwrap only adds the mount namespace with the binds
// (Grok Build lib.rs:300-368, bwrap_reexec_command_ex).

// bwrapProbe is the cached result of probing bubblewrap.
type bwrapProbe struct {
	path    string
	version string
	ok      bool
	reason  string
}

// bwrapBaseArgs are the flags every bwrap invocation (probe included) uses:
// the host root bound read-write (Landlock does the confinement), all
// capabilities dropped, devices and a fresh /proc.
func bwrapBaseArgs() []string {
	return []string{"--die-with-parent", "--cap-drop", "ALL", "--bind", "/", "/"}
}

func bwrapTailArgs() []string {
	return []string{"--dev-bind", "/dev", "/dev", "--proc", "/proc"}
}

// probeBwrap looks bwrap up in PATH and runs it once with the real flags
// around `true`: a present binary is not enough (unprivileged user
// namespaces may be disabled, or /proc may not be mountable in a container).
func probeBwrap() bwrapProbe {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return bwrapProbe{reason: "bubblewrap (bwrap) not found in PATH"}
	}
	target, err := exec.LookPath("true")
	if err != nil {
		return bwrapProbe{path: path, reason: "cannot probe bubblewrap: `true` not found in PATH"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var version string
	if out, err := exec.CommandContext(ctx, path, "--version").Output(); err == nil {
		version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "bubblewrap"))
	}

	args := append(bwrapBaseArgs(), bwrapTailArgs()...)
	args = append(args, "--", target)
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		return bwrapProbe{path: path, version: version, reason: "bubblewrap unusable: " + msg}
	}
	return bwrapProbe{path: path, version: version, ok: true}
}

// dbusSocketPaths are the D-Bus and systemd private sockets. A message on
// them can make systemd start a unit outside the sandbox, so they are always
// masked (Grok Build runtime_sockets.rs dbus_socket_deny_paths).
func dbusSocketPaths() []string {
	uid := strconv.Itoa(os.Getuid())
	return []string{
		"/run/dbus/system_bus_socket",
		"/var/run/dbus/system_bus_socket",
		"/run/systemd/private",
		"/run/user/" + uid + "/bus",
		"/run/user/" + uid + "/systemd/private",
	}
}

// runtimeSocketPaths are container-runtime API sockets: talking to a daemon
// that runs containers is a way around a restricted network, so they are
// masked when the network is restricted (runtime_sockets.rs).
func runtimeSocketPaths(home string) []string {
	uid := strconv.Itoa(os.Getuid())
	paths := []string{
		"/run/docker.sock",
		"/var/run/docker.sock",
		"/run/podman/podman.sock",
		"/var/run/podman/podman.sock",
		"/run/containerd/containerd.sock",
		"/var/run/containerd/containerd.sock",
		"/run/user/" + uid + "/docker.sock",
		"/run/user/" + uid + "/podman/podman.sock",
		"/run/user/" + uid + "/containerd/containerd.sock",
	}
	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".docker", "desktop", "docker.sock"),
			filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	return paths
}

// bwrapArgs builds the bwrap arguments (without the trailing "--" and
// command) for a policy:
//
//   - each existing protected path is bound read-only onto itself;
//   - each existing deny path is hidden: a directory under an empty read-only
//     tmpfs, anything else under /dev/null bound without device access (open
//     fails with EACCES);
//   - existing D-Bus/systemd sockets, and container-runtime sockets when the
//     network is restricted, are masked with /dev/null (connect fails).
//
// Before those, every existing directory between a writable root and a
// protected or deny path (see pinnedAncestors) is bound read-write onto
// itself. A mount point cannot be renamed or removed (EBUSY), so a command
// cannot move .git aside, recreate it without the read-only binds and plant
// a hook in the copy.
//
// Paths that do not exist are skipped: bwrap would otherwise create mount
// points on the host.
func bwrapArgs(p Policy, deny []string, home string) []string {
	args := bwrapBaseArgs()
	for _, dir := range pinnedAncestors(p.WritableRoots, append(slices.Clone(p.ProtectedPaths), deny...)) {
		args = append(args, "--bind", dir, dir)
	}
	for _, path := range p.ProtectedPaths {
		if real, ok := existingReal(path); ok {
			args = append(args, "--ro-bind", real, real)
		}
	}
	for _, path := range deny {
		real, ok := existingReal(path)
		if !ok {
			continue
		}
		if fi, err := os.Stat(real); err == nil && fi.IsDir() {
			args = append(args, "--tmpfs", real, "--remount-ro", real)
		} else {
			args = append(args, "--ro-bind", "/dev/null", real)
		}
	}
	sockets := dbusSocketPaths()
	if p.RestrictsNetwork() {
		sockets = append(sockets, runtimeSocketPaths(home)...)
	}
	for _, path := range sockets {
		if real, ok := existingReal(path); ok {
			args = append(args, "--ro-bind", "/dev/null", real)
		}
	}
	return append(args, bwrapTailArgs()...)
}

// pinnedAncestors returns the existing directories that must become mount
// points so the protected (and deny) paths cannot be swapped out from under
// their binds: every ancestor A of such a path (A != path) that lies strictly
// inside a writable root, since only there can A's parent be written and A
// be renamed or removed. This includes a writable root nested in another one
// (a workspace under /tmp). Paths are symlink-resolved; the result is sorted
// shallow first, so a parent bind never hides a child's.
func pinnedAncestors(writableRoots, paths []string) []string {
	var roots []string
	for _, r := range writableRoots {
		if real, ok := existingReal(r); ok {
			roots = append(roots, real)
		}
	}
	inside := func(dir string) bool {
		for _, r := range roots {
			if r == "/" || strings.HasPrefix(dir, r+"/") {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		real, ok := existingReal(path)
		if !ok {
			continue
		}
		for dir := filepath.Dir(real); dir != "/" && dir != "." && inside(dir); dir = filepath.Dir(dir) {
			if seen[dir] {
				continue
			}
			if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
				continue
			}
			seen[dir] = true
			out = append(out, dir)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if da, db := strings.Count(a, "/"), strings.Count(b, "/"); da != db {
			return da - db
		}
		return strings.Compare(a, b)
	})
	return out
}

// existingReal resolves symlinks in path and reports whether it exists. A
// bind must name the real location: bwrap would follow a symlinked
// destination anyway, and a dangling one cannot be bound.
func existingReal(path string) (string, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	return real, true
}

// bwrapUnavailable formats the error for UseBwrap=always without a usable
// bubblewrap.
func bwrapUnavailable(b bwrapProbe) error {
	return fmt.Errorf("%w: useBwrap is \"always\" but %s", ErrWrapFailed, b.reason)
}
