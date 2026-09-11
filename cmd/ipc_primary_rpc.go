// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// kbRelinkForwardTimeout bounds how long `pando kb relink` waits for a running
// primary to finish a forwarded relink. A forced relink of a large knowledge
// base rewrites every link row, in batches, so this is generous (like
// dbCompactForwardTimeout).
const kbRelinkForwardTimeout = 30 * time.Minute

// registerPrimaryMaintenanceHandlers registers the P4 maintenance RPCs on a
// primary bus: cronjob.reload (a secondary persisted a cron edit) and kb.relink
// (`pando kb relink` forwarded by the CLI). Called from primaryBusSetupFunc, so
// an instance that starts as primary and a promoted secondary get exactly the
// same handlers.
func registerPrimaryMaintenanceHandlers(bus *ipc.Bus, pandoApp *app.App) {
	bus.RegisterMethod(protocol.MethodCronJobReload, func(_ context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		jobs, err := decodeCronJobReloadParams(params)
		if err != nil {
			return nil, err
		}
		res, err := pandoApp.ApplyCronJobsFromPeer(jobs)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	})

	bus.RegisterMethod(protocol.MethodKBRelink, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.KBRelinkParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("kb.relink: decode params: %w", err)
			}
		}
		stats, err := pandoApp.RelinkKB(ctx, p.Force)
		if err != nil {
			return nil, err
		}
		return json.Marshal(protocol.KBRelinkResult{
			Candidates: stats.Candidates,
			Scanned:    stats.Scanned,
			Documents:  stats.Documents,
			Links:      stats.Links,
		})
	})
}

// decodeCronJobReloadParams decodes cronjob.reload params into the config type.
func decodeCronJobReloadParams(params json.RawMessage) (config.CronJobsConfig, error) {
	var p protocol.CronJobReloadParams
	if len(params) == 0 {
		return config.CronJobsConfig{}, errors.New("cronjob.reload: missing params")
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return config.CronJobsConfig{}, fmt.Errorf("cronjob.reload: decode params: %w", err)
	}
	if len(p.CronJobs) == 0 {
		return config.CronJobsConfig{}, errors.New("cronjob.reload: missing cron_jobs")
	}
	var jobs config.CronJobsConfig
	if err := json.Unmarshal(p.CronJobs, &jobs); err != nil {
		return config.CronJobsConfig{}, fmt.Errorf("cronjob.reload: decode cron_jobs: %w", err)
	}
	return jobs, nil
}

// runningPrimary is a live IPC primary for a workdir, found by
// dialRunningPrimary. The caller must Close it.
type runningPrimary struct {
	client  *ipc.Client
	rpcAddr string
	pid     int
}

func (p *runningPrimary) Close() { _ = p.client.Close() }

// call sends one RPC to the primary with the given timeout.
func (p *runningPrimary) call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	return p.client.CallWithTimeout(ctx, p.rpcAddr, method, params, timeout)
}

// dialRunningPrimary returns the live primary holding the IPC lock for
// workdir, or nil when there is none: no lock file, a released (empty) one, a
// dead PID, or a primary that does not answer instance.ping within 3 s. A CLI
// command that gets nil runs its operation locally instead.
func dialRunningPrimary(ctx context.Context, workdir string) *runningPrimary {
	info, err := ipc.ReadLockForPath(workdir)
	if err != nil || info == nil || info.RPCPort == 0 {
		return nil
	}
	client, err := ipc.NewClient(ctx)
	if err != nil {
		return nil
	}
	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", info.RPCPort)

	// Quick liveness probe: a stale lock (no live primary) means the caller
	// should run locally instead of blocking on a dead endpoint.
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, perr := client.Call(probeCtx, rpcAddr, protocol.MethodInstancePing, nil); perr != nil {
		_ = client.Close()
		return nil
	}
	return &runningPrimary{client: client, rpcAddr: rpcAddr, pid: info.PID}
}

// kbRelinkViaRunningInstance forwards a kb.relink RPC to the primary for
// workdir, if one is alive (pattern: compactViaRunningInstance). forwarded
// reports whether a live instance handled (or attempted) the request; when
// false the caller relinks in-process.
func kbRelinkViaRunningInstance(ctx context.Context, workdir string, force bool) (protocol.KBRelinkResult, bool, error) {
	primary := dialRunningPrimary(ctx, workdir)
	if primary == nil {
		return protocol.KBRelinkResult{}, false, nil
	}
	defer primary.Close()

	fmt.Printf("Forwarding the link rebuild to the running Pando instance (pid %d)...\n", primary.pid)

	raw, err := primary.call(ctx, protocol.MethodKBRelink, protocol.KBRelinkParams{Force: force}, kbRelinkForwardTimeout)
	if err != nil {
		if errors.Is(err, ipc.ErrMethodNotFound) {
			return protocol.KBRelinkResult{}, true, fmt.Errorf("the running Pando instance is too old to rebuild KB links over IPC; please stop it and retry, or upgrade it")
		}
		return protocol.KBRelinkResult{}, true, fmt.Errorf("kb relink via running instance: %w", err)
	}
	var res protocol.KBRelinkResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return protocol.KBRelinkResult{}, true, fmt.Errorf("decode running instance result: %w", err)
	}
	return res, true, nil
}
