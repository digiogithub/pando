package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	embeddedrt "github.com/digiogithub/pando/internal/runtime/embedded"
	"github.com/google/go-containerregistry/pkg/v1/empty"
)

func TestHandleContainerCapabilitiesIncludesEmbedded(t *testing.T) {
	loadTestConfig(t)
	config.Get().Container.Runtime = "embedded"

	req := httptest.NewRequest(http.MethodGet, "/api/container/capabilities", nil)
	rec := httptest.NewRecorder()

	var server Server
	server.handleContainerCapabilities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp containerCapabilitiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Current != "embedded" {
		t.Fatalf("current runtime = %q, want embedded", resp.Current)
	}

	found := false
	for _, runtime := range resp.Runtimes {
		if runtime.Type == "embedded" {
			found = true
			if !runtime.Available || !runtime.Exec || runtime.FS {
				t.Fatalf("embedded capability = %+v, want available exec-only runtime", runtime)
			}
		}
	}
	if !found {
		t.Fatal("embedded runtime capability missing from response")
	}
}

func TestHandleContainerImagesListsCachedImages(t *testing.T) {
	loadTestConfig(t)

	cacheDir := t.TempDir()
	config.Get().Container.EmbeddedCacheDir = cacheDir

	store := &embeddedrt.ImageStore{Root: cacheDir}
	if err := store.Put("alpine:3.20", empty.Image); err != nil {
		t.Fatalf("store.Put(): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/container/images", nil)
	rec := httptest.NewRecorder()

	var server Server
	server.handleContainerImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp containerImagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Images) != 1 {
		t.Fatalf("len(images) = %d, want 1", len(resp.Images))
	}
	if resp.Images[0].Ref != "index.docker.io/library/alpine:3.20" {
		t.Fatalf("ref = %q", resp.Images[0].Ref)
	}
	if resp.Total <= 0 {
		t.Fatalf("total = %d, want positive cached size", resp.Total)
	}
}

func loadTestConfig(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
}
