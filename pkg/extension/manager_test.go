package extension

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// recorder is an extension that records the lifecycle calls it receives.
type recorder struct {
	info Info
	log  *[]string

	provisionErr error
	validateErr  error
	panicOn      string
	gotRaw       map[string]any
}

func (r *recorder) ExtensionInfo() Info { return r.info }

func (r *recorder) Provision(_ context.Context, host HostServices) error {
	*r.log = append(*r.log, "provision:"+string(r.info.ID))
	r.gotRaw = host.Raw
	if r.panicOn == "provision" {
		panic("boom")
	}
	return r.provisionErr
}

func (r *recorder) Validate() error {
	*r.log = append(*r.log, "validate:"+string(r.info.ID))
	return r.validateErr
}

func (r *recorder) Cleanup() error {
	*r.log = append(*r.log, "cleanup:"+string(r.info.ID))
	if r.panicOn == "cleanup" {
		panic("boom")
	}
	return nil
}

func (r *recorder) Start(context.Context) error {
	*r.log = append(*r.log, "start:"+string(r.info.ID))
	return nil
}

func (r *recorder) Stop(context.Context) error {
	*r.log = append(*r.log, "stop:"+string(r.info.ID))
	return nil
}

// registerRecorder registers a recorder and returns the live instance the
// factory will hand to the manager.
func registerRecorder(r *Registry, id ID, log *[]string, tweak func(*recorder)) *recorder {
	inst := &recorder{info: Info{ID: id, Name: string(id), Version: "1.0.0"}, log: log}
	if tweak != nil {
		tweak(inst)
	}
	inst.info.New = func() Extension { return inst }
	r.Register(inst)
	return inst
}

func quietManager(reg *Registry, opts Options) *Manager {
	opts.Registry = reg
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return NewManager(opts)
}

func TestManagerLoadRunsLifecycleInOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.a", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"provision:tools.a", "validate:tools.a"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("lifecycle calls = %v, want %v", calls, want)
	}
	if !m.Loaded("tools.a") {
		t.Error("extension is not reported as loaded")
	}
}

func TestManagerValidateFailureTriggersCleanup(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.a", &calls, func(r *recorder) {
		r.validateErr = errors.New("bad config")
	})

	m := quietManager(reg, Options{})
	err := m.Load(context.Background())
	if err == nil {
		t.Fatal("Load returned nil, want the validation error")
	}
	if m.Loaded("tools.a") {
		t.Error("extension that failed validation is reported as loaded")
	}
	if len(calls) != 3 || calls[2] != "cleanup:tools.a" {
		t.Errorf("cleanup was not called after validation failure: %v", calls)
	}

	sts := m.Statuses()
	if len(sts) != 1 || sts[0].Err == nil || sts[0].Loaded {
		t.Errorf("status not recorded as failed: %+v", sts)
	}
}

func TestManagerPanicInProvisionIsContained(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.a", &calls, func(r *recorder) { r.panicOn = "provision" })
	registerRecorder(reg, "tools.b", &calls, nil)

	m := quietManager(reg, Options{})
	err := m.Load(context.Background())
	if err == nil {
		t.Fatal("Load returned nil, want the panic converted to an error")
	}
	if m.Loaded("tools.a") {
		t.Error("panicking extension is reported as loaded")
	}
	if !m.Loaded("tools.b") {
		t.Error("a panicking extension prevented a healthy one from loading")
	}
}

func TestManagerDisabledList(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.a", &calls, nil)

	m := quietManager(reg, Options{
		Disabled: []string{"tools.a"},
		Entries:  map[string]Entry{"tools.a": {Enabled: true}},
	})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Loaded("tools.a") {
		t.Error("Disabled did not override an explicit Enabled entry")
	}
	if len(calls) != 0 {
		t.Errorf("disabled extension was still provisioned: %v", calls)
	}
}

func TestManagerEntryDisablesAndConfigEnables(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.off", &calls, nil)
	registerRecorder(reg, "tools.cfg", &calls, nil)

	m := quietManager(reg, Options{Entries: map[string]Entry{
		"tools.off": {Enabled: false},
		"tools.cfg": {Enabled: false, Config: map[string]any{"endpoint": "https://example.test"}},
	}})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Loaded("tools.off") {
		t.Error("an entry with Enabled=false and no config should not load")
	}
	if !m.Loaded("tools.cfg") {
		t.Error("an entry with config should load even with Enabled=false")
	}
}

func TestManagerNonMITRequiresExplicitEnable(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "memory.sink.corp", &calls, func(r *recorder) {
		r.info.License = LicenseEnterprise
	})

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Loaded("memory.sink.corp") {
		t.Error("an enterprise extension loaded without being enabled in config")
	}

	m2 := quietManager(reg, Options{Entries: map[string]Entry{"memory.sink.corp": {Enabled: true}}})
	if err := m2.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m2.Loaded("memory.sink.corp") {
		t.Error("an explicitly enabled enterprise extension did not load")
	}
}

