package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// --- capturing client unit test -------------------------------------------

func TestDelegationClientCapturesRegisteredSessionOnly(t *testing.T) {
	c := newDelegationClient(Project{ID: "p1", Path: "/tmp"})

	const sid = acpsdk.SessionId("sess-1")
	sink := c.register(sid)

	feed := func(id acpsdk.SessionId, text string) {
		_ = c.SessionUpdate(context.Background(), acpsdk.SessionNotification{
			SessionId: id,
			Update: acpsdk.SessionUpdate{
				AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{Content: acpsdk.TextBlock(text)},
			},
		})
	}

	feed(sid, "hello ")
	feed(sid, "world")
	// An unregistered session must be ignored (preserves discard behavior).
	feed(acpsdk.SessionId("other"), "leak")

	if got := sink.text(); got != "hello world" {
		t.Fatalf("captured = %q, want %q", got, "hello world")
	}

	// After unregister, further updates are dropped silently.
	c.unregister(sid)
	feed(sid, "ignored")
	if got := sink.text(); got != "hello world" {
		t.Fatalf("after unregister captured = %q, want unchanged", got)
	}
}

// --- in-memory end-to-end delegation test ---------------------------------

// fakeDelegAgent is a minimal acpsdk.Agent used to drive Delegate over an
// in-memory connection. On Prompt it streams emit as an agent message chunk
// (the way a real child would emit a <pando:conclusion> block) and returns
// end_turn, unless blockUntilCancel is set, in which case it waits for a
// session/cancel notification first.
type fakeDelegAgent struct {
	conn             *acpsdk.AgentSideConnection
	sessionID        acpsdk.SessionId
	emit             string
	blockUntilCancel bool

	mu          sync.Mutex
	initialized bool
	closed      bool
	cancelled   chan struct{}
}

func newFakeDelegAgent(sessionID, emit string) *fakeDelegAgent {
	return &fakeDelegAgent{
		sessionID: acpsdk.SessionId(sessionID),
		emit:      emit,
		cancelled: make(chan struct{}),
	}
}

func (f *fakeDelegAgent) Initialize(context.Context, acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	f.mu.Lock()
	f.initialized = true
	f.mu.Unlock()
	return acpsdk.InitializeResponse{ProtocolVersion: 1}, nil
}

func (f *fakeDelegAgent) NewSession(context.Context, acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	return acpsdk.NewSessionResponse{SessionId: f.sessionID}, nil
}

func (f *fakeDelegAgent) Prompt(ctx context.Context, _ acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	if f.blockUntilCancel {
		select {
		case <-f.cancelled:
			return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonCancelled}, nil
		case <-ctx.Done():
			return acpsdk.PromptResponse{}, ctx.Err()
		case <-time.After(5 * time.Second):
			return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn}, nil
		}
	}
	if f.emit != "" {
		_ = f.conn.SessionUpdate(ctx, acpsdk.SessionNotification{
			SessionId: f.sessionID,
			Update: acpsdk.SessionUpdate{
				AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{Content: acpsdk.TextBlock(f.emit)},
			},
		})
	}
	return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn}, nil
}

func (f *fakeDelegAgent) Cancel(_ context.Context, _ acpsdk.CancelNotification) error {
	f.mu.Lock()
	select {
	case <-f.cancelled:
	default:
		close(f.cancelled)
	}
	f.mu.Unlock()
	return nil
}

func (f *fakeDelegAgent) CloseSession(context.Context, acpsdk.CloseSessionRequest) (acpsdk.CloseSessionResponse, error) {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return acpsdk.CloseSessionResponse{}, nil
}

func (f *fakeDelegAgent) Authenticate(context.Context, acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, nil
}
func (f *fakeDelegAgent) ListSessions(context.Context, acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	return acpsdk.ListSessionsResponse{}, nil
}
func (f *fakeDelegAgent) ResumeSession(context.Context, acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error) {
	return acpsdk.ResumeSessionResponse{}, nil
}
func (f *fakeDelegAgent) SetSessionConfigOption(context.Context, acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	return acpsdk.SetSessionConfigOptionResponse{}, nil
}
func (f *fakeDelegAgent) SetSessionMode(context.Context, acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	return acpsdk.SetSessionModeResponse{}, nil
}
func (f *fakeDelegAgent) SetSessionModel(context.Context, acpsdk.SetSessionModelRequest) (acpsdk.SetSessionModelResponse, error) {
	return acpsdk.SetSessionModelResponse{}, nil
}

