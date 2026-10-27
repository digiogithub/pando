package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

type mlsFake struct {
	mu     sync.Mutex
	calls  []string
	bodies map[string][]byte
}

func (f *mlsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.RequestURI()
	if r.URL.Path == "/api/v1/token" {
		fmt.Fprint(w, `{"token":"t"}`)
		return
	}
	f.calls = append(f.calls, key)
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			f.bodies[key] = b
		}
	}
	switch key {
	case "GET /api/v1/config/mcp-servers":
		fmt.Fprint(w, `{"mcpServers":[{"name":"gh","command":"","args":[],"env":[],"type":"streamable-http","url":"http://x/mcp","headers":{},
			"running":true,"tools":[{"name":"search","description":"find"}],"auth":{"type":"oauth","hasToken":false,"hasPassword":false},"authStatus":{"type":"oauth","hasTokens":false,"expired":false,"dynamicallyRegistered":false}}]}`)
	case "POST /api/v1/config/mcp-servers/gh/reload":
		fmt.Fprint(w, `{"status":"ok","name":"gh","tools":3}`)
	case "GET /api/v1/config/lsp":
		fmt.Fprint(w, `{"lsp":[{"language":"go","command":"gopls","args":[],"languages":[".go"],"filenames":[],"disabled":false,"autostart":false}],
			"activation":{"autoActivate":true,"activateOn":"edits","autoInstall":true,"runner":"auto","startupTimeout":"20s","installTimeout":"2m0s"}}`)
	case "GET /api/v1/config/lsp/catalog":
		fmt.Fprint(w, `{"servers":[{"name":"go","command":"gopls","configured":true,"availability":"installed","availabilityLabel":"installed","runState":"ready"},
			{"name":"rust","description":"rust-analyzer","command":"rust-analyzer","configured":false,"availability":"manual","availabilityLabel":"manual","runState":"stopped"}]}`)
	case "PUT /api/v1/config/lsp/activation", "PUT /api/v1/config/extensions":
		w.Write(f.bodies[key])
	case "GET /api/v1/config/extensions":
		fmt.Fprint(w, `{"skills":{"enabled":true,"paths":[]},"skillsCatalog":{"enabled":true,"baseUrl":"https://skills.sh","autoUpdate":false,"defaultScope":"global"},"lua":{"enabled":false}}`)
	case "GET /api/v1/skills/installed":
		fmt.Fprint(w, `{"skills":[{"name":"demo","description":"d","version":"1","source":"o/r","scope":"global","active":true}]}`)
	case "GET /api/v1/skills/catalog?q=gi":
		fmt.Fprint(w, `{"skills":[{"id":"1","skillId":"git","name":"git-helper","installs":1500,"source":"o/git"}]}`)
	case "DELETE /api/v1/skills/demo", "PUT /api/v1/config/mcp-servers", "PUT /api/v1/config/lsp", "POST /api/v1/skills/install":
		fmt.Fprint(w, `{}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *mlsFake) called(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == key {
			return true
		}
	}
	return false
}

type mlsH struct {
	t   *testing.T
	s   *Settings
	tt  *ui.Tester
	q   *app.QueueDispatcher
	srv *mlsFake
}

func newMLS(t *testing.T) *mlsH {
	f := &mlsFake{bodies: map[string][]byte{}}
	hs := httptest.NewServer(f)
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
	return &mlsH{t: t, s: s, q: q, srv: f, tt: ui.NewTester(func(c *ui.Context) { s.Build(c, nil) }, 1100, 800)}
}

func (h *mlsH) wait(cond func() bool) {
	h.t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if h.q.Drain() > 0 {
			h.tt.Frame()
		}
		if cond() {
			return
		}
	}
	h.t.Fatalf("timed out; texts %q", h.tt.Texts())
}

func TestMCPArgsRoundTrip(t *testing.T) {
	in := `--port 3000 --workspace "/path with spaces" 'a b' x\ y ""`
	args := mcpParseArgs(in)
	want := []string{"--port", "3000", "--workspace", "/path with spaces", "a b", "x y", ""}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %q", args)
	}
	if got := mcpParseArgs(mcpFormatArgs(args)); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("round trip = %q", got)
	}
}

func TestMCPServersListReloadAndSave(t *testing.T) {
	h := newMLS(t)
	h.s.Select("mcp-servers")
	h.tt.Frame()
	h.wait(func() bool { return h.tt.HasText("gh") && h.tt.HasText("1 tools") })
	if !h.tt.HasText("needs login") || !h.tt.HasText("Running") {
		t.Errorf("badges missing: %q", h.tt.Texts())
	}
	sec := h.s.section("mcp-servers").(*mcpSection)
	sec.busy = ""
	sec.act("gh", func(ctx context.Context) (string, error) {
		n, err := h.s.app.Client.ReloadMCPServer(ctx, "gh")
		return fmt.Sprint(n), err
	})
	h.wait(func() bool { return h.srv.called("POST /api/v1/config/mcp-servers/gh/reload") })
	// New server through the dialog form.
	sec.openForm(nil)
	sec.form.name, sec.form.typ, sec.form.url = "ext", "sse", "http://h/sse"
	sec.form.authType, sec.form.token = "bearer", "tok"
	sec.form.headers = []crKV{{"X-A", "1"}}
	h.tt.Frame()
	sec.save()
	h.wait(func() bool { return h.srv.called("PUT /api/v1/config/mcp-servers") })
	var got map[string]any
	_ = json.Unmarshal(h.srv.bodies["PUT /api/v1/config/mcp-servers"], &got)
	auth, _ := got["auth"].(map[string]any)
	if got["name"] != "ext" || got["type"] != "sse" || auth["token"] != "tok" || auth["type"] != "bearer" {
		t.Errorf("put body = %v", got)
	}
}

func TestLSPActivationCatalogAndSave(t *testing.T) {
	h := newMLS(t)
	h.s.Select("lsp")
	h.tt.Frame()
	h.wait(func() bool { return h.tt.HasText("gopls") || h.tt.HasText("Configured servers") })
	sec := h.s.section("lsp").(*lspSection)
	h.wait(func() bool { return sec.loaded })
	if len(sec.catalog) != 2 {
		t.Fatalf("catalog = %d", len(sec.catalog))
	}
	act := sec.act
	act.AutoInstall = false
	sec.saveActivation(act)
	h.wait(func() bool { return h.srv.called("PUT /api/v1/config/lsp/activation") })
	var a client.LSPActivation
	_ = json.Unmarshal(h.srv.bodies["PUT /api/v1/config/lsp/activation"], &a)
	if a.AutoInstall || a.ActivateOn != "edits" {
		t.Errorf("activation body = %+v", a)
	}
	// Enable a catalogue preset.
	sec.openForm(nil, sec.statusOf("rust"))
	if sec.form.command != "rust-analyzer" || sec.form.language != "rust" {
		t.Errorf("preset form = %+v", sec.form)
	}
	sec.saveForm()
	h.wait(func() bool { return h.srv.called("PUT /api/v1/config/lsp") })
}

func TestSkillsInstalledSearchAndConfigDirty(t *testing.T) {
	h := newMLS(t)
	h.s.Select("skills")
	h.tt.Frame()
	sec := h.s.section("skills").(*skillsSection)
	h.wait(func() bool { return sec.loaded && len(sec.installed) == 1 })
	if !h.tt.HasText("demo v1") {
		t.Errorf("installed skill not shown: %q", h.tt.Texts())
	}
	if sec.Dirty() {
		t.Error("fresh skills config is dirty")
	}
	sec.vals["skills.enabled"].b = false
	if !sec.Dirty() {
		t.Error("edit not detected")
	}
	ok := false
	sec.Save(func(b bool) { ok = b })
	h.wait(func() bool { return ok })
	var body map[string]any
	_ = json.Unmarshal(h.srv.bodies["PUT /api/v1/config/extensions"], &body)
	if sk, _ := body["skills"].(map[string]any); sk["enabled"] != false {
		t.Errorf("extensions put = %v", body)
	}
	if _, kept := body["lua"]; !kept {
		t.Error("whole extensions document must be sent")
	}
	sec.search = "gi"
	sec.runSearch()
	h.wait(func() bool { return len(sec.hits) == 1 })
	sec.install(sec.hits[0])
	h.wait(func() bool { return h.srv.called("POST /api/v1/skills/install") })
	var ib map[string]string
	_ = json.Unmarshal(h.srv.bodies["POST /api/v1/skills/install"], &ib)
	if ib["skillId"] != "git" || ib["scope"] != "global" {
		t.Errorf("install body = %v", ib)
	}
}
