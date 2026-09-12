// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestPortsForPathDeterminism verifies that the same path always yields the same ports.
func TestPortsForPathDeterminism(t *testing.T) {
	path := "/home/user/projects/my-project"
	pub1, rpc1 := PortsForPath(path)
	pub2, rpc2 := PortsForPath(path)

	if pub1 != pub2 {
		t.Errorf("PUB port not deterministic: got %d and %d for the same path", pub1, pub2)
	}
	if rpc1 != rpc2 {
		t.Errorf("RPC port not deterministic: got %d and %d for the same path", rpc1, rpc2)
	}
	if rpc1 != pub1+1 {
		t.Errorf("expected RPC port to be PUB+1, got PUB=%d RPC=%d", pub1, rpc1)
	}
}

// TestPortsForPathDifferentPaths verifies that different paths produce different ports (in practice).
func TestPortsForPathDifferentPaths(t *testing.T) {
	paths := []string{
		"/home/user/project-a",
		"/home/user/project-b",
		"/tmp/workspace",
		"/var/app/pando",
	}

	seen := make(map[int]string)
	for _, p := range paths {
		pub, _ := PortsForPath(p)
		if prev, exists := seen[pub]; exists {
			t.Logf("collision between %q and %q at port %d (hash collision, not necessarily a bug)", prev, p, pub)
		}
		seen[pub] = p
	}
}

// TestPortsForPathRange verifies that all derived ports fall within the
// configured window, and — more importantly — that the window stays clear of the
// OS ephemeral ranges (Linux 32768-60999, macOS/Windows 49152-65535) and of the
// well-known/registered ports used by common dev tooling.
func TestPortsForPathRange(t *testing.T) {
	testPaths := []string{
		"/",
		"/home/user/projects/pando",
		"/tmp/test-workspace",
		"/var/lib/pando/instance-1",
		"/Users/dev/code/project",
	}

	for _, p := range testPaths {
		pub, rpc := PortsForPath(p)
		if pub < portBase || pub >= portBase+portRange {
			t.Errorf("path %q: PUB port %d is out of range [%d, %d)", p, pub, portBase, portBase+portRange)
		}
		if rpc < portBase+1 || rpc > portBase+portRange {
			t.Errorf("path %q: RPC port %d is out of range", p, rpc)
		}
	}
}

// lowestEphemeralPort is the lowest port any of the three supported platforms may
// hand out as an ephemeral/dynamic port (Linux's default ip_local_port_range).
const lowestEphemeralPort = 32768

// TestPortsForPathWindowOutsideEphemeralRange pins the constants themselves: the
// whole window, RPC port included, must sit below the ephemeral range on every
// platform and above the ports dev tooling commonly listens on.
func TestPortsForPathWindowOutsideEphemeralRange(t *testing.T) {
	if portBase < 10000 {
		t.Errorf("portBase %d is too low; it must stay clear of well-known/registered dev ports", portBase)
	}
	highestDerived := portBase + portRange // max RPC port = (portBase+portRange-1)+1
	if highestDerived >= lowestEphemeralPort {
		t.Errorf("derived ports reach %d, which is inside the OS ephemeral range (>= %d): "+
			"a foreign socket could own the port and the primary would fail to bind",
			highestDerived, lowestEphemeralPort)
	}
	for _, forbidden := range []int{3000, 3306, 5000, 5432, 6379, 8000, 8080, 9000, 9090, 27017} {
		if forbidden >= portBase && forbidden <= highestDerived {
			t.Errorf("port %d used by common dev tooling falls inside the derived window [%d, %d]",
				forbidden, portBase, highestDerived)
		}
	}
}

// TestAcquireLockSecondaryUsesPrimaryPortsFromLockFile is the version-skew
// regression: a primary started by an older binary recorded ports from the old
// 40000-60000 window. A new binary that fails to take the lock must report those
// recorded ports, never its own freshly derived ones — otherwise it would dial
// nothing, and (since the derived ports are free) could quietly become a second
// primary.
func TestAcquireLockSecondaryUsesPrimaryPortsFromLockFile(t *testing.T) {
	dir := t.TempDir()

	const oldPub, oldRPC = 47123, 47124 // ports an older binary would have derived
	isPrimary, _, lockFile, err := AcquireLock(dir, "old-binary-primary", oldPub, oldRPC)
	if err != nil {
		t.Fatalf("AcquireLock (primary) failed: %v", err)
	}
	if !isPrimary {
		t.Fatal("first instance should be primary")
	}
	defer ReleaseLock(lockFile)

	newPub, newRPC := PortsForPath(dir)
	if newPub == oldPub {
		t.Fatalf("test setup: derived port %d must differ from the simulated old port", newPub)
	}

	isPrimary2, info, _, err := AcquireLock(dir, "new-binary-secondary", newPub, newRPC)
	if err != nil {
		t.Fatalf("AcquireLock (secondary) failed: %v", err)
	}
	if isPrimary2 {
		t.Fatal("second instance must not become primary while the lock is held")
	}
	if info == nil {
		t.Fatal("secondary got no LockInfo; it would not know where the primary listens")
	}
	if info.PubPort != oldPub || info.RPCPort != oldRPC {
		t.Errorf("secondary must use the primary's recorded ports %d/%d, got %d/%d",
			oldPub, oldRPC, info.PubPort, info.RPCPort)
	}
}