// wireDelegation builds a Manager with one in-memory instance whose connection
// is served by the given fake agent over a pair of io.Pipes (mirroring how the
// SDK's own tests pair connections without a real process).
func wireDelegation(t *testing.T, projectID string, agent *fakeDelegAgent) *Manager {
	t.Helper()

	c2aR, c2aW := io.Pipe() // client -> agent
	a2cR, a2cW := io.Pipe() // agent -> client

	delClient := newDelegationClient(Project{ID: projectID, Path: "/tmp"})
	clientConn := acpsdk.NewClientSideConnection(delClient, c2aW, a2cR)
	agentConn := acpsdk.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.conn = agentConn

	inst := &Instance{
		Project:   Project{ID: projectID, Path: "/tmp"},
		conn:      clientConn,
		delClient: delClient,
		ready:     make(chan struct{}),
		errCh:     make(chan error, 1),
	}
	close(inst.ready)

	m := &Manager{instances: map[string]*Instance{projectID: inst}}
	t.Cleanup(func() {
		_ = c2aW.Close()
		_ = a2cW.Close()
		_ = c2aR.Close()
		_ = a2cR.Close()
	})
	return m
}

func TestDelegateCapturesConclusionOverWire(t *testing.T) {
	const conclusion = "Working on it...\n<pando:conclusion>\nstatus: success\nsummary: done\n</pando:conclusion>\n"
	agent := newFakeDelegAgent("child-session-1", conclusion)
	m := wireDelegation(t, "proj-1", agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := m.Delegate(ctx, "proj-1", "do the task")
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if res.ChildSessionID != "child-session-1" {
		t.Errorf("ChildSessionID = %q, want child-session-1", res.ChildSessionID)
	}
	if res.StopReason != string(acpsdk.StopReasonEndTurn) {
		t.Errorf("StopReason = %q, want end_turn", res.StopReason)
	}
	if !strings.Contains(res.Output, "<pando:conclusion>") || !strings.Contains(res.Output, "summary: done") {
		t.Errorf("Output missing conclusion block: %q", res.Output)
	}

	agent.mu.Lock()
	initialized, closed := agent.initialized, agent.closed
	agent.mu.Unlock()
	if !initialized {
		t.Error("expected child Initialize handshake to have run")
	}
	if !closed {
		t.Error("expected delegated child session to be closed")
	}
}

func TestDelegateNoRunningInstance(t *testing.T) {
	m := &Manager{instances: map[string]*Instance{}}
	if _, err := m.Delegate(context.Background(), "missing", "x"); err != ErrInstanceNotRunning {
		t.Fatalf("err = %v, want ErrInstanceNotRunning", err)
	}
}

func TestDelegateContextCancelCancelsChildSession(t *testing.T) {
	agent := newFakeDelegAgent("child-session-2", "")
	agent.blockUntilCancel = true
	m := wireDelegation(t, "proj-2", agent)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	_, err := m.Delegate(ctx, "proj-2", "long task")
	if err == nil {
		t.Fatal("expected an error when context is cancelled mid-prompt")
	}

	select {
	case <-agent.cancelled:
		// good: the child received a session/cancel notification.
	case <-time.After(2 * time.Second):
		t.Fatal("child was not cancelled after context cancellation")
	}
}

// --- warm routing (Phase 7.3) ---------------------------------------------

// TestEnsureInstanceReusesRunning verifies EnsureInstance returns the existing
// in-memory instance without spawning a new child or consulting the service.
func TestEnsureInstanceReusesRunning(t *testing.T) {
	agent := newFakeDelegAgent("child-session-3", "")
	m := wireDelegation(t, "proj-3", agent)

	inst, err := m.EnsureInstance(context.Background(), "proj-3", false)
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	if inst == nil || inst.Project.ID != "proj-3" {
		t.Fatalf("EnsureInstance returned unexpected instance: %+v", inst)
	}
	// Reuse must never change the active project.
	if m.ActiveID() != "" {
		t.Errorf("ActiveID = %q, want empty (delegation must not switch focus)", m.ActiveID())
	}
}

// TestWarmDelegateReuseCaptures verifies WarmDelegate reuses a warm instance and
// captures the conclusion over the wire, honoring the concurrency cap.
func TestWarmDelegateReuseCaptures(t *testing.T) {
	const conclusion = "<pando:conclusion>\nstatus: success\nsummary: warm done\n</pando:conclusion>\n"
	agent := newFakeDelegAgent("child-session-4", conclusion)
	m := wireDelegation(t, "proj-4", agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := m.WarmDelegate(ctx, "proj-4", "", "do it", false /*autoStart*/, 4 /*maxConcurrent*/, 0 /*queueDepth*/, false /*allowExternal*/)
	if err != nil {
		t.Fatalf("WarmDelegate: %v", err)
	}
	if res.ChildSessionID != "child-session-4" {
		t.Errorf("ChildSessionID = %q, want child-session-4", res.ChildSessionID)
	}
	if !strings.Contains(res.Output, "summary: warm done") {
		t.Errorf("Output missing conclusion: %q", res.Output)
	}
	// The slot must be released after completion.
	if n := m.instances["proj-4"].InflightDelegations(); n != 0 {
		t.Errorf("inflight = %d after WarmDelegate, want 0", n)
	}
}

// TestWarmDelegateCapReached verifies that once the per-instance concurrency cap
// is reached, further warm delegations fall back (ErrWarmCapReached) instead of
// overloading the instance.
func TestWarmDelegateCapReached(t *testing.T) {
	agent := newFakeDelegAgent("child-session-5", "")
	agent.blockUntilCancel = true
	m := wireDelegation(t, "proj-5", agent)

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	// Start one delegation that blocks holding the only slot (maxConcurrent=1).
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = m.WarmDelegate(runCtx, "proj-5", "", "long", false, 1, 0, false)
	}()
	<-started

	// Wait until the slot is actually acquired.
	deadline := time.Now().Add(2 * time.Second)
	for m.instances["proj-5"].InflightDelegations() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("first delegation never acquired its slot")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A second delegation over the cap must be refused immediately.
	_, err := m.WarmDelegate(context.Background(), "proj-5", "", "second", false, 1, 0, false)
	if err != ErrWarmCapReached {
		t.Fatalf("err = %v, want ErrWarmCapReached", err)
	}

	runCancel() // release the blocking delegation
}

// --- Phase 7.4 bookkeeping ------------------------------------------------

// TestDelegationInfoReflectsState verifies DelegationInfo reports the in-flight
// count, the delegation-spawned flag, and running state for a manager-owned
// instance, and zero values for an unknown project.
func TestDelegationInfoReflectsState(t *testing.T) {
	inst := &Instance{Project: Project{ID: "proj-info"}}
	inst.markDelegationSpawned(true)
	if !inst.acquireDelegationSlot(0) || !inst.acquireDelegationSlot(0) {
		t.Fatal("failed to acquire delegation slots")
	}
	m := &Manager{instances: map[string]*Instance{"proj-info": inst}}

	inflight, spawned, running := m.DelegationInfo("proj-info")
	if inflight != 2 || !spawned || !running {
		t.Fatalf("DelegationInfo = (%d,%v,%v), want (2,true,true)", inflight, spawned, running)
	}

	inflight, spawned, running = m.DelegationInfo("nope")
	if inflight != 0 || spawned || running {
		t.Fatalf("DelegationInfo(unknown) = (%d,%v,%v), want (0,false,false)", inflight, spawned, running)
	}
}

// TestWarmDelegatePublishesDelegationEvents verifies that a warm delegation
// publishes an EvDelegationChanged event when the slot is acquired (count 1) and
// again when it is released (count 0), so the Projects panel can refresh live.
func TestWarmDelegatePublishesDelegationEvents(t *testing.T) {
	const conclusion = "<pando:conclusion>\nstatus: success\n</pando:conclusion>\n"
	agent := newFakeDelegAgent("child-session-6", conclusion)
	m := wireDelegation(t, "proj-ev", agent)
	m.broker = pubsub.NewBroker[ManagerEvent]()
	t.Cleanup(m.broker.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := m.broker.Subscribe(ctx)

	if _, err := m.WarmDelegate(ctx, "proj-ev", "", "do it", false, 4, 0, false); err != nil {
		t.Fatalf("WarmDelegate: %v", err)
	}

	// Drain the buffered events and record the delegation counts seen.
	var counts []int
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Payload.Type == EvDelegationChanged {
				counts = append(counts, ev.Payload.Count)
			}
			if len(counts) >= 2 {
				goto done
			}
		case <-deadline:
			goto done
		}
	}
done:
	if len(counts) < 2 || counts[0] != 1 || counts[len(counts)-1] != 0 {
		t.Fatalf("delegation event counts = %v, want first 1 and last 0", counts)
	}
}

