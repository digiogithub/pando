---
name: Self-Improvement Phase 4 — Integration with Existing Pipeline
description: Wire evaluator into session.EndSession(), prompt/builder.go UCB selection, skill injection, new Lua hook, and app.go initialization
type: project
---

# Phase 4: Integration with Existing Pipeline

This phase connects the Evaluator Service to the existing Pando lifecycle. All changes are **additive** — no existing behavior is modified.

---

## 4.1 session/session.go — Trigger evaluation on session end

**Current flow:** `EndSession()` calls `hook_session_end` Lua hook, then creates "end" snapshot.

**New flow:** After snapshot creation, trigger `EvaluatorService.EvaluateSession()`.

```go
// In session.go ServiceImpl, add optional evaluator dependency:
type serviceImpl struct {
    // ... existing fields ...
    evaluator evaluator.Service // optional, nil if disabled
}

func (s *serviceImpl) EndSession(ctx context.Context, id string) error {
    // ... existing logic: hook_session_end, snapshot ...

    // NEW: async self-evaluation (non-blocking)
    if s.evaluator != nil {
        if err := s.evaluator.EvaluateSession(ctx, id); err != nil {
            // log warning but never fail EndSession
            slog.Warn("evaluator: failed to start evaluation", "session_id", id, "err", err)
        }
    }
    return nil
}
```

The evaluator is injected via `session.NewService(db, broker, snapshots, luaMgr, evaluator)` — adding optional parameter with `nil` guard.

---

## 4.2 llm/prompt/builder.go — UCB template selection

**Current flow:** `BuildPrompt()` loads templates from `TemplateRegistry` (disk/embedded).

**New flow:** Before rendering each section, ask UCB selector for best variant. If no variant (insufficient history), use default from registry.

```go
// In PromptBuilder, add optional evaluator:
type PromptBuilder struct {
    // ... existing fields ...
    evaluator evaluator.Service // optional
}

func (b *PromptBuilder) buildSection(ctx context.Context, sectionName string) (string, error) {
    // Try UCB selection first
    if b.evaluator != nil {
        tmpl, err := b.evaluator.SelectTemplate(ctx, sectionName)
        if err == nil && tmpl != nil {
            // Record which template was used in session context
            b.recordTemplateSelection(ctx, tmpl.ID)
            return b.renderTemplateContent(ctx, tmpl.Content), nil
        }
    }
    // Fallback: original registry behavior
    return b.registry.Render(sectionName, b.data)
}
```

**Template tracking:** Store `templateID` in a context value so `EvaluateSession` can retrieve it:

```go
type contextKey string
const selectedTemplateKey contextKey = "selected_template_id"

func WithSelectedTemplate(ctx context.Context, templateID string) context.Context {
    return context.WithValue(ctx, selectedTemplateKey, templateID)
}

func SelectedTemplateFromContext(ctx context.Context) string {
    v, _ := ctx.Value(selectedTemplateKey).(string)
    return v
}
```

---

## 4.3 llm/prompt/builder.go — Skill injection

**Current flow:** Skills section is rendered from `SkillManager` (file-based `.pando/skills/`).

**New flow:** Append DB-stored skills from Skill Library after file-based skills.

```go
func (b *PromptBuilder) buildSkillsSection(ctx context.Context) (string, error) {
    base, _ := b.registry.Render("skills", b.data)

    if b.evaluator != nil && b.data.HasSkills {
        // Detect task type from current session context (heuristic or from Lua)
        taskType := b.detectTaskType(ctx)
        skills, _ := b.evaluator.GetActiveSkills(ctx, taskType)
        if len(skills) > 0 {
            base += "\n\n## Learned Optimization Rules\n"
            for _, s := range skills {
                base += fmt.Sprintf("- %s\n", s.Content)
            }
        }
    }

    return base, nil
}
```

---

## 4.4 luaengine/types.go — New hook type

Add to existing HookType constants:

```go
const (
    // ... existing hooks ...
    HookEvaluationComplete HookType = "hook_evaluation_complete"
)
```

This hook is called after evaluation completes with parameters:
- `session_id`: the evaluated session
- `reward`: total reward score
- `success_score`: S_success value
- `efficiency_score`: S_tokens value
- `template_id`: template that was evaluated
- `new_skill`: skill generated (if any)

Lua scripts can react to evaluations (e.g., send notifications, log to external systems).

---

## 4.5 app/app.go — Initialize EvaluatorService

```go
type App struct {
    // ... existing fields ...
    Evaluator *evaluator.EvaluatorService // nil if disabled
}

func New(ctx context.Context, conn *sql.DB) (*App, error) {
    // ... existing initialization ...

    // Initialize evaluator (after sessions, messages, lua are ready)
    var evalSvc *evaluator.EvaluatorService
    if cfg.Evaluator.Enabled {
        evalSvc, err = evaluator.New(cfg.Evaluator, conn, app.Messages, app.Sessions)
        if err != nil {
            slog.Warn("evaluator: failed to initialize", "err", err)
            // Non-fatal: continue without evaluator
        } else {
            // Seed default templates from disk on first run
            evalSvc.SeedDefaultTemplates(ctx, app.PromptRegistry)
            app.Evaluator = evalSvc
        }
    }

    // Wire evaluator into session service
    app.Sessions = session.NewService(db, broker, app.Snapshots, app.LuaManager, evalSvc)

    // Wire evaluator into prompt builder
    app.CoderAgent.SetEvaluator(evalSvc)

    return app, nil
}
```

---

## 4.6 Template Seeding (first run)

On first run with evaluator enabled, read all embedded templates from `TemplateRegistry` and insert them as `is_default=1` into `prompt_templates`. This gives UCB a starting point.

```go
func (e *EvaluatorService) SeedDefaultTemplates(ctx context.Context, registry *prompt.TemplateRegistry) error {
    count, _ := e.db.CountPromptTemplates(ctx)
    if count > 0 {
        return nil // already seeded
    }

    for sectionName, content := range registry.AllSections() {
        e.db.InsertPromptTemplate(ctx, db.InsertPromptTemplateParams{
            ID:        uuid.New().String(),
            Name:      sectionName,
            Section:   sectionName,
            Content:   content,
            Version:   1,
            IsDefault: true,
            IsActive:  true,
        })
    }
    return nil
}
```

**Why:** Seeding ensures the UCB system always has at least one option per section. Default templates start with `ucb_score = 9999` so LLM-generated variants must earn their score through performance.

**How to apply:** All changes use nil guards and optional injection patterns already used in Pando (e.g., snapshots, lua manager). No existing tests should break.
