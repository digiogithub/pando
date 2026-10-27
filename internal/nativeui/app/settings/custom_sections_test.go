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

type csFake struct {
	mu     sync.Mutex
	calls  []string
	bodies map[string]string
}

func (f *csFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	b, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, key)
	f.bodies[key] = string(b)
	switch key {
	case "GET /api/v1/token":
		fmt.Fprint(w, `{"token":"t"}`)
	case "GET /api/v1/config/provider-accounts":
		fmt.Fprint(w, `{"providerAccounts":[{"id":"acme","displayName":"Acme","type":"openai","apiKey":"sk-***","disabled":false}]}`)
	case "POST /api/v1/config/provider-accounts":
		fmt.Fprint(w, `{}`)
	case "POST /api/v1/config/provider-accounts/acme/test":
		fmt.Fprint(w, `{"ok":true,"modelCount":3}`)
	case "GET /api/v1/config/agents":
		fmt.Fprint(w, `{"agents":[{"name":"coder","model":"m1","maxTokens":0,"reasoningEffort":"","thinkingMode":"","autoCompact":true,"autoCompactThreshold":0.8,"contextControls":true,"extra":"keep"}]}`)
	case "PUT /api/v1/config/agents":
		w.Write(b)
	case "GET /api/v1/models":
		fmt.Fprint(w, `{"models":[{"id":"m1"},{"id":"m2"}]}`)
	case "GET /api/v1/config/decision-model":
		fmt.Fprint(w, `{"router":{"provider":"ollama","model":"q"}}`)
	case "GET /api/v1/config/api-server/basic-auth":
		fmt.Fprint(w, `{"enabled":false,"users":[{"username":"bob"}],"enforced":false,"bindHost":"localhost"}`)
	case "POST /api/v1/config/api-server/basic-auth/users":
		fmt.Fprint(w, `{"enabled":false,"users":[{"username":"bob"},{"username":"amy"}]}`)
	case "GET /api/v1/design/system":
		fmt.Fprint(w, `{"system":{"name":"x","tokens":{"color":{"bg":"#ffffff"}}},"exists":true,"tokens_path":"t","stylesheet_path":"s","contract_path":"c"}`)
	case "PUT /api/v1/design/system":
		fmt.Fprint(w, `{"system":{"name":"y","tokens":{"color":{"bg":"#000000"}}},"exists":true}`)
	case "GET /api/v1/design/system/examples":
		fmt.Fprint(w, `{"examples":[{"name":"ex","title":"Ex"}]}`)
	default:
		http.NotFound(w, r)
	}
}

func csHarness(t *testing.T) (*Settings, *ui.Tester, *app.QueueDispatcher, *csFake) {
	f := &csFake{bodies: map[string]string{}}
	hs := httptest.NewServer(f)
	t.Cleanup(hs.Close)
	c, _ := client.New(client.Options{BaseURL: hs.URL})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	s := New(a)
	return s, ui.NewTester(func(c *ui.Context) { s.Build(c, nil) }, 1100, 800), q, f
}

func csWait(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, cond func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if q.Drain() > 0 {
			tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout; %q", tt.Texts())
}

func TestCustomProviders(t *testing.T) {
	s, tt, q, f := csHarness(t)
	s.open("providers")
	tt.Frame()
	p := s.section("providers").(*providersSection)
	csWait(t, tt, q, func() bool { return tt.HasText("Acme") })
	if !tt.HasText("OpenAI") {
		t.Errorf("type label missing: %q", tt.Texts())
	}
	p.openForm(accountForm{typeLabel: "OpenAI Compatible (custom)"})
	tt.Frame()
	p.dialog = false
	tt.Frame()
	if err := tt.Click("Test"); err != nil {
		t.Fatal(err)
	}
	csWait(t, tt, q, func() bool { return p.tests["acme"] == "ok" })
	p.form = accountForm{id: "new-one", name: "New", typeLabel: "Anthropic", apiKey: "k"}
	p.saveForm()
	csWait(t, tt, q, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.bodies["POST /api/v1/config/provider-accounts"] != ""
	})
	var got map[string]any
	_ = json.Unmarshal([]byte(f.bodies["POST /api/v1/config/provider-accounts"]), &got)
	if got["id"] != "new-one" || got["type"] != "anthropic" || got["apiKey"] != "k" {
		t.Errorf("body = %v", got)
	}
	p.form = accountForm{id: "Bad ID", name: "n", typeLabel: "Anthropic"}
	n := len(f.calls)
	p.saveForm()
	if len(f.calls) != n {
		t.Error("invalid id must not be sent")
	}
}

func TestCustomAgents(t *testing.T) {
	s, tt, q, f := csHarness(t)
	s.open("agents")
	tt.Frame()
	ag := s.section("agents").(*agentsSection)
	csWait(t, tt, q, func() bool { return ag.loaded })
	if ag.Dirty() {
		t.Fatal("dirty after load")
	}
	for _, e := range ag.agents {
		e.open, e.useDecison = true, true
	}
	tt.Frame()
	if !tt.HasText("Reasoning effort") {
		t.Errorf("expanded card missing: %q", tt.Texts())
	}
	ag.agents[0].useDecison = false
	ag.agents[0].model = "m2"
	ag.agents[0].reasoning = "High"
	if !ag.Dirty() {
		t.Fatal("edit not detected")
	}
	ok := false
	ag.Save(func(b bool) { ok = b })
	csWait(t, tt, q, func() bool { return ok })
	var body struct{ Agents []map[string]any }
	_ = json.Unmarshal([]byte(f.bodies["PUT /api/v1/config/agents"]), &body)
	a := body.Agents[0]
	if a["model"] != "m2" || a["reasoningEffort"] != "high" || a["extra"] != "keep" || len(body.Agents) != len(agentNames) {
		t.Errorf("agents body = %v", body.Agents)
	}
	if ag.Dirty() {
		t.Error("dirty after save")
	}
}

func TestCustomWebUIAccessAndDesign(t *testing.T) {
	s, tt, q, f := csHarness(t)
	s.open("webui-access")
	tt.Frame()
	w := s.section("webui-access").(*webuiAccess)
	csWait(t, tt, q, func() bool { return w.loaded && tt.HasText("bob") })
	w.user, w.pass = "amy", "pw"
	if err := tt.Click("Add user"); err != nil {
		t.Fatal(err)
	}
	csWait(t, tt, q, func() bool { return len(w.st.Users) == 2 })
	if f.bodies["POST /api/v1/config/api-server/basic-auth/users"] == "" {
		t.Error("user not posted")
	}

	s.open("design-system")
	tt.Frame()
	d := s.section("design-system").(*designSystem)
	csWait(t, tt, q, func() bool { return d.loaded })
	d.tokens["color"]["bg"] = "#000000"
	if !d.Dirty() {
		t.Fatal("token edit not detected")
	}
	ok := false
	d.Save(func(b bool) { ok = b })
	csWait(t, tt, q, func() bool { return ok })
	if d.Dirty() || d.name != "y" {
		t.Errorf("after save: dirty=%v name=%s", d.Dirty(), d.name)
	}
}
