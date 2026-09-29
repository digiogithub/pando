package evaluator_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// skillJudge answers every judge call with a fixed skill proposal.
type skillJudge struct {
	skill      string
	confidence float64
}

func (j *skillJudge) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	return &provider.ProviderResponse{
		Content: fmt.Sprintf(`{"reasoning":"r","key_points":["a"],"new_skill":%q,"task_type":"code","confidence":%v}`, j.skill, j.confidence),
		Usage:   provider.TokenUsage{InputTokens: 10, OutputTokens: 10},
	}, nil
}
func (j *skillJudge) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	return nil
}
func (j *skillJudge) Model() models.Model { return models.Model{ID: "skill-judge"} }

const ruleVerifyBuild = "Run the project build and tests before reporting a task as finished, and quote the command output."

func newSkillSvc(t *testing.T, q db.Querier, wd string, msgs []message.Message) *evaluator.EvaluatorService {
	t.Helper()
	svc := newSvc(t, q, msgs, judgeAlways)
	svc.SetWorkDir(wd)
	return svc
}

func judgeSession(t *testing.T, svc *evaluator.EvaluatorService, sid string, j *skillJudge) {
	t.Helper()
	setJudge(svc, j)
	if _, err := svc.EvaluateNow(context.Background(), sid, evalOpts()); err != nil {
		t.Fatalf("evaluate %s: %v", sid, err)
	}
}

