package extension

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

// TestHostConfigCamelCaseKeyIsReachable is the regression test for the defect
// this file exists to prevent: the configuration system lowercases every key it
// reads, so a subtree an author wrote as
//
//	[Extensions.Entries."tools.a".Config]
//	baseURL = "https://example.test"
//
// reaches the extension as Raw["baseurl"]. Before the accessors folded the key
// they were asked for, host.String("baseURL", "") could never find that value,
// and the extension silently ran on its defaults.
func TestHostConfigCamelCaseKeyIsReachable(t *testing.T) {
	// Exactly what the configuration system produces from the file above.
	host := HostServices{ID: "tools.a", Raw: map[string]any{
		"baseurl":     "https://example.test",
		"clientid":    "device-1",
		"usekeyring":  true,
		"maxretries":  int64(4),
		"httptimeout": "30s",
		"scopes":      []any{"org", "device"},
	}}

	if got := host.String("baseURL", ""); got != "https://example.test" {
		t.Errorf("String(baseURL) = %q, want the configured value", got)
	}
	if got := host.String("clientID", ""); got != "device-1" {
		t.Errorf("String(clientID) = %q", got)
	}
	if got := host.Bool("useKeyring", false); !got {
		t.Error("Bool(useKeyring) did not find the configured value")
	}
	if got := host.Int("maxRetries", 0); got != 4 {
		t.Errorf("Int(maxRetries) = %d, want 4", got)
	}
	if got := host.Duration("httpTimeout", time.Second); got != 30*time.Second {
		t.Errorf("Duration(httpTimeout) = %v, want 30s", got)
	}
	if got := host.StringSlice("scopes", nil); !reflect.DeepEqual(got, []string{"org", "device"}) {
		t.Errorf("StringSlice(scopes) = %v", got)
	}
}

// TestHostConfigAnySpellingResolves covers the whole accessor surface against
// every spelling of the same key, so no accessor can drift away from the rest.
func TestHostConfigAnySpellingResolves(t *testing.T) {
	host := HostServices{ID: "tools.a", Raw: map[string]any{
		"baseurl":  "https://example.test",
		"enabled":  true,
		"retries":  int64(3),
		"ratio":    0.5,
		"timeout":  "45s",
		"scopes":   []string{"org"},
		"headers":  map[string]any{"X-Trace": "on"},
		"MixedRaw": "kept", // a host that built Raw itself, unfolded.
	}}

	for _, spelling := range []string{"baseURL", "BASEURL", "baseurl", "BaseUrl"} {
		if got := host.String(spelling, ""); got != "https://example.test" {
			t.Errorf("String(%q) = %q", spelling, got)
		}
	}
	for _, spelling := range []string{"Enabled", "ENABLED", "enabled"} {
		if !host.Bool(spelling, false) {
			t.Errorf("Bool(%q) did not resolve", spelling)
		}
	}
	for _, spelling := range []string{"Retries", "RETRIES", "retries"} {
		if got := host.Int(spelling, 0); got != 3 {
			t.Errorf("Int(%q) = %d", spelling, got)
		}
	}
	if got := host.Float64("Ratio", 0); got != 0.5 {
		t.Errorf("Float64(Ratio) = %v", got)
	}
	if got := host.Duration("TimeOut", 0); got != 45*time.Second {
		t.Errorf("Duration(TimeOut) = %v", got)
	}
	if got := host.StringSlice("SCOPES", nil); !reflect.DeepEqual(got, []string{"org"}) {
		t.Errorf("StringSlice(SCOPES) = %v", got)
	}
	if got := host.Map("Headers"); got == nil || got["X-Trace"] != "on" {
		t.Errorf("Map(Headers) = %v; nested keys must be handed over untouched", got)
	}
	// An unfolded Raw key is still reachable, which is what keeps a host that
	// builds the map by hand working.
	if got := host.String("mixedRaw", ""); got != "kept" {
		t.Errorf("String(mixedRaw) = %q, want the unfolded entry", got)
	}
	if _, ok := host.Lookup("absent"); ok {
		t.Error("Lookup reported a key that is not configured")
	}
}

// TestHostConfigCoercesAcrossEncodings checks the representations one option can
// arrive in: TOML gives int64, JSON gives float64 and the environment gives a
// string. All three must reach the same value.
func TestHostConfigCoercesAcrossEncodings(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"toml", map[string]any{"retries": int64(3), "enabled": true, "timeout": "2s"}},
		{"json", map[string]any{"retries": float64(3), "enabled": true, "timeout": "2s"}},
		{"strings", map[string]any{"retries": "3", "enabled": "true", "timeout": "2s"}},
		{"seconds", map[string]any{"retries": 3, "enabled": true, "timeout": int64(2)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := HostServices{ID: "tools.a", Raw: tc.raw}
			if got := host.Int("retries", 0); got != 3 {
				t.Errorf("Int = %d, want 3", got)
			}
			if !host.Bool("enabled", false) {
				t.Error("Bool did not resolve")
			}
			if got := host.Duration("timeout", 0); got != 2*time.Second {
				t.Errorf("Duration = %v, want 2s", got)
			}
		})
	}
}

