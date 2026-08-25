package extension

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Licensing is entitlement gating, not tamper protection.
//
// Customers receive compiled binaries built on our own pipelines, so a license
// check can only answer "was this build meant to run this module for this
// customer, and is that still true today". It cannot defend against someone
// patching the binary they were handed, and nothing here pretends otherwise.
// The real protection is that enterprise source lives in a private module.
//
// The split follows the same rule as the rest of the contract: core owns the
// format, the verification and the gate; the enterprise module owns the trusted
// public keys and where the license file comes from. That is why no public key
// appears in this package — a key compiled into the OSS core would be a key
// anyone could mint entitlements against by rebuilding core with their own.

// Entitlement names something a license permits. It is matched against
// extension IDs three ways:
//
//	memory.sink.corp   exact ID
//	memory.*           the namespace and everything under it
//	*                  every extension
type Entitlement string

// Matches reports whether this entitlement covers the given extension ID.
func (e Entitlement) Matches(id ID) bool {
	s := string(e)
	switch {
	case s == "":
		return false
	case s == "*":
		return true
	case strings.HasSuffix(s, ".*"):
		ns := strings.TrimSuffix(s, ".*")
		return string(id) == ns || strings.HasPrefix(string(id), ns+".")
	default:
		return s == string(id)
	}
}

// Entitlements is the list carried by a license.
type Entitlements []Entitlement

// Allows reports whether any entitlement in the list covers the ID.
func (es Entitlements) Allows(id ID) bool {
	for _, e := range es {
		if e.Matches(id) {
			return true
		}
	}
	return false
}

// Strings renders the entitlements for reporting.
func (es Entitlements) Strings() []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, string(e))
	}
	return out
}

// LicenseClaims is the signed payload of a license file.
//
// It is deliberately small. Everything in it is a fact about the customer's
// agreement, never a runtime setting: a license must not become a second,
// invisible configuration file.
type LicenseClaims struct {
	// Customer identifies who the license was issued to. Shown in reporting.
	Customer string `json:"customer"`
	// IssuedAt is when the license was minted.
	IssuedAt time.Time `json:"issuedAt,omitzero"`
	// ExpiresAt is when it stops being valid. The zero value means perpetual.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	// Entitlements lists what may load.
	Entitlements Entitlements `json:"entitlements"`
	// Notes is free text for humans (contract reference, support tier).
	Notes string `json:"notes,omitempty"`
}

// Expired reports whether the license has passed its expiry at the given time.
// A perpetual license (zero ExpiresAt) never expires.
func (c LicenseClaims) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// Allows reports whether the claims entitle the given extension ID, ignoring
// expiry. Callers that care about expiry check it separately so they can report
// "expired" differently from "not covered" — the two need different answers
// from whoever reads the log.
func (c LicenseClaims) Allows(id ID) bool { return c.Entitlements.Allows(id) }

// SignedLicense is the on-disk envelope: a JSON document with the claims kept
// as raw bytes.
//
// The signature covers those bytes with insignificant whitespace removed
// (encoding/json's Compact, nothing more). Compaction is the only normalisation
// applied: key order, values and unknown fields are all left exactly as the
// issuer wrote them, so a field added by a newer issuer does not invalidate an
// older reader's check. Whitespace has to be normalised because writing an
// indented, human-readable license file necessarily re-indents the claims.
type SignedLicense struct {
	// KeyID names the signing key, so keys can be rotated without breaking
	// licenses already issued under the previous one.
	KeyID string `json:"keyId"`
	// Claims is the raw JSON of the LicenseClaims.
	Claims json.RawMessage `json:"claims"`
	// Signature is the base64 (standard encoding) Ed25519 signature over Claims.
	Signature string `json:"signature"`
}

// Errors returned by VerifyLicense. They are distinguished because they mean
// different things to whoever has to act on them: a bad signature is a broken
// or forged file, an unknown key is usually a build/key-rotation mismatch, and
// an expired license is a commercial matter, not a technical one.
var (
	ErrLicenseMalformed  = errors.New("extension: malformed license")
	ErrLicenseUnknownKey = errors.New("extension: license signed by an unknown key")
	ErrLicenseSignature  = errors.New("extension: license signature does not verify")
	ErrLicenseExpired    = errors.New("extension: license expired")
)

// VerifyLicense parses and checks a signed license against a set of trusted
// public keys, keyed by key ID.
//
// It does not check expiry: an expired license still has valid claims worth
// reporting ("expired on <date>" is a far more useful message than "invalid"),
// so expiry is left to the gate. Verification failures return the claims'
// zero value.
func VerifyLicense(data []byte, keys map[string]ed25519.PublicKey) (LicenseClaims, error) {
	var env SignedLicense
	if err := json.Unmarshal(data, &env); err != nil {
		return LicenseClaims{}, fmt.Errorf("%w: %v", ErrLicenseMalformed, err)
	}
	if len(env.Claims) == 0 || env.Signature == "" {
		return LicenseClaims{}, fmt.Errorf("%w: missing claims or signature", ErrLicenseMalformed)
	}

	key, ok := keys[env.KeyID]
	if !ok {
		return LicenseClaims{}, fmt.Errorf("%w: %q", ErrLicenseUnknownKey, env.KeyID)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return LicenseClaims{}, fmt.Errorf("%w: signature is not base64: %v", ErrLicenseMalformed, err)
	}
	signed, err := compactJSON(env.Claims)
	if err != nil {
		return LicenseClaims{}, fmt.Errorf("%w: claims: %v", ErrLicenseMalformed, err)
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, signed, sig) {
		return LicenseClaims{}, ErrLicenseSignature
	}

	var claims LicenseClaims
	if err := json.Unmarshal(env.Claims, &claims); err != nil {
		return LicenseClaims{}, fmt.Errorf("%w: claims: %v", ErrLicenseMalformed, err)
	}
	return claims, nil
}

