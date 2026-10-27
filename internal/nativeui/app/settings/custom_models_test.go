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

type cmFake struct {
	mu   sync.Mutex
	puts map[string]map[string]any
}

func (f *cmFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/token":
		fmt.Fprint(w, `{"token":"t"}`)
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		f.puts[r.URL.Path] = m
		if r.URL.Path == "/api/v1/config/model-auto-mode" && len(m) > 0 {
			if rs, _ := m["routes"].([]any); len(rs) > 0 {
				if r0, _ := rs[0].(map[string]any); r0["id"] == "" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid","errors":[{"field":"modelAutoMode.routes[0].id","message":"id required"}]}`)
					return
				}
			}
		}
		w.Write(b)
	case r.URL.Path == "/api/v1/config/model-auto-mode":
		fmt.Fprint(w, `{"enabled":true,"defaultAuto":false,"threshold":0.6,"minConfidence":0,"historyPrompts":2,"routes":[{"id":"impl","description":"d","model":"m1","fallbacks":["m2"],"disabled":false}]}`)
	case r.URL.Path == "/api/v1/models":
		fmt.Fprint(w, `{"models":[{"id":"auto"},{"id":"m1"},{"id":"m2"}]}`)
	case r.URL.Path == "/api/v1/config/decision-model":
		fmt.Fprint(w, `{"router":{"provider":"ollama","baseURL":"","model":"tev1","keepAlive":"30m","headers":{},"effectiveBaseURL":"http://localhost:11434"},"timeoutMs":0}`)
	case r.URL.Path == "/api/v1/decision-model/router/health":
		fmt.Fprint(w, `{"ok":true,"report":{"ok":true}}`)
	case r.URL.Path == "/api/v1/decision-model/router/test":
		fmt.Fprint(w, `{"ok":true,"report":{"reachable":true,"authorized":true,"modelPresent":true,"isDecisionModel":true,"latencyMs":5}}`)
	case r.URL.Path == "/api/v1/model-auto-mode/playground":
		fmt.Fprint(w, `{"decision":{"routeId":"impl","matched":true,"reason":"ok","probabilities":{"impl":0.9},"latencyMs":3}}`)
	case r.URL.Path == "/api/v1/mesnada/engines":
		fmt.Fprint(w, `{"engines":[{"name":"pando","label":"Pando","kind":"native","locked":true,"enabled":true},{"name":"claude","label":"Claude","kind":"external","binary":"claude","installed":true,"path":"/bin/claude","enabled":true}]}`)
	case r.URL.Path == "/api/v1/config/services":
		fmt.Fprint(w, `{"mesnada":{"enabled":true,"server":{"host":"localhost","port":9090},"orchestrator":{"maxParallel":4,"defaultEngine":"pando","disabledEngines":["gemini"]},"acp":{"server":{}},"tui":{}},"server":{"port":9999}}`)
	default:
		http.NotFound(w, r)
	}
}

func cmHarness(t *testing.T) (*Settings, *ui.Tester, *app.QueueDispatcher, *cmFake) {
	t.Helper()
	f := &cmFake{puts: map[string]map[string]any{}}
	hs := httptest.NewServer(f)
	t.Cleanup(hs.Close)
	c, _ := client.New(client.Options{BaseURL: hs.URL})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	s := New(a)
	tt := ui.NewTester(func(c *ui.Context) { s.Build(c, nil) }, 1100, 900)
	return s, tt, q, f
}

func cmWait(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if q.Drain() > 0 {
			tt.Frame()
		}
		if cond() {
			return
		}
	}
	t.Fatalf("timed out; texts %q", tt.Texts())
}

