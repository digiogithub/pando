package config

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/spf13/viper"
)

// countingSink is a minimal logging.RemoteSink used to prove a remote sink
// set via logging.SetRemoteSink keeps receiving records across repeated
// Load() calls (Load rebuilds the slog handler and calls slog.SetDefault
// every time — including on Reload — since the sink itself is a
// process-global atomic, not tied to any one handler instance).
type countingSink struct {
	mu    sync.Mutex
	count int
}

func (s *countingSink) Enabled(_ slog.Level) bool {
	return true
}

func (s *countingSink) Handle(_ context.Context, _ time.Time, _ slog.Level, _ string, _ []slog.Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
}

func (s *countingSink) get() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// TestLoadWiresTeeHandlerAcrossReload verifies that internal/config.Load
// wraps its default-logger handler with logging.NewTeeHandler on every call
// (all 3 branches conceptually — this exercises the default pubsub-writer
// branch, i.e. no LogFile and no PANDO_DEV_DEBUG), so a remote sink set once
// via logging.SetRemoteSink keeps receiving records after a second Load()
// call (simulating what Reload() does: reset cfg, call Load again).
func TestLoadWiresTeeHandlerAcrossReload(t *testing.T) {
	isolateGlobalConfig(t)
	tmpDir := t.TempDir()

	viper.Reset()
	cfg = nil
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
		logging.SetRemoteSink(nil)
	})

	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("first Load: %v", err)
	}

	sink := &countingSink{}
	logging.SetRemoteSink(sink)
	t.Cleanup(func() { logging.SetRemoteSink(nil) })

	slog.Default().Info("first load message")
	if got := sink.get(); got < 1 {
		t.Fatalf("sink count after first Load = %d, want at least 1", got)
	}

	// Simulate Reload(): reset cfg/viper and call Load again, exactly as
	// config.Reload() does around the private cfg pointer. Load's own
	// internal bookkeeping may itself log a message or two along the way
	// (e.g. "config file not found, creating new one"), so only the delta
	// caused by the explicit message below is asserted, not an absolute
	// count.
	cfg = nil
	viper.Reset()
	if _, err := Load(tmpDir, false); err != nil {
		t.Fatalf("second Load (reload): %v", err)
	}
	afterReload := sink.get()

	slog.Default().Info("second load message")
	if got := sink.get(); got != afterReload+1 {
		t.Fatalf("sink count after reload's own message = %d, want %d (sink must survive Load rebuilding the handler)", got, afterReload+1)
	}
}
