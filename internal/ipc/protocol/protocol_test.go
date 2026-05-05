// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package protocol_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// TestTopicConstantsNonEmpty verifies that all topic constants are non-empty strings.
func TestTopicConstantsNonEmpty(t *testing.T) {
	topics := []string{
		protocol.TopicSessionList,
		protocol.TopicSessionUpdate,
		protocol.TopicSessionActivated,
		protocol.TopicSessionDeleted,
		protocol.TopicMessageAppend,
		protocol.TopicLLMToken,
		protocol.TopicLLMStart,
		protocol.TopicLLMEnd,
		protocol.TopicToolStart,
		protocol.TopicToolEnd,
		protocol.TopicInstanceHeartbeat,
		protocol.TopicInstanceShutdown,
	}
	for _, topic := range topics {
		if topic == "" {
			t.Errorf("expected non-empty topic, got empty string")
		}
	}
}

// TestTopicConstantsUnique verifies that all topic constants are unique.
func TestTopicConstantsUnique(t *testing.T) {
	topics := []string{
		protocol.TopicSessionList,
		protocol.TopicSessionUpdate,
		protocol.TopicSessionActivated,
		protocol.TopicSessionDeleted,
		protocol.TopicMessageAppend,
		protocol.TopicLLMToken,
		protocol.TopicLLMStart,
		protocol.TopicLLMEnd,
		protocol.TopicToolStart,
		protocol.TopicToolEnd,
		protocol.TopicInstanceHeartbeat,
		protocol.TopicInstanceShutdown,
	}
	seen := make(map[string]bool)
	for _, topic := range topics {
		if seen[topic] {
			t.Errorf("duplicate topic constant: %q", topic)
		}
		seen[topic] = true
	}
}

// TestSessionPayloadRoundtrip verifies that SessionPayload marshals and unmarshals correctly.
func TestSessionPayloadRoundtrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := protocol.SessionPayload{
		ID:           "test-session-id",
		Title:        "Test Session",
		UpdatedAt:    now,
		MessageCount: 42,
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal SessionPayload: %v", err)
	}

	var decoded protocol.SessionPayload
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal SessionPayload: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Title != original.Title {
		t.Errorf("Title mismatch: got %q, want %q", decoded.Title, original.Title)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt mismatch: got %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
	if decoded.MessageCount != original.MessageCount {
		t.Errorf("MessageCount mismatch: got %d, want %d", decoded.MessageCount, original.MessageCount)
	}
}

// TestLLMTokenPayloadRoundtrip verifies that LLMTokenPayload marshals and unmarshals correctly.
func TestLLMTokenPayloadRoundtrip(t *testing.T) {
	original := protocol.LLMTokenPayload{
		SessionID: "session-123",
		Token:     "Hello, world!",
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal LLMTokenPayload: %v", err)
	}

	var decoded protocol.LLMTokenPayload
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal LLMTokenPayload: %v", err)
	}

	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.Token != original.Token {
		t.Errorf("Token mismatch: got %q, want %q", decoded.Token, original.Token)
	}
}

// TestHeartbeatPayloadRoundtrip verifies that HeartbeatPayload marshals and unmarshals correctly.
func TestHeartbeatPayloadRoundtrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := protocol.HeartbeatPayload{
		InstanceID:      "instance-abc",
		ActiveSessionID: "session-xyz",
		SessionCount:    3,
		Uptime:          "1h30m0s",
		StartedAt:       now,
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal HeartbeatPayload: %v", err)
	}

	var decoded protocol.HeartbeatPayload
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal HeartbeatPayload: %v", err)
	}

	if decoded.InstanceID != original.InstanceID {
		t.Errorf("InstanceID mismatch: got %q, want %q", decoded.InstanceID, original.InstanceID)
	}
	if decoded.SessionCount != original.SessionCount {
		t.Errorf("SessionCount mismatch: got %d, want %d", decoded.SessionCount, original.SessionCount)
	}
	if decoded.Uptime != original.Uptime {
		t.Errorf("Uptime mismatch: got %q, want %q", decoded.Uptime, original.Uptime)
	}
}

// TestToolPayloadsRoundtrip verifies that ToolStartPayload and ToolEndPayload roundtrip correctly.
func TestToolPayloadsRoundtrip(t *testing.T) {
	start := protocol.ToolStartPayload{
		SessionID: "sess-1",
		ToolName:  "bash",
		CallID:    "call-1",
		Params:    `{"command":"ls"}`,
	}

	b, err := json.Marshal(start)
	if err != nil {
		t.Fatalf("marshal ToolStartPayload: %v", err)
	}
	var decodedStart protocol.ToolStartPayload
	if err := json.Unmarshal(b, &decodedStart); err != nil {
		t.Fatalf("unmarshal ToolStartPayload: %v", err)
	}
	if decodedStart.ToolName != start.ToolName {
		t.Errorf("ToolName mismatch: got %q, want %q", decodedStart.ToolName, start.ToolName)
	}

	end := protocol.ToolEndPayload{
		SessionID: "sess-1",
		ToolName:  "bash",
		CallID:    "call-1",
		IsError:   false,
		Result:    "file1.go\nfile2.go",
	}

	b, err = json.Marshal(end)
	if err != nil {
		t.Fatalf("marshal ToolEndPayload: %v", err)
	}
	var decodedEnd protocol.ToolEndPayload
	if err := json.Unmarshal(b, &decodedEnd); err != nil {
		t.Fatalf("unmarshal ToolEndPayload: %v", err)
	}
	if decodedEnd.IsError != end.IsError {
		t.Errorf("IsError mismatch: got %v, want %v", decodedEnd.IsError, end.IsError)
	}
}

// TestRPCMethodConstantsNonEmpty verifies that all RPC method constants are non-empty.
func TestRPCMethodConstantsNonEmpty(t *testing.T) {
	methods := []string{
		protocol.MethodStateSync,
		protocol.MethodSessionList,
		protocol.MethodSessionGet,
		protocol.MethodSessionActivate,
		protocol.MethodMessageSend,
		protocol.MethodSessionInterrupt,
		protocol.MethodInstancePing,
		protocol.MethodInstanceInfo,
	}
	for _, m := range methods {
		if m == "" {
			t.Errorf("expected non-empty RPC method constant, got empty string")
		}
	}
}

// TestPingResultRoundtrip verifies that PingResult marshals and unmarshals correctly.
func TestPingResultRoundtrip(t *testing.T) {
	original := protocol.PingResult{
		Status:     "ok",
		InstanceID: "inst-1",
		Uptime:     "5m30s",
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal PingResult: %v", err)
	}

	var decoded protocol.PingResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal PingResult: %v", err)
	}

	if decoded.Status != original.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, original.Status)
	}
	if decoded.InstanceID != original.InstanceID {
		t.Errorf("InstanceID mismatch: got %q, want %q", decoded.InstanceID, original.InstanceID)
	}
	if decoded.Uptime != original.Uptime {
		t.Errorf("Uptime mismatch: got %q, want %q", decoded.Uptime, original.Uptime)
	}
}
