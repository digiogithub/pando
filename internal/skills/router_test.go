package skills

import "testing"

func TestMatchSkillToPrompt(t *testing.T) {
	t.Parallel()

	metadata := SkillMetadata{
		Name:        "sql",
		WhenToUse:   "postgres tuning, query optimization, slow sql",
		Description: "Tune SQL queries safely.",
	}

	if !MatchSkillToPrompt(metadata, "Please help optimize a slow SQL query in Postgres.") {
		t.Fatalf("expected prompt to match skill")
	}
}

func TestMatchSkillToPromptSkipsDisabledOrEmptySkills(t *testing.T) {
	t.Parallel()

	if MatchSkillToPrompt(SkillMetadata{Name: "docs"}, "write release notes") {
		t.Fatalf("expected empty when-to-use to never match")
	}

	if MatchSkillToPrompt(
		SkillMetadata{Name: "docs", WhenToUse: "release notes", DisableModelInvocation: true},
		"write release notes",
	) {
		t.Fatalf("expected disabled skill to never match")
	}
}
