package app

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/telemetry"
)

// remoteSinkStopDeadline bounds how long stopping (or restarting) the
// shipper is allowed to take, whether triggered by a live toggle or by App
// shutdown: long enough to flush whatever is already queued, short enough to
// never noticeably delay a disable or a process exit.
const remoteSinkStopDeadline = 2 * time.Second

// telemetryRuntime owns the opt-in remote logging pipeline for one App
// instance: it starts/stops a internal/telemetry.Shipper as
// config.Get().Telemetry changes, live, with no process restart, by
// subscribing to config.Bus the same way internal/cronjob.Service does for
// CronJobsConfig.
type telemetryRuntime struct {
	mode string

	mu              sync.Mutex
	shipper         *telemetry.Shipper
	current         config.TelemetryConfig
	currentMinLevel slog.Level
	// closed is set once shutdown has started, under mu, before shipper is
	// cleared. apply() checks it first thing (also under mu) so a
	// config.Bus event already in flight through watch() — which reads
	// config.Get() and calls apply() with no synchronization against
	// shutdown — can never start (or restart) a shipper after shutdown has
	// already cleared internal/logging's remote sink: without this guard,
	// such a late apply() would call startLocked, reinstalling a sink and
	// leaking a shipper goroutine nothing will ever stop.
	closed bool

	busCh  chan config.ConfigChangeEvent
	cancel context.CancelFunc
}

// initTelemetry wires the telemetry runtime for this App instance and
// returns a shutdown func to be called exactly once, on App.Shutdown, before
// the process exits. mode identifies the running surface (tui, serve,
// desktop, acp, cli, ...) — internal/app.New's callers already thread this
// through as AppOptions.StartupMode for the remembrances subsystem, and it
// is reused here verbatim so every telemetry record carries the same value.
//
// When cfg is nil (should not happen in practice — New only reaches this
// point after loading config) or telemetry is disabled/unavailable, this
// still subscribes to config.Bus so a later live enable is picked up; it
// just starts with nothing running.
func initTelemetry(cfg *config.Config, mode string) func(context.Context) error {
	tr := &telemetryRuntime{mode: mode}

	if cfg != nil {
		tr.apply(cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel
	tr.busCh = make(chan config.ConfigChangeEvent, 8)
	config.Bus.Subscribe(tr.busCh)
	go tr.watch(ctx)

	return tr.shutdown
}

// watch reacts to every config.Bus event by re-reading config.Get().Telemetry
// and reconciling the running shipper against it. Like
// internal/cronjob.Service.watchConfigChanges, it does not filter by
// ChangedKeys: apply() is cheap and idempotent when nothing telemetry-related
// actually changed, so reacting to every event (a reload, an unrelated
// setting, ...) is simpler than trying to name every producer's ChangedKeys
// convention correctly.
func (tr *telemetryRuntime) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-tr.busCh:
			if !ok {
				return
			}
			cfg := config.Get()
			if cfg == nil {
				continue
			}
			tr.apply(cfg)
		}
	}
}

// apply reconciles the running shipper (if any) with cfg.Telemetry: starts
// one if telemetry just became enabled (and internal/telemetry.Available(),
// i.e. this build carries a Better Stack token), stops it if telemetry just
// became disabled or the build has no token, live-updates the debug id when
// only that changed, or restarts when the effective minimum shipped level
// changed (Options.MinLevel is read once at construction; a live setter for
// one rarely-changed field is not worth the extra surface). Safe for
// concurrent use; a no-op when cfg matches the state already in effect, or
// when shutdown has already run (see the closed field's doc).
//
// The effective minimum level clamps cfg.Telemetry.MinLevel == "debug" up to
// Info whenever the app-wide Debug flag (cfg.Debug) is off: a "debug"
// telemetry level is meant to ship the same debug-level detail the app is
// already logging locally, not to unilaterally turn on remote shipping of
// debug-level content the app itself is not even generating at that
// verbosity. See internal/logging's teeHandler.Enabled/Shipper.Enabled,
// which is what actually enforces this once MinLevel is set here.
func (tr *telemetryRuntime) apply(cfg *config.Config) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if tr.closed {
		return
	}

	next := cfg.Telemetry
	minLevel := effectiveMinLevel(next.MinLevel, cfg.Debug)

	wantRunning := next.Enabled && telemetry.Available()

	switch {
	case wantRunning && tr.shipper == nil:
		tr.startLocked(next, minLevel)
	case !wantRunning && tr.shipper != nil:
		tr.stopLocked()
	case wantRunning && tr.shipper != nil:
		if next.DebugID != tr.current.DebugID {
			tr.shipper.SetDebugID(next.DebugID)
		}
		if minLevel != tr.currentMinLevel {
			tr.stopLocked()
			tr.startLocked(next, minLevel)
		}
	}

	tr.current = next
}

