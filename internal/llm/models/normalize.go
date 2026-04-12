package models

import "strings"

var anthropicShorthandAliases = map[string]ModelID{
	"sonnet":                 Claude45Sonnet,
	"opus":                   Claude45Opus,
	"haiku":                  Claude45Haiku,
	"claude-sonnet-4.5":      Claude45Sonnet,
	"claude-opus-4.5":        Claude45Opus,
	"claude-haiku-4.5":       Claude45Haiku,
	"claude-sonnet-4":        Claude4Sonnet,
	"claude-opus-4":          Claude4Opus,
	"claude-opus-4.1":        Claude4Opus1,
	"claude-sonnet-4.6":      Claude46Sonnet,
	"claude-opus-4.6":        Claude46Opus,
	"claude-sonnet-4-6":      Claude46Sonnet,
	"claude-opus-4-6":        Claude46Opus,
}

func NormalizeModelID(input string) ModelID {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if modelID, ok := SupportedModels[ModelID(trimmed)]; ok {
		return modelID.ID
	}
	lower := strings.ToLower(trimmed)
	if alias, ok := anthropicShorthandAliases[lower]; ok {
		return alias
	}
	if strings.HasPrefix(lower, "anthropic.") {
		return ModelID(lower)
	}
	return ModelID(trimmed)
}
