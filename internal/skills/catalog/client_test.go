package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearch_Success(t *testing.T) {
	want := SearchResult{
		Query:      "git",
		SearchType: "semantic",
		Skills: []CatalogSkill{
			{ID: "owner/repo/git-helper", SkillID: "git-helper", Name: "git-helper", Installs: 1200, Source: "owner/repo"},
			{ID: "other/repo/git-log", SkillID: "git-log", Name: "git-log", Installs: 300, Source: "other/repo"},
		},
		Count:      2,
		DurationMs: 5,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if q := r.URL.Query().Get("q"); q != "git" {
			t.Errorf("unexpected q param: %s", q)
		}
		if lim := r.URL.Query().Get("limit"); lim != "5" {
			t.Errorf("unexpected limit param: %s", lim)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Search(context.Background(), "git", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if got.Query != want.Query {
		t.Errorf("Query: got %q, want %q", got.Query, want.Query)
	}
	if got.Count != want.Count {
		t.Errorf("Count: got %d, want %d", got.Count, want.Count)
	}
	if len(got.Skills) != len(want.Skills) {
		t.Fatalf("Skills length: got %d, want %d", len(got.Skills), len(want.Skills))
	}
	if got.Skills[0].Name != "git-helper" {
		t.Errorf("Skills[0].Name: got %q, want %q", got.Skills[0].Name, "git-helper")
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"noresult","searchType":"semantic","skills":[],"count":0,"duration_ms":2}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	got, err := client.Search(context.Background(), "noresult", 10)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(got.Skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(got.Skills))
	}
	if got.Count != 0 {
		t.Errorf("expected count 0, got %d", got.Count)
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lim := r.URL.Query().Get("limit"); lim != "10" {
			t.Errorf("expected default limit 10, got %s", lim)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"x","skills":[],"count":0,"duration_ms":1}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "test", 10)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestSearch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "test", 10)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSearch_NetworkError(t *testing.T) {
	// Use a server that closes immediately
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close right away

	client := NewClient(srv.URL)
	_, err := client.Search(context.Background(), "test", 10)
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}

func TestSearch_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"query":"x","skills":[],"count":0,"duration_ms":500}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Search(ctx, "test", 10)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestFormatInstalls(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, "0 installs"},
		{1, "1 installs"},
		{999, "999 installs"},
		{1000, "1K installs"},
		{1500, "1.5K installs"},
		{1200, "1.2K installs"},
		{10000, "10K installs"},
		{999999, "1000K installs"},
		{1000000, "1M installs"},
		{1500000, "1.5M installs"},
		{3000000, "3M installs"},
		{12300000, "12.3M installs"},
	}

	for _, tt := range tests {
		got := FormatInstalls(tt.count)
		if got != tt.want {
			t.Errorf("FormatInstalls(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}
