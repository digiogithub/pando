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

func (s stubEvaluatorService) EvaluateSession(_ context.Context, _ string) error { return nil }
func (s stubEvaluatorService) SelectTemplate(_ context.Context, _ string) (*evaluator.PromptTemplate, error) {
	return nil, nil
}
func (s stubEvaluatorService) GetActiveSkills(_ context.Context, _ string) ([]evaluator.Skill, error) {
	return nil, nil
}
func (s stubEvaluatorService) GetStats(_ context.Context) (*evaluator.Stats, error) { return s.stats, nil }
func (s stubEvaluatorService) IsEnabled() bool { return true }
func (s stubEvaluatorService) RecordTemplateSelection(_ context.Context, _, _ string) {}

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
			Template: evaluator.PromptTemplate{
				Name:    "template",
				Section: "base",
				Version: 1,
			},
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

	p := NewEvaluatorPage(stubEvaluatorService{stats: stats}).(*evaluatorPage)
	p.stats = stats
	p.table = evaluatorcomp.NewTableCmp(stats.Templates)
	p.skills = evaluatorcomp.NewSkillsCmp(stats.TopSkills)
	p.metrics = evaluatorcomp.NewMetricsCmp(stats)
	p.SetSize(80, 18)

	view := p.View()
	if got := lipgloss.Height(view); got > 18 {
		t.Fatalf("view height = %d, want <= 18\n%s", got, view)
	}
}
