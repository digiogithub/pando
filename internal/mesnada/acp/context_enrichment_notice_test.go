package acp

import (
	"context"
	"testing"
)

// TestNormalizeSystemMessagePassesThroughContextEnrichmentNotices pins that the
// context-enrichment start/heartbeat/done SystemMessage notices emitted by
// internal/llm/agent (see runSessionEnrichment/describeEnrichmentOutcome) reach
// the ACP client unsuppressed, exactly like the existing compaction notices do:
// normalizeSystemMessage's default case forwards anything it does not
// specifically recognize, and none of these notices trip an earlier case (they
// mention neither "compacting", "persona", "rate limit" nor any of the other
// special-cased substrings).
func TestNormalizeSystemMessagePassesThroughContextEnrichmentNotices(t *testing.T) {
	a := &PandoACPAgent{}
	cases := []string{
		"\n\n🔍 Enriching context…\n",
		"\n\n🔍 Still enriching context… 7s\n",
		"✓ Context enrichment done (1.2s) — 340 chars of context added.\n\n",
		"✓ Context enrichment done (25.0s) — 120 chars via search fallback, agent loop timed out.\n\n",
		"✗ Context enrichment timed out after 25s.\n\n",
	}
	for _, msg := range cases {
		update, normalized, suppress := a.normalizeSystemMessage(context.Background(), nil, msg)
		if suppress {
			t.Fatalf("message %q was suppressed, want it forwarded to the ACP client", msg)
		}
		if update != nil {
			t.Fatalf("message %q produced an unexpected session/usage update: %+v", msg, update)
		}
		if normalized == "" {
			t.Fatalf("message %q normalized to empty text", msg)
		}
	}
}
