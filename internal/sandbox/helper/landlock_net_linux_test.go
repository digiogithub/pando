package helper

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// newNetRuleset opens a ruleset handling TCP connect, or skips.
func newNetRuleset(tb testing.TB) landlockRuleset {
	tb.Helper()
	abi, err := LandlockABI()
	if err != nil || abi < LandlockNetABI {
		tb.Skipf("Landlock ABI %d (err %v): network rules need ABI %d", abi, err, LandlockNetABI)
	}
	attr := unix.LandlockRulesetAttr{Access_fs: handledFS(abi), Access_net: llNetConnectTCP}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		tb.Fatalf("landlock_create_ruleset: %v", errno)
	}
	return landlockRuleset{fd: int(fd), handled: attr.Access_fs}
}

// TestAllowConnectExceptCost builds the full port ruleset (without
// restricting the test process) and keeps its cost bounded: it runs on every
// sandboxed spawn.
func TestAllowConnectExceptCost(t *testing.T) {
	rs := newNetRuleset(t)
	defer unix.Close(rs.fd)
	start := time.Now()
	if err := rs.allowConnectExcept([]int{8765, 20001}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	t.Logf("65534 landlock net rules added in %v", elapsed)
	if elapsed > 2*time.Second {
		t.Fatalf("adding the port rules took %v", elapsed)
	}
}

func BenchmarkAllowConnectExcept(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rs := newNetRuleset(b)
		if err := rs.allowConnectExcept([]int{8765}); err != nil {
			b.Fatal(err)
		}
		unix.Close(rs.fd)
	}
}
