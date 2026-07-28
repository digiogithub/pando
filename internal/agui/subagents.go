package agui

import (
	"encoding/json"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	toon "github.com/toon-format/toon-go"
)

// decodeToolResult decodes a structured tool result into v.
//
// Pando's tools do not answer in JSON: tools.NewStructuredResponse renders TOON
// when it can, TOML next and indented JSON only as a last resort, and it picks
// per value — so the same tool can answer in different formats on different
// calls. Every format is tried here rather than assuming one, and the non-JSON
// ones are routed back through JSON so the struct tags stay the single
// description of the shape.
func decodeToolResult(content string, v any) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return fmt.Errorf("empty tool result")
	}
	if err := json.Unmarshal([]byte(trimmed), v); err == nil {
		return nil
	}
	if decoded, err := toon.DecodeString(trimmed); err == nil {
		if reencoded, err := json.Marshal(decoded); err == nil {
			if err := json.Unmarshal(reencoded, v); err == nil {
				return nil
			}
		}
	}
	return toml.Unmarshal([]byte(trimmed), v)
}

// Mesnada sub-agents in the shared-state document (P7).
//
// Pando delegates work to sub-agents through the mesnada_* tools. Those calls
// already stream to the client as ordinary AG-UI tool calls, but a chat
// transcript is a poor place to watch a fan-out: the page wants a list it can
// render as cards and update in place.
//
// This file derives that list from the tool traffic the adapter already
// observes. Nothing is read from the orchestrator and no new agent event type
// exists (invariants I1 and I4): a delegated task appears because the agent
// called a tool that mentions it, and its status advances when the agent looks
// it up again. The list is therefore as fresh as the agent's own knowledge —
// which is exactly what the page should show, since the agent is the one
// deciding what to do with it.

// Tool names whose results describe delegated tasks.
const (
	toolMesnadaSpawn = "mesnada_spawn_agent"
	toolMesnadaGet   = "mesnada_get_task"
	toolMesnadaList  = "mesnada_list_tasks"
	toolMesnadaWait  = "mesnada_wait_task"
	toolMesnadaAwait = "mesnada_await"
	toolMesnadaCanc  = "mesnada_cancel_task"
	toolMesnadaSwarm = "mesnada_swarm"
)

// maxSubAgents caps the rendered list. A long-running thread can spawn far more
// tasks than a sidebar can show, and the state document is re-sent in full on
// every run: it must not grow without bound.
const maxSubAgents = 50

// maxSubAgentPrompt truncates the prompt echoed to the page. The full prompt can
// be thousands of tokens and is already visible in the tool call.
const maxSubAgentPrompt = 240

// Swarm roles, reported so a page can lay out a fan-out topology.
const (
	subAgentRoleWorker      = "worker"
	subAgentRoleVerifier    = "verifier"
	subAgentRoleSynthesizer = "synthesizer"
)

// SubAgentState is one delegated mesnada task as published to the client.
// Field names are camelCase because the JSON pointers built from them are part
// of the protocol contract, like the rest of StateDoc.
type SubAgentState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Role is set for tasks created by mesnada_swarm, which assigns fixed roles.
	Role     string `json:"role,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
	Engine   string `json:"engine,omitempty"`
	Model    string `json:"model,omitempty"`
	Persona  string `json:"persona,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	// Conclusion is the delegated task's self-reported outcome
	// (success|partial|failed|blocked), present once the task concluded.
	Conclusion string `json:"conclusion,omitempty"`
	// Summary is the conclusion's one-paragraph summary, when captured.
	Summary string `json:"summary,omitempty"`
}

// mesnadaTask is the subset of models.Task the page cares about. It is decoded
// structurally rather than by importing the mesnada model, so this adapter does
// not pin itself to that package's evolution.
type mesnadaTask struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Status   string `json:"status"`
	Engine   string `json:"engine"`
	Model    string `json:"model"`
	Persona  string `json:"persona"`
	Error    string `json:"error"`
	ExitCode *int   `json:"exit_code"`

	Conclusion *struct {
		Status  string `json:"status"`
		Summary string `json:"summary"`
	} `json:"conclusion"`
}

// mesnadaResult is the union of the shapes the mesnada_* tools return.
type mesnadaResult struct {
	// Spawn / relaunch / cancel report the task inline.
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Error    string `json:"error"`
	ExitCode *int   `json:"exit_code"`

	// Get / wait wrap a full task; list returns summaries.
	Task  *mesnadaTask  `json:"task"`
	Tasks []mesnadaTask `json:"tasks"`

	// Swarm reports the topology it created.
	WorkerIDs     []string `json:"worker_ids"`
	VerifierID    string   `json:"verifier_id"`
	SynthesizerID string   `json:"synthesizer_id"`
}