// effectiveMinLevel maps a config.TelemetryConfig.MinLevel string to the
// slog.Level actually passed to the shipper, clamped to at least Info
// whenever debugOn (cfg.Debug) is false — see apply's doc for why.
func effectiveMinLevel(configured string, debugOn bool) slog.Level {
	level := parseTelemetryLevel(configured)
	if !debugOn && level < slog.LevelInfo {
		level = slog.LevelInfo
	}
	return level
}

// startLocked builds and starts a new shipper from cfg at minLevel, installs
// it as internal/logging's remote sink, and ships a "telemetry.enabled"
// record through the ordinary logging path (so it also appears in local
// logs, and is itself subject to minLevel like any other record). Caller
// must hold tr.mu.
func (tr *telemetryRuntime) startLocked(cfg config.TelemetryConfig, minLevel slog.Level) {
	opts := telemetry.Options{
		Endpoint: telemetry.Endpoint(),
		Token:    telemetry.Token(),
		DebugID:  cfg.DebugID,
		Mode:     tr.mode,
		MinLevel: minLevel,
	}
	shipper := telemetry.NewShipper(opts)
	shipper.Start(context.Background())

	tr.shipper = shipper
	tr.currentMinLevel = minLevel
	logging.SetRemoteSink(shipper)

	logging.Info("Telemetry enabled", "event", "telemetry.enabled", "mode", tr.mode)
}

// stopLocked clears the remote sink before stopping the shipper, per the
// plan: no new record can be routed to a shipper that is already draining
// its final flush. Caller must hold tr.mu.
func (tr *telemetryRuntime) stopLocked() {
	if tr.shipper == nil {
		return
	}
	logging.SetRemoteSink(nil)

	shipper := tr.shipper
	tr.shipper = nil
	tr.currentMinLevel = 0

	ctx, cancel := context.WithTimeout(context.Background(), remoteSinkStopDeadline)
	defer cancel()
	_ = shipper.Stop(ctx)
}

// shutdown stops the config.Bus subscription and, if a shipper is running,
// ships an "App shutdown" record directly against it (bypassing slog, since
// the sink is about to be cleared) before stopping it with a bounded
// deadline. Safe to call at most once (App.Shutdown only calls it once); a
// nil-shipper shutdown is a cheap no-op.
//
// closed is set under tr.mu before the shipper is captured/cleared, so any
// apply() call already blocked on tr.mu — e.g. watch()'s goroutine, which
// reads config.Get() and calls apply() with no synchronization against
// shutdown — sees it and becomes a no-op instead of starting a new shipper
// after this function has already torn the old one down (see the closed
// field's doc on telemetryRuntime).
func (tr *telemetryRuntime) shutdown(ctx context.Context) error {
	if tr.cancel != nil {
		tr.cancel()
	}
	if tr.busCh != nil {
		config.Bus.Unsubscribe(tr.busCh)
	}

	tr.mu.Lock()
	tr.closed = true
	shipper := tr.shipper
	tr.shipper = nil
	tr.currentMinLevel = 0
	tr.mu.Unlock()

	if shipper == nil {
		return nil
	}

	shipper.Handle(ctx, time.Now(), slog.LevelInfo, "App shutdown",
		[]slog.Attr{
			slog.String("event", "app.shutdown"),
			slog.String("mode", tr.mode),
		},
	)
	logging.SetRemoteSink(nil)
	return shipper.Stop(ctx)
}

// parseTelemetryLevel maps a config.TelemetryConfig.MinLevel string
// ("debug"/"info"/"warn"/"error", case-insensitive) to its slog.Level,
// defaulting to slog.LevelInfo for an empty or unrecognized value (Load's
// normalizeTelemetryDefaults already guarantees a valid, non-empty value in
// practice; this default is only a defensive fallback).
func parseTelemetryLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case config.TelemetryLevelDebug:
		return slog.LevelDebug
	case config.TelemetryLevelWarn:
		return slog.LevelWarn
	case config.TelemetryLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
