package sandbox

import (
	"os/exec"
	"testing"
	"time"
)

// TestAttachProcessTree starts a short-lived child, attaches it to a Job
// Object and releases it once it has exited. It only builds and runs on
// windows.
func TestAttachProcessTree(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	release, err := AttachProcessTree(cmd.Process)
	if err != nil {
		t.Fatalf("AttachProcessTree: %v", err)
	}
	if release == nil {
		t.Fatal("AttachProcessTree returned a nil release")
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child wait: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not exit in time")
	}

	// release must be idempotent.
	release()
	release()
}

// TestAttachProcessTree_NilProcess documents the guard against a nil
// *os.Process (e.g. a caller that forgot to check cmd.Start's error).
func TestAttachProcessTree_NilProcess(t *testing.T) {
	release, err := AttachProcessTree(nil)
	if err == nil {
		t.Fatal("expected an error for a nil process")
	}
	if release == nil {
		t.Fatal("release must still be non-nil on error, so callers can defer it unconditionally")
	}
	release()
}

func TestJobObjectsSupported(t *testing.T) {
	if !jobObjectsSupported() {
		t.Fatal("jobObjectsSupported() = false on a real Windows machine")
	}
}
