package helper

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Landlock is driven with raw syscalls on the helper's single locked thread.
// go-landlock would restrict every OS thread through libcap/psx, which under
// cgo links C code (a constructor and a signal-based all-threads syscall
// mechanism) into the whole Pando binary; the helper does not need it,
// because Landlock domains, no_new_privs and seccomp filters are per thread
// and the restricted thread is the one that calls execve, whose new image
// keeps them while every other thread disappears.

// Landlock filesystem access rights by ABI.
const (
	llExecute    = unix.LANDLOCK_ACCESS_FS_EXECUTE
	llWriteFile  = unix.LANDLOCK_ACCESS_FS_WRITE_FILE
	llReadFile   = unix.LANDLOCK_ACCESS_FS_READ_FILE
	llReadDir    = unix.LANDLOCK_ACCESS_FS_READ_DIR
	llRemoveDir  = unix.LANDLOCK_ACCESS_FS_REMOVE_DIR
	llRemoveFile = unix.LANDLOCK_ACCESS_FS_REMOVE_FILE
	llMakeChar   = unix.LANDLOCK_ACCESS_FS_MAKE_CHAR
	llMakeDir    = unix.LANDLOCK_ACCESS_FS_MAKE_DIR
	llMakeReg    = unix.LANDLOCK_ACCESS_FS_MAKE_REG
	llMakeSock   = unix.LANDLOCK_ACCESS_FS_MAKE_SOCK
	llMakeFifo   = unix.LANDLOCK_ACCESS_FS_MAKE_FIFO
	llMakeBlock  = unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK
	llMakeSym    = unix.LANDLOCK_ACCESS_FS_MAKE_SYM
	llRefer      = unix.LANDLOCK_ACCESS_FS_REFER     // ABI 2
	llTruncate   = unix.LANDLOCK_ACCESS_FS_TRUNCATE  // ABI 3
	llIoctlDev   = unix.LANDLOCK_ACCESS_FS_IOCTL_DEV // ABI 5

	llFSv1 = llExecute | llWriteFile | llReadFile | llReadDir | llRemoveDir | llRemoveFile |
		llMakeChar | llMakeDir | llMakeReg | llMakeSock | llMakeFifo | llMakeBlock | llMakeSym

	// llRead is the read-only grant: read and execute.
	llRead = llExecute | llReadFile | llReadDir
	// llFileRights are the rights meaningful on a non-directory; adding a
	// directory-only right to a file rule is EINVAL.
	llFileRights = llExecute | llWriteFile | llReadFile | llTruncate | llIoctlDev

	llNetBindTCP    = unix.LANDLOCK_ACCESS_NET_BIND_TCP    // ABI 4
	llNetConnectTCP = unix.LANDLOCK_ACCESS_NET_CONNECT_TCP // ABI 4

	llScopeAbstractUnix = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET // ABI 6
)

// LandlockABI returns the kernel's Landlock ABI version (landlock_create_ruleset
// with LANDLOCK_CREATE_RULESET_VERSION). ENOSYS means the kernel lacks
// Landlock (before 5.13); EOPNOTSUPP means it is built in but disabled.
func LandlockABI() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, errno
	}
	return int(v), nil
}

// LandlockUnavailableReason renders a LandlockABI error for Capability.Reason.
func LandlockUnavailableReason(err error) string {
	switch {
	case errors.Is(err, unix.ENOSYS):
		return "kernel without Landlock (Linux 5.13 or later required)"
	case errors.Is(err, unix.EOPNOTSUPP):
		return "Landlock is disabled on this kernel (add landlock to the lsm= boot parameter)"
	}
	return fmt.Sprintf("Landlock unavailable: %v", err)
}

// handledFS returns every filesystem right the ABI knows, i.e. best effort:
// rights a kernel does not know are neither handled nor granted.
func handledFS(abi int) uint64 {
	h := uint64(llFSv1)
	if abi >= 2 {
		h |= llRefer
	}
	if abi >= 3 {
		h |= llTruncate
	}
	if abi >= 5 {
		h |= llIoctlDev
	}
	return h
}

// landlockRuleset is an open ruleset with its handled rights.
type landlockRuleset struct {
	fd      int
	handled uint64
}

// landlockRuleNetPort is LANDLOCK_RULE_NET_PORT (ABI 4).
const landlockRuleNetPort = 2

