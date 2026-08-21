package extensions

import (
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// The memory capability moves project content off the machine, and the rule
// for that is that the user can always see it happening. This is the data
// behind that indicator: the gate as configured, the host's own counters, and
// whatever each sink says about itself.

// MemorySinkStatus is one sink's reported state.
type MemorySinkStatus struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Active      bool     `json:"active"`
	DryRun      bool     `json:"dryRun"`
	Destination string   `json:"destination,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Pending     int      `json:"pending"`
	Sent        int64    `json:"sent"`
	Dropped     int64    `json:"dropped"`
	LastSyncAt  string   `json:"lastSyncAt,omitempty"`
	LastError   string   `json:"lastError,omitempty"`
	// Reports is false when the sink does not implement MemorySyncReporter, so
	// the UI can say "shipping, state unknown" instead of "idle" — which would
	// be a lie in exactly the case that matters.
	Reports bool `json:"reports"`
}

// MemoryStatus is what /api/v1/extensions/memory returns.
type MemoryStatus struct {
	// Enabled reflects the configuration gate, not whether a sink exists.
	Enabled bool `json:"enabled"`
	// Active is true when the gate is open and something is actually wired to
	// receive events.
	Active     bool               `json:"active"`
	DryRun     bool               `json:"dryRun"`
	Mode       string             `json:"mode"`
	Scopes     []string           `json:"scopes,omitempty"`
	Paths      []string           `json:"paths,omitempty"`
	Origins    []string           `json:"origins,omitempty"`
	WrapSearch bool               `json:"wrapSearch"`
	Wrappers   []string           `json:"wrappers,omitempty"`
	Host       PublisherStats     `json:"host"`
	Sinks      []MemorySinkStatus `json:"sinks"`
}

// MemoryStatusOf assembles the status. Every field tolerates a nil manager or
// publisher: a standard build answers "off" rather than 404, so the UI has one
// code path instead of two.
func MemoryStatusOf(mgr *extension.Manager, cfg config.ExtensionsMemoryConfig, pub *MemoryPublisher) MemoryStatus {
	st := MemoryStatus{
		Enabled:    cfg.Enabled,
		DryRun:     cfg.DryRun,
		Mode:       modeName(cfg),
		Scopes:     cfg.Scopes,
		Paths:      cfg.Paths,
		Origins:    cfg.Origins,
		WrapSearch: cfg.WrapSearch,
		Host:       pub.Stats(),
		Sinks:      []MemorySinkStatus{},
	}
	if mgr == nil {
		return st
	}

	for _, sink := range extension.Capability[extension.MemorySink](mgr) {
		info := sink.ExtensionInfo()
		row := MemorySinkStatus{ID: string(info.ID), Name: info.Name}
		if reporter, ok := sink.(extension.MemorySyncReporter); ok {
			row.Reports = true
			fillSinkStatus(&row, reporter)
		}
		st.Sinks = append(st.Sinks, row)
	}

	if cfg.WrapSearch {
		for _, w := range extension.Capability[extension.RemembranceSearchWrapper](mgr) {
			st.Wrappers = append(st.Wrappers, string(w.ExtensionInfo().ID))
		}
	}

	st.Active = pub != nil && len(st.Sinks) > 0
	return st
}

// fillSinkStatus asks one sink for its state, containing a panic: a broken
// reporter must not take down the status endpoint that exists to expose it.
func fillSinkStatus(row *MemorySinkStatus, reporter extension.MemorySyncReporter) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension memory sink status panicked", "extension", row.ID, "panic", r)
			row.LastError = "status unavailable"
		}
	}()
	s := reporter.MemorySyncStatus()
	row.Active = s.Active
	row.DryRun = s.DryRun
	row.Destination = s.Destination
	row.Scopes = s.Scopes
	row.Pending = s.Pending
	row.Sent = s.Sent
	row.Dropped = s.Dropped
	row.LastError = s.LastError
	if !s.LastSyncAt.IsZero() {
		row.LastSyncAt = s.LastSyncAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
}
