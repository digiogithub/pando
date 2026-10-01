package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/message"
)

// fakeSelectorLLM is the persona-selector agent's LLM.
type fakeSelectorLLM struct {
	mu     sync.Mutex
	calls  int
	answer string
	err    error
}

func (f *fakeSelectorLLM) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &provider.ProviderResponse{Content: f.answer}, nil
}

func (f *fakeSelectorLLM) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	return nil
}

func (f *fakeSelectorLLM) Model() models.Model { return models.Model{ID: "fake-selector"} }

func (f *fakeSelectorLLM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// personaEnv wires the Auto harness (config, fake router, agent) to a persona
// manager and a persona-selector whose LLM is a counting fake.
type personaEnv struct {
	*autoEnv
	llm *fakeSelectorLLM
	mgr *persona.Manager
}

const (
	personaAssistantBody = "# Assistant\nGeneral purpose assistant instructions."
	personaQABody        = "# QA\nQuality assurance instructions."
	personaSEBody        = "# Software engineer\nSoftware engineering instructions."
)

func newPersonaEnv(t *testing.T, decision bool, opts ...autoEnvOption) *personaEnv {
	t.Helper()
	all := append([]autoEnvOption{func(c *config.Config) {
		c.PersonaAutoSelect = config.PersonaAutoSelectConfig{Enabled: true}
		c.Agents[config.AgentPersonaSelector] = config.Agent{Model: autoTask, UseDecisionModel: decision}
	}}, opts...)
	env := newAutoEnv(t, all...)

	dir := t.TempDir()
	files := map[string]string{
		"assistant.md":         "---\ndescription: General questions and small talk\n---\n" + personaAssistantBody,
		"qa.md":                "---\ndescription: Testing, test plans and bug reproduction\n---\n" + personaQABody,
		"software-engineer.md": "---\ndescription: Writing and refactoring code\n---\n" + personaSEBody,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := persona.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	llm := &fakeSelectorLLM{answer: "none"}

	prevMgr, prevSel := GetPersonaManager(), personaSelector()
	SetPersonaManager(mgr)
	SetPersonaSelector(&PersonaSelector{selectorProvider: llm, lazy: true})
	setActivePersonaForTest("")
	resetPersonaState()
	t.Cleanup(func() {
		SetPersonaManager(prevMgr)
		SetPersonaSelector(prevSel)
		setActivePersonaForTest("")
		resetPersonaState()
	})
	return &personaEnv{autoEnv: env, llm: llm, mgr: mgr}
}

func (e *personaEnv) personaSession(id string) string {
	e.t.Helper()
	e.t.Cleanup(func() { ForgetSessionPersona(id) })
	return e.session(id, autoCoder)
}

// answerPersona scripts the router's answer to the persona question.
func (e *personaEnv) answerPersona(key string, p float64) {
	e.srv.SetQuestionDecision("persona", key, map[string]float64{key: p, "none": 1 - p})
}

func personaCtx(sid string) context.Context {
	return context.WithValue(context.Background(), prompt.SessionIDKey, sid)
}

func resolveTurn(sid, text string) personaOutcome {
	return resolvePersonaContent(personaCtx(sid), personaRequest{Prompt: text, Eligible: true})
}

func personaNotices(events []AgentEvent) []string {
	var out []string
	for _, n := range notices(events) {
		if strings.HasPrefix(n, "Persona") {
			out = append(out, n)
		}
	}
	return out
}

func TestPersonaOptionOffUsesLLMUnchanged(t *testing.T) {
	env := newPersonaEnv(t, false)
	sid := env.personaSession("p-off")
	env.llm.answer = "qa"

	out := resolveTurn(sid, "write a test plan")
	if out.Content != env.mgr.GetPersona("qa") || out.Info != nil || out.Warning != "" {
		t.Fatalf("outcome = %+v", out)
	}
	if env.llm.callCount() != 1 || env.routerCalls() != 0 {
		t.Fatalf("llm calls = %d router calls = %d, want 1 and 0", env.llm.callCount(), env.routerCalls())
	}

	// No sticky logic when the option is off: "none" means no persona.
	env.llm.answer = "none"
	if out := resolveTurn(sid, "ok"); out.Content != "" {
		t.Fatalf("content = %q, want none", out.Content)
	}
}

func TestPersonaDecisionMatched(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-matched")
	env.answerPersona("qa", 0.9)

	out := resolveTurn(sid, "write a test plan")
	if out.Name != "qa" || out.Content != env.mgr.GetPersona("qa") {
		t.Fatalf("outcome = %+v", out)
	}
	if out.Info == nil || out.Info.Source != PersonaSourceDecision || !out.Info.Changed || out.Info.Probability < 0.89 {
		t.Fatalf("info = %+v", out.Info)
	}
	if env.routerCalls() != 1 || env.llm.callCount() != 0 {
		t.Fatalf("router calls = %d llm calls = %d, want 1 and 0", env.routerCalls(), env.llm.callCount())
	}
	last, ok := LastPersonaRouting(sid)
	if !ok || last.Persona != "qa" || last.Source != PersonaSourceDecision {
		t.Fatalf("LastPersonaRouting = %+v %v", last, ok)
	}
}

func TestPersonaNoneKeepsPreviousOrAssistant(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-sticky")

	// Nothing applied before: none falls back to assistant.
	env.answerPersona("none", 0.9)
	out := resolveTurn(sid, "hello")
	if out.Name != "assistant" || out.Info.Source != PersonaSourceDefault {
		t.Fatalf("first turn = %+v / %+v", out, out.Info)
	}

	env.answerPersona("qa", 0.9)
	if out := resolveTurn(sid, "write a test plan"); out.Name != "qa" {
		t.Fatalf("matched turn = %+v", out)
	}

	env.answerPersona("none", 0.9)
	out = resolveTurn(sid, "yes, do it")
	if out.Name != "qa" || out.Info.Source != PersonaSourceSticky || out.Info.Changed {
		t.Fatalf("none turn = %+v / %+v", out, out.Info)
	}

	// Low probability keeps it as well.
	env.answerPersona("software_engineer", 0.4)
	out = resolveTurn(sid, "maybe refactor?")
	if out.Name != "qa" || out.Info.Source != PersonaSourceSticky || out.Info.Reason != "low_probability" {
		t.Fatalf("low probability turn = %+v / %+v", out, out.Info)
	}
	if env.llm.callCount() != 0 {
		t.Fatalf("the LLM must not be called for a valid decision, calls = %d", env.llm.callCount())
	}
}

func TestPersonaRouterErrorFallsBackToLLM(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-fallback")
	env.srv.SetRawResponse("not json")
	env.llm.answer = "software-engineer"

	out := resolveTurn(sid, "refactor this")
	if out.Name != "software-engineer" || out.Info.Source != PersonaSourceLLM {
		t.Fatalf("outcome = %+v / %+v", out, out.Info)
	}
	if env.llm.callCount() != 1 {
		t.Fatalf("llm calls = %d, want 1", env.llm.callCount())
	}
	if !strings.Contains(out.Warning, "Persona auto-select: decision model unavailable") ||
		!strings.Contains(out.Warning, "using the fallback model") {
		t.Fatalf("warning = %q", out.Warning)
	}
	if got := personaNotice(*out.Info); got != "Persona: software-engineer (fallback model)" {
		t.Fatalf("notice = %q", got)
	}
}

func TestPersonaRouterAndLLMUnavailableNeverBlocks(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-both-down")
	env.srv.Close()
	env.llm.err = errors.New("boom")

	out := resolveTurn(sid, "do it")
	if out.Name != "assistant" || out.Info.Source != PersonaSourceDefault {
		t.Fatalf("outcome = %+v / %+v", out, out.Info)
	}

	// With a previous persona the sticky one is used.
	recordPersona(sid, "qa", nil)
	if out := resolveTurn(sid, "again"); out.Name != "qa" || out.Info.Source != PersonaSourceSticky {
		t.Fatalf("outcome = %+v / %+v", out, out.Info)
	}

	// No selector at all behaves the same.
	SetPersonaSelector(nil)
	if out := resolveTurn(sid, "again"); out.Name != "qa" {
		t.Fatalf("outcome without selector = %+v", out)
	}
}

func TestPersonaCooldownSkipsDecisionModelThenRetries(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-cooldown")
	now := time.Now()
	autoClock = func() time.Time { return now }
	env.srv.SetRawResponse("not json")

	resolveTurn(sid, "one")
	if env.routerCalls() != 1 {
		t.Fatalf("router calls = %d, want 1", env.routerCalls())
	}
	now = now.Add(30 * time.Second)
	out := resolveTurn(sid, "two")
	if env.routerCalls() != 1 {
		t.Fatalf("router calls during cooldown = %d, want still 1", env.routerCalls())
	}
	if env.llm.callCount() != 2 {
		t.Fatalf("llm calls = %d, want 2 (straight to the fallback)", env.llm.callCount())
	}
	if out.Info == nil || out.Info.ErrClass == "" {
		t.Fatalf("cooldown info = %+v", out.Info)
	}

	now = now.Add(31 * time.Second)
	env.srv.SetDecision("none", nil) // clears the scripted malformed body
	env.answerPersona("qa", 0.9)
	out = resolveTurn(sid, "three")
	if env.routerCalls() != 2 || out.Name != "qa" {
		t.Fatalf("after the cooldown: router calls = %d outcome = %+v", env.routerCalls(), out)
	}
}

func TestPersonaManualPersonaMakesNoCalls(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-manual")
	setActivePersonaForTest("qa")

	out := resolveTurn(sid, "anything")
	if out.Content != env.mgr.GetPersona("qa") || out.Info != nil {
		t.Fatalf("outcome = %+v", out)
	}
	if env.routerCalls() != 0 || env.llm.callCount() != 0 {
		t.Fatalf("calls: router %d llm %d, want none", env.routerCalls(), env.llm.callCount())
	}

	// An explicit per-session persona wins over the manual one, also without calls.
	SetSessionLLMOverrides(sid, SessionLLMOverrides{Persona: "software-engineer", PersonaScoped: true})
	t.Cleanup(func() { SetSessionLLMOverrides(sid, SessionLLMOverrides{}) })
	out = resolveTurn(sid, "anything")
	if out.Content != env.mgr.GetPersona("software-engineer") {
		t.Fatalf("content = %q", out.Content)
	}
	if env.routerCalls() != 0 || env.llm.callCount() != 0 {
		t.Fatal("an explicit persona must not trigger any call")
	}
}

func TestPersonaNonEligibleRunsReuseRememberedPersona(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-system")
	env.answerPersona("qa", 0.9)
	resolveTurn(sid, "write a test plan")
	calls := env.routerCalls()

	out := resolvePersonaContent(personaCtx(sid), personaRequest{Prompt: "tool iteration", Eligible: false})
	if out.Name != "qa" || out.Content != env.mgr.GetPersona("qa") || out.Info != nil {
		t.Fatalf("outcome = %+v", out)
	}
	if env.routerCalls() != calls || env.llm.callCount() != 0 {
		t.Fatal("a non-eligible run must make no call")
	}

	// Eligibility rules.
	a := env.a
	if !a.personaEligible(context.Background(), sid) {
		t.Fatal("a user turn of the coder must be eligible")
	}
	if a.personaEligible(withSystemInitiatedRun(context.Background()), sid) {
		t.Fatal("a system initiated run must not be eligible")
	}
	sub := &agent{agentName: config.AgentTask}
	if sub.personaEligible(context.Background(), sid) {
		t.Fatal("a subagent must not be eligible")
	}
}

func TestPersonaScopedSessionWithoutExplicitPersona(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-scoped")
	SetSessionLLMOverrides(sid, SessionLLMOverrides{PersonaScoped: true, Prompt: "Stay terse."})
	t.Cleanup(func() { SetSessionLLMOverrides(sid, SessionLLMOverrides{}) })
	setActivePersonaForTest("software-engineer") // ignored for a persona-scoped session

	env.answerPersona("qa", 0.9)
	out := resolveTurn(sid, "write a test plan")
	if want := env.mgr.GetPersona("qa") + "\n\nStay terse."; out.Content != want {
		t.Fatalf("content = %q, want %q", out.Content, want)
	}
	env.answerPersona("none", 0.9)
	out = resolveTurn(sid, "ok")
	if want := env.mgr.GetPersona("qa") + "\n\nStay terse."; out.Content != want || out.Info.Source != PersonaSourceSticky {
		t.Fatalf("sticky content = %q info %+v", out.Content, out.Info)
	}
}

func TestPersonaContentByteIdenticalAcrossTurns(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-stable")
	env.answerPersona("qa", 0.95)
	first := resolveTurn(sid, "write a test plan")
	env.answerPersona("none", 0.8)
	second := resolveTurn(sid, "continue")
	if first.Content == "" || first.Content != second.Content {
		t.Fatalf("persona content differs between turns:\n%q\n%q", first.Content, second.Content)
	}
}

func TestPersonaHotReloadOfToggles(t *testing.T) {
	env := newPersonaEnv(t, false)
	sid := env.personaSession("p-reload")
	env.llm.answer = "qa"

	// personaAutoSelect.enabled off: nothing happens.
	config.Get().PersonaAutoSelect.Enabled = false
	if out := resolveTurn(sid, "x"); out.Content != "" || env.llm.callCount() != 0 {
		t.Fatalf("disabled selection ran: %+v calls %d", out, env.llm.callCount())
	}
	// Enabled without restart: LLM path.
	config.Get().PersonaAutoSelect.Enabled = true
	if out := resolveTurn(sid, "x"); out.Name != "qa" || env.llm.callCount() != 1 {
		t.Fatalf("LLM path: %+v calls %d", out, env.llm.callCount())
	}
	// useDecisionModel switched on without restart: router path.
	a := config.Get().Agents[config.AgentPersonaSelector]
	a.UseDecisionModel = true
	config.Get().Agents[config.AgentPersonaSelector] = a
	env.answerPersona("software_engineer", 0.9)
	if out := resolveTurn(sid, "x"); out.Name != "software-engineer" || env.routerCalls() != 1 || env.llm.callCount() != 1 {
		t.Fatalf("decision path: %+v router %d llm %d", out, env.routerCalls(), env.llm.callCount())
	}
}

func TestPersonaLazyProviderRebuiltOnAgentChange(t *testing.T) {
	env := newPersonaEnv(t, false)
	sid := env.personaSession("p-lazy")
	SetPersonaSelector(NewLazyPersonaSelector())

	var mu sync.Mutex
	built := map[models.ModelID]int{}
	fake := &fakeSelectorLLM{answer: "qa"}
	providerBuilderHook = func(m models.Model, _ int) (provider.Provider, bool) {
		mu.Lock()
		built[m.ID]++
		mu.Unlock()
		return fake, true
	}

	resolveTurn(sid, "one")
	resolveTurn(sid, "two")
	mu.Lock()
	first := built[autoTask]
	mu.Unlock()
	if first != 1 || fake.callCount() != 2 {
		t.Fatalf("provider built %d times for %d calls, want cached", first, fake.callCount())
	}

	a := config.Get().Agents[config.AgentPersonaSelector]
	a.Model = autoFb1
	config.Get().Agents[config.AgentPersonaSelector] = a
	resolveTurn(sid, "three")
	mu.Lock()
	rebuilt := built[autoFb1]
	mu.Unlock()
	if rebuilt != 1 {
		t.Fatalf("provider for the new model built %d times, want 1", rebuilt)
	}

	// A model that cannot be built leaves the selection unavailable, not failing.
	a.Model = "no-such-model"
	config.Get().Agents[config.AgentPersonaSelector] = a
	if out := resolveTurn(sid, "four"); out.Content != "" {
		t.Fatalf("content = %q, want none", out.Content)
	}
}

func TestPersonaNoticeOnlyOnChange(t *testing.T) {
	env := newPersonaEnv(t, true, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	sid := env.personaSession("p-notice")

	env.answerPersona("qa", 0.9)
	first := personaNotices(env.run(sid, "write a test plan"))
	if len(first) != 1 || !strings.HasPrefix(first[0], "Persona: qa (p=0.90, ") || !strings.HasSuffix(first[0], " ms)") {
		t.Fatalf("first turn notices = %q", first)
	}
	if second := personaNotices(env.run(sid, "and the next one")); len(second) != 0 {
		t.Fatalf("same persona again must not notify, got %q", second)
	}
	env.answerPersona("none", 0.9)
	if third := personaNotices(env.run(sid, "ok")); len(third) != 0 {
		t.Fatalf("sticky turn must not notify, got %q", third)
	}
	env.answerPersona("software_engineer", 0.9)
	if fourth := personaNotices(env.run(sid, "refactor")); len(fourth) != 1 || !strings.HasPrefix(fourth[0], "Persona: software-engineer") {
		t.Fatalf("changed persona notices = %q", fourth)
	}
	if env.routerCalls() != 4 {
		t.Fatalf("router calls = %d, want one per prompt", env.routerCalls())
	}
}

func TestPersonaWarningOncePerSessionAndClass(t *testing.T) {
	env := newPersonaEnv(t, true, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	one := env.personaSession("p-warn-1")
	two := env.personaSession("p-warn-2")
	env.srv.SetRawResponse("not json")

	warnings := func(events []AgentEvent) int {
		n := 0
		for _, s := range notices(events) {
			if strings.HasPrefix(s, "Persona auto-select: decision model unavailable") {
				n++
			}
		}
		return n
	}
	if got := warnings(env.run(one, "a")); got != 1 {
		t.Fatalf("first turn warnings = %d, want 1", got)
	}
	if got := warnings(env.run(one, "b")); got != 0 {
		t.Fatalf("second turn warnings = %d, want 0", got)
	}
	if got := warnings(env.run(two, "c")); got != 1 {
		t.Fatalf("other session warnings = %d, want 1", got)
	}
}

func TestPersonaCombinedRequestOneCall(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-combined")
	env.decide("implementation", 0.93)
	env.answerPersona("software_engineer", 0.9)

	events := env.run(sid, "implement the feature")
	if final := finalEvent(events); final.Type != AgentEventTypeResponse {
		t.Fatalf("final event = %+v", final)
	}
	if got := env.routerCalls(); got != 1 {
		t.Fatalf("router requests = %d, want exactly 1 for both decisions", got)
	}
	var req struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	for _, r := range env.srv.Requests() {
		if r.Path == "/v1/systemone" {
			if err := json.Unmarshal(r.Body, &req); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, ok := req.Questions["task"]; !ok {
		t.Fatalf("questions = %v, want task and persona", req.Questions)
	}
	if _, ok := req.Questions["persona"]; !ok {
		t.Fatalf("questions = %v, want task and persona", req.Questions)
	}

	if env.provs[autoImpl].callCount() != 1 || env.provs[autoCoder].callCount() != 0 {
		t.Fatalf("model decision not applied: impl %d coder %d", env.provs[autoImpl].callCount(), env.provs[autoCoder].callCount())
	}
	info, _ := LastRouting(sid)
	if info.RouteID != "implementation" || !info.Matched {
		t.Fatalf("LastRouting = %+v", info)
	}
	pinfo, ok := LastPersonaRouting(sid)
	if !ok || pinfo.Persona != "software-engineer" || pinfo.Source != PersonaSourceDecision {
		t.Fatalf("LastPersonaRouting = %+v %v", pinfo, ok)
	}
	if len(autoNotices(events)) != 1 || len(personaNotices(events)) != 1 {
		t.Fatalf("notices = %q", notices(events))
	}
}

func TestPersonaDecisionWithAutoModeOffSendsPersonaQuestionOnly(t *testing.T) {
	env := newPersonaEnv(t, true, withAutoConfig(func(m *config.ModelAutoModeConfig) { m.Enabled = false }))
	sid := env.personaSession("p-persona-only")
	env.answerPersona("qa", 0.9)
	env.run(sid, "write a test plan")
	if env.routerCalls() != 1 {
		t.Fatalf("router requests = %d, want 1", env.routerCalls())
	}
	var req struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	for _, r := range env.srv.Requests() {
		if r.Path == "/v1/systemone" {
			_ = json.Unmarshal(r.Body, &req)
		}
	}
	if len(req.Questions) != 1 || req.Questions["persona"] == nil {
		t.Fatalf("questions = %v, want only persona", req.Questions)
	}
}

func TestPersonaCombinedFallsBackSeparatelyDuringCooldown(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-combined-cool")
	env.llm.answer = "qa"
	env.srv.SetRawResponse("not json")
	env.run(sid, "first")
	calls := env.routerCalls()
	env.run(sid, "second")
	// During the cooldown the persona question is not asked; the model route
	// still asks its own question.
	if got := env.routerCalls(); got != calls+1 {
		t.Fatalf("router requests during cooldown = %d, want %d", got, calls+1)
	}
}

func TestPersonaCancelledRunDoesNotPoisonCooldown(t *testing.T) {
	env := newPersonaEnv(t, true)
	sid := env.personaSession("p-cancel")
	recordPersona(sid, "qa", nil)
	env.srv.Delay(600 * time.Millisecond)

	ctx, cancel := context.WithCancel(personaCtx(sid))
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	route := &combinedRoute{}
	out := resolvePersonaContent(ctx, personaRequest{Prompt: "refactor", Eligible: true, Combined: route})
	if out.Warning != "" || out.Info != nil {
		t.Fatalf("cancelled run must not warn or report routing: %+v", out)
	}
	if out.Name != "qa" || out.Content != env.mgr.GetPersona("qa") {
		t.Fatalf("cancelled run must keep the sticky persona: %+v", out)
	}
	if route.Done {
		t.Fatal("the model route must not reuse the cancelled decision")
	}
	if env.llm.callCount() != 0 {
		t.Fatalf("llm fallback calls = %d, want 0", env.llm.callCount())
	}
	if got := rememberedPersona(sid); got != "qa" {
		t.Fatalf("remembered persona = %q", got)
	}

	// No cooldown: the next prompt asks the router again.
	env.srv.Delay(0)
	env.answerPersona("software_engineer", 0.9)
	before := env.routerCalls()
	out = resolveTurn(sid, "refactor again")
	if env.routerCalls() != before+1 || out.Name != "software-engineer" {
		t.Fatalf("router calls %d -> %d, outcome %+v", before, env.routerCalls(), out)
	}
}

func TestPersonaRouterTimeoutStillCoolsDownAndFallsBack(t *testing.T) {
	env := newPersonaEnv(t, true, withDecisionConfig(func(d *config.DecisionModelConfig) { d.TimeoutMs = 100 }))
	sid := env.personaSession("p-timeout")
	env.srv.Delay(600 * time.Millisecond)
	env.llm.answer = "qa"

	out := resolveTurn(sid, "write tests")
	if out.Name != "qa" || out.Info == nil || out.Info.Source != PersonaSourceLLM || out.Warning == "" {
		t.Fatalf("outcome = %+v / %+v", out, out.Info)
	}
	if env.llm.callCount() != 1 {
		t.Fatalf("llm calls = %d, want 1", env.llm.callCount())
	}
	before := env.routerCalls()
	resolveTurn(sid, "again")
	if env.routerCalls() != before {
		t.Fatal("a genuine router timeout must start the cooldown")
	}
}

func TestPersonaSessionEvictionIsLeastRecentlyUsed(t *testing.T) {
	newPersonaEnv(t, true)
	for i := 0; i < personaStateMax; i++ {
		recordPersona(fmt.Sprintf("lru-%d", i), "qa", nil)
	}
	// Touch the oldest session so the next-oldest becomes the LRU one.
	if rememberedPersona("lru-0") != "qa" {
		t.Fatal("lru-0 must be remembered")
	}
	recordPersona("lru-new", "qa", nil)
	if rememberedPersona("lru-0") != "qa" {
		t.Fatal("a recently used session must survive eviction")
	}
	if rememberedPersona("lru-1") != "" {
		t.Fatal("the least recently used session must be evicted")
	}
	if rememberedPersona("lru-new") != "qa" {
		t.Fatal("the new session must be stored")
	}
}

func TestForgetSessionPersonaClearsPersonaWarnings(t *testing.T) {
	newPersonaEnv(t, true)
	resetAutoWarnings()
	t.Cleanup(resetAutoWarnings)
	if !warnAutoOnce("w-1", "persona:timeout") || warnAutoOnce("w-1", "persona:timeout") {
		t.Fatal("warn-once baseline broken")
	}
	warnAutoOnce("w-1", "timeout") // model auto mode's own state
	ForgetSessionPersona("w-1")
	if !warnAutoOnce("w-1", "persona:timeout") {
		t.Fatal("persona warning state must be cleared")
	}
	if warnAutoOnce("w-1", "timeout") {
		t.Fatal("model auto warning state must be untouched")
	}
}

func TestPersonaOptionOffSubagentRunCallsLLM(t *testing.T) {
	env := newPersonaEnv(t, false)
	sid := env.personaSession("p-off-task")
	env.llm.answer = "qa"

	out := resolvePersonaContent(personaCtx(sid), personaRequest{Prompt: "subtask", Eligible: false})
	if out.Name != "qa" || env.llm.callCount() != 1 {
		t.Fatalf("outcome = %+v llm calls = %d", out, env.llm.callCount())
	}
	if rememberedPersona(sid) != "" {
		t.Fatal("a non-eligible run must not overwrite the session's remembered persona")
	}
}

func TestPersonaChildSessionInheritsParentPersona(t *testing.T) {
	env := newPersonaEnv(t, true)
	parent := env.personaSession("p-parent")
	child := env.personaSession("p-child")
	recordPersona(parent, "qa", nil)

	req := personaRequest{Prompt: "subtask", Eligible: false, ParentSessionID: parent}
	out := resolvePersonaContent(personaCtx(child), req)
	if out.Name != "qa" || out.Content != env.mgr.GetPersona("qa") || out.Info != nil {
		t.Fatalf("outcome = %+v", out)
	}
	if env.routerCalls() != 0 || env.llm.callCount() != 0 {
		t.Fatal("inheriting must make no call")
	}
	if rememberedPersona(child) != "" {
		t.Fatal("the inherited persona must not be written to the child")
	}

	// A persona of the child's own wins over the parent's.
	recordPersona(child, "software-engineer", nil)
	if out := resolvePersonaContent(personaCtx(child), req); out.Name != "software-engineer" {
		t.Fatalf("own persona must win: %+v", out)
	}

	// Parent without a persona: none.
	orphan := env.personaSession("p-orphan")
	empty := env.personaSession("p-empty-parent")
	out = resolvePersonaContent(personaCtx(orphan), personaRequest{Eligible: false, ParentSessionID: empty})
	if out.Name != "" || out.Content != "" {
		t.Fatalf("outcome = %+v", out)
	}
	if rememberedPersona(parent) != "qa" {
		t.Fatal("the parent state must be untouched")
	}

	// A title run in the same session reuses the remembered persona.
	out = resolvePersonaContent(personaCtx(parent), personaRequest{Eligible: false})
	if out.Name != "qa" {
		t.Fatalf("title run outcome = %+v", out)
	}
}
