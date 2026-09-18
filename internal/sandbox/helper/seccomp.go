package helper

import (
	"fmt"
	"runtime"
)

// The seccomp programs are classic BPF, hand-assembled here (no cgo, no
// libseccomp). They follow Grok Build's child_net.rs: an arch/x32 gate, the
// namespace-lockdown set, and, for a restricted network, the network set.
// The builder is pure and OS independent so tests can evaluate the programs
// for every supported architecture on any host.

// SockFilter is one classic BPF instruction (struct sock_filter).
type SockFilter struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

// BPF opcodes (linux/filter.h, linux/bpf_common.h).
const (
	bpfLD   = 0x00
	bpfJMP  = 0x05
	bpfRET  = 0x06
	bpfW    = 0x00
	bpfABS  = 0x20
	bpfJEQ  = 0x10
	bpfJSET = 0x40
	bpfK    = 0x00

	opLoadAbsW = bpfLD | bpfW | bpfABS
	opJeqK     = bpfJMP | bpfJEQ | bpfK
	opJsetK    = bpfJMP | bpfJSET | bpfK
	opRetK     = bpfRET | bpfK
)

// seccomp return values (linux/seccomp.h).
const (
	SeccompRetAllow uint32 = 0x7fff0000
	SeccompRetErrno uint32 = 0x00050000

	errnoEPERM  = 1
	errnoENOSYS = 38
)

// struct seccomp_data offsets (little-endian: the low word of an argument is
// at the lower address).
const (
	offNr   = 0
	offArch = 4
	offArgs = 16
)

func argLo(i int) uint32 { return uint32(offArgs + 8*i) }
func argHi(i int) uint32 { return uint32(offArgs + 8*i + 4) }

// Flag and constant values shared by amd64 and arm64.
const (
	x32SyscallBit = 0x40000000

	cloneNewTime   = 0x00000080
	cloneNewNS     = 0x00020000
	cloneNewCgroup = 0x02000000
	cloneNewUTS    = 0x04000000
	cloneNewIPC    = 0x08000000
	cloneNewUser   = 0x10000000
	cloneNewPID    = 0x20000000
	cloneNewNet    = 0x40000000
	// CloneNamespaceBits are the clone(2) flags that create a namespace.
	CloneNamespaceBits = cloneNewTime | cloneNewNS | cloneNewCgroup | cloneNewUTS |
		cloneNewIPC | cloneNewUser | cloneNewPID | cloneNewNet

	afUnix = 1

	// Terminal ioctls that inject input into (or control) the terminal the
	// sandboxed child shares with Pando.
	ioctlTIOCSTI   = 0x5412
	ioctlTIOCLINUX = 0x541c
)

// Arch is the per-architecture syscall table the filters need.
type Arch struct {
	Name string
	// AuditArch is the AUDIT_ARCH_* value in seccomp_data.arch.
	AuditArch uint32
	// X32 marks x86_64, whose x32 ABI shares the arch value but sets bit 30
	// of the syscall number; x32 calls are refused outright.
	X32 bool

	Mount, Umount2, PivotRoot, Unshare, Setns, Clone, Ioctl uint32
	Socket, Connect, Bind, Sendto, Sendmsg, Sendmmsg        uint32
	Listen, Accept, Accept4                                 uint32
}

// Syscalls numbered identically on every architecture (the generic table,
// 424+).
const (
	sysIoUringSetup    = 425
	sysIoUringEnter    = 426
	sysIoUringRegister = 427
	sysOpenTree        = 428
	sysMoveMount       = 429
	sysFsopen          = 430
	sysFsconfig        = 431
	sysFsmount         = 432
	sysFspick          = 433
	sysClone3          = 435
	sysMountSetattr    = 442
	sysOpenTreeAttr    = 467
)

