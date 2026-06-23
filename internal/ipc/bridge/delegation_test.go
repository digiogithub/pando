// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// --- fake DelegationRunner ---

type fakeDelegationRunner struct {
	result       protocol.DelegationRunResult
	err          error
	cancelledIDs []string
	calledParams []protocol.DelegationRunParams
	statusResult protocol.DelegationStatusResult
	statusIDs    []string
}

func (f *fakeDelegationRunner) RunDelegation(_ context.Context, params protocol.DelegationRunParams) (protocol.DelegationRunResult, error) {
	f.calledParams = append(f.calledParams, params)
	return f.result, f.err
}

func (f *fakeDelegationRunner) CancelDelegation(correlationID string) {
	f.cancelledIDs = append(f.cancelledIDs, correlationID)
}

func (f *fakeDelegationRunner) DelegationStatus(correlationID string) protocol.DelegationStatusResult {
	f.statusIDs = append(f.statusIDs, correlationID)
	return f.statusResult
}

// --- helpers to call a registered handler ---

func invokeHandler(t *testing.T, captured map[string]ipc.HandlerFunc, method string, params any) (json.RawMessage, error) {
	t.Helper()
	h, ok := captured[method]
	if !ok {
		t.Fatalf("handler %q not registered", method)
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		raw = b
	}
	return h(context.Background(), method, raw)
}

// --- tests for ping capability advertisement ---

func TestRegisterHandlersWithDelegation_PingNoAccept(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	// acceptDelegations=false → ping must advertise AcceptsDelegations=false, DelegationProtocol=0.
	bridge.RegisterHandlersWithDelegation(bus, "inst-1", svc, nil, time.Now(), nil, nil, nil, false)

	raw, err := invokeHandler(t, captured, protocol.MethodInstancePing, nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ping protocol.PingResult
	if err := json.Unmarshal(raw, &ping); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ping.AcceptsDelegations {
		t.Error("expected AcceptsDelegations=false when opt-in is off")
	}
	if ping.DelegationProtocol != 0 {
		t.Errorf("expected DelegationProtocol=0, got %d", ping.DelegationProtocol)
	}
}

func TestRegisterHandlersWithDelegation_PingAccepts(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)
	runner := &fakeDelegationRunner{}

	bridge.RegisterHandlersWithDelegation(bus, "inst-2", svc, nil, time.Now(), nil, nil, runner, true)

	raw, err := invokeHandler(t, captured, protocol.MethodInstancePing, nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ping protocol.PingResult
	if err := json.Unmarshal(raw, &ping); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ping.AcceptsDelegations {
		t.Error("expected AcceptsDelegations=true")
	}
	if ping.DelegationProtocol != protocol.DelegationProtocolVersion {
		t.Errorf("expected DelegationProtocol=%d, got %d", protocol.DelegationProtocolVersion, ping.DelegationProtocol)
	}
}

func TestRegisterHandlersWithDelegation_PingAcceptsFlagButNoRunner(t *testing.T) {
	// acceptDelegations=true but runner=nil → should still advertise false (guard).
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	bridge.RegisterHandlersWithDelegation(bus, "inst-3", svc, nil, time.Now(), nil, nil, nil, true)

	raw, err := invokeHandler(t, captured, protocol.MethodInstancePing, nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ping protocol.PingResult
	if err := json.Unmarshal(raw, &ping); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ping.AcceptsDelegations {
		t.Error("expected AcceptsDelegations=false when runner is nil")
	}
}

// --- tests for delegation.run ---

func TestDelegationRun_RejectsWhenNotAccepted(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	bridge.RegisterHandlersWithDelegation(bus, "inst-4", svc, nil, time.Now(), nil, nil, nil, false)

	params := protocol.DelegationRunParams{Prompt: "do something"}
	_, err := invokeHandler(t, captured, protocol.MethodDelegationRun, params)
	if err == nil {
		t.Fatal("expected error when delegation is not accepted")
	}
}

func TestDelegationRun_RejectsEmptyPrompt(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)
	runner := &fakeDelegationRunner{}

	bridge.RegisterHandlersWithDelegation(bus, "inst-5", svc, nil, time.Now(), nil, nil, runner, true)

	params := protocol.DelegationRunParams{Prompt: ""}
	_, err := invokeHandler(t, captured, protocol.MethodDelegationRun, params)
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestDelegationRun_InvokesRunnerAndReturnsResult(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	want := protocol.DelegationRunResult{
		SessionID:  "sess-abc",
		Output:     "hello from subagent",
		StopReason: "end_turn",
	}
	runner := &fakeDelegationRunner{result: want}

	bridge.RegisterHandlersWithDelegation(bus, "inst-6", svc, nil, time.Now(), nil, nil, runner, true)

	params := protocol.DelegationRunParams{Prompt: "do work", CorrelationID: "corr-1"}
	raw, err := invokeHandler(t, captured, protocol.MethodDelegationRun, params)
	if err != nil {
		t.Fatalf("delegation.run: %v", err)
	}

	var got protocol.DelegationRunResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, want.SessionID)
	}
	if got.Output != want.Output {
		t.Errorf("Output: got %q, want %q", got.Output, want.Output)
	}
	if len(runner.calledParams) != 1 {
		t.Fatalf("expected runner called once, got %d", len(runner.calledParams))
	}
	if runner.calledParams[0].Prompt != "do work" {
		t.Errorf("runner received wrong prompt: %q", runner.calledParams[0].Prompt)
	}
}