// TestHostConfigDefaultsWhenAbsentOrWrongType checks that a missing key and a
// key of the wrong type both fall back rather than returning a zero value.
func TestHostConfigDefaultsWhenAbsentOrWrongType(t *testing.T) {
	host := HostServices{ID: "tools.a", Raw: map[string]any{
		"retries": []any{1, 2},
		"baseurl": "",
	}}
	if got := host.Int("retries", 7); got != 7 {
		t.Errorf("Int on a list = %d, want the default", got)
	}
	if got := host.String("baseURL", "fallback"); got != "fallback" {
		t.Errorf("String on an empty value = %q, want the default", got)
	}
	if got := host.StringSlice("missing", []string{"d"}); !reflect.DeepEqual(got, []string{"d"}) {
		t.Errorf("StringSlice = %v, want the default", got)
	}
	if got := host.Map("missing"); got != nil {
		t.Errorf("Map = %v, want nil", got)
	}
	if got := host.Duration("missing", time.Minute); got != time.Minute {
		t.Errorf("Duration = %v, want the default", got)
	}
	if got := host.Float64("missing", 1.5); got != 1.5 {
		t.Errorf("Float64 = %v, want the default", got)
	}
}

// TestHostConfigEnvironmentOverride proves that a container can configure an
// extension with no configuration file at all, and that the environment wins
// over a file the way it does for core settings.
func TestHostConfigEnvironmentOverride(t *testing.T) {
	host := HostServices{ID: "control.acme", Raw: map[string]any{"baseurl": "https://from-file.test"}}

	if got := host.ConfigEnvVar("baseURL"); got != "PANDO_EXT_CONTROL_ACME_BASEURL" {
		t.Fatalf("ConfigEnvVar = %q", got)
	}

	t.Setenv("PANDO_EXT_CONTROL_ACME_BASEURL", "https://from-env.test")
	t.Setenv("PANDO_EXT_CONTROL_ACME_MAXRETRIES", "9")
	t.Setenv("PANDO_EXT_CONTROL_ACME_USEKEYRING", "true")
	t.Setenv("PANDO_EXT_CONTROL_ACME_SCOPES", "org, device")

	if got := host.String("baseURL", ""); got != "https://from-env.test" {
		t.Errorf("String(baseURL) = %q, want the environment override", got)
	}
	if got := host.Int("maxRetries", 0); got != 9 {
		t.Errorf("Int(maxRetries) = %d, want 9", got)
	}
	if !host.Bool("useKeyring", false) {
		t.Error("Bool(useKeyring) did not read the environment")
	}
	if got := host.StringSlice("scopes", nil); !reflect.DeepEqual(got, []string{"org", "device"}) {
		t.Errorf("StringSlice(scopes) = %v, want the comma-separated environment value", got)
	}

	// A host that set no ID has no namespace to read overrides from.
	anon := HostServices{Raw: map[string]any{"baseurl": "https://from-file.test"}}
	if got := anon.ConfigEnvVar("baseURL"); got != "" {
		t.Errorf("ConfigEnvVar with no ID = %q, want empty", got)
	}
	if got := anon.String("baseURL", ""); got != "https://from-file.test" {
		t.Errorf("String with no ID = %q, want the file value", got)
	}
}

// TestManagerFoldsConfigKeysAndWarnsOnCollision checks the manager half: the
// subtree an extension receives is folded, so indexing Raw directly agrees with
// the accessors, and two spellings of one option are reported rather than
// resolved by map iteration order.
func TestManagerFoldsConfigKeysAndWarnsOnCollision(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var calls []string
	reg := NewRegistry()
	inst := registerRecorder(reg, "tools.a", &calls, nil)

	m := NewManager(Options{
		Registry: reg,
		Logger:   log,
		Entries: map[string]Entry{"tools.a": {Enabled: true, Config: map[string]any{
			"baseURL": "https://camel.test",
			"BASEURL": "https://shout.test",
			"nested":  map[string]any{"X-Trace": "on"},
		}}},
	})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := inst.gotRaw["baseURL"]; ok {
		t.Error("Raw still carries an unfolded key")
	}
	got, ok := inst.gotRaw["baseurl"]
	if !ok {
		t.Fatalf("Raw = %#v, want a folded baseurl key", inst.gotRaw)
	}
	// The lexicographically first spelling wins, so the outcome does not depend
	// on map iteration order.
	if got != "https://shout.test" {
		t.Errorf("Raw[baseurl] = %v, want the deterministic winner", got)
	}
	if nested, ok := inst.gotRaw["nested"].(map[string]any); !ok || nested["X-Trace"] != "on" {
		t.Errorf("nested table was not handed over untouched: %#v", inst.gotRaw["nested"])
	}
	if !bytes.Contains(buf.Bytes(), []byte("differ only in case")) {
		t.Errorf("no collision warning was logged; log was %q", buf.String())
	}
}
