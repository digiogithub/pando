// Package agent handles spawning and managing CLI agent processes.
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/mesnada/config"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Manager coordinates multiple engine spawners.
type Manager struct {
	copilotSpawner        *CopilotSpawner
	claudeSpawner         *ClaudeSpawner
	geminiSpawner         *GeminiSpawner
	opencodeSpawner       *OpenCodeSpawner
	ollamaClaudeSpawner   *OllamaClaudeSpawner
	ollamaOpenCodeSpawner *OllamaOpenCodeSpawner
	mistralSpawner        *MistralSpawner
	acpSpawner            *ACPSpawner
	taskEngines           map[string]models.Engine // Maps task ID to engine
	mu                    sync.RWMutex
}

// NewManager creates a new agent manager.
func NewManager(cfg *mesnadaconfig.Config, logDir string, onComplete func(task *models.Task)) *Manager {
	m := &Manager{
		copilotSpawner:        NewCopilotSpawner(logDir, onComplete),
		claudeSpawner:         NewClaudeSpawner(logDir, onComplete),
		geminiSpawner:         NewGeminiSpawner(logDir, onComplete),
		opencodeSpawner:       NewOpenCodeSpawner(logDir, onComplete),
		ollamaClaudeSpawner:   NewOllamaClaudeSpawner(logDir, onComplete),
		ollamaOpenCodeSpawner: NewOllamaOpenCodeSpawner(logDir, onComplete),
		mistralSpawner:        NewMistralSpawner(logDir, onComplete),
		taskEngines:           make(map[string]models.Engine),
	}

	// Initialize ACP spawner if enabled in config
	if cfg != nil && cfg.ACP.Enabled {
		m.acpSpawner = NewACPSpawner(&cfg.ACP, logDir, onComplete)
	}

	return m
}

// Spawn starts a new agent using the appropriate engine.
func (m *Manager) Spawn(ctx context.Context, task *models.Task) error {
	engine := task.Engine
	if engine == "" {
		engine = models.DefaultEngine()
	}

	// Track which engine is handling this task
	m.mu.Lock()
	m.taskEngines[task.ID] = engine
	m.mu.Unlock()

	switch engine {
	case models.EngineClaude:
		return m.claudeSpawner.Spawn(ctx, task)
	case models.EngineGemini:
		return m.geminiSpawner.Spawn(ctx, task)
	case models.EngineOpenCode:
		return m.opencodeSpawner.Spawn(ctx, task)
	case models.EngineOllamaClaude:
		return m.ollamaClaudeSpawner.Spawn(ctx, task)
	case models.EngineOllamaOpenCode:
		return m.ollamaOpenCodeSpawner.Spawn(ctx, task)
	case models.EngineMistral:
		return m.mistralSpawner.Spawn(ctx, task)
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		if m.acpSpawner == nil {
			return fmt.Errorf("ACP engine requested but ACP is not enabled in configuration")
		}
		return m.acpSpawner.Spawn(ctx, task)
	case models.EngineCopilot:
		return m.copilotSpawner.Spawn(ctx, task)
	default:
		// Check if it's a dynamic ACP engine (prefix "acp-")
		if strings.HasPrefix(string(engine), "acp-") {
			if m.acpSpawner == nil {
				return fmt.Errorf("ACP engine requested (%s) but ACP is not enabled in configuration", engine)
			}
			return m.acpSpawner.Spawn(ctx, task)
		}
		return m.copilotSpawner.Spawn(ctx, task)
	}
}

