package evaluator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/evaluator"
)

func TestLintPatterns_DoubleBackslashAndCompileError(t *testing.T) {
	cfg := config.EvaluatorConfig{
		CorrectionsPatterns: []string{
			`(?i)\bwrong\b`,      // fine
			`(?i)\\bincorrect\\b`, // TOML single-quoted '\\b': literal double backslash
			`(unclosed`,           // does not compile
		},
		TaskPatterns: []config.TaskPatternConfig{{Pattern: `clean\\s+up`, TaskType: "refactor"}},
	}
	issues := evaluator.LintPatterns(cfg)
	kinds := map[string]int{}
	for _, is := range issues {
		kinds[is.Kind]++
		if is.Hint == "" {
			t.Errorf("issue without a fix hint: %+v", is)
		}
	}
	if kinds[evaluator.PatternIssueDoubleBackslash] != 2 || kinds[evaluator.PatternIssueCompile] != 1 {
		t.Fatalf("issues = %+v", issues)
	}
	for _, is := range issues {
		if is.Pattern == `(?i)\bwrong\b` {
			t.Errorf("valid pattern flagged: %+v", is)
		}
	}

	// The legacy bare-negation default is ignored at runtime and not linted.
	if got := evaluator.LintPatterns(config.EvaluatorConfig{CorrectionsPatterns: []string{`(?i)\bno[,.]?\b`}}); len(got) != 0 {
		t.Errorf("legacy pattern flagged: %+v", got)
	}
}

func TestDiagnose_ReportsNeverEvaluatedSessionsAndSkills(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "old-a", "", 3, 2*time.Hour)
	addSession(t, conn, "old-b", "", 2, 3*time.Hour)
	addSession(t, conn, "one-turn", "", 1, 2*time.Hour)
	addSession(t, conn, "child", "old-a", 3, 2*time.Hour)

	wd := t.TempDir()
	skillsDir := filepath.Join(wd, ".pando", "skills", "learned")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "a.md"), []byte("---\nid: a\ntitle: A\nstatus: pending\ntask_type: general\nconfidence: 0.5\ncreated: 2026-01-01T00:00:00Z\n---\nrule a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wd, ".pando", "prompts", "variants", "base"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".pando", "prompts", "variants", "base", "terse.md.tpl"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultEvalConfig()
	cfg.CorrectionsPatterns = []string{`(?i)\\bwrong\\b`}
	rep, err := evaluator.Diagnose(context.Background(), evaluator.DiagnoseOptions{Config: cfg, DB: q, WorkDir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Enabled {
		t.Errorf("Enabled = false")
	}
	if rep.EligibleSessions != 2 || rep.NeverEvaluated != 2 || rep.EvaluatedSessions != 0 {
		t.Errorf("sessions eligible/never/evaluated = %d/%d/%d, want 2/2/0", rep.EligibleSessions, rep.NeverEvaluated, rep.EvaluatedSessions)
	}
	if rep.RecentWindow != 2 || rep.RecentEvaluated != 0 || rep.LastEvaluatedAt != 0 {
		t.Errorf("recent = %d/%d last=%d", rep.RecentWindow, rep.RecentEvaluated, rep.LastEvaluatedAt)
	}
	if rep.Skills.Pending != 1 {
		t.Errorf("pending skills = %d, want 1", rep.Skills.Pending)
	}
	if len(rep.Variants.Sections) != 1 || rep.Variants.Sections[0].Section != "base" || !rep.Variants.Sections[0].Competing {
		t.Errorf("variants = %+v", rep.Variants)
	}
	if len(rep.PatternIssues) != 1 || !rep.HasProblem() || rep.ProblemLine() == "" {
		t.Errorf("pattern issue not surfaced: %+v / %q", rep.PatternIssues, rep.ProblemLine())
	}
	text := rep.Text()
	for _, want := range []string{"never evaluated: 2", "double_backslash", "pending 1", "base: 1 variant"} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q:\n%s", want, text)
		}
	}

	// Once a session is scored the "nothing evaluated" warning goes away.
	svc := newSvc(t, q, twoTurns(), nil)
	if _, err := svc.EvaluateNow(context.Background(), "old-a", evaluator.EvaluateOptions{SkipJudge: true}); err != nil {
		t.Fatal(err)
	}
	cfg.CorrectionsPatterns = nil
	rep, err = evaluator.Diagnose(context.Background(), evaluator.DiagnoseOptions{Config: cfg, DB: q, WorkDir: wd, Service: svc})
	if err != nil {
		t.Fatal(err)
	}
	if rep.NeverEvaluated != 1 || rep.RecentEvaluated != 1 || rep.LastEvaluatedAt == 0 || rep.HasProblem() {
		t.Errorf("after evaluation: never=%d recent=%d last=%d problem=%v", rep.NeverEvaluated, rep.RecentEvaluated, rep.LastEvaluatedAt, rep.HasProblem())
	}
	if !rep.Background.Known {
		t.Errorf("service state not reported")
	}
}

func TestDiagnose_DisabledExplainsWhy(t *testing.T) {
	rep, err := evaluator.Diagnose(context.Background(), evaluator.DiagnoseOptions{Config: config.EvaluatorConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Enabled || len(rep.DisabledReasons) < 2 {
		t.Fatalf("expected disabled with reasons (disabled + no model), got %+v", rep.DisabledReasons)
	}
	if rep.HasProblem() {
		t.Errorf("a disabled evaluator must not raise the banner")
	}
}
