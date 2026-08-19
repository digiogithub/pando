// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
)

// agentMessageRunner adapts agent.Service to bridge.MessageRunner: it starts the
// run and drains the event channel in the background so the RPC caller returns as
// soon as the agent goroutine is launched. Live progress reaches the caller over
// the instance's PUB stream.
type agentMessageRunner struct {
	agent agent.Service
}

func (r agentMessageRunner) RunMessage(ctx context.Context, sessionID string, content string) error {
	// Detach from the RPC request context so the run outlives the HTTP/RPC call.
	events, err := r.agent.Run(context.WithoutCancel(ctx), sessionID, content)
	if err != nil {
		return err
	}
	go func() {
		for range events { //nolint:revive // drain so the agent is not blocked
		}
	}()
	return nil
}

// registerBridgeHandlers wires the IPC JSON-RPC handlers for an instance bus,
// including the opt-in hot-peer delegation handler (B3, `delegation.run`) when
// the instance has AcceptDelegations enabled and a coder agent is available.
//
// With AcceptDelegations off (the default) it is behaviourally identical to
// bridge.RegisterHandlers: no delegation handler is registered and the instance
// advertises AcceptsDelegations=false over instance.ping, so peers never route a
// delegation to it. Centralising this keeps every entrypoint (tui/acp/serve/
// app/desktop, plus failover-promoted buses) consistent.
func registerBridgeHandlers(bus *ipc.Bus, instanceID string, pandoApp *app.App) {
	accept := config.Get().Mesnada.Delegation.AcceptDelegations

	var delRunner bridge.DelegationRunner
	if accept && pandoApp.CoderAgent != nil {
		delRunner = bridge.NewAgentDelegationRunner(pandoApp.Sessions, pandoApp.CoderAgent)
	}

	// Remote message.send / session.interrupt: the web-UI Instances panel drives
	// another instance's session through these, so wire them to the local agent
	// when one exists (they returned "agent runner not available" before).
	var runner bridge.MessageRunner
	var interrupter bridge.SessionInterrupter
	if pandoApp.CoderAgent != nil {
		runner = agentMessageRunner{agent: pandoApp.CoderAgent}
		interrupter = pandoApp.CoderAgent
	}

	bridge.RegisterHandlersWithDelegation(
		bus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now(),
		runner, interrupter, delRunner, delRunner != nil,
	)

	// db.compact — run a VACUUM on this (primary) instance's writer connection so
	// secondaries and the `pando db compact` CLI can reclaim space without opening
	// a second writer. CompactDatabase runs locally here because this bus only ever
	// exists on the primary.
	bus.RegisterMethod(protocol.MethodDBCompact, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.DBCompactParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		res, err := pandoApp.CompactDatabase(ctx, p.Incremental, p.EnableAutoVacuum)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	})
}