func TestManagerConfigReachesExtension(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	inst := registerRecorder(reg, "tools.a", &calls, nil)

	m := quietManager(reg, Options{Entries: map[string]Entry{
		"tools.a": {Enabled: true, Config: map[string]any{"endpoint": "https://example.test", "retries": int64(3)}},
	}})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	host := HostServices{Raw: inst.gotRaw}
	if got := host.String("endpoint", ""); got != "https://example.test" {
		t.Errorf("String(endpoint) = %q", got)
	}
	if got := host.Int("retries", 0); got != 3 {
		t.Errorf("Int(retries) = %d, want 3", got)
	}
	if got := host.Bool("missing", true); !got {
		t.Error("Bool did not fall back to the default")
	}
}

func TestManagerDependencyOrdering(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	// "z.dependent" sorts after its dependency alphabetically only by accident;
	// use IDs where alphabetical order is the wrong order.
	registerRecorder(reg, "a.dependent", &calls, func(r *recorder) {
		r.info.RequiresExtensions = []ID{"z.base"}
	})
	registerRecorder(reg, "z.base", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if calls[0] != "provision:z.base" {
		t.Errorf("dependency was not loaded first: %v", calls)
	}
	if !m.Loaded("a.dependent") {
		t.Error("dependent extension did not load")
	}
}

func TestManagerMissingDependencyReportsError(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "a.dependent", &calls, func(r *recorder) {
		r.info.RequiresExtensions = []ID{"absent.base"}
	})

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err == nil {
		t.Fatal("Load returned nil, want a missing-dependency error")
	}
	if m.Loaded("a.dependent") {
		t.Error("extension with an unmet dependency was loaded")
	}
}

func TestManagerStartStopAndCleanupOrder(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "a.first", &calls, nil)
	registerRecorder(reg, "b.second", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	calls = calls[:0]

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if calls[0] != "start:a.first" || calls[1] != "start:b.second" {
		t.Errorf("Start ran out of load order: %v", calls)
	}

	calls = calls[:0]
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if calls[0] != "stop:b.second" || calls[1] != "stop:a.first" {
		t.Errorf("Stop did not run in reverse load order: %v", calls)
	}

	calls = calls[:0]
	m.Cleanup()
	if len(calls) != 2 || calls[0] != "cleanup:b.second" || calls[1] != "cleanup:a.first" {
		t.Errorf("Cleanup did not run in reverse load order: %v", calls)
	}
	if m.Loaded("a.first") || m.Loaded("b.second") {
		t.Error("extensions still reported as loaded after Cleanup")
	}
	// Cleanup must be idempotent.
	m.Cleanup()
}

func TestManagerCleanupPanicIsContained(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "a.bad", &calls, func(r *recorder) { r.panicOn = "cleanup" })
	registerRecorder(reg, "b.good", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	calls = calls[:0]
	m.Cleanup() // must not panic
	if len(calls) != 2 {
		t.Errorf("cleanup did not reach every extension: %v", calls)
	}
}

func TestManagerUnload(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.a", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := m.Unload("tools.a"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if m.Loaded("tools.a") {
		t.Error("extension still loaded after Unload")
	}
	if err := m.Unload("tools.a"); err == nil {
		t.Error("Unload of an unloaded extension returned nil error")
	}
}

// capTool is an extension implementing ToolProvider, used to check capability
// discovery.
type capTool struct {
	info Info
}

func (c capTool) ExtensionInfo() Info { return c.info }
func (c capTool) Tools() []Tool       { return nil }

func TestCapabilityDiscovery(t *testing.T) {
	reg := NewRegistry()
	info := Info{ID: "tools.cap", Name: "cap", Version: "1.0.0"}
	info.New = func() Extension { return capTool{info: info} }
	reg.Register(capTool{info: info})

	var calls []string
	registerRecorder(reg, "tools.plain", &calls, nil)

	m := quietManager(reg, Options{})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	providers := Capability[ToolProvider](m)
	if len(providers) != 1 {
		t.Fatalf("Capability found %d ToolProviders, want 1", len(providers))
	}
	if providers[0].ExtensionInfo().ID != "tools.cap" {
		t.Errorf("wrong extension discovered: %s", providers[0].ExtensionInfo().ID)
	}
	if got := Capability[ToolProvider](nil); got != nil {
		t.Error("Capability on a nil manager should return nil")
	}
}

func TestManagerStatusesCoverEveryRegisteredExtension(t *testing.T) {
	var calls []string
	reg := NewRegistry()
	registerRecorder(reg, "tools.on", &calls, nil)
	registerRecorder(reg, "tools.off", &calls, nil)

	m := quietManager(reg, Options{Disabled: []string{"tools.off"}})
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	sts := m.Statuses()
	if len(sts) != 2 {
		t.Fatalf("Statuses returned %d entries, want 2", len(sts))
	}
	// Statuses is sorted by ID, so "tools.off" comes first.
	if !sts[0].Disabled || sts[0].Info.ID != "tools.off" {
		t.Errorf("unexpected first status: %+v", sts[0])
	}
	if !sts[1].Loaded || sts[1].Info.ID != "tools.on" {
		t.Errorf("unexpected second status: %+v", sts[1])
	}
	if sts[0].String() == "" {
		t.Error("Status.String returned empty")
	}
}
