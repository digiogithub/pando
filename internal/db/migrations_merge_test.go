package db

import (
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"
)

// The versions goose sees after the kb/events migrations moved to the rag
// module must be exactly the ones it saw before, in the same order, or an
// existing pando.db would either re-run a migration or skip one.
var wantMigrations = []string{
	"20250424200609_initial.sql",
	"20250515105448_add_summary_message_id.sql",
	"20260309000001_add_rag.sql",
	"20260311000001_add_kb.sql",
	"20260311000002_add_events.sql",
	"20260312000001_add_code_indexing.sql",
	"20260313000001_add_mcp_gateway.sql",
	"20260318000001_add_self_improvement.sql",
	"20260320000001_add_code_fts_triggers.sql",
	"20260419120000_add_projects.sql",
	"20260601000002_add_session_goals.sql",
	"20260601000003_add_acp_session_state.sql",
	"20260611000001_add_kb_memory.sql",
	"20260627000001_code_index_drop_source_code.sql",
	"20260630000001_add_code_edges.sql",
	"20260713000001_add_kb_links.sql",
	"20260716191017_add_session_token_usage_detail.sql",
	"20260728000001_add_agui_threads.sql",
	"20260826000001_add_design.sql",
	"20260914000001_add_kb_chunk_embedding_meta.sql",
	"20260925000001_add_mcp_server_fingerprints.sql",
	"20260929000001_add_session_score_components.sql",
	"20260929000002_add_session_score_judge_tokens.sql",
	"20260929000003_add_template_variants.sql",
	"20260929000004_add_reviewable_skills.sql",
	"20261001214000_add_project_web_runtime.sql",
}

func TestMergedMigrationsKeepVersionsAndOrder(t *testing.T) {
	got, err := fs.Glob(FS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i] = got[i][len("migrations/"):]
	}
	// New migrations may be appended after the list above; the prefix that
	// existed when the rag schema moved must not change.
	if len(got) < len(wantMigrations) || !slices.Equal(got[:len(wantMigrations)], wantMigrations) {
		t.Fatalf("merged migrations changed:\n got %v\nwant prefix %v", got, wantMigrations)
	}
	seen := map[string]string{}
	for _, name := range got {
		v := name[:8+6]
		if prev, dup := seen[v]; dup {
			t.Fatalf("version %s used by %s and %s", v, prev, name)
		}
		seen[v] = name
		if b, err := fs.ReadFile(FS, "migrations/"+name); err != nil || len(b) == 0 {
			t.Fatalf("read %s: %v (len %d)", name, err, len(b))
		}
	}
}

func TestMergedMigrationsFS(t *testing.T) {
	if err := fstest.TestFS(FS, "migrations/20250424200609_initial.sql", "migrations/20260311000001_add_kb.sql"); err != nil {
		t.Fatal(err)
	}
}
