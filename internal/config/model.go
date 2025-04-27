package config

import (
	"fmt"

	"github.com/opencode-ai/opencode/internal/llm/models"
)

func GetModel(model models.ModelID, provider models.ModelProvider) (models.Model, error) {
	if model == "" {
		return models.Model{}, fmt.Errorf("model id is empty")
	}

	m, foundModel := models.SupportedModels[model]

	if foundModel {
		return m, nil
	}

	providerName := m.Provider
	if providerName == "" {
		providerName = provider
	}

	if providerName == "" {
		return models.Model{}, fmt.Errorf("model %s not found", model)
	}
	providerCfg, foundProvider := cfg.Providers[providerName]
	if !foundProvider {
		return models.Model{}, fmt.Errorf("provider %s not supported", providerName)
	}
	if providerCfg.Disabled {
		return models.Model{}, fmt.Errorf("provider %s is not enabled", providerName)
	}

	// try to find the model in the provider config
	if !foundModel {
		m, foundModel = providerCfg.Models[model]
		// Add some default behavior
		if foundModel {
			m.Provider = providerName
			if m.ID == "" {
				m.ID = model
			}
			if m.APIModel == "" {
				m.APIModel = string(model)
			}
			if m.Name == "" {
				m.Name = fmt.Sprintf("%s: %s", providerName, model)
			}
			if m.Ref != "" {
				existingModel, foundExisting := models.SupportedModels[models.ModelID(m.Ref)]
				if foundExisting {
					m.CostPer1MIn = existingModel.CostPer1MIn
					m.CostPer1MInCached = existingModel.CostPer1MInCached
					m.CostPer1MOut = existingModel.CostPer1MOut
					m.CostPer1MOutCached = existingModel.CostPer1MOutCached
					m.ContextWindow = existingModel.ContextWindow
					m.DefaultMaxTokens = existingModel.DefaultMaxTokens
					m.ContextWindow = existingModel.ContextWindow
				}
			}
			if m.DefaultMaxTokens == 0 {
				m.DefaultMaxTokens = 4096
			}
			if m.ContextWindow == 0 {
				m.ContextWindow = 50_000
			}
		}

		// if not found create a simple model just based on the model id
		if !foundModel {
			m = models.Model{
				ID:               model,
				APIModel:         string(model),
				Provider:         providerName,
				Name:             fmt.Sprintf("%s: %s", providerName, model),
				DefaultMaxTokens: 4096,
			}
		}
	}
	return m, nil
}
