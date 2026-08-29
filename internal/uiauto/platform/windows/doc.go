// Package windows implements the Windows UI Automation (UIA) accessibility
// backend for the Pando Desktop Controller (internal/uiauto), Phase 4 of
// [[pando/plans/desktop_controller_uiauto_plan.md]]. It talks to UIA over
// COM using github.com/go-ole/go-ole plus hand-written vtable calls for the
// UIA-specific interfaces go-ole does not itself bind — no cgo.
//
// Design notes (mirrors the structure of the Phase 2 Linux AT-SPI2 backend,
// internal/uiauto/platform/linux):
//
//   - Every UIA element is identified, durably, by its RuntimeId (an []int32
//     UIA hands out per element that is stable across separate COM calls,
//     unlike the IUIAutomationElement COM pointer itself, which is not safe
//     to keep across calls/threads without care). RuntimeId is encoded to a
//     string (runtimeid.go) and stashed in core.Element.Native.Data, mirroring
//     how the Linux backend stashes its (busName, objectPath) accessibleRef.
//
//   - The live COM pointers are NOT stored on core.Element (which must stay a
//     plain, platform-independent value type). Instead the windows-only
//     backend keeps a mutex-guarded handle table (encoded RuntimeId -> live
//     *uiaElement) and resolves an incoming Element back to its COM pointer
//     through that table. A ref whose RuntimeId is not (or no longer) in the
//     table surfaces as STALE_REF/ELEMENT_NOT_FOUND, never a crash — see
//     backend_windows.go's resolveElement.
//
//   - All COM calls happen on a single dedicated OS thread: UIA client
//     objects are apartment-threaded (COINIT_APARTMENTTHREADED) and are not
//     safe to touch from arbitrary goroutines. worker_windows.go runs a
//     goroutine that calls runtime.LockOSThread and CoInitializeEx once, then
//     serves every COM request from a channel for the lifetime of the
//     backend; Close stops the worker and calls CoUninitialize.
//
//   - Traversal batches one cross-process hop per tree level: instead of one
//     round trip per attribute per node (which is what an incremental,
//     per-node AT-SPI-style walk would cost over COM's much higher per-call
//     overhead), each level is fetched with a single
//     IUIAutomationElement::FindAll(TreeScope_Children, TrueCondition,
//     cacheRequest) call using a CacheRequest that pre-fetches
//     Name/ControlType/AutomationId/ClassName/BoundingRectangle/IsEnabled/
//     IsOffscreen/HasKeyboardFocus for every child in that one call. This is
//     UIA's analogue of the Linux backend's per-object property batching:
//     the traversal is still selector-driven and prunes branches exactly the
//     way findRec does in the Linux backend (see traverse.go, which is
//     platform-independent and shared by both a fake test provider and the
//     real windows one via the nodeProvider interface), but the "one round
//     trip per object" cost of AT-SPI becomes "one round trip per tree
//     level" here, since UIA lets a single call fetch a whole batch of
//     cached children at once.
//
// Files without a `//go:build windows` tag hold everything that does not
// touch COM (ControlType -> role mapping, RuntimeId encode/decode, the
// generic selector-driven traversal algorithm over a small nodeProvider
// interface, and best-effort HRESULT -> core.ErrorCode mapping) so they
// build and are unit tested on any platform, including this Linux dev
// machine. Everything that actually calls into COM lives in files tagged
// `//go:build windows` and is compile-verified only (GOOS=windows go build)
// — it has never been exercised against a real Windows UI Automation
// provider.
package windows
