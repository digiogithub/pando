package helper

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// seccompData mirrors struct seccomp_data.
type seccompData struct {
	nr   uint32
	arch uint32
	args [6]uint64
}

// bytes lays the struct out little-endian, as the kernel presents it on
// amd64 and arm64.
func (d seccompData) bytes() []byte {
	b := make([]byte, 64)
	binary.LittleEndian.PutUint32(b[0:], d.nr)
	binary.LittleEndian.PutUint32(b[4:], d.arch)
	// ip (8..16) stays zero.
	for i, a := range d.args {
		binary.LittleEndian.PutUint64(b[16+8*i:], a)
	}
	return b
}

// evalBPF is a minimal classic-BPF interpreter covering the opcodes
// BuildFilter emits (ld [k], jeq #k, jset #k, ret #k). It fails on anything
// else, so a builder change that needs more opcodes shows up here.
func evalBPF(prog []SockFilter, d seccompData) (uint32, error) {
	data := d.bytes()
	var acc uint32
	for pc, steps := 0, 0; pc < len(prog); pc++ {
		if steps++; steps > len(prog) {
			return 0, fmt.Errorf("loop at %d", pc)
		}
		ins := prog[pc]
		switch ins.Code {
		case opLoadAbsW:
			if ins.K%4 != 0 || int(ins.K)+4 > len(data) {
				return 0, fmt.Errorf("pc %d: bad load offset %d", pc, ins.K)
			}
			acc = binary.LittleEndian.Uint32(data[ins.K:])
		case opJeqK:
			if acc == ins.K {
				pc += int(ins.Jt)
			} else {
				pc += int(ins.Jf)
			}
		case opJsetK:
			if acc&ins.K != 0 {
				pc += int(ins.Jt)
			} else {
				pc += int(ins.Jf)
			}
		case opRetK:
			return ins.K, nil
		default:
			return 0, fmt.Errorf("pc %d: unsupported opcode %#x", pc, ins.Code)
		}
	}
	return 0, fmt.Errorf("fell off the end of the program")
}

const (
	retAllow  = SeccompRetAllow
	retEPERM  = SeccompRetErrno | errnoEPERM
	retENOSYS = SeccompRetErrno | errnoENOSYS

	auditArchI386 = 0x40000003
	sigchld       = 17
	afInet        = 2
	afInet6       = 10
)

// syscall numbers not in Arch (harmless calls used as "allowed" probes).
var readNr = map[string]uint32{"amd64": 0, "arm64": 63}

type filterCase struct {
	name string
	d    seccompData
	want uint32
}

func check(t *testing.T, prog []SockFilter, cases []filterCase) {
	t.Helper()
	for _, c := range cases {
		got, err := evalBPF(prog, c.d)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %#x, want %#x", c.name, got, c.want)
		}
	}
}

func call(a Arch, nr uint32, args ...uint64) seccompData {
	d := seccompData{nr: nr, arch: a.AuditArch}
	copy(d.args[:], args)
	return d
}

