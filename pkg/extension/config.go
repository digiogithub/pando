package extension

import "context"

// Configuration capability: how an extension imposes configuration on the host
// and marks parts of it as not locally editable.
//
// The shape is deliberately one-way and declarative. An extension does not get
// a write handle on the configuration; it returns a *document* describing what
// the configuration should say, and the host merges that document on top of
// what it read from files and the environment. That keeps two properties core
// cares about:
//
//   - The host stays the only writer. There is no ordering hazard between an
//     extension mutating configuration and core reading it, because the merge
//     happens at one point in the load path and nowhere else.
//   - The overlay is reproducible. Loading the configuration again asks the
//     provider again, so the value the host runs with is always the value the
//     provider would produce now, not the residue of an earlier write.
//
// Where the document comes from is entirely the extension's business: a remote
// policy server, a signed file it caches, a hard-coded table for a kiosk
// build. Core never learns the source and never persists the overlay.

// ConfigOverlay is one configuration document an extension imposes, together
// with the keys it does not want edited locally.
//
// Paths in Locked and Additive are dotted paths into the configuration
// document ("tui.theme", "internalTools.braveApiKey"), matched
// case-insensitively segment by segment, because configuration keys are
// case-insensitive throughout. A path names either a leaf or a whole subtree:
// locking "mcpServers" locks every server under it.
type ConfigOverlay struct {
	// Values is the overlay document. Its shape mirrors the configuration
	// file: section names at the top level, nested maps below. Keys absent
	// from it are left exactly as the host loaded them.
	//
	// Merge semantics, applied per key:
	//   - a scalar replaces the loaded value;
	//   - a map is merged key by key, recursively;
	//   - a list of objects that all carry an "id" is merged by that id, the
	//     overlay entries first, so an overlay can add or redefine one entry
	//     without restating the rest;
	//   - any other list replaces the loaded list, unless its path is named in
	//     Additive, in which case the two lists are unioned, loaded values
	//     first, duplicates dropped.
	Values map[string]any

	// Locked lists the paths the host must refuse to write locally. A write
	// through any configuration mutator that would change a locked path fails
	// with a typed error, and the surfaces report the key as managed by an
	// extension rather than as a failed save.
	//
	// Locking a path that the overlay does not set is allowed and means
	// "freeze whatever is there": the value stays whatever the files said, but
	// nobody may change it from inside Pando.
	Locked []string

	// Additive lists the list-valued paths that should be unioned with the
	// loaded value rather than replacing it. Use it for lists where local
	// entries are legitimate additions (extra context paths, extra banned
	// commands) rather than a competing choice.
	Additive []string

	// Source is a short human-readable label for where the document came from,
	// used in log lines and in the change event. Optional; the extension ID is
	// used when it is empty.
	Source string
}

// ConfigOverlayProvider is implemented by extensions that impose configuration
// on the host.
//
// The host calls ConfigOverlay during configuration load, which happens before
// the extension is started and may happen again at any time afterwards. The
// call must therefore be cheap and must not block on the network: an extension
// that fetches its document remotely caches it and serves the cache here,
// refreshing out of band and calling ConfigOverlayController.ReapplyOverlays
// when the cache changes.
//
// Returning an error means "I have nothing to say right now": the host logs it
// and continues with the configuration it already has. It is never a startup
// failure, because an optional capability must not be able to stop Pando from
// running.
type ConfigOverlayProvider interface {
	Extension
	ConfigOverlay(ctx context.Context) (ConfigOverlay, error)
}

// ConfigOverlayController is the host side of the same capability: the handle
// an extension uses to tell the host that its overlay has changed.
//
// It is deliberately not a "set this value" call. The extension says only that
// the answer to ConfigOverlay is now different; the host decides when to ask
// and re-runs the whole load path so that files, environment and every
// registered overlay are combined exactly as they are at startup.
//
// Available as HostServices.ConfigOverlays. It is nil in hosts that do not
// support overlays, so check before calling.
type ConfigOverlayController interface {
	// ReapplyOverlays re-runs configuration load, asking every registered
	// provider for its current document. It returns the load error, if any.
	// Calls are serialised by the host; a call made while another is in
	// progress waits for it.
	ReapplyOverlays(ctx context.Context) error
}