func learnedFiles(t *testing.T, wd string) []evaluator.SkillFile {
	t.Helper()
	files, err := evaluator.ReadLearnedSkills(wd)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSkills_ProposalIsPendingAndApprovalReachesOnlyNewSessions(t *testing.T) {
	conn, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	addSession(t, conn, "src", "", 6, time.Hour)
	svc := newSkillSvc(t, q, wd, turns(6))

	judgeSession(t, svc, "src", &skillJudge{skill: ruleVerifyBuild, confidence: 0.85})

	files := learnedFiles(t, wd)
	if len(files) != 1 {
		t.Fatalf("expected one proposal file, got %d", len(files))
	}
	f := files[0]
	if f.Status != evaluator.SkillStatusPending || f.TaskType != "code" || f.Confidence != 0.85 ||
		f.SourceSession != "src" || f.JudgeModel != "skill-judge" || f.Content != ruleVerifyBuild {
		t.Fatalf("unexpected proposal: %+v", f)
	}
	if f.Title == "code skill" || strings.HasSuffix(f.Path, "/code-skill.md") || !strings.HasPrefix(f.Title, "Run the project build") {
		t.Fatalf("title/id not generated from the rule: %q (%s)", f.Title, f.Path)
	}
	raw, _ := os.ReadFile(f.Path)
	if !strings.HasPrefix(string(raw), "---\nid: "+f.ID+"\n") || !strings.Contains(string(raw), "status: pending") {
		t.Fatalf("unexpected file layout:\n%s", raw)
	}

	// A low-confidence proposal writes nothing.
	addSession(t, conn, "low", "", 6, time.Hour)
	judgeSession(t, svc, "low", &skillJudge{skill: "Prefer small commits when refactoring shared packages.", confidence: 0.5})
	if n := len(learnedFiles(t, wd)); n != 1 {
		t.Fatalf("low-confidence proposal created a file (%d files)", n)
	}

	// Pending: nothing is injected, whatever the session.
	inj, err := svc.SessionSkills(ctx, "s-before", "code")
	if err != nil || len(inj) != 0 {
		t.Fatalf("pending skill injected: %v %v", inj, err)
	}

	if _, err := svc.ReviewSkill(ctx, f.ID, evaluator.SkillStatusApproved); err != nil {
		t.Fatal(err)
	}
	if learnedFiles(t, wd)[0].Status != evaluator.SkillStatusApproved {
		t.Fatal("approve did not rewrite the file")
	}

	// The session that already started keeps its frozen (empty) set...
	inj, _ = svc.SessionSkills(ctx, "s-before", "code")
	if len(inj) != 0 {
		t.Fatalf("approved skill leaked into a running session: %v", inj)
	}
	// ...a new session gets it, in this and in a restarted process.
	inj, err = svc.SessionSkills(ctx, "s-after", "code")
	if err != nil || len(inj) != 1 || inj[0].Content != ruleVerifyBuild {
		t.Fatalf("new session should get the approved skill: %v %v", inj, err)
	}
	svc2 := newSkillSvc(t, q, wd, turns(6))
	inj2, _ := svc2.SessionSkills(ctx, "s-after", "code")
	if len(inj2) != 1 || inj2[0].ID != inj[0].ID {
		t.Fatalf("frozen set not restored after restart: %v", inj2)
	}
	if inj2, _ = svc2.SessionSkills(ctx, "s-before", "code"); len(inj2) != 0 {
		t.Fatalf("empty frozen set not restored after restart: %v", inj2)
	}

	// usage_count is once per session, not per prompt build.
	for i := 0; i < 5; i++ {
		_, _ = svc.SessionSkills(ctx, "s-after", "code")
		_, _ = svc2.SessionSkills(ctx, "s-after", "code")
	}
	row, err := q.GetSkill(ctx, f.ID)
	if err != nil || row.UsageCount != 1 || row.Status != evaluator.SkillStatusApproved || row.IsActive != 1 {
		t.Fatalf("mirror row = %+v (%v), want 1 use, approved, active", row, err)
	}
}

func TestSkills_SuccessRateIsMeanRewardOfInjectedSessions(t *testing.T) {
	conn, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	addSession(t, conn, "src", "", 6, time.Hour)
	svc := newSkillSvc(t, q, wd, turns(6))
	judgeSession(t, svc, "src", &skillJudge{skill: ruleVerifyBuild, confidence: 0.9})
	id := learnedFiles(t, wd)[0].ID
	if _, err := svc.ReviewSkill(ctx, id, evaluator.SkillStatusApproved); err != nil {
		t.Fatal(err)
	}

	// Three sessions get the skill injected, one more does not.
	for _, sid := range []string{"a", "b", "c", "plain"} {
		addSession(t, conn, sid, "", 2, time.Hour)
	}
	for _, sid := range []string{"a", "b", "c"} {
		if inj, _ := svc.SessionSkills(ctx, sid, "code"); len(inj) != 1 {
			t.Fatalf("session %s not injected", sid)
		}
	}
	if inj, _ := svc.SessionSkills(ctx, "plain", "code"); len(inj) != 1 {
		t.Fatal("plain session should also be injected; it is only used as a control below")
	}
	if _, err := q.GetSkill(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`DELETE FROM session_skill_injections WHERE session_id='plain'`); err != nil {
		t.Fatal(err)
	}

	eval := newSkillSvc(t, q, wd, twoTurns())
	for _, sid := range []string{"a", "b", "c", "plain"} {
		if _, err := eval.EvaluateNow(ctx, sid, evaluator.EvaluateOptions{Force: true, SkipJudge: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Different rewards: a re-score of one session goes through the delta path.
	if _, err := eval.RecordFeedback(ctx, "b", "bad", "no"); err != nil {
		t.Fatal(err)
	}

	var sum float64
	for _, sid := range []string{"a", "b", "c"} {
		sc, err := q.GetSessionScore(ctx, sid)
		if err != nil {
			t.Fatal(err)
		}
		sum += sc.Reward
	}
	row, err := q.GetSkill(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mean := sum / 3
	if row.EvalCount != 3 || abs(row.SuccessRate-mean) > 1e-9 {
		t.Fatalf("eval_count=%d success_rate=%f, want 3 sessions at mean reward %f", row.EvalCount, row.SuccessRate, mean)
	}
	if sc, _ := q.GetSessionScore(ctx, "b"); sc.Reward >= 0.3 {
		t.Fatalf("test needs a low re-scored reward, got %f", sc.Reward)
	}
}

func TestSkills_RejectedRuleIsNotProposedAgain(t *testing.T) {
	conn, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	addSession(t, conn, "s1", "", 6, time.Hour)
	addSession(t, conn, "s2", "", 6, time.Hour)
	svc := newSkillSvc(t, q, wd, turns(6))

	judgeSession(t, svc, "s1", &skillJudge{skill: ruleVerifyBuild, confidence: 0.9})
	id := learnedFiles(t, wd)[0].ID
	if _, err := svc.ReviewSkill(ctx, id, evaluator.SkillStatusRejected); err != nil {
		t.Fatal(err)
	}

	judgeSession(t, svc, "s2", &skillJudge{
		skill:      "Always run the project build and tests before reporting a task finished; quote the command output.",
		confidence: 0.95,
	})
	if files := learnedFiles(t, wd); len(files) != 1 || files[0].Status != evaluator.SkillStatusRejected {
		t.Fatalf("a rule similar to a rejected one was re-proposed: %+v", files)
	}

	// A different rule is still accepted.
	addSession(t, conn, "s3", "", 6, time.Hour)
	judgeSession(t, svc, "s3", &skillJudge{skill: "Prefer editing existing files over creating new modules for small fixes.", confidence: 0.9})
	if n := len(learnedFiles(t, wd)); n != 2 {
		t.Fatalf("distinct rule not proposed: %d files", n)
	}
}

func TestSkills_FileIsSourceOfTruthForMirror(t *testing.T) {
	_, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	svc := newSkillSvc(t, q, wd, turns(2))

	// A hand-written approved file becomes active after a sync; deleting it deactivates the mirror.
	f := evaluator.SkillFile{ID: "hand-written", Title: "Hand written", Status: evaluator.SkillStatusApproved,
		TaskType: "general", Confidence: 1, Created: time.Now(), Content: "Check git status before committing."}
	if err := os.MkdirAll(evaluator.LearnedSkillsDir(wd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evaluator.LearnedSkillsDir(wd), "hand-written.md"), f.Marshal(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncLearnedSkills(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetActiveSkills(ctx, "code"); len(got) != 1 || got[0].ID != "hand-written" {
		t.Fatalf("approved file not active after sync: %v", got)
	}
	if err := os.Remove(filepath.Join(evaluator.LearnedSkillsDir(wd), "hand-written.md")); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncLearnedSkills(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetActiveSkills(ctx, "code"); len(got) != 0 {
		t.Fatalf("deleted file still active: %v", got)
	}
}

func TestSkills_LegacyRowsAreExportedPendingNotInjected(t *testing.T) {
	conn, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	if _, err := conn.Exec(`INSERT INTO skill_library(id,title,content,task_type,is_active,created_at,updated_at) VALUES ('old-1','code skill','Keep diffs small and focused on the request.','code',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	svc := newSkillSvc(t, q, wd, turns(2))
	if err := svc.SyncLearnedSkills(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncLearnedSkills(ctx); err != nil { // second pass mirrors the exported file
		t.Fatal(err)
	}
	files := learnedFiles(t, wd)
	if len(files) != 1 || files[0].Status != evaluator.SkillStatusPending || files[0].ID != "old-1" {
		t.Fatalf("legacy row not exported as pending: %+v", files)
	}
	if got, _ := svc.GetActiveSkills(ctx, "code"); len(got) != 0 {
		t.Fatalf("legacy skill injected without review: %v", got)
	}
}

func TestSkills_ApprovingBeyondMaxEvictsLowestRanked(t *testing.T) {
	_, q := setupTestDB(t)
	wd := t.TempDir()
	ctx := context.Background()
	svc := newSvc(t, q, turns(2), func(c *config.EvaluatorConfig) { c.MaxSkills = 1 })
	svc.SetWorkDir(wd)
	for i, txt := range []string{"Alpha rule about migrations ordering.", "Beta guidance covering dependency upgrades."} {
		f := evaluator.SkillFile{ID: fmt.Sprintf("r%d", i), Title: txt, Status: evaluator.SkillStatusPending,
			TaskType: "general", Created: time.Now().Add(time.Duration(i) * time.Second), Content: txt}
		if err := os.MkdirAll(evaluator.LearnedSkillsDir(wd), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evaluator.LearnedSkillsDir(wd), f.ID+".md"), f.Marshal(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ReviewSkill(ctx, "r0", evaluator.SkillStatusApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewSkill(ctx, "r1", evaluator.SkillStatusApproved); err != nil {
		t.Fatal(err)
	}
	approved, _ := svc.ListSkills(ctx, evaluator.SkillStatusApproved, "")
	if len(approved) != 1 || approved[0].ID != "r1" {
		t.Fatalf("expected only the newly approved skill to survive: %+v", approved)
	}
	if rej, _ := svc.ListSkills(ctx, evaluator.SkillStatusRejected, ""); len(rej) != 1 || rej[0].ID != "r0" {
		t.Fatalf("evicted skill should be rejected: %+v", rej)
	}
}