func TestFilterNamespaceLockdown(t *testing.T) {
	for _, a := range []Arch{ArchAMD64, ArchARM64} {
		t.Run(a.Name, func(t *testing.T) {
			prog, err := BuildFilter(a, FilterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cases := []filterCase{
				{"read", call(a, readNr[a.Name]), retAllow},
				{"clone plain fork", call(a, a.Clone, sigchld), retAllow},
				{"clone thread", call(a, a.Clone, 0x3d0f00), retAllow}, // CLONE_VM|FS|FILES|SIGHAND|THREAD|SYSVSEM|SETTLS|PARENT_SETTID|CHILD_CLEARTID
				{"clone3", call(a, sysClone3), retENOSYS},
				{"unshare", call(a, a.Unshare, cloneNewUser), retEPERM},
				{"setns", call(a, a.Setns), retEPERM},
				{"ioctl TIOCSTI", call(a, a.Ioctl, 0, ioctlTIOCSTI), retEPERM},
				{"ioctl TIOCLINUX", call(a, a.Ioctl, 0, ioctlTIOCLINUX), retEPERM},
				{"ioctl TCGETS", call(a, a.Ioctl, 0, 0x5401), retAllow},
				{"connect (network allowed)", call(a, a.Connect), retAllow},
				{"socket AF_INET (network allowed)", call(a, a.Socket, afInet), retAllow},
				{"io_uring_setup (network allowed)", call(a, sysIoUringSetup), retAllow},
				{"wrong arch", seccompData{nr: readNr[a.Name], arch: auditArchI386}, retEPERM},
			}
			for _, bit := range []uint64{cloneNewTime, cloneNewNS, cloneNewCgroup, cloneNewUTS,
				cloneNewIPC, cloneNewUser, cloneNewPID, cloneNewNet} {
				cases = append(cases, filterCase{fmt.Sprintf("clone %#x", bit), call(a, a.Clone, bit|sigchld), retEPERM})
			}
			for _, nr := range a.MountSyscalls() {
				cases = append(cases, filterCase{fmt.Sprintf("mount-family %d", nr), call(a, nr), retEPERM})
			}
			// High bits of the flags word must not hide a namespace bit.
			cases = append(cases, filterCase{"clone high bits + NEWUSER", call(a, a.Clone, 1<<40|cloneNewUser), retEPERM})
			if a.X32 {
				cases = append(cases,
					filterCase{"x32 read", call(a, x32SyscallBit|readNr[a.Name]), retEPERM},
					filterCase{"x32 connect", call(a, x32SyscallBit|a.Connect), retEPERM})
			}
			check(t, prog, cases)
		})
	}
}

func TestFilterNetworkStrict(t *testing.T) {
	for _, a := range []Arch{ArchAMD64, ArchARM64} {
		t.Run(a.Name, func(t *testing.T) {
			prog, err := BuildFilter(a, FilterOptions{RestrictNetwork: true})
			if err != nil {
				t.Fatal(err)
			}
			cases := []filterCase{
				{"read", call(a, readNr[a.Name]), retAllow},
				{"socket AF_UNIX", call(a, a.Socket, afUnix), retAllow},
				{"socket AF_INET", call(a, a.Socket, afInet), retEPERM},
				{"socket AF_INET6", call(a, a.Socket, afInet6), retEPERM},
				{"sendto connected", call(a, a.Sendto, 3, 0, 0, 0, 0, 0), retAllow},
				{"sendto addressed", call(a, a.Sendto, 3, 0, 0, 0, 0x7ffc0000, 16), retEPERM},
				{"sendto addressed high half", call(a, a.Sendto, 3, 0, 0, 0, 0x7ffc_00000000, 16), retEPERM},
				{"clone3 still ENOSYS", call(a, sysClone3), retENOSYS},
				{"clone NEWNET", call(a, a.Clone, cloneNewNet), retEPERM},
				{"wrong arch", seccompData{nr: a.Connect, arch: auditArchI386}, retEPERM},
			}
			for _, nr := range append(a.NetworkSyscalls(), IoUringSyscalls()...) {
				cases = append(cases, filterCase{fmt.Sprintf("network %d", nr), call(a, nr), retEPERM})
			}
			check(t, prog, cases)
		})
	}
}

func TestFilterNetworkAllowUnix(t *testing.T) {
	for _, a := range []Arch{ArchAMD64, ArchARM64} {
		t.Run(a.Name, func(t *testing.T) {
			prog, err := BuildFilter(a, FilterOptions{RestrictNetwork: true, AllowUnixSockets: true})
			if err != nil {
				t.Fatal(err)
			}
			cases := []filterCase{
				{"socket AF_UNIX", call(a, a.Socket, afUnix), retAllow},
				{"socket AF_INET", call(a, a.Socket, afInet), retEPERM},
				{"socket AF_INET6", call(a, a.Socket, afInet6), retEPERM},
				{"socket AF_NETLINK", call(a, a.Socket, 16), retEPERM},
				{"connect", call(a, a.Connect), retAllow},
				{"bind", call(a, a.Bind), retAllow},
				{"listen", call(a, a.Listen), retAllow},
				{"sendmsg", call(a, a.Sendmsg), retAllow},
				{"sendto addressed", call(a, a.Sendto, 3, 0, 0, 0, 0x7ffc0000, 16), retAllow},
				{"unshare", call(a, a.Unshare), retEPERM},
			}
			for _, nr := range IoUringSyscalls() {
				cases = append(cases, filterCase{fmt.Sprintf("io_uring %d", nr), call(a, nr), retEPERM})
			}
			if a.X32 {
				cases = append(cases, filterCase{"x32 socket", call(a, x32SyscallBit|a.Socket, afUnix), retEPERM})
			}
			check(t, prog, cases)
		})
	}
}

func TestArchFor(t *testing.T) {
	if _, err := ArchFor("riscv64"); err == nil {
		t.Fatal("ArchFor(riscv64) should fail")
	}
	for _, name := range []string{"amd64", "arm64"} {
		a, err := ArchFor(name)
		if err != nil || a.Name != name {
			t.Fatalf("ArchFor(%s) = %v, %v", name, a.Name, err)
		}
	}
}
