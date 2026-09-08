// Package telemetry ships Pando's logs to a remote Better Stack source, for
// users who want to share diagnostics with the maintainers. It is opt-in and
// off by default (see internal/config.TelemetryConfig).
//
// This file holds the build-time secret plumbing. The Better Stack ingest
// source token is never committed and never stored in user configuration: it
// is injected at link time via -ldflags (see the Makefile's
// TELEMETRY_LDFLAGS and .goreleaser.yml), so a binary built without the
// secret (go install, `make build-fast`, a fork) simply reports telemetry as
// unavailable and the settings UI keeps the toggle disabled.
//
// Local development: PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build
package telemetry

import (
	"net"
	"net/url"
	"os"
)

// sourceToken is the Better Stack ingest source token. Empty in a plain `go
// build`/`go install`; set at release time via
// -X github.com/digiogithub/pando/internal/telemetry.sourceToken=... .
// Never logged, never persisted, and never exposed except through Token().
var sourceToken = ""

// ingestHost is the Better Stack ingest hostname. Not a secret — it is a
// package default, overridable via -ldflags for self-hosters or via
// PANDO_TELEMETRY_ENDPOINT for local development against a mock server.
var ingestHost = "s2751484.us-west-2a.betterstackdata.com"

// hasCustomEndpoint reports whether PANDO_TELEMETRY_ENDPOINT overrides the
// default, official Better Stack ingest host.
func hasCustomEndpoint() bool {
	return os.Getenv("PANDO_TELEMETRY_ENDPOINT") != ""
}

// Token returns the Better Stack source token used to authenticate ingest
// requests.
//
// PANDO_TELEMETRY_TOKEN, when set, always overrides the build-time value —
// useful for local development and for self-hosted forks that want to inject
// their own token without a custom build. But when PANDO_TELEMETRY_ENDPOINT
// also overrides the default ingest host (a custom endpoint), the built-in
// build-time token is NEVER used as a fallback, even if PANDO_TELEMETRY_TOKEN
// is unset: sourceToken is a secret scoped to Pando's own official ingest
// host, and falling back to it here would send it to whatever arbitrary
// endpoint PANDO_TELEMETRY_ENDPOINT happens to point at (a misconfigured
// environment, a compromised one, or simply a self-hoster's own relay that
// was never meant to receive Pando's official token). A custom endpoint
// therefore requires its own PANDO_TELEMETRY_TOKEN; without one this returns
// empty, and Available() is false.
func Token() string {
	env := os.Getenv("PANDO_TELEMETRY_TOKEN")
	if hasCustomEndpoint() {
		return env
	}
	if env != "" {
		return env
	}
	return sourceToken
}

// Endpoint returns the full ingest URL log records are POSTed to.
// PANDO_TELEMETRY_ENDPOINT, when set, overrides it entirely (scheme
// included), so a local mock ingest server can be addressed over plain http
// during development and testing.
func Endpoint() string {
	if env := os.Getenv("PANDO_TELEMETRY_ENDPOINT"); env != "" {
		return env
	}
	return "https://" + ingestHost
}

// Available reports whether telemetry can actually be shipped right now:
//   - a source token is present (see Token — a custom endpoint never falls
//     back to the built-in one, so this alone already enforces that rule);
//   - for a custom endpoint (PANDO_TELEMETRY_ENDPOINT set), the endpoint is
//     either https, or plain http to a loopback address
//     (127.0.0.1/localhost/::1) — the only case a plain-http endpoint is an
//     acceptable trade-off is same-machine local development/testing (the
//     Phase 6 E2E suite's http://127.0.0.1:<port> mock ingest server,
//     specifically). A non-loopback http endpoint would send the token and
//     every shipped record in the clear over the network, so it is treated
//     as unavailable rather than silently allowed.
//
// The default (official) endpoint has no such restriction: it is always
// https (see Endpoint), so this check is a no-op for the common case of an
// unmodified build with no PANDO_TELEMETRY_ENDPOINT override.
func Available() bool {
	if Token() == "" {
		return false
	}
	if !hasCustomEndpoint() {
		return true
	}
	return isSecureOrLoopbackEndpoint(Endpoint())
}

// isSecureOrLoopbackEndpoint reports whether endpoint is https, or http to a
// loopback address (127.0.0.1, ::1, or the hostname "localhost").
func isSecureOrLoopbackEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}
