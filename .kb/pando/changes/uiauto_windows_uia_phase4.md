---
created_at: 2026-08-29T18:50:15.414556949Z
updated_at: 2026-08-29T18:50:15.414556949Z
tags:
    - change
    - desktop
    - uiauto
    - windows
    - uia
---
# Phase 4 — Windows UI Automation backend (`internal/uiauto/platform/windows`)

Part of [[desktop_controller_uiauto_plan]]. Follows [[uiauto_core_phase0]], [[uiauto_tools_phase1]], [[uiauto_linux_atspi_phase2]], [[uiauto_input_screen_phase3]].

Date: 2026-08-29.

## What changed

New package `internal/uiauto/platform/windows` (20 files, ~2526 lines incl. tests) implementing `core.Backend` for Windows UI Automation, plus the registration file `internal/uiauto/backends_windows.go`.

Files: `backend_windows.go` (the 9 `core.Backend` methods), `automation_windows.go` (IUIAutomation object creation), `comcall_windows.go` (hand-built vtable dispatch via `syscall.SyscallN`), `worker_windows.go` (dedicated COM apartment thread), `cacherequest_windows.go` (subtree prefetch), `patterns_windows.go` (control patterns), `uielement_windows.go`, `process_windows.go` (PID -> process name), plus the build-tag-free, unit-tested logic: `controltype.go` (ControlType id -> `core.Role`), `runtimeid.go` (RuntimeId encode/decode as the durable identity), `hresult.go` (HRESULT -> `core.DesktopError`), `element.go`, `traverse.go` (selector-driven filtering).

## Why

Windows is the platform with the most mature accessibility infrastructure; UIA gives the control tree, names, roles, states, bounds and invokable patterns the agent needs, so `desktop_*` tools work without screenshots.

## Key design points

- **No cgo**: COM through `github.com/go-ole/go-ole` plus hand-written vtable calls (`syscall.SyscallN` on slot indices, each documented with the named slot from the public UIAutomationClient IDL).
- **COM apartment threading**: every COM call is funnelled through one dedicated OS thread (`runtime.LockOSThread` in a worker goroutine fed by a request channel), because UIA interface pointers are not safe to touch from arbitrary goroutines.
- **CacheRequest prefetch**: `AddProperty` for Name/ControlType/AutomationId/ClassName/BoundingRectangle/IsEnabled/IsOffscreen/HasKeyboardFocus with `TreeScope_Subtree` + `FindAllBuildCache`, so a subtree crosses the process boundary in one hop; the parsed `core.Selector` is then applied locally. Still bounded by `limit`, a depth cap and `ctx` cancellation — no whole-desktop walk.
- **Identity**: UIA interface pointers are not stable, so the durable key is the **RuntimeId** int array stored in `Element.Native.Data`; live pointers live in a per-snapshot handle table released in `Close()`. A RuntimeId that no longer resolves surfaces as `STALE_REF`/`ELEMENT_NOT_FOUND`, never a crash.
- Unsupported pattern -> `ACTION_FAILED`, so the Manager's `ActionResolver` falls back to the Phase 3 physical input layer.

## Verification — COMPILE-VERIFIED ONLY

**This backend has never been run against a real Windows machine or a real COM implementation.** Verified:
- `GOOS=windows go build ./internal/uiauto/...` — clean
- `GOOS=windows go vet ./internal/uiauto/...` — only the expected `possible misuse of unsafe.Pointer` at `comcall_windows.go:40`, which is the unavoidable uintptr->Pointer vtable-slot dereference of the FFI style (documented in place)
- `go build ./...` and `go test ./internal/uiauto/...` on linux — pass; 18 tests in this package cover the build-tag-free logic (ControlType mapping, RuntimeId round-trip, HRESULT mapping, selector filtering against a synthetic tree)
- `gofmt -l internal/uiauto` — empty

**Highest risk**: vtable slot-index drift. If Microsoft's public vtable order was misremembered anywhere, calls will dispatch to the wrong method at runtime. Every call site names its slot in a comment so a first run on real Windows can audit them.

Note: `GOOS=windows go build ./...` over the whole repo fails for a pre-existing unrelated reason — `github.com/madeindigio/go-tree-sitter` is cgo-only and excludes all its files on non-linux.

## Deferred

Real-hardware validation; UIA event subscriptions (Phase 7); `CUIAutomation8`-only features.
