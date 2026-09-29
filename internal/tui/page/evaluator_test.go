package page

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/evaluator"
	evaluatorcomp "github.com/digiogithub/pando/internal/tui/components/evaluator"
)

type stubEvaluatorService struct {
	stats *evaluator.Stats
}

func (s stubEvaluatorService) EvaluateSession(_ context.Context, _ string) error  { return nil }
func (s stubEvaluatorService) MarkCompleted(_ context.Context, _, _ string) error { return nil }
func (s stubEvaluatorService) SelectVariant(_ context.Context, _, _ string, c []string) (string, error) {
	return c[0], nil
}
func (s stubEvaluatorService) GetActiveSkills(_ context.Context, _ string) ([]evaluator.Skill, error) {
	return nil, nil
}
func (s stubEvaluatorService) ListSkills(_ context.Context, _, _ string) ([]evaluator.Skill, error) {
	return nil, nil
}
func (s stubEvaluatorService) ReviewSkill(_ context.Context, _, _ string) (*evaluator.Skill, error) {
	return nil, nil
}
func (s stubEvaluatorService) GetStats(_ context.Context) (*evaluator.Stats, error) {
	return s.stats, nil
}
func (s stubEvaluatorService) IsEnabled() bool              { return true }
func (s stubEvaluatorService) ClassifyTask(_ string) string { return "general" }

func TestEvaluatorViewStaysWithinTerminalHeight(t *testing.T) {
	stats := &evaluator.Stats{
		IsEnabled:        true,
		TotalEvaluations: 42,
		AvgReward:        0.73,
		SkillCount:       20,
		Templates:        make([]evaluator.TemplateStats, 0, 25),
		TopSkills:        make([]evaluator.Skill, 0, 30),
	}
	for i := 0; i < 25; i++ {
		stats.Templates = append(stats.Templates, evaluator.TemplateStats{
			Rank:      i + 1,
			TimesUsed: 10 + i,
			AvgReward: 0.5,
			UCBScore:  1.2,
			VariantID: "base/workflow#terse",
			Section:   "base/workflow",
			Variant:   "terse",
		})
	}
	for i := 0; i < 30; i++ {
		stats.TopSkills = append(stats.TopSkills, evaluator.Skill{
			TaskType:    "debug",
			SuccessRate: 0.9,
			UsageCount:  5,
			Content:     strings.Repeat("skill content ", 6),
		})
	}

	stats.Problem = "Evaluator is enabled but none of the last 20 eligible sessions was evaluated. Run `pando evaluator doctor`."
	for i := 0; i < 14; i++ {
		stats.Daily = append(stats.Daily, evaluator.DailyMetric{Day: "2026-09-01", Evaluations: int64(i), JudgeCalls: 1, JudgePromptTokens: 100})
	}
	for i := 0; i < 20; i++ {
		stats.RecentSessions = append(stats.RecentSessions, evaluator.SessionDetail{
			SessionID: "session-with-a-rather-long-identifier", Reward: 0.5, UserCorrections: 1,
			Breakdown: evaluator.Breakdown{
				Components: map[string]float64{"success": 0.6, "tokens": 0.4},
				Weights:    map[string]float64{"success": 0.7, "tokens": 0.3},
			},
		})
	}

	p := NewEvaluatorPage(stubEvaluatorService{stats: stats}).(*evaluatorPage)
	p.stats = stats
	p.loading = false
	p.sessions = evaluatorcomp.NewSessionsTableCmp(stats.RecentSessions)
	p.table = evaluatorcomp.NewTableCmp(stats.Templates)
	p.skills = evaluatorcomp.NewSkillsCmp(stats.TopSkills)
	p.metrics = evaluatorcomp.NewMetricsCmp(stats)
	p.SetSize(80, 18)

	view := p.View()
	if got := lipgloss.Height(view); got > 18 {
		t.Fatalf("view height = %d, want <= 18\n%s", got, view)
	}
	if !strings.Contains(view, "pando evaluator doctor") {
		t.Errorf("problem banner missing from view:\n%s", view)
	}

	// The sessions panel shows reward and top components and also fits.
	p.showSessions = true
	view = p.View()
	if got := lipgloss.Height(view); got > 18 {
		t.Fatalf("sessions view height = %d, want <= 18\n%s", got, view)
	}
	if !strings.Contains(view, "success 0.60") || !strings.Contains(view, "0.50") {
		t.Errorf("sessions panel does not show reward/components:\n%s", view)
	}
}
