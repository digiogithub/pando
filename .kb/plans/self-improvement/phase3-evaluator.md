---
name: Self-Improvement Phase 3 — Core Evaluator Service
description: New internal/evaluator/ package with reward calculator, UCB selector, LLM judge, and skill library manager
type: project
---

# Phase 3: Core Evaluator Service

**New Package:** `internal/evaluator/`

## File Structure

```
internal/evaluator/
├── service.go      # Main service, EvaluatorService interface
├── reward.go       # Reward function R = α*S_success + β*S_tokens
├── ucb.go          # UCB algorithm and template selection
├── judge.go        # LLM-as-Judge using cheap provider
└── skill.go        # Skill library CRUD and injection
```

---

## service.go

```go
package evaluator

type Service interface {
    // EvaluateSession triggers async (or sync) evaluation of a completed session.
    EvaluateSession(ctx context.Context, sessionID string) error

    // SelectTemplate returns the best prompt template for a given section name
    // using UCB algorithm. Falls back to default if not enough history.
    SelectTemplate(ctx context.Context, sectionName string) (*PromptTemplate, error)

    // GetActiveSkills returns skills to inject into the prompt for the given task type.
    GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error)

    // GetStats returns the current UCB rankings and skill library summary.
    GetStats(ctx context.Context) (*Stats, error)
}

type EvaluatorService struct {
    cfg      config.EvaluatorConfig
    db       *db.Queries
    messages message.Service
    sessions session.Service
    judge    *Judge         // LLM-as-Judge instance
    patterns []*regexp.Regexp // compiled correction patterns
    mu       sync.Mutex
}

func New(cfg config.EvaluatorConfig, conn *sql.DB, msgs message.Service, sess session.Service) (*EvaluatorService, error)
```

---

## reward.go

```go
// RewardResult holds the decomposed reward for a session.
type RewardResult struct {
    Total            float64
    SuccessScore     float64   // 1.0 = no corrections, 0.0 = corrections detected
    EfficiencyScore  float64   // normalized token efficiency [0,1]
    PromptTokens     int64
    CompletionTokens int64
    MessageCount     int64
    UserCorrections  int       // count of detected correction messages
}

// Calculate computes R = α*S_success + β*S_tokens for a session.
// It loads all messages for the session and analyzes them.
func (s *EvaluatorService) Calculate(ctx context.Context, sessionID string) (RewardResult, error) {
    msgs, _ := s.messages.List(ctx, sessionID)

    corrections := 0
    var promptTokens, completionTokens int64

    for _, msg := range msgs {
        // Count corrections in user messages
        if msg.Role == message.RoleUser {
            for _, p := range s.patterns {
                if p.MatchString(extractText(msg)) {
                    corrections++
                    break
                }
            }
        }
        // Accumulate tokens from Finish parts
        promptTokens += extractPromptTokens(msg)
        completionTokens += extractCompletionTokens(msg)
    }

    sSuccess := 1.0
    if corrections > 0 {
        sSuccess = math.Max(0, 1.0 - float64(corrections)*0.3)
    }

    // Normalize efficiency: compare to rolling baseline from DB
    baseline := s.getTokenBaseline(ctx)
    totalTokens := float64(promptTokens + completionTokens)
    sTokens := 0.5 // neutral if no baseline
    if baseline > 0 {
        sTokens = math.Max(0, math.Min(1, 1.0 - (totalTokens-baseline)/baseline))
    }

    total := s.cfg.AlphaWeight*sSuccess + s.cfg.BetaWeight*sTokens

    return RewardResult{
        Total:            total,
        SuccessScore:     sSuccess,
        EfficiencyScore:  sTokens,
        PromptTokens:     promptTokens,
        CompletionTokens: completionTokens,
        MessageCount:     int64(len(msgs)),
        UserCorrections:  corrections,
    }, nil
}
```

---

## ucb.go

```go
// UCBScore computes the UCB1 value for a template.
// UCB_i = avg_reward_i + c * sqrt(ln(N) / n_i)
func UCBScore(avgReward float64, totalSessions int, timesUsed int, explorationC float64) float64 {
    if timesUsed == 0 {
        return math.MaxFloat64 // never tried = highest priority
    }
    exploration := explorationC * math.Sqrt(math.Log(float64(totalSessions))/float64(timesUsed))
    return avgReward + exploration
}

// SelectTemplate picks the template with the highest UCB score for a given section.
// Returns nil (use default) if total evaluated sessions < MinSessionsForUCB.
func (s *EvaluatorService) SelectTemplate(ctx context.Context, sectionName string) (*PromptTemplate, error) {
    total, _ := s.db.CountSessionScores(ctx)
    if int(total) < s.cfg.MinSessionsForUCB {
        return nil, nil // not enough data yet
    }

    templates, _ := s.db.ListActiveTemplates(ctx, sectionName)
    if len(templates) == 0 {
        return nil, nil
    }

    var best *db.PromptTemplate
    bestScore := -math.MaxFloat64

    for _, t := range templates {
        stats, _ := s.db.GetUCBStats(ctx, t.ID)
        score := UCBScore(stats.AvgReward, int(total), int(stats.TimesUsed), s.cfg.ExplorationC)
        if score > bestScore {
            bestScore = score
            tmp := t
            best = &tmp
        }
    }

    return toPromptTemplate(best), nil
}
```

