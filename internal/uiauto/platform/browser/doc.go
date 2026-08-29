// Package browser implements the Chrome DevTools Protocol accessibility
// backend (registered as "cdp") for the Pando Desktop Controller
// (internal/uiauto): when the UI in question is a Chrome/Edge/Chromium
// page, it serves the normalized core.Element model straight from the CDP
// Accessibility/DOM domains of an already-running browser session, instead
// of the OS accessibility API. One core.Element model, multiple sources.
//
// It never launches a browser itself. RegisterSession/UnregisterSession
// (session.go) let the existing browser_* agent tools
// (internal/llm/tools/browser_session.go) publish/retract the chromedp
// session this backend rides on; Available reports honestly (all-false
// Capabilities/PLATFORM_NOT_SUPPORTED) when none is registered, matching
// the NullBackend/Manager contract every other platform backend follows.
// Manager is a process-wide singleton (see internal/uiauto.Manager.Shared),
// so a single registered-session slot -- rather than a per-pando-session
// table -- is sufficient: the backend simply operates against whichever
// browser session the browser_* tools most recently opened or reused.
//
// The CDP wire access (conn.go) sits behind the axConn interface, so the
// traversal (traverse.go), role/property mapping (element.go), action
// dispatch (actions.go) and error mapping in backend.go are all unit-tested
// against a fake in-memory CDP responder (fake_conn_test.go) without a real
// browser; backend_integration_test.go additionally drives a real, locally
// detected browser end to end when one is available, skipping otherwise.
package browser
