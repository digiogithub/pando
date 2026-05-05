// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"encoding/json"
	"os"
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

// TestPortsForPathRange verifies that all ports fall within [40000, 60001).
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
