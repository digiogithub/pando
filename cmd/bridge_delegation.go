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
)

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

	bridge.RegisterHandlersWithDelegation(
		bus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now(),
		nil, nil, delRunner, delRunner != nil,
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