// --- Phase 7.5 e2e ---------------------------------------------------------

// parallelFakeAgent serves several delegated sessions at once. Each NewSession
// gets a unique session id; Prompt signals its arrival and then blocks on a
// shared barrier so the test can assert that two prompts are simultaneously
// in-flight inside ONE warm instance (proving neither blocks the other). Each
// session emits a conclusion tagged with its own session id so the test can
// verify the captured output is not cross-wired between sessions.
type parallelFakeAgent struct {
	*fakeDelegAgent
	seq     int32
	arrived chan string   // session ids as they reach Prompt
	release chan struct{} // closed to release the barrier
}

func (p *parallelFakeAgent) NewSession(context.Context, acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	n := atomic.AddInt32(&p.seq, 1)
	return acpsdk.NewSessionResponse{SessionId: acpsdk.SessionId(fmt.Sprintf("sess-%d", n))}, nil
}

func (p *parallelFakeAgent) Prompt(ctx context.Context, req acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	select {
	case p.arrived <- string(req.SessionId):
	case <-ctx.Done():
		return acpsdk.PromptResponse{}, ctx.Err()
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return acpsdk.PromptResponse{}, ctx.Err()
	}
	emit := fmt.Sprintf("<pando:conclusion>\nstatus: success\nsummary: %s\n</pando:conclusion>\n", req.SessionId)
	_ = p.conn.SessionUpdate(ctx, acpsdk.SessionNotification{
		SessionId: req.SessionId,
		Update: acpsdk.SessionUpdate{
			AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{Content: acpsdk.TextBlock(emit)},
		},
	})
	return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn}, nil
}

