package project

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// DelegateExternal runs promptText inside an external (editor-launched) peer
// instance reachable over the IPC ZeroMQ bus. It is only called when the
// caller has opted in via allowExternal=true in WarmDelegate and EnsureInstance
// confirmed the project is served by an external instance.
//
// The function:
//  1. Resolves the project path and reads the on-disk IPC lock to find the peer's
//     RPC endpoint.
//  2. Performs a capability check (instance.ping) to confirm the peer accepts
//     delegations at the required protocol version.
//  3. Issues a blocking delegation.run RPC, wiring a ctx-cancel watcher that
//     fires delegation.cancel over a separate short-lived client so that a
//     cancelled ctx never leaves the remote session running.
//
// Returns ErrExternalUnreachable when the peer cannot be contacted and
// ErrExternalDelegationRefused when the peer is alive but has opted out.
func (m *Manager) DelegateExternal(ctx context.Context, projectID, projectPath, promptText, correlationID string) (*DelegateResult, error) {
	// 1. Resolve path.
	path := projectPath
	if path == "" {
		proj, err := m.service.Get(ctx, projectID)
		if err != nil {
			return nil, ErrExternalUnreachable
		}
		path = proj.Path
	}
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, ErrExternalUnreachable
	}

	// 2. Read the on-disk IPC lock file.
	info, err := ipc.ReadLockForPath(resolved)
	if err != nil || info == nil || !pidIsAlive(info.PID) {
		return nil, ErrExternalUnreachable
	}

	endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", info.RPCPort)

	// 3. Open main IPC client.
	client, err := ipc.NewClient(ctx)
	if err != nil {
		return nil, ErrExternalUnreachable
	}
	defer client.Close() //nolint:errcheck

	// 4. Capability check: ping and verify delegation support.
	raw, err := client.Call(ctx, endpoint, protocol.MethodInstancePing, nil)
	if err != nil {
		return nil, ErrExternalUnreachable
	}
	var pr protocol.PingResult
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, ErrExternalUnreachable
	}
	if !pr.AcceptsDelegations || pr.DelegationProtocol < protocol.DelegationProtocolVersion {
		return nil, ErrExternalDelegationRefused
	}

	// 5. Fire a ctx-cancel watcher before the blocking delegation.run call.
	//    On cancellation it sends delegation.cancel over a separate ephemeral
	//    client (the main client's Call is tied to ctx and cannot be reused).
	params := protocol.DelegationRunParams{
		Prompt:        promptText,
		Cwd:           resolved,
		CorrelationID: correlationID,
	}

	runDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelClient, cerr := ipc.NewClient(context.Background())
			if cerr != nil {
				return
			}
			defer cancelClient.Close() //nolint:errcheck
			_, _ = cancelClient.Call(
				context.Background(),
				endpoint,
				protocol.MethodDelegationCancel,
				protocol.DelegationCancelParams{CorrelationID: correlationID},
			)
		case <-runDone:
		}
	}()

	// 6. Blocking delegation.run call.
	raw, err = client.Call(ctx, endpoint, protocol.MethodDelegationRun, params)
	close(runDone)

	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("external delegation failed: %w", err)
	}

	var res protocol.DelegationRunResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("external delegation: unmarshal result: %w", err)
	}

	return &DelegateResult{
		ChildSessionID: res.SessionID,
		Output:         res.Output,
		StopReason:     res.StopReason,
		External:       true,
	}, nil
}

// RecoverExternalDelegation queries an external (editor-launched) peer for the
// state — and, if finished, the cached result — of a delegation that was issued
// before this process restarted (external re-attach, A2). It is the recovery
// counterpart of DelegateExternal: a read-only delegation.status RPC that never
// re-runs the delegation.
//
// It returns one of the protocol.DelegationState* strings:
//   - "completed": the returned *DelegateResult carries the recovered output (the
//     caller feeds it through the normal conclusion pipeline);
//   - "running": the delegation is still executing on the peer (result is nil);
//   - "unknown": the peer has no record of this correlation id (result is nil).
//
// It returns ErrExternalUnreachable when the peer cannot be contacted and
// ErrExternalDelegationRefused when the peer is alive but does not support
// recovery (opted out, or speaks a delegation protocol older than the status RPC).
// Those error cases mean "cannot recover" — the caller marks the task failed, the
// same outcome as before this feature.
func (m *Manager) RecoverExternalDelegation(ctx context.Context, projectID, projectPath, correlationID string) (*DelegateResult, string, error) {
	if correlationID == "" {
		return nil, "", ErrExternalUnreachable
	}

	// 1. Resolve path.
	path := projectPath
	if path == "" {
		proj, err := m.service.Get(ctx, projectID)
		if err != nil {
			return nil, "", ErrExternalUnreachable
		}
		path = proj.Path
	}
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, "", ErrExternalUnreachable
	}

	// 2. Read the on-disk IPC lock file.
	info, err := ipc.ReadLockForPath(resolved)
	if err != nil || info == nil || !pidIsAlive(info.PID) {
		return nil, "", ErrExternalUnreachable
	}

	endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", info.RPCPort)

	client, err := ipc.NewClient(ctx)
	if err != nil {
		return nil, "", ErrExternalUnreachable
	}
	defer client.Close() //nolint:errcheck

	// 3. Capability check: the peer must accept delegations and speak a protocol
	//    version that includes delegation.status.
	raw, err := client.Call(ctx, endpoint, protocol.MethodInstancePing, nil)
	if err != nil {
		return nil, "", ErrExternalUnreachable
	}
	var pr protocol.PingResult
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, "", ErrExternalUnreachable
	}
	if !pr.AcceptsDelegations || pr.DelegationProtocol < protocol.DelegationProtocolVersion {
		return nil, "", ErrExternalDelegationRefused
	}

	// 4. Query the delegation status by correlation id.
	raw, err = client.Call(ctx, endpoint, protocol.MethodDelegationStatus,
		protocol.DelegationStatusParams{CorrelationID: correlationID})
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", fmt.Errorf("external delegation status failed: %w", err)
	}
	var st protocol.DelegationStatusResult
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, "", fmt.Errorf("external delegation status: unmarshal result: %w", err)
	}

	switch st.State {
	case protocol.DelegationStateCompleted:
		return &DelegateResult{
			ChildSessionID: st.SessionID,
			Output:         st.Output,
			StopReason:     st.StopReason,
			External:       true,
		}, protocol.DelegationStateCompleted, nil
	case protocol.DelegationStateRunning:
		return nil, protocol.DelegationStateRunning, nil
	default:
		return nil, protocol.DelegationStateUnknown, nil
	}
}