// SignLicense produces a signed license document. It lives here so that the
// issuing tool and the verifier can never drift apart on the format; the
// private key is the issuer's business and never appears in a build.
func SignLicense(claims LicenseClaims, keyID string, priv ed25519.PrivateKey) ([]byte, error) {
	if keyID == "" {
		return nil, errors.New("extension: SignLicense needs a key ID")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("extension: SignLicense needs an ed25519 private key")
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("extension: marshal claims: %w", err)
	}
	// json.Marshal already emits compact JSON; compacting again costs nothing
	// and keeps signing and verification reading from one definition.
	raw, err = compactJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("extension: compact claims: %w", err)
	}
	env := SignedLicense{
		KeyID:     keyID,
		Claims:    raw,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw)),
	}
	return json.MarshalIndent(env, "", "  ")
}

// compactJSON strips insignificant whitespace, which is what both signing and
// verification agree to sign over.
func compactJSON(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// LicenseStatus is what the host reports about licensing: the CLI, the API and
// the WebUI all render this and nothing else.
//
// It carries no secret: no key material, no file contents, and Source is a
// description of where the license came from, not necessarily a path.
type LicenseStatus struct {
	// Present is true when a license document was found at all. False with a
	// non-empty Error means "looked, and could not read one".
	Present bool `json:"present"`
	// Valid is true when the license verified and has not expired.
	Valid bool `json:"valid"`
	// Customer is who it was issued to.
	Customer string `json:"customer,omitempty"`
	// ExpiresAt is the expiry; zero means perpetual.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	// Entitlements is what the license grants, for display.
	Entitlements []string `json:"entitlements,omitempty"`
	// Source describes where the license was loaded from.
	Source string `json:"source,omitempty"`
	// Error explains why an otherwise present license is not valid.
	Error string `json:"error,omitempty"`
}

// LicenseProvider is implemented by the extension that owns licensing for a
// build. At most one is expected; if several are compiled in, the manager uses
// the first in load order and says so.
//
// A LicenseProvider is never gated by its own check — a licensing extension
// that had to license itself could never start.
type LicenseProvider interface {
	Extension

	// Entitled reports whether the extension described by info may load. A nil
	// error allows it; any error blocks it and is reported verbatim in that
	// extension's Status.
	Entitled(info Info) error

	// LicenseStatus describes the current license for reporting.
	LicenseStatus() LicenseStatus
}

// LicenseGate is a ready-made LicenseProvider implementation over a set of
// verified claims. Enterprise modules embed it rather than reimplementing the
// entitlement and expiry rules, so that every build answers the same way.
//
// The zero value is not usable; build one with NewLicenseGate or
// NewUnlicensedGate.
type LicenseGate struct {
	claims  LicenseClaims
	source  string
	loadErr error
	present bool
	now     func() time.Time
}

// NewLicenseGate builds a gate over claims that have already been verified.
// source describes where they came from, for reporting.
func NewLicenseGate(claims LicenseClaims, source string) *LicenseGate {
	return &LicenseGate{claims: claims, source: source, present: true, now: time.Now}
}

// NewUnlicensedGate builds a gate for a build that has no usable license: none
// was found, or it failed to verify. Every non-exempt extension is refused, and
// the reason travels with the refusal so the log says what actually happened
// rather than a bare "not licensed".
func NewUnlicensedGate(source string, err error) *LicenseGate {
	return &LicenseGate{source: source, loadErr: err, present: err != nil, now: time.Now}
}

// SetClock replaces the gate's clock. Tests use it; production does not.
func (g *LicenseGate) SetClock(now func() time.Time) {
	if now != nil {
		g.now = now
	}
}

// Entitled applies the gate rules: MIT extensions always pass, everything else
// needs a valid, unexpired license that covers its ID.
func (g *LicenseGate) Entitled(info Info) error {
	if info.License == "" || info.License == LicenseMIT {
		return nil
	}
	if g.loadErr != nil {
		return fmt.Errorf("no usable license: %w", g.loadErr)
	}
	if !g.present {
		return errors.New("no license installed")
	}
	if g.claims.Expired(g.clock()) {
		return fmt.Errorf("%w on %s", ErrLicenseExpired, g.claims.ExpiresAt.Format(time.DateOnly))
	}
	if !g.claims.Allows(info.ID) {
		return fmt.Errorf("license for %q does not entitle %s", g.claims.Customer, info.ID)
	}
	return nil
}

// LicenseStatus renders the gate's state.
func (g *LicenseGate) LicenseStatus() LicenseStatus {
	st := LicenseStatus{Present: g.present, Source: g.source}
	if g.loadErr != nil {
		st.Error = g.loadErr.Error()
		return st
	}
	if !g.present {
		return st
	}
	st.Customer = g.claims.Customer
	st.ExpiresAt = g.claims.ExpiresAt
	st.Entitlements = g.claims.Entitlements.Strings()
	if g.claims.Expired(g.clock()) {
		st.Error = "expired on " + g.claims.ExpiresAt.Format(time.DateOnly)
		return st
	}
	st.Valid = true
	return st
}

// Claims exposes the verified claims, for enterprise code that needs a fact
// from the license beyond entitlement (a support tier, a customer name in a
// header). It returns a copy of the slice so a caller cannot edit the gate.
func (g *LicenseGate) Claims() LicenseClaims {
	out := g.claims
	out.Entitlements = append(Entitlements(nil), g.claims.Entitlements...)
	return out
}

func (g *LicenseGate) clock() time.Time {
	if g.now == nil {
		return time.Now()
	}
	return g.now()
}
