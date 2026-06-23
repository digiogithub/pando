package project

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// delegationClientName identifies this manager as the ACP client during the
// Initialize handshake with a child instance opened for delegation.
const delegationClientName = "pando-delegation"

// DelegateResult is the raw outcome of running a delegated prompt inside a warm
// per-project instance. The orchestrator (warm-target routing) wraps it into a
// terminal models.Task — populating Output so the existing conclusion pipeline
// (conclusion.Enrich → supervisor) extracts the <pando:conclusion> block — and
// fills software-owned launch metadata (model, parent session, correlation,
// depth) which this transport layer does not know about.
type DelegateResult struct {
	// ChildSessionID is the ACP session id created in the child for this run.
	// It is stored on the task (ACPSessionID) for correlation/recovery.
	ChildSessionID string
	// Output is the agent's full streamed message text for the turn.
	Output string
	// StopReason is the ACP stop reason reported by the child (e.g. end_turn).
	StopReason string
	// External is true when this result came from an external (editor-launched)
	// peer reached over the IPC bus (DelegateExternal, B3) rather than a
	// manager-spawned warm child. Used by the orchestrator for metrics attribution.
	External bool
}

// Delegate runs promptText inside the already-running ("warm") child ACP
// instance for projectID and captures the agent's streamed output over the
// wire. It opens a fresh ephemeral session in the child (the child keeps its own
// session history; the orchestrator's FileStore remains the single source of
// truth) and closes it on completion.
//
// It returns ErrInstanceNotRunning when no manager-owned instance is live for
// the project — auto-start is the responsibility of the warm-target routing
// layer. If ctx is cancelled while the turn is in flight, the child session is
// cancelled and the context error is returned so the caller can fall back to the
// cold path.
func (m *Manager) Delegate(ctx context.Context, projectID, promptText string) (*DelegateResult, error) {
	m.mu.RLock()
	inst, ok := m.instances[projectID]
	m.mu.RUnlock()
	if !ok || inst == nil {
		return nil, ErrInstanceNotRunning
	}
	return m.delegateOn(ctx, projectID, inst, promptText)
}

// WarmDelegate is the high-level warm-routing entry point used by the orchestrator
// adapter. It ensures a warm instance for the project (reuse-then-autostart,
// excluding external instances), enforces the per-instance concurrency cap, and
// runs promptText in the instance, capturing the conclusion over the wire.
//
// projectID may be empty, in which case projectPath is resolved to a registered
// project. autoStart selects reuse-then-autostart (true) vs reuse-only (false).
// maxConcurrent (>0) bounds concurrent delegated sessions per instance.
// queueDepth (>0) bounds a FIFO of delegations that wait for a slot when the cap
// is reached instead of cold-falling-back immediately (item A3); 0 keeps the
// cold-fallback-on-cap behaviour. A queued delegation that loses its context
// (ctx cancelled) or whose instance starts closing still returns ErrWarmCapReached.
// allowExternal (B3), when true, routes to an external (editor-launched) peer over
// the IPC ZeroMQ bus instead of returning ErrExternalInstance; the external peer
// manages its own concurrency so no manager slot is acquired on this path.
// correlationID is the orchestrator's idempotency key for the delegated task; it
// is threaded to DelegateExternal on the external path so a cancel can target the
// right remote run. An empty correlationID is synthesized for the external path.
//
// It returns a sentinel error the caller should treat as "use the cold path":
// ErrInstanceNotRunning (reuse-only, nothing running), ErrExternalInstance (the
// project is served by an editor-launched instance and allowExternal is false),
// ErrProjectNeedsInit (no config to start a child), ErrProjectNotRegistered
// (unknown project), or ErrWarmCapReached (cap reached and queue full/disabled).
// Any other error is a genuine warm-run failure.
func (m *Manager) WarmDelegate(ctx context.Context, projectID, projectPath, promptText string, autoStart bool, maxConcurrent, queueDepth int, allowExternal bool, correlationID string) (*DelegateResult, error) {
	id, err := m.resolveProjectID(ctx, projectID, projectPath)
	if err != nil {
		return nil, err
	}

	inst, err := m.EnsureInstance(ctx, id, autoStart)
	if err != nil {
		// B3: if the project is served by an external peer and the caller opted
		// in, route directly over IPC — no manager slot is acquired.
		if err == ErrExternalInstance && allowExternal {
			// Prefer the orchestrator-provided CorrelationID for idempotent
			// external delegation; synthesize one only when the caller did not
			// supply a key (e.g. a direct WarmDelegate call without a task).
			cid := correlationID
			if cid == "" {
				cid = fmt.Sprintf("ext-%s-%d", id, time.Now().UnixNano())
			}
			return m.DelegateExternal(ctx, id, projectPath, promptText, cid)
		}
		return nil, err
	}

	if !inst.acquireDelegationSlotOrQueue(ctx, maxConcurrent, queueDepth) {
		return nil, ErrWarmCapReached
	}
	m.publishDelegationChanged(id, inst.InflightDelegations())
	defer func() {
		inst.releaseDelegationSlot()
		m.publishDelegationChanged(id, inst.InflightDelegations())
	}()

	return m.delegateOn(ctx, id, inst, promptText)
}

// resolveProjectID returns the project id to use for warm routing. When
// projectID is non-empty it is used as-is; otherwise projectPath is canonicalised
// and looked up in the registry. Returns ErrProjectNotRegistered when neither
// yields a known project.
func (m *Manager) resolveProjectID(ctx context.Context, projectID, projectPath string) (string, error) {
	if projectID != "" {
		return projectID, nil
	}
	if projectPath == "" {
		return "", ErrProjectNotRegistered
	}
	resolved, err := resolvePath(projectPath)
	if err != nil {
		return "", ErrProjectNotRegistered
	}
	proj, err := m.service.GetByPath(ctx, resolved)
	if err != nil || proj == nil {
		return "", ErrProjectNotRegistered
	}
	return proj.ID, nil
}

