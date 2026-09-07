package extension

import "context"

// UI policy capability: how an extension tells the host's settings surfaces
// what to show, what to show without letting it be edited, and what to say
// about it.
//
// A standalone Pando renders every settings section it knows about, and every
// field in them is editable unless the configuration itself is locked. That is
// the right default for a host that answers only to the person at the keyboard.
// A host whose configuration is imposed from somewhere else is a different
// situation: whole sections stop being meaningful (an account the operator owns
// is not configured here) and the ones that remain need to say who owns them.
//
// The capability is a presentation policy, expressed in the same vocabulary as
// the rest of the configuration surface: dotted configuration paths. An
// extension says "these paths are not this host's business" and "these paths
// are shown but not changed here"; the host decides how a terminal, a browser
// and a desktop window each express that. Core learns nothing about why, and
// nothing about the system the policy comes from.
//
// Hiding is presentation, never protection. A path the policy hides is also
// refused by the configuration write path, so a client that never renders the
// section still cannot write it; a value that must not change for reasons that
// matter is locked through ConfigOverlay.Locked, which is enforcement rather
// than presentation.

// UIPolicy is what a UI policy provider asks the host's settings surfaces to
// do. The zero value asks for nothing, which is exactly how an unextended
// Pando behaves.
//
// Paths in HiddenSections and ReadOnlySections are dotted paths into the
// configuration document ("providerAccounts", "internalTools.braveApiKey"),
// matched case-insensitively segment by segment, the same way ConfigOverlay
// treats Locked. A path names either a leaf or a whole subtree: hiding
// "mcpServers" hides every server under it.
type UIPolicy struct {
	// HiddenSections lists the paths the surfaces should not render at all. A
	// section whose every field is hidden disappears with them.
	//
	// The host also refuses local writes to a hidden path, so that hiding is
	// not the only thing standing between a client and the value.
	HiddenSections []string

	// ReadOnlySections lists the paths the surfaces should render with their
	// value visible but not editable, marked with ReadOnlyLabel. Writes to
	// them are refused for the same reason as hidden paths.
	//
	// A path that is in both lists is hidden: the stronger statement wins.
	ReadOnlySections []string

	// ReadOnlyLabel is the short caller-supplied text a surface shows on a
	// read-only field, in place of the host's own generic marker ("Managed by
	// the operator", the name of the system that owns the value). Optional;
	// the host uses its default marker when it is empty.
	ReadOnlyLabel string

	// Banner is the notice the surfaces show above the settings, explaining
	// once what the rest of the policy does to individual fields. Optional.
	Banner UIPolicyBanner
}

// UIPolicyBanner is the notice a surface renders above its settings.
type UIPolicyBanner struct {
	// Text is the message. Keep it to one line: the terminal renders it as a
	// single row above the sections.
	Text string

	// Link is an optional URL the message refers to, rendered as a plain URL
	// on surfaces that cannot make it clickable.
	Link string
}

// Empty reports whether the policy asks for nothing at all.
func (p UIPolicy) Empty() bool {
	return len(p.HiddenSections) == 0 && len(p.ReadOnlySections) == 0 &&
		p.Banner.Text == "" && p.Banner.Link == ""
}

// UIPolicyProvider is implemented by extensions that shape the host's settings
// surfaces.
//
// The host asks at render time, never once at start-up, so a policy that
// appears or disappears while Pando runs (an enrolment completing, a session
// ending) is reflected the next time a surface is drawn without a restart. The
// call must therefore answer from state the extension already holds and must
// never block on the network; the host bounds it with a short timeout and
// contains a panic, and treats either as "no policy".
//
// Returning false means "nothing to say right now", which leaves every surface
// behaving exactly as an unextended Pando. That is the required behaviour
// rather than a fallback: an optional capability must never be able to degrade
// the host that loads it.
//
// When several extensions provide a policy, the host merges them: the hidden
// and read-only sets are unioned, so any provider can hide a path and none can
// unhide one, while the single-valued ReadOnlyLabel and Banner are taken from
// the first provider in load order that sets them and later ones are dropped.
// A policy is a restriction; merging it can only ever restrict further.
type UIPolicyProvider interface {
	Extension
	UIPolicy(ctx context.Context) (UIPolicy, bool)
}
