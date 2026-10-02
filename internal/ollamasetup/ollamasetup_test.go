package ollamasetup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeOllama(t *testing.T, pullLines []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.12.0"}`))
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"nomic-embed-text:latest"}]}`))
	})
	mux.HandleFunc("POST /api/pull", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model == "" {
			http.Error(w, "missing model", http.StatusBadRequest)
			return
		}
		for _, line := range pullLines {
			fmt.Fprintln(w, line)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func waitJob(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := m.Get(id)
		if !ok {
			t.Fatalf("job %s vanished", id)
		}
		if job.State != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return Job{}
}

func TestDetectRunningOllama(t *testing.T) {
	srv := fakeOllama(t, nil)
	st := NewManager().Detect(t.Context(), srv.URL)
	if !st.Running || !st.Installed || st.Version != "0.12.0" {
		t.Fatalf("unexpected status: %+v", st)
	}
	if !HasModel(st.Models, "nomic-embed-text") {
		t.Fatalf("untagged name must match the :latest model: %v", st.Models)
	}
	if len(st.InstallOptions) == 0 {
		t.Fatal("no install options")
	}
}

func TestPullReportsProgressAndSuccess(t *testing.T) {
	srv := fakeOllama(t, []string{
		`{"status":"pulling manifest"}`,
		`{"status":"pulling abc","digest":"sha256:abc","total":100,"completed":40}`,
		`{"status":"pulling abc","digest":"sha256:abc","total":100,"completed":100}`,
		`{"status":"success"}`,
	})
	m := NewManager()
	job, err := m.Pull(srv.URL, "hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, m, job.ID)
	if done.State != "done" || done.Total != 100 || done.Completed != 100 {
		t.Fatalf("unexpected job: %+v", done)
	}
}

func TestPullSurfacesOllamaError(t *testing.T) {
	srv := fakeOllama(t, []string{`{"error":"pull model manifest: file does not exist"}`})
	m := NewManager()
	job, err := m.Pull(srv.URL, "nope")
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, m, job.ID)
	if done.State != "error" || done.Error == "" {
		t.Fatalf("expected error job: %+v", done)
	}
}

func TestPullRejectsInvalidModelName(t *testing.T) {
	for _, name := range []string{"", "a b", "x;rm -rf /", "$(id)"} {
		if _, err := NewManager().Pull("http://127.0.0.1:1", name); err == nil {
			t.Fatalf("model name %q accepted", name)
		}
	}
}

func TestInstallRejectsUnknownOption(t *testing.T) {
	if _, err := NewManager().Install("curl-pipe-anything"); err == nil {
		t.Fatal("unknown install option accepted")
	}
}
