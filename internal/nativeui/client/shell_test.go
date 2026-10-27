package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUIPrefsRoundTripAndSettings(t *testing.T) {
	var stored UIPrefs
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ui/preferences", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var p UIPrefs
			_ = json.NewDecoder(r.Body).Decode(&p)
			if p.ThemeMode != "" {
				stored.ThemeMode = p.ThemeMode
			}
			if p.SidebarCollapsed != nil {
				stored.SidebarCollapsed = p.SidebarCollapsed
			}
		}
		_ = json.NewEncoder(w).Encode(stored)
	})
	mux.HandleFunc("/api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"desktop_tray_on_minimize_close":true,"default_model":"m"}`))
	})
	c := testClient(t, mux)
	ctx := context.Background()
	yes := true
	got, err := c.PutUIPrefs(ctx, UIPrefs{ThemeMode: "dark", SidebarCollapsed: &yes})
	if err != nil || got.ThemeMode != "dark" || got.SidebarCollapsed == nil || !*got.SidebarCollapsed {
		t.Fatalf("put = %+v, %v", got, err)
	}
	s, err := c.GetDesktopSettings(ctx)
	if err != nil || !s.TrayOnMinimizeClose || s.DefaultModel != "m" {
		t.Fatalf("settings = %+v, %v", s, err)
	}
}

func TestNotificationAndConfigStreams(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notifications/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: {}\n\nevent: notification\ndata: {\"id\":\"1\",\"source\":\"agent\",\"message\":\"done\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/api/v1/config/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: {\"ts\":1}\n\ndata: {\"section\":\"Desktop\",\"source\":\"webui\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	c := testClient(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notes := make(chan Notification, 4)
	go c.SubscribeNotifications(ctx, func(n Notification) { notes <- n })
	select {
	case n := <-notes:
		if n.Source != "agent" || n.Message != "done" {
			t.Fatalf("notification = %+v", n)
		}
	case <-ctx.Done():
		t.Fatal("no notification")
	}
	evs := make(chan ConfigEvent, 4)
	go c.SubscribeConfigEvents(ctx, func(e ConfigEvent) { evs <- e })
	first, second := <-evs, <-evs
	if !first.Connected || second.Section != "Desktop" {
		t.Fatalf("events = %+v %+v", first, second)
	}
}

func TestProjectsVersionAndErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"a","name":"A","web_state":"running"},{"id":"b","name":"B","web_state":"stopped"}]}`))
	})
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v1","latest":"v2","update_available":true,"checkable":true,"release_url":"https://x"}`))
	})
	c := testClient(t, mux)
	ps, err := c.ListProjects(context.Background())
	if err != nil || len(ps) != 2 || !ProjectOpen(ps[0]) || ProjectOpen(ps[1]) {
		t.Fatalf("projects = %+v, %v", ps, err)
	}
	v, err := c.Version(context.Background())
	if err != nil || !v.UpdateAvailable || v.Latest != "v2" {
		t.Fatalf("version = %+v, %v", v, err)
	}
	if !IsAuthError(ErrUnauthorized) || !strings.Contains(DescribeError(ErrUnauthorized), "401") {
		t.Fatal("401 not described")
	}
}
