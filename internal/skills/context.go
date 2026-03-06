package skills

import (
	"fmt"
	"strings"
	"sync"
)

// ContextManager tracks token usage and manages skill loading levels.
type ContextManager struct {
	manager           *SkillManager
	maxContextTokens  int
	usedTokens        int
	evictionThreshold float64
	activeLoads       map[string]int
	mu                sync.RWMutex
}

func NewContextManager(manager *SkillManager, maxContextTokens int) *ContextManager {
	cm := &ContextManager{
		manager:           manager,
		maxContextTokens:  maxContextTokens,
		evictionThreshold: 0.80,
		activeLoads:       make(map[string]int),
	}
	cm.refreshUsedTokens()
	return cm
}

// GetMetadataPrompt returns the always-loaded Level 1 metadata for all skills
// formatted for injection into the system prompt. ~50 tokens per skill.
func (cm *ContextManager) GetMetadataPrompt() string {
	metadata := cm.manager.GetAllMetadata()
	if len(metadata) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("Available skills:\n")
	for _, skill := range metadata {
		builder.WriteString("- ")
		builder.WriteString(skill.Name)

		description := compactText(skill.Description)
		whenToUse := compactText(skill.WhenToUse)
		if description != "" {
			builder.WriteString(": ")
			builder.WriteString(description)
		}
		if whenToUse != "" {
			if description == "" {
				builder.WriteString(":")
			}
			builder.WriteString(" (when-to-use: ")
			builder.WriteString(whenToUse)
			builder.WriteString(")")
		}
		builder.WriteByte('\n')
	}

	return strings.TrimRight(builder.String(), "\n")
}

// ActivateSkill loads Level 2 instructions for a skill, evicting if needed.
func (cm *ContextManager) ActivateSkill(name string) (string, error) {
	requiredTokens, alreadyLoaded, err := cm.requiredInstructionTokens(name)
	if err != nil {
		return "", err
	}

	cm.beginLoad(name)
	defer cm.endLoad(name)

	if !alreadyLoaded && requiredTokens > 0 {
		cm.evictUntilRoom(name, requiredTokens)
	}

	instructions, err := cm.manager.GetInstructions(name)
	if err != nil {
		return "", err
	}

	cm.refreshUsedTokens()
	return instructions, nil
}

// ShouldActivate checks if a skill should be auto-activated based on prompt.
func (cm *ContextManager) ShouldActivate(prompt string) []string {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}

	metadata := cm.manager.GetAllMetadata()
	matches := make([]string, 0, len(metadata))
	for _, skill := range metadata {
		if MatchSkillToPrompt(skill, prompt) {
			matches = append(matches, skill.Name)
		}
	}

	return matches
}

// EstimateTokens estimates token count (~4 chars per token).
func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return (len(trimmed) + 3) / 4
}

// TokenUsage returns current token usage stats.
func (cm *ContextManager) TokenUsage() (used, max int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.usedTokens, cm.maxContextTokens
}

func (cm *ContextManager) requiredInstructionTokens(name string) (int, bool, error) {
	cm.manager.mu.RLock()
	skill, ok := cm.manager.skills[name]
	if !ok {
		cm.manager.mu.RUnlock()
		return 0, false, fmt.Errorf("skill %q not found", name)
	}
	if skill.LoadedLevel >= LevelInstructions && strings.TrimSpace(skill.Instructions) != "" {
		cm.manager.mu.RUnlock()
		return 0, true, nil
	}
	sourcePath := skill.SourcePath
	cm.manager.mu.RUnlock()

	parsed, err := ParseSkillFile(sourcePath)
	if err != nil {
		return 0, false, err
	}

	return EstimateTokens(parsed.Instructions), false, nil
}

func (cm *ContextManager) evictUntilRoom(protected string, requiredTokens int) {
	threshold := cm.thresholdTokens()
	if threshold <= 0 || requiredTokens <= 0 {
		return
	}

	for {
		cm.mu.RLock()
		used := cm.usedTokens
		cm.mu.RUnlock()
		if used+requiredTokens <= threshold {
			return
		}
		if !cm.evictOne(protected) {
			return
		}
	}
}

func (cm *ContextManager) evictOne(protected string) bool {
	cm.manager.mu.Lock()
	defer cm.manager.mu.Unlock()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	queueLen := len(cm.manager.loadOrder)
	for i := 0; i < queueLen; i++ {
		name := cm.manager.loadOrder[0]
		cm.manager.loadOrder = cm.manager.loadOrder[1:]

		skill, ok := cm.manager.skills[name]
		if !ok || strings.TrimSpace(skill.Instructions) == "" {
			continue
		}
		if name == protected || cm.activeLoads[name] > 0 {
			cm.manager.loadOrder = append(cm.manager.loadOrder, name)
			continue
		}

		cm.usedTokens -= EstimateTokens(skill.Instructions)
		if cm.usedTokens < 0 {
			cm.usedTokens = 0
		}

		skill.Instructions = ""
		skill.Resources = nil
		skill.LoadedLevel = LevelMetadata
		return true
	}

	return false
}

func (cm *ContextManager) refreshUsedTokens() {
	cm.manager.mu.RLock()
	used := 0
	for _, skill := range cm.manager.skills {
		if skill.LoadedLevel >= LevelInstructions && strings.TrimSpace(skill.Instructions) != "" {
			used += EstimateTokens(skill.Instructions)
		}
	}
	cm.manager.mu.RUnlock()

	cm.mu.Lock()
	cm.usedTokens = used
	cm.mu.Unlock()
}

func (cm *ContextManager) thresholdTokens() int {
	if cm.maxContextTokens <= 0 {
		return 0
	}

	threshold := int(float64(cm.maxContextTokens) * cm.evictionThreshold)
	if threshold <= 0 {
		return cm.maxContextTokens
	}
	return threshold
}

func (cm *ContextManager) beginLoad(name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.activeLoads[name]++
}

func (cm *ContextManager) endLoad(name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.activeLoads[name] <= 1 {
		delete(cm.activeLoads, name)
		return
	}
	cm.activeLoads[name]--
}

func compactText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