// TestAcquireLockUnreadableLockInfoIsNotPrimary verifies that a held lock whose
// info cannot be parsed is reported as ErrPrimaryLockHeld and never as "I am
// primary" — the one outcome that would put two writers on the same database.
func TestAcquireLockUnreadableLockInfoIsNotPrimary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock semantics differ on Windows; covered by waitForLockInfo there")
	}
	dir := t.TempDir()

	isPrimary, _, lockFile, err := AcquireLock(dir, "primary", 20100, 20101)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock (primary) failed: isPrimary=%v err=%v", isPrimary, err)
	}
	defer ReleaseLock(lockFile)

	// Corrupt the lock file content while the flock is still held.
	if writeErr := os.WriteFile(lockFilePath(dir), []byte("{not json"), 0o600); writeErr != nil {
		t.Fatalf("corrupt lock file: %v", writeErr)
	}

	isPrimary2, _, _, err2 := AcquireLock(dir, "second", 20200, 20201)
	if isPrimary2 {
		t.Fatal("second instance became primary while another process holds the lock")
	}
	if !errors.Is(err2, ErrPrimaryLockHeld) {
		t.Errorf("expected ErrPrimaryLockHeld, got %v", err2)
	}
}

// TestStartBusWithRetryFailsLoudlyOnPortConflict verifies that a taken port
// produces an ErrBusBindFailed instead of a silent "continuing without IPC".
func TestStartBusWithRetryFailsLoudlyOnPortConflict(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	taken := l.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	bus := NewBus("bind-conflict-test")
	startErr := StartBusWithRetry(ctx, bus, taken, taken+1)
	if startErr == nil {
		_ = bus.Shutdown()
		t.Fatal("expected the bind to fail on an occupied port")
	}
	if !errors.Is(startErr, ErrBusBindFailed) {
		t.Errorf("expected ErrBusBindFailed, got %v", startErr)
	}
}

// TestAcquireLock verifies that acquiring a lock on a temp directory works.
func TestAcquireLock(t *testing.T) {
	dir := t.TempDir()

	isPrimary, info, lockFile, err := AcquireLock(dir, "test-instance-1", 41000, 41001)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer ReleaseLock(lockFile)

	if !isPrimary {
		t.Fatal("expected to be primary on first lock acquisition")
	}
	if info == nil {
		t.Fatal("expected non-nil LockInfo")
	}
	if info.InstanceID != "test-instance-1" {
		t.Errorf("expected InstanceID=%q, got %q", "test-instance-1", info.InstanceID)
	}
	if info.PubPort != 41000 {
		t.Errorf("expected PubPort=41000, got %d", info.PubPort)
	}
	if info.RPCPort != 41001 {
		t.Errorf("expected RPCPort=41001, got %d", info.RPCPort)
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected PID=%d, got %d", os.Getpid(), info.PID)
	}
}

// TestAcquireLockSecondInstance verifies that a second acquisition fails and returns the primary's info.
func TestAcquireLockSecondInstance(t *testing.T) {
	dir := t.TempDir()

	isPrimary1, _, lockFile1, err := AcquireLock(dir, "instance-1", 42000, 42001)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer ReleaseLock(lockFile1)

	if !isPrimary1 {
		t.Fatal("expected first instance to be primary")
	}

	// Second instance tries to acquire the same lock.
	isPrimary2, info2, lockFile2, err := AcquireLock(dir, "instance-2", 42000, 42001)
	if err != nil {
		t.Fatalf("second AcquireLock failed: %v", err)
	}
	if lockFile2 != nil {
		defer ReleaseLock(lockFile2)
	}

	if isPrimary2 {
		t.Fatal("expected second instance to NOT be primary")
	}
	if info2 == nil {
		t.Fatal("expected second instance to receive primary's LockInfo")
	}
	if info2.InstanceID != "instance-1" {
		t.Errorf("expected primary InstanceID=%q, got %q", "instance-1", info2.InstanceID)
	}
}

// TestEnvelopeMarshalUnmarshal verifies that an Envelope survives a JSON roundtrip.
func TestEnvelopeMarshalUnmarshal(t *testing.T) {
	type SamplePayload struct {
		Message string `json:"message"`
		Count   int    `json:"count"`
	}

	payload := SamplePayload{Message: "hello", Count: 42}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	original := Envelope{
		InstanceID: "inst-abc",
		ProjectID:  "proj-xyz",
		SessionID:  "sess-123",
		Topic:      "chat.message",
		Timestamp:  time.Date(2025, 5, 5, 12, 0, 0, 0, time.UTC),
		Payload:    rawPayload,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal Envelope: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal Envelope: %v", err)
	}

	if decoded.InstanceID != original.InstanceID {
		t.Errorf("InstanceID mismatch: got %q want %q", decoded.InstanceID, original.InstanceID)
	}
	if decoded.ProjectID != original.ProjectID {
		t.Errorf("ProjectID mismatch: got %q want %q", decoded.ProjectID, original.ProjectID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch: got %q want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.Topic != original.Topic {
		t.Errorf("Topic mismatch: got %q want %q", decoded.Topic, original.Topic)
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v want %v", decoded.Timestamp, original.Timestamp)
	}

	// Verify payload roundtrip.
	var decodedPayload SamplePayload
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("unmarshal inner payload: %v", err)
	}
	if decodedPayload.Message != payload.Message || decodedPayload.Count != payload.Count {
		t.Errorf("payload mismatch: got %+v want %+v", decodedPayload, payload)
	}
}

// TestEnvelopeOptionalSessionID verifies that SessionID is omitted when empty.
func TestEnvelopeOptionalSessionID(t *testing.T) {
	env := Envelope{
		InstanceID: "inst-1",
		ProjectID:  "proj-1",
		Topic:      "system.ready",
		Timestamp:  time.Now().UTC(),
		Payload:    json.RawMessage(`{}`),
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify sessionId key is absent when empty.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["sessionId"]; ok {
		t.Error("expected sessionId to be omitted when empty, but it was present")
	}
}