// TestWarmInstanceServesParallelSessions verifies that a single warm instance
// serves 2 delegated sessions concurrently: both prompts are in-flight at the
// same time (the barrier only releases once both arrived), each captures its own
// conclusion without cross-talk, the in-flight peak is 2, and slots return to 0.
func TestWarmInstanceServesParallelSessions(t *testing.T) {
	const projectID = "proj-parallel"

	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()

	delClient := newDelegationClient(Project{ID: projectID, Path: "/tmp"})
	clientConn := acpsdk.NewClientSideConnection(delClient, c2aW, a2cR)

	agent := &parallelFakeAgent{
		fakeDelegAgent: newFakeDelegAgent("", ""),
		arrived:        make(chan string, 2),
		release:        make(chan struct{}),
	}
	agentConn := acpsdk.NewAgentSideConnection(agent, a2cW, c2aR)
	agent.conn = agentConn

	inst := &Instance{
		Project:   Project{ID: projectID, Path: "/tmp"},
		conn:      clientConn,
		delClient: delClient,
		ready:     make(chan struct{}),
		errCh:     make(chan error, 1),
	}
	close(inst.ready)
	m := &Manager{instances: map[string]*Instance{projectID: inst}}
	t.Cleanup(func() {
		_ = c2aW.Close()
		_ = a2cW.Close()
		_ = c2aR.Close()
		_ = a2cR.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type outcome struct {
		res *DelegateResult
		err error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			res, err := m.WarmDelegate(ctx, projectID, "", "do it", false, 2, 0, false)
			results <- outcome{res, err}
		}()
	}

	// Both prompts must arrive before either is released — proves the two
	// sessions run in parallel inside the one warm instance.
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case sid := <-agent.arrived:
			seen[sid] = true
		case <-ctx.Done():
			t.Fatalf("only %d prompt(s) arrived; parallel sessions appear to block", i)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 distinct sessions in-flight, got %v", seen)
	}
	if n := inst.InflightDelegations(); n != 2 {
		t.Errorf("inflight peak = %d, want 2", n)
	}
	close(agent.release)

	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("WarmDelegate: %v", r.err)
			}
			want := "summary: " + r.res.ChildSessionID
			if !strings.Contains(r.res.Output, want) {
				t.Errorf("session %s captured cross-wired output %q, want %q", r.res.ChildSessionID, r.res.Output, want)
			}
		case <-ctx.Done():
			t.Fatal("WarmDelegate did not complete after barrier release")
		}
	}
	if n := inst.InflightDelegations(); n != 0 {
		t.Errorf("inflight = %d after completion, want 0", n)
	}
}

