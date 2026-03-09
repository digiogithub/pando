package models

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheFileName = ".pando_models.json"

func cacheFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, cacheFileName), nil
}

// SaveModelCache saves dynamically discovered models to a local cache file.
// This allows models to be available immediately on the next startup.
func SaveModelCache() error {
	path, err := cacheFilePath()
	if err != nil {
		return err
	}

	toCache := make(map[ModelID]Model)
	dynamicModels.Range(func(key, value any) bool {
		id := key.(ModelID)
		model := value.(Model)
		toCache[id] = model
		return true
	})

	if len(toCache) == 0 {
		return nil
	}

	data, err := json.MarshalIndent(toCache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// LoadModelCache loads previously cached dynamic models into SupportedModels.
// Cached models do not overwrite static entries (Azure, VertexAI, Bedrock).
func LoadModelCache() error {
	path, err := cacheFilePath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // No cache yet, not an error
	}
	if err != nil {
		return err
	}

	var cached map[ModelID]Model
	if err := json.Unmarshal(data, &cached); err != nil {
		return err
	}

	for id, model := range cached {
		if _, exists := SupportedModels[id]; !exists {
			SupportedModels[id] = model
			dynamicModels.Store(id, model)
		}
	}

	return nil
}
