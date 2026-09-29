package evaluator

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/digiogithub/pando/internal/evaluator"
)

// NewSessionsTableCmp creates a table of the recently evaluated sessions:
// reward, corrections, the top reward components and how many variants and
// skills the session ran with.
func NewSessionsTableCmp(sessions []evaluator.SessionDetail) TableComponent {
	columns := []table.Column{
		{Title: "Session", Width: 22},
		{Title: "Reward", Width: 7},
		{Title: "Corr", Width: 5},
		{Title: "Components", Width: 28},
		{Title: "Var/Skl", Width: 8},
		{Title: "Judge", Width: 6},
	}
	tableModel := table.New(table.WithColumns(columns))
	tableModel.Focus()

	rows := make([]table.Row, 0, len(sessions))
	for _, s := range sessions {
		name := s.Title
		if strings.TrimSpace(name) == "" {
			name = s.SessionID
		}
		judge := "-"
		if s.JudgeModel != "" {
			judge = "yes"
		}
		rows = append(rows, table.Row{
			truncateRunes(name, 22),
			fmt.Sprintf("%.2f", s.Reward),
			fmt.Sprintf("%d", s.UserCorrections),
			componentsText(s),
			fmt.Sprintf("%d/%d", len(s.Variants), len(s.Skills)),
			judge,
		})
	}
	tableModel.SetRows(rows)
	return &tableCmp{table: tableModel, weights: []int{22, 7, 5, 30, 8, 6}}
}

// componentsText renders the two highest-weight components, e.g. "success 0.60 tokens 0.50".
func componentsText(s evaluator.SessionDetail) string {
	top := s.TopComponents(2)
	if len(top) == 0 {
		return fmt.Sprintf("success %.2f eff %.2f", s.SuccessScore, s.EfficiencyScore)
	}
	parts := make([]string, 0, len(top))
	for _, c := range top {
		parts = append(parts, fmt.Sprintf("%s %.2f", c.Name, c.Score))
	}
	return strings.Join(parts, " ")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