---

## judge.go

```go
// JudgeOutput is the structured response from the LLM judge.
type JudgeOutput struct {
    Reasoning string   `json:"reasoning"`
    KeyPoints []string `json:"key_points"`
    NewSkill  string   `json:"new_skill"` // optional: 1-2 line rule for skill library
    TaskType  string   `json:"task_type"` // detected task type
    Confidence float64 `json:"confidence"` // judge's confidence [0,1]
}

// JudgePrompt is the system prompt template for the evaluator model.
const JudgePromptTemplate = `You are an expert code review evaluator. Analyze this AI assistant session transcript.

Session used Strategy: {{.TemplateName}} (version {{.TemplateVersion}})
User corrections detected: {{.Corrections}}
Token usage: {{.Tokens}}

Analyze the transcript and respond ONLY with a JSON object:
{
  "reasoning": "brief explanation of what worked or didn't",
  "key_points": ["point1", "point2", "point3"],
  "new_skill": "optional: a 1-2 line instruction rule that would improve future sessions",
  "task_type": "code|refactor|debug|explain|general",
  "confidence": 0.0-1.0
}

Focus on: clarity of instructions, efficiency, whether the agent understood intent correctly.`

// Evaluate calls the judge model with the session transcript and returns structured output.
func (j *Judge) Evaluate(ctx context.Context, sessionID string, transcript string, meta JudgeMeta) (*JudgeOutput, error) {
    // Build judge prompt
    // Call cheap provider (separate from main agent provider)
    // Parse JSON response
    // Return JudgeOutput
}
```

---

## skill.go

```go
// InjectSkills appends relevant skills to the prompt data for the current session.
// Skills are selected by task type and success_rate DESC.
func (s *EvaluatorService) GetActiveSkills(ctx context.Context, taskType string) ([]Skill, error) {
    skills, err := s.db.ListActiveSkillsByType(ctx, db.ListActiveSkillsByTypeParams{
        TaskType:  taskType,
        IsActive:  true,
        Limit:     10,
    })
    // ...
}

// SaveSkillFromJudge persists a new skill generated by the judge.
// Enforces MaxSkills limit by deactivating lowest success_rate skills.
func (s *EvaluatorService) SaveSkillFromJudge(ctx context.Context, output *JudgeOutput, sessionID, templateID string) error {
    if output.NewSkill == "" || output.Confidence < 0.7 {
        return nil // skip low-confidence or empty skills
    }
    // Enforce max skills limit
    count, _ := s.db.CountActiveSkills(ctx)
    if count >= int64(s.cfg.MaxSkills) {
        s.db.DeactivateLowestSkill(ctx) // deactivate worst performing
    }
    // Insert new skill
    return s.db.InsertSkill(ctx, /* ... */)
}
```

---

## EvaluateSession Flow (async goroutine)

```go
func (s *EvaluatorService) EvaluateSession(ctx context.Context, sessionID string) error {
    if s.cfg.Async {
        go func() {
            bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
            defer cancel()
            s.runEvaluation(bgCtx, sessionID)
        }()
        return nil
    }
    return s.runEvaluation(ctx, sessionID)
}

func (s *EvaluatorService) runEvaluation(ctx context.Context, sessionID string) error {
    // 1. Calculate reward
    reward, err := s.Calculate(ctx, sessionID)

    // 2. Get template used for this session (stored in context or session metadata)
    templateID := s.getSessionTemplate(ctx, sessionID)

    // 3. Store session_scores row
    s.db.InsertSessionScore(ctx, /* ... */)
    // Trigger auto-updates UCB stats via DB trigger

    // 4. Call LLM judge if reward above threshold (don't waste tokens on bad sessions)
    if reward.Total > 0.5 || reward.SuccessScore == 1.0 {
        transcript := s.buildTranscript(ctx, sessionID)
        judgeOutput, err := s.judge.Evaluate(ctx, sessionID, transcript, /* meta */)

        // 5. Save new skill if judge generated one
        if judgeOutput != nil {
            s.SaveSkillFromJudge(ctx, judgeOutput, sessionID, templateID)
        }
    }

    return nil
}
```

**Why:** Async by default to never block the user. Judge only called on moderately successful sessions (reward > 0.5) to minimize API costs. Skill threshold at confidence 0.7 avoids low-quality rules accumulating.

**How to apply:** Initialize in app.go. Wire EvaluateSession into session.EndSession hook. Wire SelectTemplate into prompt/builder.go.
