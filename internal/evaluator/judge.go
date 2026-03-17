package evaluator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
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

Focus on: clarity of instructions, efficiency, whether the agent understood user intent correctly.

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