// ArchAMD64 and ArchARM64 are the supported architectures.
var (
	ArchAMD64 = Arch{
		Name: "amd64", AuditArch: 0xc000003e, X32: true,
		Mount: 165, Umount2: 166, PivotRoot: 155, Unshare: 272, Setns: 308, Clone: 56, Ioctl: 16,
		Socket: 41, Connect: 42, Bind: 49, Sendto: 44, Sendmsg: 46, Sendmmsg: 307,
		Listen: 50, Accept: 43, Accept4: 288,
	}
	ArchARM64 = Arch{
		Name: "arm64", AuditArch: 0xc00000b7,
		Mount: 40, Umount2: 39, PivotRoot: 41, Unshare: 97, Setns: 268, Clone: 220, Ioctl: 29,
		Socket: 198, Connect: 203, Bind: 200, Sendto: 206, Sendmsg: 211, Sendmmsg: 269,
		Listen: 201, Accept: 202, Accept4: 242,
	}
)

// NativeArch returns the table for the running architecture.
func NativeArch() (Arch, error) {
	return ArchFor(runtime.GOARCH)
}

// ArchFor returns the table for a GOARCH.
func ArchFor(goarch string) (Arch, error) {
	switch goarch {
	case "amd64":
		return ArchAMD64, nil
	case "arm64":
		return ArchARM64, nil
	}
	return Arch{}, fmt.Errorf("seccomp filter not implemented for linux/%s", goarch)
}

// MountSyscalls are the mount-API calls the namespace lockdown refuses.
func (a Arch) MountSyscalls() []uint32 {
	return []uint32{a.Mount, a.Umount2, a.PivotRoot, sysOpenTree, sysOpenTreeAttr, sysMoveMount,
		sysFsopen, sysFsconfig, sysFsmount, sysFspick, sysMountSetattr}
}

// NetworkSyscalls are refused under a restricted network when AF_UNIX is not
// kept (Grok Build child_net.rs:178-199); sendto only with a destination
// address, so send(2) on an already connected socketpair keeps working.
func (a Arch) NetworkSyscalls() []uint32 {
	return []uint32{a.Connect, a.Bind, a.Sendmsg, a.Sendmmsg, a.Listen, a.Accept, a.Accept4}
}

// IoUringSyscalls can create and connect sockets without entering the
// syscalls above, so they are refused under a restricted network.
func IoUringSyscalls() []uint32 {
	return []uint32{sysIoUringSetup, sysIoUringEnter, sysIoUringRegister}
}

// FilterOptions selects the parts of the program.
type FilterOptions struct {
	// RestrictNetwork adds the network set.
	RestrictNetwork bool
	// AllowUnixSockets (with RestrictNetwork) refuses only socket(2) of non
	// AF_UNIX families and io_uring, keeping local IPC usable.
	AllowUnixSockets bool
}

// asm is a tiny forward-jump assembler with labels.
type asm struct {
	ins    []SockFilter
	jt, jf []string // symbolic targets; "" means fall through
	labels map[string]int
}

func (a *asm) stmt(code uint16, k uint32) {
	a.ins = append(a.ins, SockFilter{Code: code, K: k})
	a.jt = append(a.jt, "")
	a.jf = append(a.jf, "")
}

func (a *asm) jump(code uint16, k uint32, jt, jf string) {
	a.ins = append(a.ins, SockFilter{Code: code, K: k})
	a.jt = append(a.jt, jt)
	a.jf = append(a.jf, jf)
}

func (a *asm) label(name string) {
	if a.labels == nil {
		a.labels = map[string]int{}
	}
	a.labels[name] = len(a.ins)
}

func (a *asm) assemble() ([]SockFilter, error) {
	out := make([]SockFilter, len(a.ins))
	copy(out, a.ins)
	resolve := func(i int, name string) (uint8, error) {
		if name == "" {
			return 0, nil
		}
		target, ok := a.labels[name]
		if !ok {
			return 0, fmt.Errorf("seccomp: undefined label %q", name)
		}
		off := target - i - 1
		if off < 0 || off > 255 {
			return 0, fmt.Errorf("seccomp: jump to %q out of range (%d)", name, off)
		}
		return uint8(off), nil
	}
	for i := range out {
		var err error
		if out[i].Jt, err = resolve(i, a.jt[i]); err != nil {
			return nil, err
		}
		if out[i].Jf, err = resolve(i, a.jf[i]); err != nil {
			return nil, err
		}
	}
	if len(out) > 4096 {
		return nil, fmt.Errorf("seccomp: program too long (%d)", len(out))
	}
	return out, nil
}