// landlockNetPortAttr is struct landlock_net_port_attr (packed, no padding).
type landlockNetPortAttr struct {
	allowedAccess uint64
	port          uint64
}

// applyLandlock builds the ruleset for spec and restricts the calling thread.
// The caller has locked the OS thread and set no_new_privs.
func applyLandlock(spec Spec, abi int) error {
	attr := unix.LandlockRulesetAttr{Access_fs: handledFS(abi)}
	guardPorts := false
	if spec.RestrictsNetwork() {
		// No net rules are added, so every TCP bind/connect is refused: a
		// second layer under the seccomp socket filter.
		if abi >= LandlockNetABI {
			attr.Access_net = llNetBindTCP | llNetConnectTCP
		}
		if abi >= 6 {
			attr.Scoped = llScopeAbstractUnix
		}
	} else if abi >= LandlockNetABI && len(spec.DenyConnectPorts) > 0 {
		// Guarded ports: TCP connect is handled, and every port except the
		// guarded ones is granted below. Bind stays unhandled.
		attr.Access_net = llNetConnectTCP
		guardPorts = true
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	rs := landlockRuleset{fd: int(fd), handled: attr.Access_fs}
	defer unix.Close(rs.fd)

	all := rs.handled
	for _, p := range spec.ReadDirs {
		if err := rs.add(p, llRead, false); err != nil {
			return err
		}
	}
	for _, p := range spec.WriteDirs {
		if err := rs.add(p, all, false); err != nil {
			return err
		}
	}
	for _, p := range spec.WriteFiles {
		if err := rs.add(p, llFileRights, false); err != nil {
			return err
		}
	}
	for _, p := range spec.Devices {
		if err := rs.add(p, all, true); err != nil {
			return err
		}
	}
	if guardPorts {
		if err := rs.allowConnectExcept(spec.DenyConnectPorts); err != nil {
			return err
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rs.fd), 0, 0); errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", errno)
	}
	return nil
}

// add grants access beneath path. Missing paths are skipped (policies list
// roots that may not exist). For devices, a node that cannot be opened (the
// ENXIO of /dev/tty without a controlling terminal, see Grok Build
// profiles.rs:180-194) is skipped too rather than aborting the ruleset.
func (rs landlockRuleset) add(path string, access uint64, device bool) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		if skippable(err, device) {
			return nil
		}
		return fmt.Errorf("landlock rule %s: %w", path, err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("landlock rule %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &= llFileRights
	}
	access &= rs.handled
	if access == 0 {
		return nil
	}
	if device && path == "/dev/tty" {
		// O_PATH never calls the driver's open; probe the controlling
		// terminal like a real consumer would, so a missing one (ENXIO) is
		// skipped. Other nodes are not opened (opening /dev/ptmx would
		// allocate a pseudo-terminal).
		if f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOCTTY|unix.O_NONBLOCK, 0); err != nil {
			if skippable(err, true) {
				return nil
			}
		} else {
			f.Close()
		}
	}
	attr := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rs.fd),
		unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("landlock_add_rule %s: %w", path, errno)
	}
	return nil
}

// allowConnectExcept grants TCP connect to every port except deny. Landlock
// has no port ranges and no deny rules, so this is one landlock_add_rule per
// port (65 536 syscalls, a few milliseconds; see BenchmarkAllowConnectExcept).
func (rs landlockRuleset) allowConnectExcept(deny []int) error {
	var denied [65536]bool
	for _, port := range deny {
		if port > 0 && port <= 65535 {
			denied[port] = true
		}
	}
	attr := landlockNetPortAttr{allowedAccess: llNetConnectTCP}
	for port := 0; port <= 65535; port++ {
		if denied[port] {
			continue
		}
		attr.port = uint64(port)
		if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rs.fd),
			landlockRuleNetPort, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); errno != 0 {
			return fmt.Errorf("landlock_add_rule tcp port %d: %w", port, errno)
		}
	}
	return nil
}

func skippable(err error, device bool) bool {
	switch {
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR), errors.Is(err, unix.ELOOP),
		errors.Is(err, unix.EACCES):
		return true
	case device && (errors.Is(err, unix.ENXIO) || errors.Is(err, unix.ENODEV) ||
		errors.Is(err, unix.EPERM) || errors.Is(err, unix.EIO)):
		return true
	}
	return false
}
