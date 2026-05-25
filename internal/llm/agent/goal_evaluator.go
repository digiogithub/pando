package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
)

type GoalEvaluator interface {
	Evaluate(ctx context.Context, goal *GoalState, lastMessage string, todos []llmtools.TodoItem) (EvaluationResult, error)
}

type EvaluationResult struct {
	Complete      bool
	Blocked       bool
	Stalled       bool
	Reason        string
	Progress      string
	NextStep      string
	Confidence    float64
	ProgressMade  bool
	Signature     string
	TodoSignature string
}

type HeuristicGoalEvaluator struct {
	stallThreshold int
}

func NewHeuristicGoalEvaluator() *HeuristicGoalEvaluator {
	return NewHeuristicGoalEvaluatorWithThreshold(3)
}

func NewHeuristicGoalEvaluatorWithThreshold(stallThreshold int) *HeuristicGoalEvaluator {
	if stallThreshold <= 0 {
		stallThreshold = 3
	}
	return &HeuristicGoalEvaluator{stallThreshold: stallThreshold}
}

var (
	explicitGoalCompletedPattern = regexp.MustCompile(`(?is)\bGOAL COMPLETED\b\s*:?\s*(.*)`)
	explicitGoalBlockedPattern   = regexp.MustCompile(`(?is)\bGOAL BLOCKED\b\s*:?\s*(.*)`)

	patternCompletionRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(successfully (implemented|completed|finished|resolved|verified))\b`),
		regexp.MustCompile(`(?i)\b(all (tests|checks) (pass|passed|are passing))\b`),
		regexp.MustCompile(`(?i)\b(verified (working|it works|the fix works|the implementation works))\b`),
		regexp.MustCompile(`(?i)\b(goal achieved)\b`),
		regexp.MustCompile(`(?i)\b(completed successfully)\b`),
	}

	actionProgressRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(implemented|created|updated|modified|fixed|resolved|refactored|added|ran|verified|tested|inspected|analyzed|validated|wired|integrated)\b`),
		regexp.MustCompile(`(?i)\b(all tests pass|tests passed|verified working|working as expected)\b`),
	}

	blockedRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(cannot continue without|can't continue without)\b`),
		regexp.MustCompile(`(?i)\b(blocked by|waiting for user input|waiting for credentials|waiting for permission|waiting for access)\b`),
		regexp.MustCompile(`(?i)\b(missing credentials|missing permission|missing access|need credentials|need permission|need access)\b`),
	}

	remainingWorkRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(still need(?:s)? to|remaining|remain(?:ing)? tasks?|next step|next steps|pending|follow-?up|left to do|continue|continuing|not yet|todo)\b`),
		regexp.MustCompile(`(?i)\b(will now|i will now|next i(?:'|’)ll|then i(?:'|’)ll)\b`),
	}

	noProgressRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(no progress|unable to make progress|haven't made progress|have not made progress)\b`),
		regexp.MustCompile(`(?i)\b(still stuck|same issue|same blocker|still failing|still blocked)\b`),
	}
)

