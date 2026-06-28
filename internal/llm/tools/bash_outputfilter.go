package tools

import (
	"path/filepath"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/outputfilter"
	"github.com/digiogithub/pando/internal/logging"
)

var (
	outputFilterOnce   sync.Once
	outputFilterEngine *outputfilter.Engine
	outputFilterMu     sync.RWMutex
)

// projectFilterFile is the conventional project-local filter override path,
// resolved relative to the working directory.
const projectFilterFile = ".pando/filters.toml"

// getOutputFilterEngine lazily builds the shared filter engine from the
// embedded defaults plus any configured override paths and the project-local
// .pando/filters.toml. It is fail-safe: a load error still yields a usable
// engine (built-ins only) and is logged once.
func getOutputFilterEngine() *outputfilter.Engine {
	outputFilterMu.RLock()
	e := outputFilterEngine
	outputFilterMu.RUnlock()
	if e != nil {
		return e
	}

	outputFilterOnce.Do(func() {
		paths := outputFilterPaths()
		eng, err := outputfilter.Load(paths...)
		if err != nil {
			logging.Warn("output filter: some filters failed to load, using the rest", "error", err)
		}
		outputFilterMu.Lock()
		outputFilterEngine = eng
		outputFilterMu.Unlock()
	})

	outputFilterMu.RLock()
	defer outputFilterMu.RUnlock()
	return outputFilterEngine
}

// ResetOutputFilterEngine clears the cached engine so the next use reloads
// filters. Call after a config or filter-file change (hot reload).
func ResetOutputFilterEngine() {
	outputFilterMu.Lock()
	outputFilterEngine = nil
	outputFilterOnce = sync.Once{}
	outputFilterMu.Unlock()
}

// outputFilterPaths returns the override filter files in precedence order:
// project-local first, then user-configured paths.
func outputFilterPaths() []string {
	var paths []string
	if wd := config.WorkingDirectory(); wd != "" {
		paths = append(paths, filepath.Join(wd, projectFilterFile))
	}
	if cfg := config.Get(); cfg != nil {
		paths = append(paths, cfg.Bash.OutputFilterPaths...)
	}
	return paths
}

// outputFilterResult carries the compressed output plus the matched
// filter/parser name and the byte sizes before and after compression. When no
// compression is applied Name is empty and Before/After are left zero so the
// caller can omit them from the tool-result metadata.
type outputFilterResult struct {
	Output string
	Name   string
	Before int
	After  int
}

// applyOutputFilter compresses a command's output using the matching filter,
// unless filtering is disabled. It is fail-safe: it always returns a valid
// result and on any issue returns the original output unchanged (Name empty).
func applyOutputFilter(command, output string) outputFilterResult {
	res := outputFilterResult{Output: output}
	if command == "" || output == "" {
		return res
	}
	if cfg := config.Get(); cfg != nil && cfg.Bash.OutputFilterDisabled {
		return res
	}

	engine := getOutputFilterEngine()
	if engine == nil {
		return res
	}

	filtered, applied, name := engine.Apply(command, output)
	if !applied {
		return res
	}
	logging.Debug("output filter applied",
		"filter", name,
		"chars_before", len(output),
		"chars_after", len(filtered),
	)
	res.Output = filtered
	res.Name = name
	res.Before = len(output)
	res.After = len(filtered)
	return res
}
