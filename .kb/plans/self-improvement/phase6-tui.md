---
name: Self-Improvement Phase 6 — TUI Evaluator Page
description: New TUI page showing UCB template rankings, skill library, and self-improvement metrics dashboard
type: project
---

# Phase 6: TUI Evaluator Page

**New Files:**
- `internal/tui/page/evaluator.go` — Main evaluator page (Bubble Tea model)
- `internal/tui/components/evaluator/table.go` — UCB rankings table component
- `internal/tui/components/evaluator/skills.go` — Skill library list component
- `internal/tui/components/evaluator/metrics.go` — Stats header component

---

## Page Structure

The evaluator page has 3 panels:
1. **Metrics header**: total evaluations, avg reward, best template, skill count
2. **Template UCB Rankings table**: sorted by UCB score, shows avg_reward, times_used
3. **Skill Library list**: top skills sorted by success_rate, with task_type and usage

Navigation between panels with Tab/Shift+Tab.

---

## evaluator.go (page model)

```go
package page

type EvaluatorPage struct {
    evaluator   evaluator.Service
    stats       *evaluator.Stats
    activePanel int       // 0=metrics, 1=templates, 2=skills
    table       *evaluatorcomp.Table
    skillsList  *evaluatorcomp.SkillsList
    metrics     *evaluatorcomp.Metrics
    loading     bool
    err         error
    width       int
    height      int
}

// Messages
type evaluatorLoadedMsg struct { stats *evaluator.Stats }
type evaluatorErrMsg struct { err error }

func (p EvaluatorPage) Init() tea.Cmd {
    return p.loadStats()
}

func (p EvaluatorPage) loadStats() tea.Cmd {
    return func() tea.Msg {
        stats, err := p.evaluator.GetStats(context.Background())
        if err != nil {
            return evaluatorErrMsg{err: err}
        }
        return evaluatorLoadedMsg{stats: stats}
    }
}

func (p EvaluatorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "r":
            p.loading = true
            return p, p.loadStats() // manual refresh
        case "d":
            // delete selected skill (with confirm dialog)
        case "tab":
            p.activePanel = (p.activePanel + 1) % 3
        }
    case evaluatorLoadedMsg:
        p.loading = false
        p.stats = msg.stats
        p.table.SetData(msg.stats.Templates)
        p.skillsList.SetData(msg.stats.TopSkills)
        p.metrics.SetData(msg.stats)
    case tea.WindowSizeMsg:
        p.width, p.height = msg.Width, msg.Height
    }
    // ... delegate to active panel
}

func (p EvaluatorPage) View() string {
    if p.loading {
        return "Loading evaluator stats..."
    }
    if !p.evaluator.IsEnabled() {
        return renderDisabledMessage()
    }

    return lipgloss.JoinVertical(
        lipgloss.Left,
        p.metrics.View(),
        lipgloss.JoinHorizontal(
            lipgloss.Top,
            p.table.View(),    // 60% width
            p.skillsList.View(), // 40% width
        ),
        renderHelp(),
    )
}
```

---

## table.go — UCB Rankings

```
┌─ Prompt Templates (UCB Ranking) ────────────────────────────────────┐
│ # │ Name              │ Section      │ Ver │ Used │ Avg R │ UCB     │
│ 1 │ coder_base_v1    │ base         │  1  │  47  │ 0.82  │ 0.91 ★  │ (default)
│ 2 │ coder_concise_v2 │ base         │  2  │  12  │ 0.79  │ 0.95    │ (LLM-gen)
│ 3 │ capabilities_v1  │ capabilities │  1  │  59  │ 0.76  │ 0.80 ★  │ (default)
│ 4 │ skills_verbose   │ skills       │  3  │   3  │  ???  │ 9999    │ (new)
└──────────────────────────────────────────────────────────────────────┘
[r] Refresh  [d] Deactivate  [Enter] View content  [Tab] Switch panel
```

Columns: Rank, Name, Section, Version, Times Used, Avg Reward, UCB Score, Type (default/generated)

---

## skills.go — Skill Library

```
┌─ Skill Library (10/100) ─────────────────────────────────────────────┐
│ code    │ ✓ 0.91 │ 23× │ Always show file path before editing        │
│ debug   │ ✓ 0.88 │ 11× │ Start with reproducing the error first      │
│ general │ ✓ 0.85 │  8× │ Confirm understanding before writing code   │
│ refactor│ ✓ 0.72 │  5× │ Preserve public API when refactoring        │
│ explain │ ✓ 0.68 │  3× │ Use examples when explaining complex topics │
└──────────────────────────────────────────────────────────────────────┘
[d] Deactivate  [Tab] Switch panel
```

Columns: Task Type, Success Rate (colored: green >0.8, yellow >0.6, red <0.6), Uses, Content (truncated)

---

## metrics.go — Header Stats

```
┌─ Self-Improvement Metrics ─────────────────────────────────────────────────┐
│  Evaluations: 147    Avg Reward: 0.81    Best Template: coder_base_v1      │
│  Skills: 10/100      Last Eval: 2m ago   Judge Model: claude-haiku-4-5     │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## page/page.go — Register new page

Add to existing page type constants and navigation:

```go
const (
    // ... existing ...
    PageEvaluator PageType = "evaluator"
)

// In key handler for page navigation (e.g., Ctrl+E or dedicated shortcut)
case "ctrl+e":
    if app.Evaluator != nil {
        return m.switchPage(PageEvaluator)
    }
```

---

## keys.go — New keybindings

```go
var EvaluatorKeys = KeyMap{
    Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
    Deactivate: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deactivate")),
    ViewDetail: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
    SwitchPanel: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
}
```

---

## Disabled State

When `evaluator.enabled = false`, the page shows an info message:

```
Self-Improvement is disabled.

To enable it, add to your config:

  [evaluator]
  enabled = true
  model = "claude-haiku-4-5-20251001"
  provider = "anthropic"

Then restart Pando. The system will begin evaluating sessions
and learning from your interactions automatically.
```

**Why:** Follows exact same patterns as existing SnapshotPage (table + details), LogsPage, and OrchestratorPage. Graceful disabled state avoids confusing users who haven't opted in.

**How to apply:** Register in tui.go alongside existing pages. No changes to routing needed if using the existing page switch mechanism.
