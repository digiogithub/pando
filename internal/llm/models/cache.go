package models

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheFileName = ".pando_models.json"

// cacheSchemaVersion guards the on-disk model cache against entries written by a
// build that did not persist a field the current code depends on. A cache whose
// version does not match is discarded instead of loaded: serving metadata that
// silently lacks, say, SupportedEndpoints is worse than having no cache at all,
// because the missing field is indistinguishable from a legitimately empty one
// and sends models down the wrong API route until the next refresh lands.
//
// Bump this whenever a newly persisted Model field changes behaviour.
//
//	1: added SupportedEndpoints (Copilot /responses vs /chat/completions routing)
const cacheSchemaVersion = 1

// modelCacheFile is the on-disk cache layout. Pre-versioning caches were a bare
// map[ModelID]Model, which decodes here with Version == 0 and no Models, and is
// therefore dropped.
type modelCacheFile struct {
	Version int               `json:"version"`
	Models  map[ModelID]Model `json:"models"`
}

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

	data, err := json.MarshalIndent(modelCacheFile{Version: cacheSchemaVersion, Models: toCache}, "", "  ")
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

	var cached modelCacheFile
	if err := json.Unmarshal(data, &cached); err != nil {
		// A pre-versioning cache is a JSON object of models, so it decodes
		// cleanly into the struct; anything else is corrupt and equally
		// disposable — the next refresh rewrites it.
		return nil
	}
	if cached.Version != cacheSchemaVersion {
		return nil
	}

	for id, model := range cached.Models {
		if _, exists := SupportedModels[id]; !exists {
			SupportedModels[id] = model
			dynamicModels.Store(id, model)
		}
	}

	return nil
}
