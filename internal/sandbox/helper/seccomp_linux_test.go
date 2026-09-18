package helper

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// TestNativeArchTable cross-checks the hard-coded table of the running
// architecture against golang.org/x/sys/unix.
func TestNativeArchTable(t *testing.T) {
	a, err := NativeArch()
	if err != nil {
		t.Skipf("unsupported arch %s: %v", runtime.GOARCH, err)
	}
	want := map[string][2]uint32{
		"mount":             {a.Mount, unix.SYS_MOUNT},
		"umount2":           {a.Umount2, unix.SYS_UMOUNT2},
		"pivot":             {a.PivotRoot, unix.SYS_PIVOT_ROOT},
		"unshare":           {a.Unshare, unix.SYS_UNSHARE},
		"setns":             {a.Setns, unix.SYS_SETNS},
		"clone":             {a.Clone, unix.SYS_CLONE},
		"ioctl":             {a.Ioctl, unix.SYS_IOCTL},
		"socket":            {a.Socket, unix.SYS_SOCKET},
		"connect":           {a.Connect, unix.SYS_CONNECT},
		"bind":              {a.Bind, unix.SYS_BIND},
		"sendto":            {a.Sendto, unix.SYS_SENDTO},
		"sendmsg":           {a.Sendmsg, unix.SYS_SENDMSG},
		"sendmmsg":          {a.Sendmmsg, unix.SYS_SENDMMSG},
		"listen":            {a.Listen, unix.SYS_LISTEN},
		"accept":            {a.Accept, unix.SYS_ACCEPT},
		"accept4":           {a.Accept4, unix.SYS_ACCEPT4},
		"clone3":            {sysClone3, unix.SYS_CLONE3},
		"open_tree":         {sysOpenTree, unix.SYS_OPEN_TREE},
		"move_mount":        {sysMoveMount, unix.SYS_MOVE_MOUNT},
		"fsopen":            {sysFsopen, unix.SYS_FSOPEN},
		"fsconfig":          {sysFsconfig, unix.SYS_FSCONFIG},
		"fsmount":           {sysFsmount, unix.SYS_FSMOUNT},
		"fspick":            {sysFspick, unix.SYS_FSPICK},
		"mount_setattr":     {sysMountSetattr, unix.SYS_MOUNT_SETATTR},
		"open_tree_attr":    {sysOpenTreeAttr, unix.SYS_OPEN_TREE_ATTR},
		"io_uring_setup":    {sysIoUringSetup, unix.SYS_IO_URING_SETUP},
		"io_uring_enter":    {sysIoUringEnter, unix.SYS_IO_URING_ENTER},
		"io_uring_register": {sysIoUringRegister, unix.SYS_IO_URING_REGISTER},
	}
	for name, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s: table %d, x/sys/unix %d", name, v[0], v[1])
		}
	}
	if runtime.GOARCH == "amd64" && a.AuditArch != unix.AUDIT_ARCH_X86_64 {
		t.Errorf("audit arch %#x", a.AuditArch)
	}
	if runtime.GOARCH == "arm64" && a.AuditArch != unix.AUDIT_ARCH_AARCH64 {
		t.Errorf("audit arch %#x", a.AuditArch)
	}
	if uint32(unix.TIOCSTI) != ioctlTIOCSTI || uint32(unix.TIOCLINUX) != ioctlTIOCLINUX {
		t.Errorf("ioctl numbers differ from x/sys/unix")
	}
	if uint32(unix.CLONE_NEWUSER|unix.CLONE_NEWNS|unix.CLONE_NEWPID|unix.CLONE_NEWNET|unix.CLONE_NEWUTS|
		unix.CLONE_NEWIPC|unix.CLONE_NEWCGROUP|unix.CLONE_NEWTIME) != CloneNamespaceBits {
		t.Errorf("CloneNamespaceBits differ from x/sys/unix")
	}
}
