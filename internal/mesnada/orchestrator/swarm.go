package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// SwarmWorkerSpec is one parallel worker card in a swarm.
type SwarmWorkerSpec struct {
	Prompt  string
	Engine  string
	Model   string
	Persona string
}

// SwarmRoleSpec carries optional engine/model/persona overrides and extra
// instructions for the verifier or synthesizer role.
type SwarmRoleSpec struct {
	Engine  string
	Model   string
	Persona string
	Extra   string // appended to the built-in brief
}

// SwarmSpec describes a fan-out → verify → synthesize workgraph to create.
type SwarmSpec struct {
	Goal        string
	Workers     []SwarmWorkerSpec
	Verifier    SwarmRoleSpec
	Synthesizer SwarmRoleSpec
	WorkDir     string
	// ParentSessionID is the calling agent session; it becomes the shared swarm id
	// so every card sees the same blackboard (P1) and inherits conclusion
	// forwarding (P2). Required — without it the swarm has no coordination scope.
	ParentSessionID string
	ProjectID       string
	ProjectPath     string
	Depth           int
	// IdempotencyKey, when set, makes CreateSwarm return the existing topology on a
	// repeated call instead of duplicating the graph (keyed within the swarm's
	// blackboard). Empty ⇒ always create fresh.
	IdempotencyKey string
}

// SwarmCreated holds the ids produced by CreateSwarm.
type SwarmCreated struct {
	WorkerIDs     []string `json:"worker_ids"`
	VerifierID    string   `json:"verifier_id"`
	SynthesizerID string   `json:"synthesizer_id"`
}

const swarmTopologyKeyPrefix = "swarm:topology:"

// CreateSwarm builds a durable fan-out → verify(gate) → synthesize workgraph on
// top of the existing Dependencies DAG: parallel workers start immediately, the
// verifier waits for every worker, and the synthesizer gates on the verifier's
// conclusion (GateDeps) so it only starts once the verifier closes with
// status=success. Every card shares ParentSessionID, so P1 blackboard coordination
// and P2 conclusion forwarding apply automatically.
func (o *Orchestrator) CreateSwarm(ctx context.Context, spec SwarmSpec) (*SwarmCreated, error) {
	goal := strings.TrimSpace(spec.Goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}
	if len(spec.Workers) == 0 {
		return nil, fmt.Errorf("at least one worker is required")
	}
	if strings.TrimSpace(spec.ParentSessionID) == "" {
		return nil, fmt.Errorf("parent_session_id is required (it is the swarm coordination id)")
	}
	for i, w := range spec.Workers {
		if strings.TrimSpace(w.Prompt) == "" {
			return nil, fmt.Errorf("workers[%d].prompt is required", i)
		}
	}

	// Idempotency: return the existing topology if this key already produced one
	// and its tasks still exist.
	if spec.IdempotencyKey != "" {
		if existing := o.existingSwarm(spec.ParentSessionID, spec.IdempotencyKey); existing != nil {
			return existing, nil
		}
	}

	base := func() models.SpawnRequest {
		return models.SpawnRequest{
			WorkDir:         spec.WorkDir,
			Background:      true,
			ParentSessionID: spec.ParentSessionID,
			ProjectID:       spec.ProjectID,
			ProjectPath:     spec.ProjectPath,
			Depth:           spec.Depth,
		}
	}

	// Workers — no dependencies, start immediately.
	workerIDs := make([]string, 0, len(spec.Workers))
	for i, w := range spec.Workers {
		req := base()
		req.Prompt = w.Prompt
		req.Engine = models.Engine(w.Engine)
		req.Model = w.Model
		req.Persona = w.Persona
		task, err := o.Spawn(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to spawn worker %d: %w", i, err)
		}
		workerIDs = append(workerIDs, task.ID)
	}

	// Verifier — waits for every worker; its conclusion is the gate.
	vReq := base()
	vReq.Prompt = verifierBrief(goal, spec.Verifier.Extra)
	vReq.Engine = models.Engine(spec.Verifier.Engine)
	vReq.Model = spec.Verifier.Model
	vReq.Persona = spec.Verifier.Persona
	vReq.Dependencies = workerIDs
	verifier, err := o.Spawn(ctx, vReq)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn verifier: %w", err)
	}

	// Synthesizer — gates on the verifier passing (GateDeps).
	sReq := base()
	sReq.Prompt = synthesizerBrief(goal, spec.Synthesizer.Extra)
	sReq.Engine = models.Engine(spec.Synthesizer.Engine)
	sReq.Model = spec.Synthesizer.Model
	sReq.Persona = spec.Synthesizer.Persona
	sReq.Dependencies = []string{verifier.ID}
	sReq.GateDeps = []string{verifier.ID}
	synth, err := o.Spawn(ctx, sReq)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn synthesizer: %w", err)
	}

	created := &SwarmCreated{
		WorkerIDs:     workerIDs,
		VerifierID:    verifier.ID,
		SynthesizerID: synth.ID,
	}

	// Persist the topology on the blackboard for idempotency + audit.
	if payload, err := json.Marshal(created); err == nil {
		_ = o.PostNote(spec.ParentSessionID, swarmTopologyKeyPrefix+swarmKeyOrGoal(spec), payload, "swarm-orchestrator", "")
	}

	return created, nil
}

