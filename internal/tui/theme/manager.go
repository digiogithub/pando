package theme

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2/styles"
	"github.com/digiogithub/pando/internal/config"
)

// Manager handles theme registration, selection, and retrieval.
// It maintains a registry of available themes and tracks the currently active theme.
type Manager struct {
	themes      map[string]Theme
	currentName string
	mu          sync.RWMutex
}

// Global instance of the theme manager
var globalManager = &Manager{
	themes:      make(map[string]Theme),
	currentName: "",
}

// RegisterTheme adds a new theme to the registry.
// If this is the first theme registered, it becomes the default.
// A "-nobg" variant is automatically registered alongside each theme.
func RegisterTheme(name string, t Theme) {
	globalManager.mu.Lock()
	defer globalManager.mu.Unlock()

	globalManager.themes[name] = t

	// Auto-register a transparent-background variant (skip for nobg themes themselves)
	if !strings.HasSuffix(name, "-nobg") {
		globalManager.themes[name+"-nobg"] = &NoBackgroundWrapper{Theme: t}
	}

	// If this is the first theme, make it the default
	if globalManager.currentName == "" {
		globalManager.currentName = name
	}
}

// SetTheme changes the active theme to the one with the specified name.
// Returns an error if the theme doesn't exist.
func SetTheme(name string) error {
	if err := ApplyTheme(name); err != nil {
		return err
	}

	return updateConfigTheme(name)
}

// ApplyTheme changes the active theme without writing to the config file.
func ApplyTheme(name string) error {
	globalManager.mu.Lock()
	defer globalManager.mu.Unlock()

	delete(styles.Registry, "charm")
	if _, exists := globalManager.themes[name]; !exists {
		return fmt.Errorf("theme '%s' not found", name)
	}

	globalManager.currentName = name
	return nil
}

// CurrentTheme returns the currently active theme.
// If no theme is set, it returns nil.
func CurrentTheme() Theme {
	globalManager.mu.RLock()
	defer globalManager.mu.RUnlock()

	if globalManager.currentName == "" {
		return nil
	}

	return globalManager.themes[globalManager.currentName]
}

// CurrentThemeName returns the name of the currently active theme.
func CurrentThemeName() string {
	globalManager.mu.RLock()
	defer globalManager.mu.RUnlock()

	return globalManager.currentName
}

// AvailableThemes returns a list of all registered theme names.
func AvailableThemes() []string {
	globalManager.mu.RLock()
	defer globalManager.mu.RUnlock()

	names := make([]string, 0, len(globalManager.themes))
	for name := range globalManager.themes {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if a == "pando" {
			return -1
		} else if b == "pando" {
			return 1
		}
		return strings.Compare(a, b)
	})
	return names
}

// GetTheme returns a specific theme by name.
// Returns nil if the theme doesn't exist.
func GetTheme(name string) Theme {
	globalManager.mu.RLock()
	defer globalManager.mu.RUnlock()

	return globalManager.themes[name]
}

// updateConfigTheme updates the theme setting in the configuration file
func updateConfigTheme(themeName string) error {
	if config.Get() == nil {
		return nil
	}
	// Use the config package to update the theme
	return config.UpdateTheme(themeName)
}
