package agent

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/digiogithub/pando/internal/db"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
)

// countingMemoryInjector returns a different block on every build, so a test can
// tell a frozen block from a rebuilt one, and records the queries it was given.
type countingMemoryInjector struct {
	mu      sync.Mutex
	builds  int
	queries []string
}

func (c *countingMemoryInjector) BuildMemoryBlock(_ context.Context, query string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.builds++
	c.queries = append(c.queries, query)
	return fmt.Sprintf("<memories>build %d</memories>", c.builds)
}

func installCountingMemoryInjector(t *testing.T) *countingMemoryInjector {
	t.Helper()
	inj := &countingMemoryInjector{}
	prev := globalMemoryInjector
	SetMemoryInjector(inj)
	frozenMemoryBlocks.Lock()
	prevBlocks := frozenMemoryBlocks.bySession
	frozenMemoryBlocks.bySession = make(map[string]string)
	frozenMemoryBlocks.Unlock()
	t.Cleanup(func() {
		SetMemoryInjector(prev)
		frozenMemoryBlocks.Lock()
		frozenMemoryBlocks.bySession = prevBlocks
		frozenMemoryBlocks.Unlock()
	})
	return inj
}

func sessionPromptCtx(sessionID, query string) context.Context {
	return withMemoryQuery(context.WithValue(context.Background(), prompt.SessionIDKey, sessionID), query)
}

func TestSessionMemoryBlockIsFrozenAcrossTurns(t *testing.T) {
	inj := installCountingMemoryInjector(t)

	first := sessionMemoryBlock(sessionPromptCtx("s1", "how do I run the tests?"))
	second := sessionMemoryBlock(sessionPromptCtx("s1", "now fix the failing one"))

	if first != second {
		t.Fatalf("block changed between turns: %q then %q", first, second)
	}
	if inj.builds != 1 {
		t.Fatalf("expected one build per session, got %d", inj.builds)
	}
	if len(inj.queries) != 1 || inj.queries[0] != "how do I run the tests?" {
		t.Fatalf("expected the first turn's prompt as search query, got %q", inj.queries)
	}
}

func TestSessionMemoryBlockIsPerSession(t *testing.T) {
	inj := installCountingMemoryInjector(t)

	a := sessionMemoryBlock(sessionPromptCtx("s1", "q1"))
	b := sessionMemoryBlock(sessionPromptCtx("s2", "q2"))

	if a == b {
		t.Fatalf("two sessions shared one frozen block: %q", a)
	}
	if inj.builds != 2 {
		t.Fatalf("expected one build per session, got %d", inj.builds)
	}
}

func TestInvalidateSessionMemoryBlockRebuildsWithNextQuery(t *testing.T) {
	inj := installCountingMemoryInjector(t)

	before := sessionMemoryBlock(sessionPromptCtx("s1", "before compaction"))
	invalidateSessionMemoryBlock("s1")
	after := sessionMemoryBlock(sessionPromptCtx("s1", "after compaction"))

	if before == after {
		t.Fatal("block was not rebuilt after invalidation")
	}
	if got := inj.queries[len(inj.queries)-1]; got != "after compaction" {
		t.Fatalf("rebuild should search with the current prompt, got %q", got)
	}
}

func TestSessionMemoryBlockWithoutSessionIsNotFrozen(t *testing.T) {
	inj := installCountingMemoryInjector(t)

	sessionMemoryBlock(withMemoryQuery(context.Background(), "q"))
	sessionMemoryBlock(withMemoryQuery(context.Background(), "q"))

	if inj.builds != 2 {
		t.Fatalf("session-less requests must not share a frozen block, got %d builds", inj.builds)
	}
	frozenMemoryBlocks.Lock()
	n := len(frozenMemoryBlocks.bySession)
	frozenMemoryBlocks.Unlock()
	if n != 0 {
		t.Fatalf("session-less request was cached: %d entries", n)
	}
}

func TestSessionMemoryBlockWithoutInjector(t *testing.T) {
	installCountingMemoryInjector(t)
	SetMemoryInjector(nil)
	if got := sessionMemoryBlock(sessionPromptCtx("s1", "q")); got != "" {
		t.Fatalf("expected no block without an injector, got %q", got)
	}
}