func TestModelAutoModeRoundTripAndFieldErrors(t *testing.T) {
	s, tt, q, f := cmHarness(t)
	s.open("model-auto-mode")
	m := s.section("model-auto-mode").(*modelAutoMode)
	cmWait(t, tt, q, func() bool { return m.loaded && m.inUse.loaded })
	if m.Dirty() {
		t.Fatal("fresh config dirty")
	}
	tt.Frame()
	if !tt.HasText("Routes (1/25)") || !tt.HasText("tev1") && !tt.HasText("ollama/tev1") {
		t.Errorf("render: %q", tt.Texts())
	}
	m.threshold = "0.8"
	m.routes[0].fb[1], m.routes[0].nfb = "m1", 2
	if !m.Dirty() {
		t.Fatal("edit undetected")
	}
	ok := false
	m.Save(func(b bool) { ok = b })
	cmWait(t, tt, q, func() bool { return !m.saving })
	put := f.puts["/api/v1/config/model-auto-mode"]
	if !ok || put["threshold"] != 0.8 {
		t.Fatalf("put = %v ok=%v", put, ok)
	}
	fbs, _ := put["routes"].([]any)[0].(map[string]any)["fallbacks"].([]any)
	if len(fbs) != 2 {
		t.Errorf("fallbacks = %v", fbs)
	}
	if _, has := put["router"]; has {
		t.Error("decision provider must not be sent")
	}
	// A server-side validation error is shown per field.
	m.routes[0].id = ""
	m.Save(func(bool) {})
	cmWait(t, tt, q, func() bool { return len(m.fieldErrs) > 0 })
	tt.Frame()
	if !tt.HasText("id required") {
		t.Errorf("field error not shown: %q", tt.Texts())
	}
	// Playground.
	m.prompt = "fix the bug"
	m.runPlayground()
	cmWait(t, tt, q, func() bool { return m.pg != nil })
	tt.Frame()
	if !tt.HasText("Route: impl") {
		t.Errorf("playground: %q", tt.Texts())
	}
}

func TestDecisionModelSaveAndTest(t *testing.T) {
	s, tt, q, f := cmHarness(t)
	s.open("decision-model")
	d := s.section("decision-model").(*decisionModel)
	cmWait(t, tt, q, func() bool { return d.loaded })
	d.timeout, d.model = "2000", "other"
	d.apiKey = "secret"
	if !d.Dirty() {
		t.Fatal("not dirty")
	}
	d.Save(func(bool) {})
	cmWait(t, tt, q, func() bool { return !d.saving && !d.Dirty() })
	put := f.puts["/api/v1/config/decision-model"]
	rt, _ := put["router"].(map[string]any)
	if put["timeoutMs"] != float64(2000) || rt["model"] != "other" || rt["apiKey"] != "secret" {
		t.Errorf("put = %v", put)
	}
	d.test()
	cmWait(t, tt, q, func() bool { return d.testResult != nil })
	tt.Frame()
	if !tt.HasText("Reachable") {
		t.Errorf("report: %q", tt.Texts())
	}
}

func TestMesnadaEnginesAndNesting(t *testing.T) {
	s, tt, q, f := cmHarness(t)
	s.open("mesnada")
	m := s.section("mesnada").(*mesnadaSection)
	cmWait(t, tt, q, func() bool { return m.loaded && len(m.engines) == 2 })
	tt.Frame()
	if m.Dirty() {
		t.Fatal("defaults must not make the page dirty")
	}
	if !m.vals["delegation.allowNestedSubagents"].b || m.vals["delegation.maxDepth"].s != "3" {
		t.Errorf("nesting defaults: %+v", m.vals["delegation.maxDepth"])
	}
	m.vals[mnDisabled].l = setEngineDisabled(m.vals[mnDisabled].l, "claude-code", true)
	m.vals["delegation.allowNestedSubagents"].b = false
	m.Save(func(bool) {})
	cmWait(t, tt, q, func() bool { return !m.saving && !m.Dirty() })
	put := f.puts["/api/v1/config/services"]
	mn, _ := put["mesnada"].(map[string]any)
	dis, _ := mn["orchestrator"].(map[string]any)["disabledEngines"].([]any)
	del, _ := mn["delegation"].(map[string]any)
	if len(dis) != 2 || del["allowNestedSubagents"] != false {
		t.Errorf("put = %v", put)
	}
	if _, ok := put["server"]; !ok {
		t.Error("other services subtrees must be kept")
	}
	if got := setEngineDisabled(nil, "pando", true); len(got) != 0 {
		t.Errorf("native engine disabled: %v", got)
	}
}
