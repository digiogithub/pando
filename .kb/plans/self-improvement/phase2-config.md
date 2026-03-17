---
name: Self-Improvement Phase 2 — Config Extension
description: New [evaluator] config section with cheap model selection, α/β weights, UCB exploration factor, and correction detection
type: project
---

# Phase 2: Config Extension

**File:** `internal/config/config.go` — add EvaluatorConfig struct and wire into root Config

## New Struct

```go
// EvaluatorConfig controls the self-improvement loop behavior.
type EvaluatorConfig struct {
    // Enabled activates the evaluation loop. Default: false (opt-in).
    Enabled bool `toml:"enabled"`

    // Model is the cheap/fast model used for LLM-as-Judge evaluation.
    // Example: "claude-haiku-4-5-20251001", "gemini-1.5-flash", "gpt-4o-mini"
    Model models.ModelID `toml:"model"`

    // Provider specifies which provider to use for the judge model.
    // If empty, uses the same provider as the coder agent.
    Provider string `toml:"provider"`

    // AlphaWeight is the importance of task success in reward (default 0.8).
    // R = Alpha * S_success + Beta * S_tokens
    AlphaWeight float64 `toml:"alphaWeight"`

    // BetaWeight is the importance of token efficiency in reward (default 0.2).
    BetaWeight float64 `toml:"betaWeight"`

    // ExplorationC is the UCB exploration factor (default 1.41 = sqrt(2)).
    // Higher = more exploration of untested templates.
    ExplorationC float64 `toml:"explorationC"`

    // MinSessionsForUCB is the minimum number of evaluated sessions before
    // UCB selection activates. Below this threshold, use default template. (default 5)
    MinSessionsForUCB int `toml:"minSessionsForUCB"`

    // CorrectionsPatterns is a list of regex patterns that indicate the user
    // is correcting the agent. Used to compute S_success.
    // Defaults: ["no[,.]", "wrong", "incorrect", "that's not", "not what", "mistake"]
    CorrectionsPatterns []string `toml:"correctionsPatterns"`

    // MaxTokensBaseline is the rolling average window for token efficiency
    // normalization. (default 50 sessions)
    MaxTokensBaseline int `toml:"maxTokensBaseline"`

    // MaxSkills is the maximum number of active skills in the library (default 100).
    MaxSkills int `toml:"maxSkills"`

    // JudgePromptTemplate is the path to a custom judge prompt template.
    // If empty, uses the built-in judge prompt.
    JudgePromptTemplate string `toml:"judgePromptTemplate"`

    // Async runs evaluation in a background goroutine after session end (default true).
    // Set to false for synchronous evaluation (useful for testing).
    Async bool `toml:"async"`
}
```

## Default Values Function

```go
func defaultEvaluatorConfig() EvaluatorConfig {
    return EvaluatorConfig{
        Enabled:             false,
        AlphaWeight:         0.8,
        BetaWeight:          0.2,
        ExplorationC:        1.41, // sqrt(2)
        MinSessionsForUCB:   5,
        MaxTokensBaseline:   50,
        MaxSkills:           100,
        Async:               true,
        CorrectionsPatterns: []string{
            `(?i)\bno[,.]?\b`,
            `(?i)\bwrong\b`,
            `(?i)\bincorrect\b`,
            `(?i)that'?s not`,
            `(?i)not what i`,
            `(?i)\bmistake\b`,
            `(?i)\bfix that\b`,
            `(?i)\bthat's wrong\b`,
            `(?i)\bvuelve a\b`,       // Spanish correction
            `(?i)\bno era eso\b`,
            `(?i)\bte equivocaste\b`,
        },
    }
}
```

## TOML Example

```toml
[evaluator]
enabled = true
model = "claude-haiku-4-5-20251001"
provider = "anthropic"
alphaWeight = 0.8
betaWeight = 0.2
explorationC = 1.41
minSessionsForUCB = 5
maxSkills = 100
async = true
correctionsPatterns = [
    "(?i)\\bno[,.]?\\b",
    "(?i)\\bwrong\\b",
    "(?i)te equivocaste",
]
```

## Wire into Root Config

In `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    Evaluator EvaluatorConfig `toml:"evaluator"`
}

func defaultConfig() Config {
    return Config{
        // ... existing defaults ...
        Evaluator: defaultEvaluatorConfig(),
    }
}
```

## Validation

Add to `config.Validate()`:
```go
if cfg.Evaluator.Enabled {
    if cfg.Evaluator.AlphaWeight + cfg.Evaluator.BetaWeight != 1.0 {
        // warn but don't error — normalize internally
    }
    if cfg.Evaluator.Model == "" {
        return fmt.Errorf("evaluator.model is required when evaluator is enabled")
    }
}
```

**Why:** Follows existing config patterns (struct with toml tags, default function, explicit validation). Disabled by default so existing users are not affected. Spanish correction patterns included since the primary user (developer) works in Spanish.

**How to apply:** Add EvaluatorConfig to config.go alongside existing AgentConfig, SnapshotsConfig, etc. Wire in app.go after config load.
