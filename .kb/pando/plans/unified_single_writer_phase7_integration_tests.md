# Phase 7 — Integration Tests (Multi-Process)

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** medium
**Effort:** medium

## 1. Goal

Write Go integration tests that spawn multiple Pando processes in the same temp working directory and verify the single-writer architecture end-to-end: only one process opens SQLite in RW mode, secondary reads are local, and all writes reach the primary through IPC.

## 2. Why

### 2.1 Current test gap

Current tests (`go test ./internal/ipc/...`) test individual components in isolation. There are no tests that:

- Start two real Pando processes in the same path
- Verify lock acquisition (one primary, one secondary)
- Verify writes from the secondary reach the primary
- Verify reads from the secondary are served locally
- Verify failover behaviour (Phase 5)
- Verify cross-entrypoint behaviour (TUI + serve + desktop)

### 2.2 Regression protection

The single-writer invariant is subtle. Integration tests prevent regressions when future changes touch the lock, bootstrap, or proxy layers.

### 2.3 Confidence for refactoring

Phases 1-5 involve significant refactoring of bootstrap code. Multi-process tests give confidence that nothing is broken.

## 3. Test Strategy

### 3.1 Test location

```
test/integration/single_writer/
```

Use `go test` with build tag `integration`:

```go
//go:build integration
```

Run with:

```bash
go test -tags=integration ./test/integration/single_writer/... -v -timeout 120s
```

### 3.2 Test infrastructure

A helper that creates a temp directory and launches Pando subprocesses:

```go
// test/integration/single_writer/helpers_test.go

package single_writer_test

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "syscall"
    "testing"
    "time"
)

// pandoBinary is the path to the compiled pando binary.
var pandoBinary = os.Getenv("PANDO_BINARY")

// testEnv creates a temp workdir and optional config for an integration test.
type testEnv struct {
    Workdir string
    cleanup func()
}

func newTestEnv(t *testing.T) *testEnv {
    t.Helper()
    dir := t.TempDir()
    return &testEnv{
        Workdir: dir,
        cleanup:  func() { os.RemoveAll(dir) },
    }
}

// startInstance launches a Pando subprocess in the given workdir.
// Returns a handle to the process and a channel that receives its exit code.
type instance struct {
    Cmd    *exec.Cmd
    Done   chan error
    PID    int
    Workdir string
}

func startPrimary(t *testing.T, workdir string, args ...string) *instance {
    return startInstance(t, workdir, args...)
}

func startSecondary(t *testing.T, workdir string, args ...string) *instance {
    return startInstance(t, workdir, args...)
}

func startInstance(t *testing.T, workdir string, args ...string) *instance {
    t.Helper()
    binary := pandoBinary
    if binary == "" {
        binary = "../../pando" // fallback for local runs
    }

    cmd := exec.Command(binary, args...)
    cmd.Dir = workdir
    cmd.Env = append(os.Environ(), "PANDO_DATA_DIR="+filepath.Join(workdir, ".pando"))
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Start(); err != nil {
        t.Fatalf("failed to start pando: %v", err)
    }

    done := make(chan error, 1)
    go func() { done <- cmd.Wait() }()

    return &instance{
        Cmd:     cmd,
        Done:    done,
        PID:     cmd.Process.Pid,
        Workdir: workdir,
    }
}

func (inst *instance) stop(t *testing.T) {
    t.Helper()
    _ = inst.Cmd.Process.Signal(syscall.SIGTERM)
    select {
    case <-inst.Done:
    case <-time.After(5 * time.Second):
        _ = inst.Cmd.Process.Kill()
        <-inst.Done
    }
}

func (inst *instance) waitForExit(t *testing.T, timeout time.Duration) error {
    t.Helper()
    select {
    case err := <-inst.Done:
        return err
    case <-time.After(timeout):
        t.Fatal("timeout waiting for instance to exit")
        return nil
    }
}
```

### 3.3 Test Cases

#### Test 1: First instance becomes primary