// TestWarmDelegateDoesNotChangeActiveID verifies that running a delegated task
// through a warm instance never switches the user's focused (active) project.
func TestWarmDelegateDoesNotChangeActiveID(t *testing.T) {
	const conclusion = "<pando:conclusion>\nstatus: success\n</pando:conclusion>\n"
	agent := newFakeDelegAgent("child-session-active", conclusion)
	m := wireDelegation(t, "proj-active", agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := m.WarmDelegate(ctx, "proj-active", "", "go", false, 4, 0, false); err != nil {
		t.Fatalf("WarmDelegate: %v", err)
	}
	if m.ActiveID() != "" {
		t.Errorf("ActiveID = %q, want empty (delegation must not switch focus)", m.ActiveID())
	}
}

// TestStopReportCancelsInflightDelegations verifies decision 3: stopping an
// instance that has in-flight delegated sessions reports the cancelled count,
// cancels the instance, and removes it from the manager (the orchestrator then
// takes the cold-path fallback, reaching a terminal conclusion — covered by the
// orchestrator's TestTryStartWarmRunFailureIsTerminal).
func TestStopReportCancelsInflightDelegations(t *testing.T) {
	const projectID = "proj-stop"

	cancelled := false
	inst := &Instance{
		Project: Project{ID: projectID, Path: "/tmp"},
		cmd:     &exec.Cmd{}, // Process nil → SIGTERM/Kill guards are skipped
		cancel:  func() { cancelled = true },
		errCh:   make(chan error, 1),
	}
	close(inst.errCh) // StopReport's drain returns immediately
	// Two delegated sessions are in flight when the stop arrives.
	if !inst.acquireDelegationSlot(0) || !inst.acquireDelegationSlot(0) {
		t.Fatal("failed to acquire delegation slots")
	}

	m := &Manager{
		instances: map[string]*Instance{projectID: inst},
		broker:    pubsub.NewBroker[ManagerEvent](),
	}
	t.Cleanup(m.broker.Shutdown)

	n, err := m.StopReport(context.Background(), projectID)
	if err != nil {
		t.Fatalf("StopReport: %v", err)
	}
	if n != 2 {
		t.Errorf("cancelled = %d, want 2", n)
	}
	if !cancelled {
		t.Error("instance cancel func was not called")
	}
	if _, ok := m.instances[projectID]; ok {
		t.Error("instance still present after StopReport, want removed")
	}
}

// --- B3 hot-peer IPC delegation -------------------------------------------

// TestExternalDelegationSentinelsDistinct verifies the two new error sentinels
// are defined, non-nil, and distinct from each other and from existing errors.
func TestExternalDelegationSentinelsDistinct(t *testing.T) {
	if ErrExternalDelegationRefused == nil {
		t.Fatal("ErrExternalDelegationRefused is nil")
	}
	if ErrExternalUnreachable == nil {
		t.Fatal("ErrExternalUnreachable is nil")
	}
	if ErrExternalDelegationRefused == ErrExternalUnreachable {
		t.Fatal("sentinels must be distinct")
	}
	if ErrExternalDelegationRefused == ErrExternalInstance {
		t.Fatal("ErrExternalDelegationRefused must differ from ErrExternalInstance")
	}
	if ErrExternalUnreachable == ErrExternalInstance {
		t.Fatal("ErrExternalUnreachable must differ from ErrExternalInstance")
	}
}

// writeFakeLockFile creates a minimal .pando/ipc.lock in dir with the given pid
// and rpcPort so that ReadLockForPath / Runtime detect an "external" instance.
func writeFakeLockFile(t *testing.T, dir string, pid, rpcPort int) {
	t.Helper()
	pandoDir := filepath.Join(dir, ".pando")
	if err := os.MkdirAll(pandoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info := map[string]interface{}{
		"instance_id": "fake-external",
		"pid":         pid,
		"pub_port":    0,
		"rpc_port":    rpcPort,
		"started_at":  time.Now(),
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pandoDir, "ipc.lock"), data, 0o644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
}

// TestDelegateExternalNoLockFile verifies that DelegateExternal returns
// ErrExternalUnreachable when no IPC lock file exists for the project path.
func TestDelegateExternalNoLockFile(t *testing.T) {
	m := &Manager{instances: map[string]*Instance{}}
	dir := t.TempDir() // empty — no .pando/ipc.lock

	_, err := m.DelegateExternal(context.Background(), "proj-x", dir, "hello", "corr-1")
	if err != ErrExternalUnreachable {
		t.Fatalf("err = %v, want ErrExternalUnreachable", err)
	}
}

// TestDelegateExternalDeadPid verifies that DelegateExternal returns
// ErrExternalUnreachable when the lock file exists but the PID is not alive.
func TestDelegateExternalDeadPid(t *testing.T) {
	dir := t.TempDir()
	// PID 0 is never a live user process.
	writeFakeLockFile(t, dir, 0, 9999)

	m := &Manager{instances: map[string]*Instance{}}

	_, err := m.DelegateExternal(context.Background(), "proj-x", dir, "hello", "corr-2")
	if err != ErrExternalUnreachable {
		t.Fatalf("err = %v, want ErrExternalUnreachable", err)
	}
}

// TestWarmDelegateExternalFalsePassthrough verifies that with allowExternal=false
// and an external instance present, WarmDelegate still returns ErrExternalInstance
// (unchanged legacy behaviour — no IPC call is made).
func TestWarmDelegateExternalFalsePassthrough(t *testing.T) {
	dir := t.TempDir()
	// Write a lock file with the current process PID so pidIsAlive returns true,
	// making Runtime see a live external instance.
	writeFakeLockFile(t, dir, os.Getpid(), 19999)

	// Build a service that knows about this project.
	svc := &fakeExtService{proj: Project{ID: "proj-ext", Path: dir}}
	m := &Manager{
		service:   svc,
		instances: map[string]*Instance{},
	}

	_, err := m.WarmDelegate(context.Background(), "proj-ext", dir, "prompt", false, 4, 0, false)
	if err != ErrExternalInstance {
		t.Fatalf("err = %v, want ErrExternalInstance (allowExternal=false must not route over IPC)", err)
	}
}

// fakeExtService is a minimal Service implementation used by external delegation
// tests. Only Get and GetByPath need to work.
type fakeExtService struct {
	proj Project
}

func (f *fakeExtService) Create(_ context.Context, _, _ string) (*Project, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeExtService) Get(_ context.Context, id string) (*Project, error) {
	if id == f.proj.ID {
		p := f.proj
		return &p, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeExtService) GetByPath(_ context.Context, path string) (*Project, error) {
	if path == f.proj.Path {
		p := f.proj
		return &p, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeExtService) List(_ context.Context) ([]Project, error)   { return []Project{f.proj}, nil }
func (f *fakeExtService) Delete(_ context.Context, _ string) error    { return nil }
func (f *fakeExtService) Rename(_ context.Context, _, _ string) error { return nil }
func (f *fakeExtService) UpdateStatus(_ context.Context, _, _ string, _, _ int) error {
	return nil
}
func (f *fakeExtService) MarkInitialized(_ context.Context, _ string) error { return nil }
func (f *fakeExtService) TouchLastOpened(_ context.Context, _ string) error { return nil }
