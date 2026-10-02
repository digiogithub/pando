package cmd

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestResolveStartupContextPublicBasePath(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PROJECT_ID", "project-1")
	t.Setenv("PANDO_PUBLIC_BASE", "/api/v1/projects/project-1/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.Mode != "project-child" {
		t.Fatalf("mode = %q, want project-child", ctx.Mode)
	}
	if ctx.PublicBasePath != "/api/v1/projects/project-1/web" {
		t.Fatalf("public base path = %q, want %q", ctx.PublicBasePath, "/api/v1/projects/project-1/web")
	}
}

func TestResolveStartupContextIgnoresInvalidPublicBasePath(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PROJECT_ID", "project-1")
	t.Setenv("PANDO_PUBLIC_BASE", "/not/a/project/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.PublicBasePath != "" {
		t.Fatalf("public base path = %q, want empty", ctx.PublicBasePath)
	}
}

func TestResolveStartupContextIgnoresPublicBasePathOutsideChildMode(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "")
	t.Setenv("PANDO_PROJECT_ID", "")
	t.Setenv("PANDO_PUBLIC_BASE", "/api/v1/projects/project-1/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.Mode != "serve" {
		t.Fatalf("mode = %q, want serve", ctx.Mode)
	}
	if ctx.PublicBasePath != "" {
		t.Fatalf("public base path = %q, want empty", ctx.PublicBasePath)
	}
}

func TestResolveStartupContextConsumesChildToken(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PROJECT_ID", "project-1")
	t.Setenv("PANDO_CHILD_API_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("PANDO_PARENT_PID", "4242")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.APIToken != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("api token = %q", ctx.APIToken)
	}
	if ctx.ParentPID != 4242 {
		t.Fatalf("parent pid = %d, want 4242", ctx.ParentPID)
	}
	if _, ok := os.LookupEnv("PANDO_CHILD_API_TOKEN"); ok {
		t.Fatal("PANDO_CHILD_API_TOKEN must be removed from the process environment")
	}
}

func TestResolveStartupContextPublicBasePathRejectsOddIDs(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PUBLIC_BASE", `/api/v1/projects/a"><script>/web`)

	if ctx := resolveStartupContext(t.TempDir(), "serve"); ctx.PublicBasePath != "" {
		t.Fatalf("public base path = %q, want empty", ctx.PublicBasePath)
	}
}

func TestWatchParentProcessFiresWhenGone(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	pid := cmd.Process.Pid
	gone := make(chan struct{})
	go watchParentProcess(context.Background(), pid, 20*time.Millisecond, func() { close(gone) })

	select {
	case <-gone:
		t.Fatal("watchdog fired while the parent was alive")
	case <-time.After(100 * time.Millisecond):
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire after the parent exited")
	}
}