```go
func TestFirstInstanceIsPrimary(t *testing.T) {
    env := newTestEnv(t)
    defer env.cleanup()

    // Start first instance in non-interactive mode (short-lived)
    prim := startPrimary(t, env.Workdir, "-p", "say hello", "-f", "text", "--yolo", "--cwd", env.Workdir)
    err := prim.waitForExit(t, 30*time.Second)
    if err != nil {
        t.Fatalf("primary failed: %v", err)
    }

    // Verify: ipc.lock file exists and indicates primary
    lockPath := filepath.Join(env.Workdir, ".pando", "ipc.lock")
    if _, err := os.Stat(lockPath); os.IsNotExist(err) {
        t.Fatal("ipc.lock not found — primary should have created it")
    }

    // Verify: SQLite file exists (created by primary)
    dbPath := filepath.Join(env.Workdir, ".pando", "pando.db")
    if _, err := os.Stat(dbPath); os.IsNotExist(err) {
        t.Fatal("pando.db not found — primary should have created it")
    }
}
```

#### Test 2: Second instance becomes secondary

```go
func TestSecondInstanceIsSecondary(t *testing.T) {
    env := newTestEnv(t)
    defer env.cleanup()

    // Start primary (background, non-interactive)
    prim := startPrimary(t, env.Workdir, "-p", "say hello", "-f", "text", "--yolo", "--cwd", env.Workdir)
    defer prim.stop(t)

    // Wait for primary to initialise
    time.Sleep(3 * time.Second)

    // Start secondary (should detect lock and become secondary)
    sec := startSecondary(t, env.Workdir, "-p", "say world", "-f", "text", "--yolo", "--cwd", env.Workdir)
    err := sec.waitForExit(t, 30*time.Second)

    // Secondary should complete without error
    if err != nil {
        t.Fatalf("secondary failed: %v", err)
    }
}
```

#### Test 3: Write from secondary reaches primary

```go
func TestWriteFromSecondaryReachesPrimary(t *testing.T) {
    env := newTestEnv(t)
    defer env.cleanup()

    // Start primary in serve mode (long-lived)
    prim := startPrimary(t, env.Workdir, "serve", "--port", "18765", "--host", "127.0.0.1", "--debug")
    defer prim.stop(t)
    time.Sleep(3 * time.Second) // wait for server to be ready

    // Start a CLI subagent that creates a session (write)
    sec := startSecondary(t, env.Workdir, "-p", "create a new session titled integration-test", "-f", "text", "--yolo", "--cwd", env.Workdir)
    err := sec.waitForExit(t, 60*time.Second)
    if err != nil {
        t.Fatalf("secondary CLI failed: %v", err)
    }

    // Verify: the write reached the primary by checking DB
    // (query the DB directly from the test process)
    dbPath := filepath.Join(env.Workdir, ".pando", "pando.db")
    // ... query and assert session was created
}
```

#### Test 4: Primary-first with app mode as secondary

```go
func TestAppModeAsSecondary(t *testing.T) {
    env := newTestEnv(t)
    defer env.cleanup()

    // Start primary in TUI mode
    prim := startPrimary(t, env.Workdir, "app", "--port", "18765", "--host", "127.0.0.1")
    defer prim.stop(t)
    time.Sleep(3 * time.Second)

    // Start another instance in serve mode (should be secondary)
    sec := startSecondary(t, env.Workdir, "serve", "--port", "18766", "--host", "127.0.0.1")
    defer sec.stop(t)
    time.Sleep(3 * time.Second)

    // Verify: only one process holds ipc.lock
    // (can't easily verify without internal API, but a test helper could check)
}
```

#### Test 5: Cross-entrypoint — TUI first, then desktop, then serve

