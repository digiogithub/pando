// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
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
}
