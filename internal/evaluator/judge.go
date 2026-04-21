package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
)

const defaultJudgePrompt = `You are an expert AI assistant evaluator. Analyze this conversation transcript between an AI coding assistant and a user.

Template used: {{.TemplateName}} (version {{.TemplateVersion}})
User corrections detected: {{.Corrections}}
Total tokens used: {{.Tokens}}

Analyze the transcript and respond ONLY with a valid JSON object (no markdown, no explanation outside JSON):
{
  "reasoning": "brief explanation of what worked or did not work",
  "key_points": ["point1", "point2", "point3"],
  "new_skill": "optional: a 1-2 line instruction rule that would improve future sessions (empty string if none)",
  "task_type": "one of: code, refactor, debug, explain, general",
  "confidence": 0.0
}

Focus on the following quality dimensions:

1. **Scope compliance**: Did the agent respect explicit boundaries set by the user? Look for phrases like "NO", "NEVER", "only change X", "do it directly", "don't write a script". Violations (e.g., generating a script when told not to) are serious failures.

2. **Step-by-step adherence**: When the user provided numbered or bulleted steps, did the agent follow them in order without skipping?

3. **Constraint handling**: Did the agent treat ALL-CAPS instructions and explicit prohibitions as hard constraints, or did it silently work around them?

4. **Anti-patterns to penalise**:
   - Generating automation scripts when the user asked for a direct action ("hazlo directamente", "do it yourself")
   - Adding unrequested features, refactors, or improvements outside the task scope
   - Substituting a different tool or approach than the one explicitly specified
   - Providing verbose summaries or post-action explanations when not requested
   - Asking redundant clarifying questions after sufficient context was already given

5. **Iterative correction handling**: When the user provided feedback or corrections (including in Spanish: "arréglalo", "así no", "está mal", "no era eso"), did the agent incorporate them precisely without drifting?

6. **Context utilisation**: Did the agent make use of provided file references, error messages, and project context, or did it ignore them?

If you identify a recurring pattern that caused quality issues, encode it as a concise rule in "new_skill" (e.g., "Never generate a shell script when the user explicitly requests a direct action; perform the action using available tools instead.").

TRANSCRIPT:
{{.Transcript}}`

// JudgeMeta holds metadata passed to the judge prompt.
type JudgeMeta struct {
	TemplateName    string
	TemplateVersion int
	Corrections     int
	Tokens          int64
	Transcript      string
}

// renderJudgePrompt renders the judge prompt template with the given metadata.
func renderJudgePrompt(promptTemplate string, meta JudgeMeta) (string, error) {
	if promptTemplate == "" {
		promptTemplate = defaultJudgePrompt
	}
	t, err := template.New("judge").Parse(promptTemplate)
	if err != nil {
		return "", fmt.Errorf("judge prompt template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, meta); err != nil {
		return "", fmt.Errorf("judge prompt template execute: %w", err)
	}
	return buf.String(), nil
}

// parseJudgeOutput attempts to parse the LLM judge response as JudgeOutput JSON.
// It's lenient: if JSON parsing fails, returns a partial result with reasoning only.
func parseJudgeOutput(raw string) (*JudgeOutput, error) {
	// Try to find JSON in the response (model might wrap it)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return &JudgeOutput{Reasoning: raw, Confidence: 0}, nil
	}
	jsonStr := raw[start : end+1]

	var out JudgeOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return &JudgeOutput{Reasoning: raw, Confidence: 0}, nil
	}
	return &out, nil
}

// Judge calls an LLM model to evaluate session quality.
type Judge struct {
	p provider.Provider
}

// newJudge creates a Judge from evaluator config.
// Returns nil (not an error) if no model is configured.
func newJudge(cfg config.EvaluatorConfig) (*Judge, error) {
	if cfg.Model == "" {
		return nil, nil
	}

	globalCfg := config.Get()
	if globalCfg == nil {
		return nil, nil
	}

	// Look up model in supported models; fall back to a minimal model struct for dynamic models.
	model, ok := models.SupportedModels[cfg.Model]
	if !ok {
		providerName := models.ModelProvider(cfg.Provider)
		model = models.Model{
			ID:               cfg.Model,
			Name:             string(cfg.Model),
			Provider:         providerName,
			APIModel:         string(cfg.Model),
			DefaultMaxTokens: 1024,
		}
	}

	// Override provider if explicitly configured.
	providerName := model.Provider
	if cfg.Provider != "" {
		providerName = models.ModelProvider(cfg.Provider)
	}

	providerCfg, ok := globalCfg.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("evaluator judge: provider %q not configured", providerName)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("evaluator judge: provider %q is disabled", providerName)
	}

	p, err := provider.NewProvider(providerName,
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithMaxTokens(1024),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluator judge: create provider: %w", err)
	}

	return &Judge{p: p}, nil
}

// Evaluate calls the judge model with the session transcript and returns structured output.
func (j *Judge) Evaluate(ctx context.Context, meta JudgeMeta, customPromptTemplate string) (*JudgeOutput, error) {
	if j == nil {
		return nil, nil
	}

	prompt, err := renderJudgePrompt(customPromptTemplate, meta)
	if err != nil {
		return nil, fmt.Errorf("judge evaluate: render prompt: %w", err)
	}

	msgs := []message.Message{
		{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: prompt}},
		},
	}

	resp, err := j.p.SendMessages(ctx, msgs, nil)
	if err != nil {
		return nil, fmt.Errorf("judge evaluate: send messages: %w", err)
	}

	return parseJudgeOutput(resp.Content)
}