```go
func TestCrossEntrypointSingleWriter(t *testing.T) {
    env := newTestEnv(t)
    defer env.cleanup()

    // 1. Start serve (first — should be primary)
    serve := startPrimary(t, env.Workdir, "serve", "--port", "18765", "--host", "127.0.0.1")
    defer serve.stop(t)
    time.Sleep(3 * time.Second)

    // 2. Start app (second — should be secondary)
    app := startSecondary(t, env.Workdir, "app", "--port", "18766", "--host", "127.0.0.1")
    defer app.stop(t)
    time.Sleep(3 * time.Second)

    // 3. Start desktop (third — should be secondary)
    // Note: desktop may require a display; use a headless test variant
    // ... skip or use DO_NOT_OPEN_BROWSER env var ...

    // Verify: all instances can write through the primary
    cli := startSecondary(t, env.Workdir, "-p", "say something", "-f", "text", "--yolo", "--cwd", env.Workdir)
    err := cli.waitForExit(t, 30*time.Second)
    if err != nil {
        t.Fatalf("CLI subagent failed: %v", err)
    }
}
```

#### Test 6: Primary death, secondary detects

```go
func TestPrimaryDeathDetectedBySecondary(t *testing.T) {
    t.Skip("Phase 5: failover not yet implemented")

    env := newTestEnv(t)
    defer env.cleanup()

    prim := startPrimary(t, env.Workdir, "serve", "--port", "18765")
    time.Sleep(3 * time.Second)

    sec := startSecondary(t, env.Workdir, "serve", "--port", "18766")
    time.Sleep(3 * time.Second)

    // Kill primary
    prim.stop(t)
    time.Sleep(20 * time.Second) // wait for heartbeat timeout

    // Verify: secondary detected primary death (logs)
    // Verify: secondary either promoted or failed gracefully
}
```

#### Test 7: Concurrent writes from multiple secondaries

```go
func TestConcurrentWritesFromMultipleSecondaries(t *testing.T) {
    t.Skip("Requires Phase 3 coordinator for deterministic ordering")

    env := newTestEnv(t)
    defer env.cleanup()

    // Start primary in serve mode
    prim := startPrimary(t, env.Workdir, "serve", "--port", "18765")
    defer prim.stop(t)
    time.Sleep(3 * time.Second)

    // Start 3 secondary CLI instances concurrently
    var wg sync.WaitGroup
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            sec := startSecondary(t, env.Workdir,
                "-p", fmt.Sprintf("session-%d", idx),
                "-f", "text", "--yolo", "--cwd", env.Workdir)
            err := sec.waitForExit(t, 60*time.Second)
            if err != nil {
                t.Errorf("secondary %d failed: %v", idx, err)
            }
        }(i)
    }
    wg.Wait()

    // Verify: all sessions were created in DB
}
```

### 3.4 Makefile Target

```makefile
# Makefile

.PHONY: test-integration
test-integration: build
    PANDO_BINARY=./pando go test -tags=integration ./test/integration/single_writer/... -v -timeout 180s
```

### 3.5 CI Setup

Add to CI pipeline:
```yaml
- name: Integration tests (single-writer)
  run: make test-integration
```

## 4. Acceptance Criteria

- [ ] `test/integration/single_writer/` directory with helper functions
- [ ] Test: first instance is primary
- [ ] Test: second instance is secondary
- [ ] Test: write from secondary reaches primary
- [ ] Test: cross-entrypoint single-writer (serve + app)
- [ ] Test: concurrent writes from multiple secondaries (Phase 3)
- [ ] Test: primary death detection (Phase 5)
- [ ] All tests pass with `go test -tags=integration`
- [ ] `make test-integration` target added
- [ ] Tests run in CI

## 5. Risks

- **Medium risk.** Multi-process tests are slower and flakier than unit tests. Need generous timeouts and cleanup.
- Build dependency: tests need a compiled `pando` binary.
- Desktop mode tests may need a display → use headless test variants or skip.

## 6. Dependencies

- Phase 1 (unified bootstrap)
- Phase 2 (write contract) — for error handling in tests
- Phase 3 (serialisation loop) — for concurrent write tests
- Phase 5 (failover) — for failover test

## 7. Estimated effort

3-5 days.