// Cancel stops a running agent.
func (m *Manager) Cancel(taskID string) error {
	engine := m.getTaskEngine(taskID)

	switch engine {
	case models.EngineClaude:
		return m.claudeSpawner.Cancel(taskID)
	case models.EngineGemini:
		return m.geminiSpawner.Cancel(taskID)
	case models.EngineOpenCode:
		return m.opencodeSpawner.Cancel(taskID)
	case models.EngineOllamaClaude:
		return m.ollamaClaudeSpawner.Cancel(taskID)
	case models.EngineOllamaOpenCode:
		return m.ollamaOpenCodeSpawner.Cancel(taskID)
	case models.EngineMistral:
		return m.mistralSpawner.Cancel(taskID)
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		if m.acpSpawner != nil {
			return m.acpSpawner.Cancel(taskID)
		}
		return fmt.Errorf("ACP spawner not available")
	default:
		// Check for dynamic ACP engines
		if strings.HasPrefix(string(engine), "acp-") && m.acpSpawner != nil {
			return m.acpSpawner.Cancel(taskID)
		}
		return m.copilotSpawner.Cancel(taskID)
	}
}

// Pause stops a running agent without marking it as cancelled.
func (m *Manager) Pause(taskID string) error {
	engine := m.getTaskEngine(taskID)

	switch engine {
	case models.EngineClaude:
		return m.claudeSpawner.Pause(taskID)
	case models.EngineGemini:
		return m.geminiSpawner.Pause(taskID)
	case models.EngineOpenCode:
		return m.opencodeSpawner.Pause(taskID)
	case models.EngineOllamaClaude:
		return m.ollamaClaudeSpawner.Cancel(taskID)
	case models.EngineOllamaOpenCode:
		return m.ollamaOpenCodeSpawner.Cancel(taskID)
	case models.EngineMistral:
		return m.mistralSpawner.Pause(taskID)
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		if m.acpSpawner != nil {
			return m.acpSpawner.Pause(taskID)
		}
		return fmt.Errorf("ACP spawner not available")
	default:
		// Check for dynamic ACP engines
		if strings.HasPrefix(string(engine), "acp-") && m.acpSpawner != nil {
			return m.acpSpawner.Pause(taskID)
		}
		return m.copilotSpawner.Pause(taskID)
	}
}

// Wait blocks until a task completes or context is cancelled.
func (m *Manager) Wait(ctx context.Context, taskID string) error {
	engine := m.getTaskEngine(taskID)

	switch engine {
	case models.EngineClaude:
		return m.claudeSpawner.Wait(ctx, taskID)
	case models.EngineGemini:
		return m.geminiSpawner.Wait(ctx, taskID)
	case models.EngineOpenCode:
		return m.opencodeSpawner.Wait(ctx, taskID)
	case models.EngineOllamaClaude:
		// Wait not implemented for ollama spawners, return nil
		return nil
	case models.EngineOllamaOpenCode:
		// Wait not implemented for ollama spawners, return nil
		return nil
	case models.EngineMistral:
		return m.mistralSpawner.Wait(ctx, taskID)
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		if m.acpSpawner != nil {
			return m.acpSpawner.Wait(ctx, taskID)
		}
		return fmt.Errorf("ACP spawner not available")
	default:
		// Check for dynamic ACP engines
		if strings.HasPrefix(string(engine), "acp-") && m.acpSpawner != nil {
			return m.acpSpawner.Wait(ctx, taskID)
		}
		return m.copilotSpawner.Wait(ctx, taskID)
	}
}

// IsRunning checks if a task is currently running.
func (m *Manager) IsRunning(taskID string) bool {
	engine := m.getTaskEngine(taskID)

	switch engine {
	case models.EngineClaude:
		return m.claudeSpawner.IsRunning(taskID)
	case models.EngineGemini:
		return m.geminiSpawner.IsRunning(taskID)
	case models.EngineOpenCode:
		return m.opencodeSpawner.IsRunning(taskID)
	case models.EngineOllamaClaude:
		return m.ollamaClaudeSpawner.IsRunning(taskID)
	case models.EngineOllamaOpenCode:
		return m.ollamaOpenCodeSpawner.IsRunning(taskID)
	case models.EngineMistral:
		return m.mistralSpawner.IsRunning(taskID)
	case models.EngineACP, models.EngineACPClaudeCode, models.EngineACPCodex, models.EngineACPCustom:
		if m.acpSpawner != nil {
			return m.acpSpawner.IsRunning(taskID)
		}
		return false
	default:
		// Check for dynamic ACP engines
		if strings.HasPrefix(string(engine), "acp-") && m.acpSpawner != nil {
			return m.acpSpawner.IsRunning(taskID)
		}
		return m.copilotSpawner.IsRunning(taskID)
	}
}