func (e *HeuristicGoalEvaluator) Evaluate(_ context.Context, goal *GoalState, lastMessage string, todos []llmtools.TodoItem) (EvaluationResult, error) {
	if goal == nil {
		return EvaluationResult{}, fmt.Errorf("goal cannot be nil")
	}

	progress := normalizeGoalText(lastMessage)
	todoSig := todoSignature(todos)
	nextStep := nextActionFromTodos(todos)
	if nextStep == "" {
		nextStep = extractNextStep(progress)
	}

	result := EvaluationResult{
		Progress:      progress,
		NextStep:      nextStep,
		Confidence:    0.20,
		TodoSignature: todoSig,
	}
	if result.Progress == "" && len(todos) > 0 {
		result.Progress = llmtools.TodoWriteSummary(todos, 0)
	}

	if matched := explicitGoalBlockedPattern.FindStringSubmatch(lastMessage); len(matched) > 0 {
		result.Blocked = true
		result.Confidence = 1.0
		result.Reason = fallbackReason(matched[1], result.Progress, "goal reported as blocked")
		result.Progress = fallbackReason(result.Progress, result.Reason, "GOAL BLOCKED")
		result.Signature = buildEvaluationSignature(result.NextStep, result.TodoSignature)
		result.ProgressMade = true
		return result, nil
	}

	if matched := explicitGoalCompletedPattern.FindStringSubmatch(lastMessage); len(matched) > 0 {
		result.Complete = true
		result.Confidence = 1.0
		result.Reason = fallbackReason(matched[1], result.Progress, "goal reported as completed")
		result.Progress = fallbackReason(result.Progress, result.Reason, "GOAL COMPLETED")
		result.NextStep = ""
		result.Signature = buildEvaluationSignature(result.NextStep, result.TodoSignature)
		result.ProgressMade = true
		return result, nil
	}

	planUpdated := todoSig != "" && todoSig != goal.InitialTodoSignature
	allTodosCompleted := planUpdated && allTodosComplete(todos)
	hasCompletionPattern := matchesAny(patternCompletionRules, progress)
	hasRemainingWork := matchesAny(remainingWorkRules, progress)

	if matchesAny(blockedRules, progress) && !hasCompletionPattern {
		result.Blocked = true
		result.Confidence = 0.92
		result.Reason = fallbackReason(progress, "goal is blocked pending external input", "goal blocked")
		result.Signature = buildEvaluationSignature(result.NextStep, result.TodoSignature)
		result.ProgressMade = true
		return result, nil
	}

	switch {
	case allTodosCompleted && !hasRemainingWork:
		result.Complete = true
		result.Confidence = 0.95
		result.Reason = fallbackReason(progress, "all tracked goal tasks are completed", "goal complete")
		result.NextStep = ""
	case hasCompletionPattern && !hasRemainingWork && (todoSig == "" || allTodosCompleted):
		result.Complete = true
		result.Confidence = 0.82
		result.Reason = fallbackReason(progress, "response indicates the goal is complete", "goal complete")
		result.NextStep = ""
	}

	result.Signature = buildEvaluationSignature(result.NextStep, result.TodoSignature)
	result.ProgressMade = detectProgress(goal, progress, result)
	if !result.ProgressMade && goal.ConsecutiveNoProgress+1 >= e.stallThreshold {
		result.Stalled = true
		result.Reason = fallbackReason(progress, "goal made no measurable progress across consecutive iterations", "goal stalled")
	}

	return result, nil
}

func detectProgress(goal *GoalState, progress string, result EvaluationResult) bool {
	if result.Complete || result.Blocked {
		return true
	}

	if result.TodoSignature != "" && result.TodoSignature != goal.LastTodoSignature {
		return true
	}

	if matchesAny(noProgressRules, progress) {
		return false
	}

	if matchesAny(actionProgressRules, progress) {
		return true
	}

	if result.Signature != "" && result.Signature != goal.LastEvaluationSignature {
		return true
	}

	return false
}

func nextActionFromTodos(todos []llmtools.TodoItem) string {
	for _, item := range todos {
		if strings.EqualFold(item.Status, "in_progress") {
			return normalizeGoalText(item.Content)
		}
	}
	for _, item := range todos {
		if strings.EqualFold(item.Status, "pending") {
			return normalizeGoalText(item.Content)
		}
	}
	return ""
}

func allTodosComplete(todos []llmtools.TodoItem) bool {
	if len(todos) == 0 {
		return false
	}
	for _, item := range todos {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "completed") {
			return false
		}
	}
	return true
}

func todoSignature(todos []llmtools.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}
	parts := make([]string, 0, len(todos))
	for _, item := range todos {
		content := normalizeGoalText(item.Content)
		status := strings.ToLower(strings.TrimSpace(item.Status))
		priority := strings.ToLower(strings.TrimSpace(item.Priority))
		if content == "" && status == "" && priority == "" {
			continue
		}
		parts = append(parts, status+"|"+priority+"|"+content)
	}
	return strings.Join(parts, "||")
}

func extractNextStep(progress string) string {
	if progress == "" {
		return ""
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?im)^\s*next step\s*:\s*(.+)$`),
		regexp.MustCompile(`(?im)^\s*next\s*:\s*(.+)$`),
		regexp.MustCompile(`(?im)^\s*remaining work\s*:\s*(.+)$`),
	}
	for _, pattern := range patterns {
		if matched := pattern.FindStringSubmatch(progress); len(matched) > 1 {
			return normalizeGoalText(matched[1])
		}
	}

	return ""
}

func buildEvaluationSignature(nextStep string, todoSignature string) string {
	return normalizeGoalText(nextStep) + "|" + todoSignature
}

func normalizeGoalText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func fallbackReason(values ...string) string {
	for _, value := range values {
		normalized := normalizeGoalText(value)
		if normalized != "" {
			return normalized
		}
	}
	return ""
}
