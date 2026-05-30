package evaluator

import (
	"regexp"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// compiledPattern holds a pre-compiled regex and its associated task type.
type compiledPattern struct {
	re       *regexp.Regexp
	taskType string
}

// compileTaskPatterns compiles EvaluatorConfig.TaskPatterns into ready-to-use regexes.
// Patterns that fail to compile are silently skipped.
func compileTaskPatterns(patterns []config.TaskPatternConfig) []compiledPattern {
	out := make([]compiledPattern, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(`(?i)` + p.Pattern)
		if err != nil {
			continue
		}
		out = append(out, compiledPattern{re: re, taskType: p.TaskType})
	}
	return out
}

// ClassifyTask returns a task type label from the user's first message.
// It uses compiled patterns from config, evaluated in order; first match wins.
// Returns "general" if no pattern matches or the text is empty.
func (s *EvaluatorService) ClassifyTask(text string) string {
	if s == nil || text == "" {
		return "general"
	}
	lower := strings.ToLower(text)
	for _, cp := range s.taskPatterns {
		if cp.re.MatchString(lower) {
			return cp.taskType
		}
	}
	return "general"
}