// existingSwarm returns a previously created topology for this idempotency key if
// its tasks still exist, else nil.
func (o *Orchestrator) existingSwarm(swarmID, key string) *SwarmCreated {
	want := swarmTopologyKeyPrefix + key
	for _, e := range o.ListNotes(swarmID) {
		if e.Key != want {
			continue
		}
		var created SwarmCreated
		if err := json.Unmarshal(e.Value, &created); err != nil {
			return nil
		}
		if created.VerifierID == "" || created.SynthesizerID == "" {
			return nil
		}
		if _, err := o.store.Get(created.SynthesizerID); err != nil {
			return nil // topology was cleaned up; allow a fresh create
		}
		return &created
	}
	return nil
}

// swarmKeyOrGoal returns the idempotency key used to store/lookup the topology.
func swarmKeyOrGoal(spec SwarmSpec) string {
	if spec.IdempotencyKey != "" {
		return spec.IdempotencyKey
	}
	// No explicit key: derive a stable-ish label from the goal so audit notes are
	// readable. Not used for dedup (dedup only runs when IdempotencyKey is set).
	g := strings.TrimSpace(spec.Goal)
	if len(g) > 40 {
		g = g[:40]
	}
	return g
}

func verifierBrief(goal, extra string) string {
	b := "You are the swarm VERIFIER.\n\n" +
		"Goal:\n" + goal + "\n\n" +
		"Review every worker's handoff (see the dependency conclusions) and the shared " +
		"blackboard. Decide whether the combined evidence fully satisfies the goal.\n\n" +
		"Close your run with a <pando:conclusion> block:\n" +
		"- status=success ONLY when the evidence is sufficient and correct.\n" +
		"- status=blocked otherwise, and state the EXACT missing or wrong work in the summary.\n\n" +
		"The synthesizer will not start until you pass (status=success)."
	if strings.TrimSpace(extra) != "" {
		b += "\n\n" + extra
	}
	return b
}

func synthesizerBrief(goal, extra string) string {
	b := "You are the swarm SYNTHESIZER.\n\n" +
		"Goal:\n" + goal + "\n\n" +
		"The verifier has passed the gate. Combine the verified worker outputs (see the " +
		"dependency conclusions and the shared blackboard) into the final deliverable. Do not " +
		"redo the workers' work; integrate and reconcile it. Close with a <pando:conclusion> block."
	if strings.TrimSpace(extra) != "" {
		b += "\n\n" + extra
	}
	return b
}