// EnsureInstance returns a running manager-owned instance for projectID,
// reusing an existing one or auto-starting a new child WITHOUT changing the
// active project (delegation must never switch the user's focused project).
//
// When the project is served by an external (editor-launched) instance it
// returns ErrExternalInstance — such instances are never warm targets. When no
// instance is running and autoStart is false it returns ErrInstanceNotRunning;
// when autoStart is true but the path has no Pando config it returns
// ErrProjectNeedsInit.
func (m *Manager) EnsureInstance(ctx context.Context, projectID string, autoStart bool) (*Instance, error) {
	// Fast path: reuse an already-running manager-owned instance.
	m.mu.RLock()
	if inst, ok := m.instances[projectID]; ok && inst != nil {
		m.mu.RUnlock()
		return inst, nil
	}
	m.mu.RUnlock()

	proj, err := m.service.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("ensure instance: get project %s: %w", projectID, err)
	}

	// An external instance serving this project is never a warm target.
	if running, external, _ := m.Runtime(projectID, proj.Path); running && external {
		return nil, ErrExternalInstance
	}

	if !autoStart {
		return nil, ErrInstanceNotRunning
	}

	resolvedPath, err := resolvePath(proj.Path)
	if err != nil {
		return nil, fmt.Errorf("ensure instance: resolve path %s: %w", proj.Path, err)
	}
	if !config.HasConfigFileAt(resolvedPath) {
		return nil, ErrProjectNeedsInit
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check under the write lock: another goroutine may have started it.
	if inst, ok := m.instances[projectID]; ok && inst != nil {
		return inst, nil
	}

	inst, err := m.spawnChild(*proj)
	if err != nil {
		return nil, fmt.Errorf("ensure instance: spawn ACP child for %s: %w", proj.Path, err)
	}
	// Tag as delegation-spawned so the Projects panel can distinguish it from a
	// user-activated instance.
	inst.markDelegationSpawned(true)
	m.instances[projectID] = inst

	// Mark running in the registry but do NOT touch activeID — this is a
	// delegation-spawned instance, not a user-focused project switch.
	pid := 0
	if inst.cmd != nil && inst.cmd.Process != nil {
		pid = inst.cmd.Process.Pid
	}
	_ = m.service.UpdateStatus(ctx, projectID, StatusRunning, pid, 0)
	m.broker.Publish(pubsub.UpdatedEvent, ManagerEvent{
		Type:      EvStatusChanged,
		ProjectID: projectID,
		Status:    StatusRunning,
	})

	return inst, nil
}

// delegateOn runs promptText against an already-resolved instance, capturing the
// agent's streamed output over the wire (see Delegate / WarmDelegate).
func (m *Manager) delegateOn(ctx context.Context, projectID string, inst *Instance, promptText string) (*DelegateResult, error) {
	if err := m.ensureInitialized(ctx, inst); err != nil {
		return nil, fmt.Errorf("initialize child instance: %w", err)
	}

	newResp, err := inst.conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        inst.Project.Path,
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		return nil, fmt.Errorf("open delegated session: %w", err)
	}
	sid := newResp.SessionId

	sink := inst.delClient.register(sid)
	defer inst.delClient.unregister(sid)

	// Mirror an orchestrator/ctx cancellation onto the child session so a stopped
	// or aborted delegation never leaves the child running its turn. The watcher
	// exits as soon as Prompt returns.
	promptDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Use a background context — ctx is already cancelled.
			if cancelErr := inst.conn.Cancel(context.Background(), acpsdk.CancelNotification{SessionId: sid}); cancelErr != nil {
				logging.Debug("project: failed to cancel delegated session", "project", projectID, "session", string(sid), "error", cancelErr)
			}
		case <-promptDone:
		}
	}()

	promptResp, promptErr := inst.conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: sid,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(promptText)},
	})
	close(promptDone)

	// Best-effort close of the ephemeral child session regardless of outcome.
	m.closeDelegatedSession(inst, sid)

	if promptErr != nil {
		// Prefer the context error so callers can detect cancellation explicitly.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("delegated prompt failed: %w", promptErr)
	}

	return &DelegateResult{
		ChildSessionID: string(sid),
		Output:         sink.text(),
		StopReason:     string(promptResp.StopReason),
	}, nil
}

// ensureInitialized performs the ACP Initialize handshake on the child
// connection exactly once. The child stdio server is ready as soon as the
// process starts, but Prompt/NewSession require an initialized connection.
func (m *Manager) ensureInitialized(ctx context.Context, inst *Instance) error {
	inst.initOnce.Do(func() {
		_, inst.initErr = inst.conn.Initialize(ctx, acpsdk.InitializeRequest{
			ProtocolVersion: 1,
			ClientInfo: &acpsdk.Implementation{
				Name:    delegationClientName,
				Version: pandoVersionForDelegation(),
			},
		})
	})
	return inst.initErr
}

// closeDelegatedSession best-effort closes an ephemeral delegated session in the
// child. Failures are logged but never propagated — the captured output has
// already been read by the caller.
func (m *Manager) closeDelegatedSession(inst *Instance, sid acpsdk.SessionId) {
	if _, err := inst.conn.UnstableCloseSession(context.Background(), acpsdk.UnstableCloseSessionRequest{SessionId: sid}); err != nil {
		logging.Debug("project: failed to close delegated session", "session", string(sid), "error", err)
	}
}

// pandoVersionForDelegation returns a version string for the Initialize
// handshake. The child does not depend on the exact value.
func pandoVersionForDelegation() string {
	return "1.0.0"
}