// TestSystemPromptIsByteStableAcrossTurns guards the prompt-cache contract: two
// turns of one session must produce the same system prompt, memory block
// included, or the provider re-reads the whole history uncached every turn.
func TestSystemPromptIsByteStableAcrossTurns(t *testing.T) {
	installCountingMemoryInjector(t)
	prevCfg := config.Get()
	config.SetForTests(&config.Config{WorkingDir: t.TempDir()})
	t.Cleanup(func() { config.SetForTests(prevCfg) })

	first := buildSystemMessage(sessionPromptCtx("s1", "turn one"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	time.Sleep(1100 * time.Millisecond) // crosses a second boundary: a clock in the prompt would show
	second := buildSystemMessage(sessionPromptCtx("s1", "turn two"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")

	if first != second {
		t.Fatalf("system prompt changed between turns of one session:\n--- first\n%s\n--- second\n%s", first, second)
	}
	if !strings.HasPrefix(first, "<memories>build 1</memories>") {
		t.Fatalf("frozen memory block not prepended: %.80q", first)
	}
}

// variantEvaluatorAdapter exposes an *evaluator.EvaluatorService to the prompt
// package the way internal/app does.
type variantEvaluatorAdapter struct{ svc *evaluator.EvaluatorService }

func (a variantEvaluatorAdapter) SelectVariant(ctx context.Context, sessionID, section string, c []string) (string, error) {
	return a.svc.SelectVariant(ctx, sessionID, section, c)
}
func (a variantEvaluatorAdapter) SessionSkills(context.Context, string, string) ([]prompt.PromptEvaluatorSkill, error) {
	return nil, nil
}
func (a variantEvaluatorAdapter) ClassifyTask(string) string { return "general" }

// TestSystemPromptWithVariantsIsByteStableAcrossTurnsAndRestarts extends the
// prompt-cache contract to template variants: the variant frozen for a session
// is served on every turn and by a fresh evaluator on the same database.
func TestSystemPromptWithVariantsIsByteStableAcrossTurnsAndRestarts(t *testing.T) {
	installCountingMemoryInjector(t)
	wd := t.TempDir()
	prevCfg := config.Get()
	config.SetForTests(&config.Config{WorkingDir: wd})
	t.Cleanup(func() { config.SetForTests(prevCfg) })
	t.Setenv("HOME", t.TempDir())

	dir := filepath.Join(wd, ".pando", "prompts", "variants", "base", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terse.md.tpl"), []byte("TERSE-WORKFLOW-VARIANT"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("pando-sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`
CREATE TABLE session_template_selections (session_id TEXT NOT NULL, section TEXT NOT NULL, variant_id TEXT NOT NULL, selected_at INTEGER NOT NULL, PRIMARY KEY (session_id, section));
CREATE TABLE prompt_variant_stats (variant_id TEXT PRIMARY KEY, section TEXT NOT NULL, times_used INTEGER NOT NULL DEFAULT 0, total_reward REAL NOT NULL DEFAULT 0.0, avg_reward REAL NOT NULL DEFAULT 0.0, updated_at INTEGER NOT NULL);
INSERT INTO session_template_selections VALUES ('s1', 'base/workflow', 'base/workflow#terse', 1);`); err != nil {
		t.Fatal(err)
	}

	newSvc := func() *evaluator.EvaluatorService {
		svc, err := evaluator.New(config.EvaluatorConfig{
			Enabled: true, ExplorationC: 1.41, MinSessionsForUCB: 5,
			Templates: config.TemplatesConfig{Enabled: true},
		}, db.New(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	prompt.SetGlobalEvaluator(variantEvaluatorAdapter{svc: newSvc()})
	t.Cleanup(func() { prompt.SetGlobalEvaluator(nil) })

	first := buildSystemMessage(sessionPromptCtx("s1", "turn one"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	time.Sleep(1100 * time.Millisecond)
	second := buildSystemMessage(sessionPromptCtx("s1", "turn two"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	if first != second {
		t.Fatalf("system prompt with variants changed between turns:\n--- first\n%s\n--- second\n%s", first, second)
	}
	if !strings.Contains(first, "TERSE-WORKFLOW-VARIANT") {
		t.Fatalf("frozen variant not rendered:\n%s", first)
	}

	// Restart: a fresh evaluator on the same DB serves the same bytes.
	prompt.SetGlobalEvaluator(variantEvaluatorAdapter{svc: newSvc()})
	third := buildSystemMessage(sessionPromptCtx("s1", "turn three"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	if first != third {
		t.Fatalf("system prompt changed across an evaluator restart")
	}

	// A new session with no persisted choice is frozen on its first build.
	a := buildSystemMessage(sessionPromptCtx("s2", "q"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	b := buildSystemMessage(sessionPromptCtx("s2", "q2"), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	if a != b {
		t.Fatalf("new session prompt not stable across turns")
	}
}

// skillEvaluatorAdapter exposes SessionSkills of an *evaluator.EvaluatorService
// to the prompt package the way internal/app does.
type skillEvaluatorAdapter struct{ svc *evaluator.EvaluatorService }

func (a skillEvaluatorAdapter) SelectVariant(_ context.Context, _, _ string, c []string) (string, error) {
	return c[0], nil
}
func (a skillEvaluatorAdapter) SessionSkills(ctx context.Context, sessionID, taskType string) ([]prompt.PromptEvaluatorSkill, error) {
	skills, err := a.svc.SessionSkills(ctx, sessionID, taskType)
	out := make([]prompt.PromptEvaluatorSkill, len(skills))
	for i, sk := range skills {
		out[i] = prompt.PromptEvaluatorSkill{Content: sk.Content}
	}
	return out, err
}
func (a skillEvaluatorAdapter) ClassifyTask(string) string { return "general" }

func writeLearnedSkill(t *testing.T, wd, id, status, rule string) {
	t.Helper()
	f := evaluator.SkillFile{ID: id, Title: rule, Status: status, TaskType: "general", Confidence: 0.9, Created: time.Now(), Content: rule}
	dir := evaluator.LearnedSkillsDir(wd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), f.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSystemPromptWithLearnedSkillsIsByteStableWhenSkillApprovedMidSession
// guards the prompt-cache contract for learned skills: the set injected into a
// session is frozen on its first build, so approving a skill mid-session does
// not change the prompt; only the next session gets it.
func TestSystemPromptWithLearnedSkillsIsByteStableWhenSkillApprovedMidSession(t *testing.T) {
	installCountingMemoryInjector(t)
	wd := t.TempDir()
	prevCfg := config.Get()
	config.SetForTests(&config.Config{WorkingDir: wd})
	t.Cleanup(func() { config.SetForTests(prevCfg) })
	t.Setenv("HOME", t.TempDir())

	conn, err := sql.Open("pando-sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`
CREATE TABLE skill_library (id TEXT PRIMARY KEY, title TEXT NOT NULL, content TEXT NOT NULL, source_session_id TEXT, source_template_id TEXT, task_type TEXT NOT NULL DEFAULT 'general', usage_count INTEGER NOT NULL DEFAULT 0, success_rate REAL NOT NULL DEFAULT 0.0, is_active INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'legacy', confidence REAL NOT NULL DEFAULT 0.0, judge_model TEXT NOT NULL DEFAULT '', eval_count INTEGER NOT NULL DEFAULT 0, reward_total REAL NOT NULL DEFAULT 0.0);
CREATE TABLE session_skill_injections (session_id TEXT NOT NULL, skill_id TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0, injected_at INTEGER NOT NULL, PRIMARY KEY (session_id, skill_id));`); err != nil {
		t.Fatal(err)
	}
	svc, err := evaluator.New(config.EvaluatorConfig{Enabled: true, MaxSkills: 10}, db.New(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt.SetGlobalEvaluator(skillEvaluatorAdapter{svc: svc})
	t.Cleanup(func() { prompt.SetGlobalEvaluator(nil) })

	writeLearnedSkill(t, wd, "rule-a", "approved", "RULE-A-ALWAYS-RUN-TESTS")
	writeLearnedSkill(t, wd, "rule-b", "pending", "RULE-B-KEEP-DIFFS-SMALL")

	build := func(session, q string) string {
		return buildSystemMessage(sessionPromptCtx(session, q), config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, "")
	}
	first := build("skill-s1", "turn one")
	if !strings.Contains(first, "RULE-A-ALWAYS-RUN-TESTS") || strings.Contains(first, "RULE-B") {
		t.Fatalf("only the approved skill must be injected:\n%s", first)
	}

	// Approve B mid-session: the running session keeps its bytes.
	writeLearnedSkill(t, wd, "rule-b", "approved", "RULE-B-KEEP-DIFFS-SMALL")
	time.Sleep(1100 * time.Millisecond)
	second := build("skill-s1", "turn two")
	if first != second {
		t.Fatalf("system prompt changed after a skill was approved mid-session:\n--- first\n%s\n--- second\n%s", first, second)
	}

	// The next new session gets both skills.
	next := build("skill-s2", "turn one")
	if !strings.Contains(next, "RULE-A-ALWAYS-RUN-TESTS") || !strings.Contains(next, "RULE-B-KEEP-DIFFS-SMALL") {
		t.Fatalf("new session should see both approved skills:\n%s", next)
	}
	if again := build("skill-s2", "turn two"); again != next {
		t.Fatal("new session prompt not stable across turns")
	}
}
