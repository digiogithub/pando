//go:build !windows

package cmd

import (
	"syscall"
	"testing"
	"time"
)

// exitRecorder stands in for os.Exit and reports the exit code it was called
// with on a channel.
func exitRecorder() (func(int), <-chan int) {
	codes := make(chan int, 1)
	return func(code int) { codes <- code }, codes
}

// A signal delivered while the command starts up must end the shutdown
// context instead of taking the default action (killing the process), and arm
// the watchdog.
func TestWatchShutdownSignalEndsContextAndArmsWatchdog(t *testing.T) {
	exit, codes := exitRecorder()
	ctx, _, release := watchShutdown("Test", 50*time.Millisecond, exit, syscall.SIGUSR1)
	defer release()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown context not done after the signal")
	}
	select {
	case code := <-codes:
		if code != 1 {
			t.Fatalf("watchdog exit code = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire after a signal")
	}
}

// request (a parent-gone shutdown, for example) behaves like a signal.
func TestWatchShutdownRequestArmsWatchdog(t *testing.T) {
	exit, codes := exitRecorder()
	ctx, request, release := watchShutdown("Test", 10*time.Millisecond, exit, syscall.SIGUSR1)
	defer release()

	request()
	<-ctx.Done()
	select {
	case <-codes:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire after a shutdown request")
	}
}

// A command that returns without any shutdown request (the desktop window was
// closed) must not be force-exited by the watchdog afterwards.
func TestWatchShutdownReleaseDisarmsWatchdog(t *testing.T) {
	exit, codes := exitRecorder()
	ctx, _, release := watchShutdown("Test", 10*time.Millisecond, exit, syscall.SIGUSR1)

	release()
	release() // idempotent
	<-ctx.Done()
	select {
	case code := <-codes:
		t.Fatalf("watchdog fired with code %d after a normal release", code)
	case <-time.After(200 * time.Millisecond):
	}
}
