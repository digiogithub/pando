package settings

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func TestRemembrancesSectionAgainstFakeServer(t *testing.T) {
	fake := newFake()
	fake.docs["/api/v1/config/services"] = `{"mesnada":{"enabled":false},"remembrances":{"enabled":true,"kb_path":"./.kb","chunk_size":512,
		"document_embedding_provider":"ollama","document_embedding_model":"nomic","use_same_model":true,
		"context_enrichment_enabled":true,"context_enrichment_agent_loop_silent":true}}`
	fake.docs["/api/v1/remembrances/projects"] = `[{"project_id":"p1","name":"Proj","root_path":"/r","status":"indexed"}]`
	fake.docs["/api/v1/remembrances/embedding-models"] = `{"models":[{"id":"nomic","name":"Nomic"}],"source":"api"}`
	posts := map[string]int{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fake.mu.Lock()
			posts[r.URL.Path]++
			fake.mu.Unlock()
			if r.URL.Path == "/api/v1/remembrances/reindex" {
				fmt.Fprint(w, `{"started":[{"project_id":"p1"}]}`)
				return
			}
			fmt.Fprint(w, `{}`)
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer hs.Close()
	c, _ := client.New(client.Options{BaseURL: hs.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	s := New(a)
	h := &harness{t: t, s: s, q: q, srv: fake}
	h.tt = ui.NewTester(func(c *ui.Context) { s.Build(c, nil) }, 1100, 4000)
	h.s.open("remembrances")
	sec := s.section("remembrances").(*docPage)
	rt := sec.Doc.Tail
	_ = rt
	h.wait(func() bool { return sec.loaded })
	h.wait(func() bool { return h.tt.HasText("Proj") })
	if sec.Dirty() {
		t.Fatal("fresh doc dirty")
	}
	sec.vals["chunk_size"].s = "1024"
	sec.vals["context_enrichment_agent_loop_silent"].b = false // inverse switch -> stored true->false? shown value true means silent=false
	done := false
	sec.Save(func(bool) { done = true })
	h.wait(func() bool { return done })
	put := h.srv.put("/api/v1/config/services")
	rem, _ := put["remembrances"].(map[string]any)
	if rem["chunk_size"] != float64(1024) {
		t.Errorf("chunk_size = %v", rem["chunk_size"])
	}
	if _, ok := put["mesnada"]; !ok {
		t.Error("whole services doc must be sent")
	}
	if !h.tt.HasText("Re-index all") {
		t.Errorf("reindex row missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Re-index now"); err != nil {
		t.Fatal(err)
	}
	h.wait(func() bool { fake.mu.Lock(); defer fake.mu.Unlock(); return posts["/api/v1/remembrances/reindex"] == 1 })
}
