package tlsutil

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// lockHolderEnv makes the test binary act as a process that takes the TLS
// directory lock and then blocks until it is killed.
const lockHolderEnv = "PANDO_TLSUTIL_LOCK_HOLDER_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(lockHolderEnv); dir != "" {
		if _, err := lockTLSDir(dir); err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString("locked\n")
		select {}
	}
	os.Exit(m.Run())
}

func TestLockTLSDirExcludesOtherHolders(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockTLSDir(dir)
	if err != nil {
		t.Fatalf("lockTLSDir: %v", err)
	}

	other, err := os.OpenFile(filepath.Join(dir, lockFileName), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := tryLockFile(other); !errors.Is(err, errLockBusy) {
		t.Fatalf("second lock while held: got %v, want errLockBusy", err)
	}

	unlock()
	if err := tryLockFile(other); err != nil {
		t.Fatalf("second lock after release: %v", err)
	}
}

// A process killed while it holds the lock must not block later starts: the
// OS releases the lock with the process, unlike the former lock directory
// that stayed behind until it was a minute old.
func TestLockTLSDirReleasedWhenHolderIsKilled(t *testing.T) {
	dir := t.TempDir()
	tlsDir := filepath.Join(dir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	holder := exec.Command(os.Args[0], "-test.run=^$")
	holder.Env = append(os.Environ(), lockHolderEnv+"="+tlsDir)
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("lock holder did not report the lock: %q, %v", line, err)
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()

	start := time.Now()
	if _, err := EnsureCert(dir); err != nil {
		t.Fatalf("EnsureCert after the lock holder was killed: %v", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("EnsureCert waited %s for a lock whose holder is dead", waited)
	}
}
