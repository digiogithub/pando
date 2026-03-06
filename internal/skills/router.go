package skills

import "strings"

// MatchSkillToPrompt checks if a skill should activate based on its when-to-use field.
func MatchSkillToPrompt(metadata SkillMetadata, prompt string) bool {
	if metadata.WhenToUse == "" || metadata.DisableModelInvocation {
		return false
	}

	promptLower := normalizeSkillMatchText(prompt)
	if promptLower == "" {
		return false
	}

	triggers := strings.Split(strings.ToLower(metadata.WhenToUse), ",")
	for _, trigger := range triggers {
		trigger = strings.TrimSpace(trigger)
		if trigger == "" {
			continue
		}

		normalizedTrigger := normalizeSkillMatchText(trigger)
		if normalizedTrigger == "" {
			continue
		}

		if strings.Contains(promptLower, normalizedTrigger) {
			return true
		}

		words := significantSkillWords(normalizedTrigger)
		if len(words) == 0 {
			continue
		}

		matched := true
		for _, word := range words {
			if !strings.Contains(promptLower, word) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func normalizeSkillMatchText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte(' ')
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}

func significantSkillWords(text string) []string {
	words := strings.Fields(text)
	filtered := words[:0]
	for _, word := range words {
		if len(word) <= 2 {
			continue
		}
		switch word {
		case "the", "and", "for", "with", "from", "into", "this", "that", "your", "when", "use":
			continue
		}
		filtered = append(filtered, word)
	}
	return filtered
}
