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
