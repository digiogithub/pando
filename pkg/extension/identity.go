package extension

import "context"

// Identity capability: how an extension tells the host who is using it.
//
// A standalone Pando has no notion of a user. It runs as whoever started it,
// and the facts it records about a write ("which project", "which instance")
// are all it can honestly know. A host that is enrolled in some larger
// system — a directory, an SSO session, a device management agent — does know
// more, but that knowledge lives entirely in whatever extension performs the
// enrolment.
//
// This capability is the one-way channel between the two. The extension
// answers a question; the host decides what, if anything, to do with the
// answer. Core never asks for credentials, never authenticates anybody, and
// never persists an identity: it copies the user id into the attribution it
// already attaches to memory events and session index metadata, and that is
// the whole of it.

// Identity is what an identity provider knows about the person the host is
// running for. Every field is optional; a provider that only knows a stable
// opaque user id fills UserID and leaves the rest empty.
type Identity struct {
	// UserID is a stable identifier for the user, opaque to the host. It is
	// the only field the host itself consumes: it becomes the user id on the
	// attribution attached to memory events and session index metadata.
	UserID string

	// Email is the user's address, when the provider knows one. The host does
	// not read, log or persist it; it exists so that a capability which needs
	// it (a sink writing to a store that keys on address) can take it from the
	// same place as everything else rather than growing a second channel.
	Email string

	// DeviceID identifies the machine or enrolment the identity was issued
	// for. Same contract as Email: carried, not consumed.
	DeviceID string

	// Groups lists the group or role names the provider associates with the
	// user. Authorisation is never decided by the host from this list: a
	// permission that matters is enforced by the service that owns the
	// resource, not by the process asking.
	Groups []string
}

// IdentityProvider is implemented by extensions that can say who the user is.
//
// The host calls Identity at the moment it needs the answer, never once at
// startup, so a sign-in or a sign-out that happens while Pando runs takes
// effect on the next event without a restart. That makes the call latency
// sensitive: it sits on the path of an ordinary memory write, so it must
// answer from state the extension already holds and must never block on the
// network. The host bounds it with a short timeout and contains a panic, but
// a provider that is routinely slow will be felt.
//
// Returning false means "I do not know right now" — not signed in yet, token
// expired, enrolment lost. The host then behaves exactly as an unextended
// Pando does, which is the required behaviour rather than a fallback: an
// optional capability must never be able to degrade the host that loads it.
//
// When several extensions provide an identity, the first one in load order
// that returns true wins.
type IdentityProvider interface {
	Extension
	Identity(ctx context.Context) (Identity, bool)
}
