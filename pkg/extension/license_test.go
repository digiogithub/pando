package extension

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testKeys(t *testing.T) (map[string]ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return map[string]ed25519.PublicKey{"k1": pub}, priv
}

func TestEntitlementMatching(t *testing.T) {
	cases := []struct {
		ent  Entitlement
		id   ID
		want bool
	}{
		{"memory.sink.corp", "memory.sink.corp", true},
		{"memory.sink.corp", "memory.sink.other", false},
		{"memory.*", "memory.sink.corp", true},
		{"memory.*", "memory", true},
		{"memory.*", "memorybank.sink", false},
		{"*", "anything.at.all", true},
		{"", "memory.sink.corp", false},
	}
	for _, c := range cases {
		if got := c.ent.Matches(c.id); got != c.want {
			t.Errorf("Entitlement(%q).Matches(%q) = %v, want %v", c.ent, c.id, got, c.want)
		}
	}
}

func TestSignAndVerifyLicense(t *testing.T) {
	keys, priv := testKeys(t)
	claims := LicenseClaims{
		Customer:     "ACME",
		IssuedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Entitlements: Entitlements{"memory.*"},
	}
	doc, err := SignLicense(claims, "k1", priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	got, err := VerifyLicense(doc, keys)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Customer != "ACME" || !got.Allows("memory.sink.corp") {
		t.Fatalf("claims did not round-trip: %+v", got)
	}
}

func TestVerifyLicenseRejectsTamperedClaims(t *testing.T) {
	keys, priv := testKeys(t)
	doc, err := SignLicense(LicenseClaims{Customer: "ACME", Entitlements: Entitlements{"memory.sink.corp"}}, "k1", priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Widen the entitlement without re-signing: the signature covers the raw
	// claims bytes, so any edit at all must fail.
	tampered := strings.Replace(string(doc), "memory.sink.corp", "*", 1)
	if tampered == string(doc) {
		t.Fatal("test did not modify the document")
	}
	if _, err := VerifyLicense([]byte(tampered), keys); !errors.Is(err, ErrLicenseSignature) {
		t.Fatalf("err = %v, want ErrLicenseSignature", err)
	}
}

func TestVerifyLicenseUnknownKey(t *testing.T) {
	_, priv := testKeys(t)
	otherKeys, _ := testKeys(t)
	doc, _ := SignLicense(LicenseClaims{Customer: "ACME"}, "k9", priv)
	if _, err := VerifyLicense(doc, otherKeys); !errors.Is(err, ErrLicenseUnknownKey) {
		t.Fatalf("err = %v, want ErrLicenseUnknownKey", err)
	}
}

func TestVerifyLicenseMalformed(t *testing.T) {
	keys, _ := testKeys(t)
	for name, doc := range map[string]string{
		"not json":       "{{{",
		"no signature":   `{"keyId":"k1","claims":{"customer":"x"}}`,
		"bad base64 sig": `{"keyId":"k1","claims":{"customer":"x"},"signature":"!!!"}`,
	} {
		if _, err := VerifyLicense([]byte(doc), keys); !errors.Is(err, ErrLicenseMalformed) {
			t.Errorf("%s: err = %v, want ErrLicenseMalformed", name, err)
		}
	}
}

func TestVerifyLicenseAcceptsUnknownClaimFields(t *testing.T) {
	// A newer issuer adding a field must not break an older reader: the
	// signature is over the bytes, and JSON ignores what it does not know.
	_, priv := testKeys(t)
	pub := priv.Public().(ed25519.PublicKey)
	raw := []byte(`{"customer":"ACME","entitlements":["memory.*"],"supportTier":"gold"}`)
	env := SignedLicense{KeyID: "k1", Claims: raw}
	env.Signature = base64Std(ed25519.Sign(priv, raw))
	doc, _ := json.Marshal(env)

	claims, err := VerifyLicense(doc, map[string]ed25519.PublicKey{"k1": pub})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !claims.Allows("memory.sink.corp") {
		t.Fatal("entitlement lost")
	}
}

func TestLicenseGateRules(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	gate := NewLicenseGate(LicenseClaims{
		Customer:     "ACME",
		ExpiresAt:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Entitlements: Entitlements{"memory.*"},
	}, "test")
	gate.SetClock(func() time.Time { return now })

	mit := Info{ID: "tools.demo", License: LicenseMIT}
	covered := Info{ID: "memory.sink.corp", License: LicenseEnterprise}
	uncovered := Info{ID: "api.audit.corp", License: LicenseEnterprise}

	if err := gate.Entitled(mit); err != nil {
		t.Fatalf("MIT extension gated: %v", err)
	}
	if err := gate.Entitled(covered); err != nil {
		t.Fatalf("entitled extension refused: %v", err)
	}
	if err := gate.Entitled(uncovered); err == nil {
		t.Fatal("unentitled extension allowed")
	}

	if st := gate.LicenseStatus(); !st.Valid || st.Customer != "ACME" {
		t.Fatalf("status = %+v, want valid ACME", st)
	}
}

func TestLicenseGateExpiry(t *testing.T) {
	gate := NewLicenseGate(LicenseClaims{
		Customer:     "ACME",
		ExpiresAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Entitlements: Entitlements{"*"},
	}, "test")
	gate.SetClock(func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) })

	err := gate.Entitled(Info{ID: "memory.sink.corp", License: LicenseEnterprise})
	if !errors.Is(err, ErrLicenseExpired) {
		t.Fatalf("err = %v, want ErrLicenseExpired", err)
	}
	st := gate.LicenseStatus()
	if st.Valid || !st.Present || st.Error == "" {
		t.Fatalf("status = %+v, want present-but-invalid with a reason", st)
	}
	// MIT still loads: an expired enterprise license must not brick the core.
	if err := gate.Entitled(Info{ID: "tools.demo", License: LicenseMIT}); err != nil {
		t.Fatalf("MIT refused under an expired license: %v", err)
	}
}

func TestPerpetualLicenseNeverExpires(t *testing.T) {
	gate := NewLicenseGate(LicenseClaims{Customer: "ACME", Entitlements: Entitlements{"*"}}, "test")
	gate.SetClock(func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) })
	if err := gate.Entitled(Info{ID: "memory.sink.corp", License: LicenseEnterprise}); err != nil {
		t.Fatalf("perpetual license expired: %v", err)
	}
}

func TestUnlicensedGateReportsTheReason(t *testing.T) {
	gate := NewUnlicensedGate("/etc/pando/license.json", errors.New("open: no such file"))
	err := gate.Entitled(Info{ID: "memory.sink.corp", License: LicenseEnterprise})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("err = %v, want the underlying reason", err)
	}
	if st := gate.LicenseStatus(); st.Valid || st.Error == "" || st.Source == "" {
		t.Fatalf("status = %+v, want an invalid status carrying source and error", st)
	}
}

func base64Std(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