// observeMesnadaLocked folds a mesnada tool result into the sub-agent list and
// returns the deltas describing the change. It reports handled=false for tools
// that say nothing about delegated tasks, so the caller can carry on with its
// other rules.
//
// The caller must hold s.mu.
func (s *stateTracker) observeMesnadaLocked(name, input, content string) (events []Event, handled bool) {
	switch strings.ToLower(name) {
	case toolMesnadaSpawn, toolMesnadaGet, toolMesnadaList,
		toolMesnadaWait, toolMesnadaCanc, toolMesnadaSwarm:
	case toolMesnadaAwait:
		return s.observeAwaitLocked(input), true
	default:
		return nil, false
	}

	var res mesnadaResult
	if err := decodeToolResult(content, &res); err != nil {
		// A tool that answered with prose (an error message, typically) tells us
		// nothing about the topology; the tool call itself is already streamed.
		return nil, true
	}

	var out []Event

	// A swarm creates its whole topology in one call, before any of the tasks
	// has a status worth reporting.
	for _, id := range res.WorkerIDs {
		out = append(out, s.upsertSubAgentLocked(SubAgentState{
			ID: id, Status: "pending", Role: subAgentRoleWorker,
		})...)
	}
	if res.VerifierID != "" {
		out = append(out, s.upsertSubAgentLocked(SubAgentState{
			ID: res.VerifierID, Status: "pending", Role: subAgentRoleVerifier,
		})...)
	}
	if res.SynthesizerID != "" {
		out = append(out, s.upsertSubAgentLocked(SubAgentState{
			ID: res.SynthesizerID, Status: "pending", Role: subAgentRoleSynthesizer,
		})...)
	}

	// Spawn and relaunch answer with the task inline. The prompt is not echoed
	// back, so it is recovered from the call's own arguments.
	if res.TaskID != "" {
		entry := SubAgentState{
			ID:       res.TaskID,
			Status:   res.Status,
			Error:    res.Error,
			ExitCode: res.ExitCode,
			Prompt:   truncatePrompt(subAgentPromptFromInput(input)),
		}
		out = append(out, s.upsertSubAgentLocked(entry)...)
	}

	if res.Task != nil {
		out = append(out, s.upsertSubAgentLocked(subAgentFromTask(*res.Task))...)
	}

	// A listing is a query over every task in the store, including tasks this
	// thread never spawned and tasks belonging to other projects. Only entries
	// already on the board are refreshed from it, so the page's list stays "what
	// this conversation delegated" instead of drifting into a global task table.
	for _, task := range res.Tasks {
		if _, known := s.subAgentIndex[task.ID]; !known {
			continue
		}
		out = append(out, s.upsertSubAgentLocked(subAgentFromTask(task))...)
	}

	return out, true
}

// observeAwaitLocked marks the tasks a mesnada_await call is blocking on. The
// tool answers with prose and puts the ids in its metadata, which the tool
// result does not carry here, so the ids come from the call's arguments.
func (s *stateTracker) observeAwaitLocked(input string) []Event {
	var params struct {
		TaskIDs []string `json:"task_ids"`
	}
	if input == "" || json.Unmarshal([]byte(input), &params) != nil {
		return nil
	}
	var out []Event
	for _, id := range params.TaskIDs {
		if id == "" {
			continue
		}
		out = append(out, s.upsertSubAgentLocked(SubAgentState{ID: id, Status: "awaited"})...)
	}
	return out
}

// upsertSubAgentLocked adds or refreshes one entry and returns the delta, or nil
// when nothing observable changed.
//
// Fields absent from the incoming entry never erase what is already known: a
// cancel result reports a status and nothing else, and it must not blank out the
// prompt a previous spawn recorded.
func (s *stateTracker) upsertSubAgentLocked(entry SubAgentState) []Event {
	if entry.ID == "" {
		return nil
	}
	if idx, ok := s.subAgentIndex[entry.ID]; ok {
		merged := mergeSubAgent(s.doc.SubAgents[idx], entry)
		if sameJSON(s.doc.SubAgents[idx], merged) {
			return nil
		}
		s.doc.SubAgents[idx] = merged
		return []Event{NewStateDelta([]JSONPatchOperation{
			{Op: "replace", Path: jsonPointerIndex("/subAgents", idx), Value: merged},
		})}
	}
	if len(s.doc.SubAgents) >= maxSubAgents {
		return nil
	}
	if entry.Status == "" {
		entry.Status = "unknown"
	}
	s.subAgentIndex[entry.ID] = len(s.doc.SubAgents)
	s.doc.SubAgents = append(s.doc.SubAgents, entry)
	return []Event{NewStateDelta([]JSONPatchOperation{
		{Op: "add", Path: "/subAgents/-", Value: entry},
	})}
}

// mergeSubAgent overlays the non-empty fields of update onto prev.
func mergeSubAgent(prev, update SubAgentState) SubAgentState {
	out := prev
	if update.Status != "" {
		out.Status = update.Status
	}
	if update.Role != "" {
		out.Role = update.Role
	}
	if update.Prompt != "" {
		out.Prompt = update.Prompt
	}
	if update.Engine != "" {
		out.Engine = update.Engine
	}
	if update.Model != "" {
		out.Model = update.Model
	}
	if update.Persona != "" {
		out.Persona = update.Persona
	}
	if update.Error != "" {
		out.Error = update.Error
	}
	if update.ExitCode != nil {
		out.ExitCode = update.ExitCode
	}
	if update.Conclusion != "" {
		out.Conclusion = update.Conclusion
	}
	if update.Summary != "" {
		out.Summary = update.Summary
	}
	return out
}

func subAgentFromTask(task mesnadaTask) SubAgentState {
	out := SubAgentState{
		ID:       task.ID,
		Status:   task.Status,
		Prompt:   truncatePrompt(task.Prompt),
		Engine:   task.Engine,
		Model:    task.Model,
		Persona:  task.Persona,
		Error:    task.Error,
		ExitCode: task.ExitCode,
	}
	if task.Conclusion != nil {
		out.Conclusion = task.Conclusion.Status
		out.Summary = truncatePrompt(task.Conclusion.Summary)
	}
	return out
}

// subAgentPromptFromInput pulls the delegated prompt out of a spawn call.
func subAgentPromptFromInput(input string) string {
	if input == "" {
		return ""
	}
	var params struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(input), &params) != nil {
		return ""
	}
	return params.Prompt
}

func truncatePrompt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxSubAgentPrompt {
		return s
	}
	// Cut on a rune boundary: the value is copied into JSON as-is.
	cut := maxSubAgentPrompt
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
