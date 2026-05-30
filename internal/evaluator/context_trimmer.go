package evaluator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"text/template"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/message"
)

// ToolInfo carries the minimal information the trimmer needs to describe a tool.
type ToolInfo struct {
	Name        string
	Description string
}

const defaultTrimmerPrompt = `You are a task router for an AI coding assistant.
Analyze the user's first message and the available tools.
Return ONLY a JSON object (no markdown, no text outside JSON):
{
  "task_type": "one of: code|debug|refactor|explain|test|search|general",
  "relevant_tool_names": ["tool1", "tool2"],
  "skip_sections": ["capabilities/web_search"],
  "confidence": 0.85
}

Rules:
- relevant_tool_names must always include: bash, edit, view, glob, grep, write, patch, ls
- Only include tools that are actually needed for the task
- skip_sections should list capability sections not needed (e.g. capabilities/web_search for a pure refactor)
- confidence reflects how certain you are about the classification (0.0-1.0)

Available tools:
{{range .Tools}}- {{.Name}}: {{.Description}}
{{end}}

User's first message:
{{.FirstMessage}}`

// trimmerPromptData holds data for the trimmer prompt template.
type trimmerPromptData struct {
	Tools        []ToolInfo
	FirstMessage string
}

// trimmerOutput is the structured response from the trimmer LLM call.
type trimmerOutput struct {
	TaskType          string   `json:"task_type"`
	RelevantToolNames []string `json:"relevant_tool_names"`
	SkipSections      []string `json:"skip_sections"`
	Confidence        float64  `json:"confidence"`
}

// ContextTrimmer uses a cheap LLM to produce a ContextProfile at session start.
// It complements the post-session Judge: instead of "what went wrong?", it asks
// "what is needed for this task?".
type ContextTrimmer struct {
	judge  *Judge
	cfg    config.EvaluatorConfig
	cache  map[string]*ContextProfile
	cacheMu sync.Mutex
}

// NewContextTrimmer creates a ContextTrimmer that reuses the evaluator's judge
// infrastructure. Returns nil if the evaluator has no judge configured.
func NewContextTrimmer(cfg config.EvaluatorConfig, j *Judge) *ContextTrimmer {
	if j == nil {
		return nil
	}
	return &ContextTrimmer{
		judge: j,
		cfg:   cfg,
		cache: make(map[string]*ContextProfile),
	}
}

// ProfileTask analyzes the user's first message and returns a ContextProfile
// describing which tools and prompt sections are relevant for the task.
//
// The LLM call is bounded by a 3-second timeout; on timeout or error the method
// returns nil so callers can fall back to defaults. Results are cached by a hash
// of (firstMessage + toolNames) to avoid redundant LLM calls.
func (ct *ContextTrimmer) ProfileTask(
	ctx context.Context,
	firstMessage string,
	availableTools []ToolInfo,
) (*ContextProfile, error) {
	if ct == nil || firstMessage == "" {
		return nil, nil
	}

	cacheKey := ct.cacheKey(firstMessage, availableTools)
	ct.cacheMu.Lock()
	if cached, ok := ct.cache[cacheKey]; ok {
		ct.cacheMu.Unlock()
		slog.Debug("context_trimmer: cache hit", "task_type", cached.TaskType)
		return cached, nil
	}
	ct.cacheMu.Unlock()

	// Bound the LLM call to 3 seconds so it never blocks session start.
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	prompt, err := ct.renderPrompt(firstMessage, availableTools)
	if err != nil {
		return nil, fmt.Errorf("context_trimmer: render prompt: %w", err)
	}

	msgs := []message.Message{
		{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: prompt}},
		},
	}

	resp, err := ct.judge.p.SendMessages(callCtx, msgs, nil)
	if err != nil {
		slog.Debug("context_trimmer: LLM call failed, using defaults", "err", err)
		return nil, nil //nolint:nilerr // intentional fallback
	}

	out, err := parseTrimmerOutput(resp.Content)
	if err != nil || out == nil {
		slog.Debug("context_trimmer: parse failed, using defaults", "err", err)
		return nil, nil //nolint:nilerr // intentional fallback
	}

	profile := &ContextProfile{
		TaskType:          out.TaskType,
		RelevantToolNames: out.RelevantToolNames,
		SkipSections:      out.SkipSections,
		Confidence:        out.Confidence,
	}

	slog.Debug("context_trimmer: profile produced",
		"task_type", profile.TaskType,
		"tools", len(profile.RelevantToolNames),
		"skip_sections", len(profile.SkipSections),
		"confidence", profile.Confidence,
	)

	ct.cacheMu.Lock()
	ct.cache[cacheKey] = profile
	ct.cacheMu.Unlock()

	return profile, nil
}

// renderPrompt renders the trimmer prompt template with the given inputs.
func (ct *ContextTrimmer) renderPrompt(firstMessage string, tools []ToolInfo) (string, error) {
	tmpl, err := template.New("trimmer").Parse(defaultTrimmerPrompt)
	if err != nil {
		return "", fmt.Errorf("parse trimmer template: %w", err)
	}
	data := trimmerPromptData{
		Tools:        tools,
		FirstMessage: firstMessage,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute trimmer template: %w", err)
	}
	return buf.String(), nil
}

// parseTrimmerOutput extracts JSON from the LLM response.
// Lenient: returns nil (not an error) for malformed responses so callers fall back.
func parseTrimmerOutput(raw string) (*trimmerOutput, error) {
	start := bytes.IndexByte([]byte(raw), '{')
	end := bytes.LastIndexByte([]byte(raw), '}')
	if start == -1 || end == -1 || end <= start {
		return nil, nil
	}
	var out trimmerOutput
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, nil //nolint:nilerr // lenient parse
	}
	return &out, nil
}

// cacheKey builds a stable hash for (firstMessage, toolNames) to avoid redundant LLM calls.
func (ct *ContextTrimmer) cacheKey(firstMessage string, tools []ToolInfo) string {
	h := sha256.New()
	h.Write([]byte(firstMessage))
	for _, t := range tools {
		h.Write([]byte(t.Name))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