// BuildFilter assembles the helper's seccomp program for arch:
//
//   - wrong architecture or x32 syscall number: EPERM;
//   - mount API (mount, umount2, pivot_root, open_tree[_attr], move_mount,
//     fsopen, fsconfig, fsmount, fspick, mount_setattr), unshare, setns:
//     EPERM, so a child cannot build a user namespace and remount its way
//     out of bubblewrap's read-only binds;
//   - clone with any CLONE_NEW* flag: EPERM; clone3: ENOSYS (its flags live
//     in memory classic BPF cannot read; libc falls back to clone);
//   - ioctl TIOCSTI / TIOCLINUX: EPERM (terminal input injection);
//   - with RestrictNetwork: io_uring_*: EPERM; socket(2) of a non-AF_UNIX
//     family: EPERM; and unless AllowUnixSockets, connect, bind, sendmsg,
//     sendmmsg, listen, accept, accept4 and sendto with an address: EPERM.
//
// Everything else is allowed.
func BuildFilter(arch Arch, opts FilterOptions) ([]SockFilter, error) {
	a := &asm{}
	a.stmt(opLoadAbsW, offArch)
	a.jump(opJeqK, arch.AuditArch, "", "eperm")
	a.stmt(opLoadAbsW, offNr)
	if arch.X32 {
		a.jump(opJsetK, x32SyscallBit, "eperm", "")
	}
	denied := append(arch.MountSyscalls(), arch.Unshare, arch.Setns)
	if opts.RestrictNetwork {
		denied = append(denied, IoUringSyscalls()...)
		if !opts.AllowUnixSockets {
			denied = append(denied, arch.NetworkSyscalls()...)
		}
	}
	for _, nr := range denied {
		a.jump(opJeqK, nr, "eperm", "")
	}
	a.jump(opJeqK, sysClone3, "enosys", "")
	a.jump(opJeqK, arch.Clone, "clone", "")
	a.jump(opJeqK, arch.Ioctl, "ioctl", "")
	if opts.RestrictNetwork {
		a.jump(opJeqK, arch.Socket, "socket", "")
		if !opts.AllowUnixSockets {
			a.jump(opJeqK, arch.Sendto, "sendto", "")
		}
	}
	a.stmt(opRetK, SeccompRetAllow)

	a.label("clone")
	a.stmt(opLoadAbsW, argLo(0))
	a.jump(opJsetK, CloneNamespaceBits, "eperm", "allow")

	a.label("ioctl")
	a.stmt(opLoadAbsW, argLo(1))
	a.jump(opJeqK, ioctlTIOCSTI, "eperm", "")
	a.jump(opJeqK, ioctlTIOCLINUX, "eperm", "allow")

	if opts.RestrictNetwork {
		a.label("socket")
		a.stmt(opLoadAbsW, argLo(0))
		a.jump(opJeqK, afUnix, "allow", "eperm")
		if !opts.AllowUnixSockets {
			// sendto(fd, buf, len, flags, dest_addr, addrlen): refuse a
			// non-NULL dest_addr (both halves of the pointer).
			a.label("sendto")
			a.stmt(opLoadAbsW, argLo(4))
			a.jump(opJeqK, 0, "", "eperm")
			a.stmt(opLoadAbsW, argHi(4))
			a.jump(opJeqK, 0, "allow", "eperm")
		}
	}

	a.label("allow")
	a.stmt(opRetK, SeccompRetAllow)
	a.label("eperm")
	a.stmt(opRetK, SeccompRetErrno|errnoEPERM)
	a.label("enosys")
	a.stmt(opRetK, SeccompRetErrno|errnoENOSYS)
	return a.assemble()
}
