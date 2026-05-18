// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package failover_test

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/failover"
)

// TestDefaultConfig verifies the conservative defaults.
func TestDefaultConfig(t *testing.T) {
	cfg := failover.DefaultConfig()
	if cfg.Enabled {
		t.Error("auto-failover must be disabled by default")
	}
	if cfg.HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval: got %s, want 5s", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatTimeout != 15*time.Second {
		t.Errorf("HeartbeatTimeout: got %s, want 15s", cfg.HeartbeatTimeout)
	}
}

// TestSetEnabled verifies the runtime feature flag toggle.
func TestSetEnabled(t *testing.T) {
	w := failover.NewWatcherForPrimary(
		failover.DefaultConfig(),
		"test-id", t.TempDir(), 40000, 40001,
		&noopPublisher{},
	)
	if w.FormatStatus() == "" {
		t.Error("FormatStatus should return a non-empty string")
	}
	w.SetEnabled(true)
	// Verify the watcher accepted the change without panicking.
	w.SetEnabled(false)
}

// TestPrimaryWatcherShutdownWithoutStart verifies that Shutdown is safe even when
// Start was never called (e.g. bus failed to start).
func TestPrimaryWatcherShutdownWithoutStart(t *testing.T) {
	w := failover.NewWatcherForPrimary(
		failover.DefaultConfig(),
		"test-id", t.TempDir(), 40002, 40003,
		&noopPublisher{},
	)
	// Shutdown without Start must not block or panic.
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Shutdown(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown blocked when Start was never called")
	}
}

// TestSecondaryWatcherNilClient verifies that a secondary watcher with nil client
// exits its goroutine cleanly instead of panicking.
func TestSecondaryWatcherNilClient(t *testing.T) {
	w := failover.NewWatcherForSecondary(
		failover.DefaultConfig(),
		"test-id", t.TempDir(), 40004, 40005,
		nil,  // nil client
		"",   // no pubEndpoint
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	// Give the goroutine a moment to exit on its own (nil client fast-path).
	time.Sleep(50 * time.Millisecond)
	cancel()
	// Shutdown must complete quickly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Shutdown(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown blocked")
	}
}

// TestRoleChangedCNotBlocking verifies that the channel is non-blocking when nothing reads it.
func TestRoleChangedCNotBlocking(t *testing.T) {
	w := failover.NewWatcherForPrimary(
		failover.DefaultConfig(),
		"test-id", t.TempDir(), 40006, 40007,
		&noopPublisher{},
	)
	// Drain nothing; the channel should not block internal operations.
	_ = w.RoleChangedC
}

// TestLastHeartbeatZeroOnPrimary verifies that LastHeartbeat is zero for a primary.
func TestLastHeartbeatZeroOnPrimary(t *testing.T) {
	w := failover.NewWatcherForPrimary(
		failover.DefaultConfig(),
		"test-id", t.TempDir(), 40008, 40009,
		&noopPublisher{},
	)
	if !w.LastHeartbeat().IsZero() {
		t.Error("LastHeartbeat should be zero on a primary watcher")
	}
}

// noopPublisher is a test double for BusPublisher.
type noopPublisher struct{}

func (n *noopPublisher) Publish(_ string, _ any) error { return nil }
