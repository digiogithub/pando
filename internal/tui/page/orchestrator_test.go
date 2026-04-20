package page

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/app"
	mesnadaOrchestrator "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	mesnadaModels "github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestOrchestratorViewStaysWithinTerminalHeight(t *testing.T) {
	p := NewOrchestratorPage(&app.App{
		MesnadaOrchestrator: &mesnadaOrchestrator.Orchestrator{},
	}).(*orchestratorPage)

	p.SetSize(80, 18)
	p.showDetail = true

	now := time.Now()
	output := strings.Repeat("tool output line\n", 40)
	tasks := make([]*mesnadaModels.Task, 0, 24)
	for i := 0; i < 24; i++ {
		tasks = append(tasks, &mesnadaModels.Task{
			ID:        string(rune('a'+i)) + "-task",
			Prompt:    "Investigate the orchestrator layout overflow in the terminal UI",
			Output:    output,
			Status:    mesnadaModels.TaskStatusRunning,
			Engine:    mesnadaModels.EnginePando,
			Model:     "gpt-5.4",
			CreatedAt: now,
		})
	}
	p.setTasks(tasks, tasks[0].ID)

	view := p.View()
	if got := lipgloss.Height(view); got > 18 {
		t.Fatalf("view height = %d, want <= 18\n%s", got, view)
	}
}
