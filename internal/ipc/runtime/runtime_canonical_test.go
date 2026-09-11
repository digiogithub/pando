package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/ipc"
)

// TestCanonicalWorkdirMapsSpellingsToSamePortsAndLock guards G6: relative,
// absolute and symlinked spellings of one directory must derive the same ports
// and contend on the same lock.
func TestCanonicalWorkdirMapsSpellingsToSamePortsAndLock(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "project")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "project-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	t.Chdir(base)

	want := canonicalWorkdir(real)
	wantPub, wantRPC := ipc.PortsForPath(want)
	for _, spelling := range []string{real, "project", "./project/", "project/../project", link, link + "/."} {
		got := canonicalWorkdir(spelling)
		if got != want {
			t.Errorf("canonicalWorkdir(%q) = %q, want %q", spelling, got, want)
			continue
		}
		if pub, rpc := ipc.PortsForPath(got); pub != wantPub || rpc != wantRPC {
			t.Errorf("ports for %q = %d/%d, want %d/%d", spelling, pub, rpc, wantPub, wantRPC)
		}
	}

	// Same lock: taking it through the symlink blocks the relative spelling.
	isPrimary, _, f, err := ipc.AcquireLock(canonicalWorkdir(link), "via-link", wantPub, wantRPC)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock via link: primary=%v err=%v", isPrimary, err)
	}
	defer ipc.ReleaseLock(f)
	isPrimary, info, _, err := ipc.AcquireLock(canonicalWorkdir("project"), "via-relative", wantPub, wantRPC)
	if err != nil {
		t.Fatalf("AcquireLock via relative path: %v", err)
	}
	if isPrimary || info == nil || info.InstanceID != "via-link" {
		t.Fatalf("relative spelling got primary=%v info=%+v, want the lock held by via-link", isPrimary, info)
	}
}

// TestCanonicalWorkdirFallsBack keeps a usable path when symlink resolution
// fails (missing directory).
func TestCanonicalWorkdirFallsBack(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if got := canonicalWorkdir(missing); got != missing {
		t.Fatalf("canonicalWorkdir(%q) = %q, want the absolute path unchanged", missing, got)
	}
}