// RunningCount returns the total number of currently running processes.
func (m *Manager) RunningCount() int {
	count := m.copilotSpawner.RunningCount() +
		m.claudeSpawner.RunningCount() +
		m.geminiSpawner.RunningCount() +
		m.opencodeSpawner.RunningCount() +
		m.mistralSpawner.RunningCount()

	// Count ollama spawners processes
	m.ollamaClaudeSpawner.mu.RLock()
	count += len(m.ollamaClaudeSpawner.processes)
	m.ollamaClaudeSpawner.mu.RUnlock()

	m.ollamaOpenCodeSpawner.mu.RLock()
	count += len(m.ollamaOpenCodeSpawner.processes)
	m.ollamaOpenCodeSpawner.mu.RUnlock()

	// Count ACP spawner processes if enabled
	if m.acpSpawner != nil {
		count += m.acpSpawner.RunningCount()
	}

	return count
}

// Shutdown cancels all running processes.
func (m *Manager) Shutdown() {
	m.copilotSpawner.Shutdown()
	m.claudeSpawner.Shutdown()
	m.geminiSpawner.Shutdown()
	m.opencodeSpawner.Shutdown()
	m.mistralSpawner.Shutdown()
	m.ollamaClaudeSpawner.Cleanup()
	m.ollamaOpenCodeSpawner.Cleanup()

	// Shutdown ACP spawner if enabled
	if m.acpSpawner != nil {
		m.acpSpawner.Shutdown()
	}
}

// getTaskEngine returns the engine used for a task.
func (m *Manager) getTaskEngine(taskID string) models.Engine {
	m.mu.RLock()
	defer m.mu.RUnlock()

	engine, exists := m.taskEngines[taskID]
	if !exists {
		return models.DefaultEngine()
	}
	return engine
}

// CleanupTask removes the engine tracking for a completed task.
func (m *Manager) CleanupTask(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.taskEngines, taskID)
}

// GetProcess returns information about a running process (legacy support).
func (m *Manager) GetProcess(taskID string) (*Process, bool) {
	return m.copilotSpawner.GetProcess(taskID)
}

// ValidateEngine checks if an engine string is valid.
func ValidateEngine(engine string) error {
	e := models.Engine(engine)
	// Allow dynamic ACP engines (prefix "acp-")
	if strings.HasPrefix(engine, "acp-") {
		return nil
	}
	if e != "" && !models.ValidEngine(e) {
		return fmt.Errorf("invalid engine: %s (valid: copilot, claude, gemini, opencode, ollama-claude, ollama-opencode, mistral, acp, acp-*, acp-claude, acp-codex, acp-custom)", engine)
	}
	return nil
}

// ACPSessionControl sends a control command to an active ACP session.
// This method is part of Phase 5 API but the actual implementation
// will be completed in Phase 6 (ACP Client Enhancement).
func (m *Manager) ACPSessionControl(taskID, action, message, mode string) (interface{}, error) {
	engine := m.getTaskEngine(taskID)

	// Check if this is an ACP engine
	if engine != models.EngineACP && engine != models.EngineACPClaudeCode &&
		engine != models.EngineACPCodex && engine != models.EngineACPCustom &&
		!strings.HasPrefix(string(engine), "acp-") {
		return nil, fmt.Errorf("task %s is not using an ACP engine (engine: %s)", taskID, engine)
	}

	if m.acpSpawner == nil {
		return nil, fmt.Errorf("ACP spawner not available")
	}

	return m.acpSpawner.SessionControl(taskID, action, message, mode)
}
