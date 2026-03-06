package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestMesnadaDefaults(t *testing.T) {
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})

	configureViper()
	setDefaults(false)

	var loaded Config
	if err := viper.Unmarshal(&loaded); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if got := loaded.Mesnada.Server.Host; got != "127.0.0.1" {
		t.Fatalf("mesnada.server.host = %q, want %q", got, "127.0.0.1")
	}
	if got := loaded.Mesnada.Server.Port; got != 9767 {
		t.Fatalf("mesnada.server.port = %d, want %d", got, 9767)
	}
	if got := loaded.Mesnada.Orchestrator.MaxParallel; got != 5 {
		t.Fatalf("mesnada.orchestrator.maxParallel = %d, want %d", got, 5)
	}
	if got := loaded.Mesnada.Orchestrator.DefaultEngine; got != "copilot" {
		t.Fatalf("mesnada.orchestrator.defaultEngine = %q, want %q", got, "copilot")
	}
	if !loaded.Mesnada.TUI.Enabled {
		t.Fatal("mesnada.tui.enabled = false, want true")
	}
	if !loaded.Mesnada.TUI.WebUI {
		t.Fatal("mesnada.tui.webui = false, want true")
	}
}
