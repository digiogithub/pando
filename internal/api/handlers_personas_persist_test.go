package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/spf13/viper"
)

func TestSetActivePersonaPersistsAcrossRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	_ = config.Reload()
	viper.Reset()
	t.Cleanup(func() { viper.Reset() })
	if _, err := config.Load(dir, false); err != nil {
		t.Fatal(err)
	}

	// "" (Auto) needs no persona manager, so it exercises persistence alone.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/personas/active", strings.NewReader(`{"name":""}`))
	rec := httptest.NewRecorder()
	(&Server{}).handleSetActivePersona(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}

	_ = config.Reload()
	viper.Reset()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, auto, set := config.ActivePersonaChoice(); !auto || !set {
		t.Fatalf("auto=%v set=%v after restart", auto, set)
	}
}
