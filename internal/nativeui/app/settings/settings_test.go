package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

// fakeServer serves config documents and records PUT bodies.
type fakeServer struct {
	mu   sync.Mutex
	docs map[string]string
	puts map[string][]byte
}

func newFake() *fakeServer {
	return &fakeServer{
		docs: map[string]string{
			"/api/v1/settings": `{"home_directory":"/h","working_directory":"/w","default_model":"m","desktop_tray_on_minimize_close":false,
				"tool_discovery_enabled":true,"tool_discovery_mode":"auto","tool_discovery_max_direct_tools":64,"theme":"pando-light",
				"telemetry_available":true,"telemetry_enabled":false,"future_unknown_key":{"a":1},"big":1000000}`,
			"/api/v1/config/bash":    `{"bannedCommands":["rm"],"allowedCommands":[]}`,
			"/api/v1/config/sandbox": `{"config":{"mode":"","network":"restricted","writableRoots":["/tmp"]},"status":"active"}`,
			"/api/v1/config/services": `{"snapshots":{"enabled":true,"maxSnapshots":50,"maxFileSize":"10MB","autoCleanupDays":30,"excludePatterns":[]},
				"server":{"enabled":true,"host":"localhost","port":9999,"requireAuth":false},"mesnada":{"enabled":false}}`,
		},
		puts: map[string][]byte{},
	}
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/v1/token" {
		fmt.Fprint(w, `{"token":"t"}`)
		return
	}
	if r.Method == http.MethodPut {
		b, _ := io.ReadAll(r.Body)
		f.puts[r.URL.Path] = b
		w.Write(b)
		return
	}
	if d, ok := f.docs[r.URL.Path]; ok {
		fmt.Fprint(w, d)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeServer) put(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m map[string]any
	if b := f.puts[path]; b != nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

type harness struct {
	t   *testing.T
	s   *Settings
	tt  *ui.Tester
	q   *app.QueueDispatcher
	srv *fakeServer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := newFake()
	hs := httptest.NewServer(fake)
	t.Cleanup(hs.Close)
	c, err := client.New(client.Options{BaseURL: hs.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	s := New(a)
	h := &harness{t: t, s: s, q: q, srv: fake}
	h.tt = ui.NewTester(func(c *ui.Context) { s.Build(c, nil) }, 1100, 800)
	return h
}

// settle drains goroutine updates until cond holds.
func (h *harness) wait(cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.q.Drain() > 0 {
			h.tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("timed out; texts %q", h.tt.Texts())
}

func TestEveryCategoryBuilds(t *testing.T) {
	h := newHarness(t)
	for _, cat := range h.s.cats {
		h.s.open(cat.id)
		for range 3 {
			h.tt.Frame()
			h.q.Drain()
		}
		if h.s.section(cat.id) == nil {
			t.Errorf("category %s has no section", cat.id)
		}
	}
	if n := len(h.s.cats); n != 23 {
		t.Errorf("categories = %d, want 23", n)
	}
}

func TestGeneralSavesTrayOptionAndKeepsUnknownKeys(t *testing.T) {
	h := newHarness(t)
	h.tt.Frame()
	sec := h.s.section("general").(*docPage)
	h.wait(func() bool { return sec.loaded && h.tt.HasText("Tray") || sec.loaded })
	if sec.Dirty() {
		t.Fatal("fresh document is dirty")
	}
	sec.vals["desktop_tray_on_minimize_close"].b = true
	sec.vals["tool_discovery_max_direct_tools"].s = "80"
	h.tt.Frame()
	if !sec.Dirty() {
		t.Fatal("edit not detected")
	}
	ok := false
	sec.Save(func(b bool) { ok = b })
	h.wait(func() bool { return ok })
	put := h.srv.put("/api/v1/settings")
	if put["desktop_tray_on_minimize_close"] != true {
		t.Errorf("tray option not sent: %v", put)
	}
	if put["tool_discovery_max_direct_tools"] != float64(80) {
		t.Errorf("number not sent: %v", put["tool_discovery_max_direct_tools"])
	}
	if _, ok := put["future_unknown_key"]; !ok {
		t.Error("unknown key lost in the round trip")
	}
	b, _ := json.Marshal(put["big"])
	if string(b) != "1000000" {
		t.Errorf("large number rewritten: %s", b)
	}
	if sec.Dirty() {
		t.Error("still dirty after save")
	}
}

func TestSubDocumentsPutShape(t *testing.T) {
	h := newHarness(t)
	// Sandbox: only the inner config is sent; "workspace-write" is stored as "".
	h.s.open("sandbox")
	sb := h.s.section("sandbox").(*docPage)
	h.wait(func() bool { return sb.loaded })
	sb.vals["mode"].sel = h.s.app.T("settings.sandbox.modeReadOnly")
	done := false
	sb.Save(func(bool) { done = true })
	h.wait(func() bool { return done })
	put := h.srv.put("/api/v1/config/sandbox")
	if put["mode"] != "read-only" || put["network"] != "restricted" {
		t.Errorf("sandbox put = %v", put)
	}
	if _, wrapped := put["config"]; wrapped {
		t.Error("sandbox body must be the config object only")
	}
	// Snapshots live inside the services document, which is sent whole.
	h.s.open("snapshots")
	sn := h.s.section("snapshots").(*docPage)
	h.wait(func() bool { return sn.loaded })
	sn.vals["maxSnapshots"].s = "10"
	done = false
	sn.Save(func(bool) { done = true })
	h.wait(func() bool { return done })
	svc := h.srv.put("/api/v1/config/services")
	snap, _ := svc["snapshots"].(map[string]any)
	if snap["maxSnapshots"] != float64(10) {
		t.Errorf("services put = %v", svc)
	}
	if _, ok := svc["server"]; !ok {
		t.Error("services body must keep the other subtrees")
	}
}

func TestUnsavedGuardOnCategorySwitch(t *testing.T) {
	h := newHarness(t)
	h.tt.Frame()
	sec := h.s.section("general").(*docPage)
	h.wait(func() bool { return sec.loaded })
	sec.vals["debug"].b = true
	h.s.Select("bash")
	h.tt.Frame()
	if !h.s.dialog || h.s.active != "general" {
		t.Fatalf("switch must ask first (dialog=%v active=%s)", h.s.dialog, h.s.active)
	}
	if !h.tt.HasText("Unsaved changes") {
		t.Errorf("dialog not shown: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Discard changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.s.active != "bash" || sec.Dirty() {
		t.Errorf("discard should switch (active=%s dirty=%v)", h.s.active, sec.Dirty())
	}
	// A guarded leave of the whole area.
	h.wait(func() bool { return h.s.section("bash").St().loaded })
	bash := h.s.section("bash").(*docPage)
	bash.vals["bannedCommands"].l = append(bash.vals["bannedCommands"].l, "dd")
	left := false
	if h.s.ConfirmLeave(func() { left = true }) {
		t.Error("ConfirmLeave must hold with unsaved edits")
	}
	h.tt.Frame()
	if err := h.tt.Click("Save and continue"); err != nil {
		t.Fatal(err)
	}
	h.wait(func() bool { return left })
	if got, _ := h.srv.put("/api/v1/config/bash")["bannedCommands"].([]any); len(got) != 2 {
		t.Errorf("bash put = %v", got)
	}
}

func TestDeepLinkParam(t *testing.T) {
	h := newHarness(t)
	h.s.Select("mcp-gateway")
	if h.s.active != "mcp-gateway" {
		t.Errorf("active = %s", h.s.active)
	}
	if got := pathsOverlap("mcpServers", "mcpServers.github"); !got {
		t.Error("policy path overlap")
	}
}
