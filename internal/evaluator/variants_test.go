package evaluator_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/evaluator"
)

const testSection = "base/workflow"

var testCandidates = []string{testSection + "#default", testSection + "#terse"}

func variantStats(t *testing.T, svc *evaluator.EvaluatorService, id string) (times int64, avg float64) {
	t.Helper()
	stats, err := svc.GetStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range stats.Templates {
		if ts.VariantID == id {
			return int64(ts.TimesUsed), ts.AvgReward
		}
	}
	return 0, 0
}

func TestSelectVariant_UCBPrefersBetterVariantAfterThreshold(t *testing.T) {
	conn, q := setupTestDB(t)
	svc := newSvc(t, q, twoTurns(), func(c *config.EvaluatorConfig) { c.MinSessionsForUCB = 4 })
	ctx := context.Background()

	var picks []string
	for i := 1; i <= 5; i++ {
		sid := fmt.Sprintf("ucb-%d", i)
		addSession(t, conn, sid, "", 2, time.Hour)
		v, err := svc.SelectVariant(ctx, sid, testSection, testCandidates)
		if err != nil {
			t.Fatal(err)
		}
		picks = append(picks, v)
		rating := "bad"
		if v == testCandidates[1] {
			rating = "good" // variant B does better
		}
		if _, err := svc.RecordFeedback(ctx, sid, rating, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Below the threshold the least selected variant is explored in turn.
	want := []string{testCandidates[0], testCandidates[1], testCandidates[0], testCandidates[1]}
	for i, w := range want {
		if picks[i] != w {
			t.Fatalf("exploration pick %d = %q, want %q (all: %v)", i+1, picks[i], w, picks)
		}
	}

	addSession(t, conn, "ucb-6", "", 2, time.Hour)
	v, err := svc.SelectVariant(ctx, "ucb-6", testSection, testCandidates)
	if err != nil {
		t.Fatal(err)
	}
	if v != testCandidates[1] {
		t.Fatalf("sixth session selected %q, want the better variant %q (picks: %v)", v, testCandidates[1], picks)
	}
	if times, _ := variantStats(t, svc, testCandidates[1]); times < 2 {
		t.Errorf("variant B times_used = %d, want >= 2", times)
	}
}

func TestSelectVariant_FrozenPerSessionAndAcrossProcesses(t *testing.T) {
	conn, q := setupTestDB(t)
	addSession(t, conn, "fz", "", 2, time.Hour)
	ctx := context.Background()

	svc1 := newSvc(t, q, twoTurns(), nil)
	other := "base/environment"
	a1, _ := svc1.SelectVariant(ctx, "fz", testSection, testCandidates)
	b1, _ := svc1.SelectVariant(ctx, "fz", other, []string{other + "#default", other + "#x"})

	// Make the other variant look far better, then "restart": a fresh service on
	// the same DB must return the persisted choice, not re-select.
	if _, err := conn.Exec(`INSERT INTO prompt_variant_stats(variant_id, section, times_used, total_reward, avg_reward, updated_at) VALUES (?, ?, 50, 49, 0.98, 1)`, testCandidates[1], testSection); err != nil {
		t.Fatal(err)
	}
	svc2 := newSvc(t, q, twoTurns(), nil)
	a2, _ := svc2.SelectVariant(ctx, "fz", testSection, testCandidates)
	b2, _ := svc2.SelectVariant(ctx, "fz", other, []string{other + "#default", other + "#x"})
	if a1 != a2 || b1 != b2 {
		t.Fatalf("selection changed across processes: %q/%q -> %q/%q", a1, b1, a2, b2)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM session_template_selections WHERE session_id='fz'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("expected 2 persisted selections (one per section), got %d (%v)", n, err)
	}

	// Evaluate with the fresh process: both sections get one use each.
	res, err := svc2.EvaluateNow(ctx, "fz", evaluator.EvaluateOptions{Force: true, SkipJudge: true})
	if err != nil || res.Skipped != "" {
		t.Fatalf("evaluate: %v %q", err, res.Skipped)
	}
	for _, id := range []string{a1, b1} {
		var times int64
		var total float64
		if err := conn.QueryRow(`SELECT times_used, total_reward FROM prompt_variant_stats WHERE variant_id=?`, id).Scan(&times, &total); err != nil {
			t.Fatalf("stats for %s: %v", id, err)
		}
		wantTimes := int64(1)
		if id == testCandidates[1] {
			wantTimes = 51 // pre-seeded 50 + this session
		}
		if times != wantTimes {
			t.Errorf("%s times_used = %d, want %d", id, times, wantTimes)
		}
	}
}

func TestSelectVariant_InertCases(t *testing.T) {
	conn, q := setupTestDB(t)
	ctx := context.Background()

	off := newSvc(t, q, twoTurns(), func(c *config.EvaluatorConfig) { c.Templates.Enabled = false })
	if v, err := off.SelectVariant(ctx, "s1", testSection, testCandidates); err != nil || v != testCandidates[0] {
		t.Errorf("kill switch: got %q, %v; want default", v, err)
	}
	on := newSvc(t, q, twoTurns(), nil)
	if v, _ := on.SelectVariant(ctx, "", testSection, testCandidates); v != testCandidates[0] {
		t.Errorf("no session: got %q, want default", v)
	}
	if v, _ := on.SelectVariant(ctx, "s1", testSection, testCandidates[:1]); v != testCandidates[0] {
		t.Errorf("single candidate: got %q, want default", v)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM session_template_selections`).Scan(&n); err != nil || n != 0 {
		t.Errorf("inert cases must not write selections, got %d (%v)", n, err)
	}

	// A frozen variant whose file disappeared falls back to default.
	if _, err := on.SelectVariant(ctx, "gone", testSection, []string{testCandidates[0], testCandidates[1]}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE session_template_selections SET variant_id = ? WHERE session_id='gone'`, testSection+"#deleted"); err != nil {
		t.Fatal(err)
	}
	fresh := newSvc(t, q, twoTurns(), nil)
	if v, _ := fresh.SelectVariant(ctx, "gone", testSection, testCandidates); v != testCandidates[0] {
		t.Errorf("stale frozen variant: got %q, want default", v)
	}
}