func TestDelegationRun_PropagatesRunnerError(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	runner := &fakeDelegationRunner{err: errors.New("agent blew up")}

	bridge.RegisterHandlersWithDelegation(bus, "inst-7", svc, nil, time.Now(), nil, nil, runner, true)

	params := protocol.DelegationRunParams{Prompt: "do work"}
	_, err := invokeHandler(t, captured, protocol.MethodDelegationRun, params)
	if err == nil {
		t.Fatal("expected error from runner to propagate")
	}
}

// --- tests for delegation.cancel ---

func TestDelegationCancel_NoOpsWhenNotAccepted(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	// Not accepting delegations — cancel should still return OK.
	bridge.RegisterHandlersWithDelegation(bus, "inst-8", svc, nil, time.Now(), nil, nil, nil, false)

	params := protocol.DelegationCancelParams{CorrelationID: "corr-99"}
	raw, err := invokeHandler(t, captured, protocol.MethodDelegationCancel, params)
	if err != nil {
		t.Fatalf("delegation.cancel: %v", err)
	}
	var ok protocol.OKResult
	if err := json.Unmarshal(raw, &ok); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ok.OK {
		t.Error("expected OK=true")
	}
}

func TestDelegationCancel_CallsRunner(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)
	runner := &fakeDelegationRunner{}

	bridge.RegisterHandlersWithDelegation(bus, "inst-9", svc, nil, time.Now(), nil, nil, runner, true)

	params := protocol.DelegationCancelParams{CorrelationID: "corr-42"}
	raw, err := invokeHandler(t, captured, protocol.MethodDelegationCancel, params)
	if err != nil {
		t.Fatalf("delegation.cancel: %v", err)
	}
	var ok protocol.OKResult
	if err := json.Unmarshal(raw, &ok); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ok.OK {
		t.Error("expected OK=true")
	}
	if len(runner.cancelledIDs) != 1 || runner.cancelledIDs[0] != "corr-42" {
		t.Errorf("expected runner.CancelDelegation(corr-42), got %v", runner.cancelledIDs)
	}
}

func TestDelegationStatus_CallsRunnerWhenAccepting(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)
	runner := &fakeDelegationRunner{
		statusResult: protocol.DelegationStatusResult{
			State:     protocol.DelegationStateCompleted,
			SessionID: "s-1",
			Output:    "recovered output",
		},
	}

	bridge.RegisterHandlersWithDelegation(bus, "inst-st", svc, nil, time.Now(), nil, nil, runner, true)

	params := protocol.DelegationStatusParams{CorrelationID: "corr-st"}
	raw, err := invokeHandler(t, captured, protocol.MethodDelegationStatus, params)
	if err != nil {
		t.Fatalf("delegation.status: %v", err)
	}
	var res protocol.DelegationStatusResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.State != protocol.DelegationStateCompleted || res.Output != "recovered output" {
		t.Errorf("status result = %+v, want completed/recovered output", res)
	}
	if len(runner.statusIDs) != 1 || runner.statusIDs[0] != "corr-st" {
		t.Errorf("expected runner.DelegationStatus(corr-st), got %v", runner.statusIDs)
	}
}

func TestDelegationStatus_RefusedWhenNotAccepting(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	// acceptDelegations=false → handler registered but refuses.
	bridge.RegisterHandlersWithDelegation(bus, "inst-st2", svc, nil, time.Now(), nil, nil, nil, false)

	if _, err := invokeHandler(t, captured, protocol.MethodDelegationStatus,
		protocol.DelegationStatusParams{CorrelationID: "x"}); err == nil {
		t.Fatal("expected delegation.status to refuse when not accepting")
	}
}

// --- tests for RegisterHandlersWithAgent backward compatibility ---

func TestRegisterHandlersWithAgent_PingAdvertisesNoDelegation(t *testing.T) {
	svc := newMockSessionService(nil)
	captured := make(map[string]ipc.HandlerFunc)
	bus := newCapturingBus(captured)

	bridge.RegisterHandlersWithAgent(bus, "inst-compat", svc, nil, time.Now(), nil, nil)

	raw, err := invokeHandler(t, captured, protocol.MethodInstancePing, nil)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	var ping protocol.PingResult
	if err := json.Unmarshal(raw, &ping); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ping.AcceptsDelegations {
		t.Error("RegisterHandlersWithAgent must not advertise AcceptsDelegations")
	}
	if ping.DelegationProtocol != 0 {
		t.Errorf("RegisterHandlersWithAgent must advertise DelegationProtocol=0, got %d", ping.DelegationProtocol)
	}
}

// agentDelegationRunner inflight-map and cancel mechanics are covered by the
// internal package tests in delegation_runner_internal_test.go (package bridge).
