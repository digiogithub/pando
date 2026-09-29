package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
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
