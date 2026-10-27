// Package engines is the single catalogue of Mesnada subagent engines and the
// live policy that governs them: which engines the user disabled in the Mesnada
// settings, and how deep delegations may nest (mesnada.delegation.maxDepth).
//
// Every surface that can start a subagent (the native spawn/swarm tools, the
// mesnada MCP server, the orchestrator) and every surface that shows engines
// (WebUI API, TUI settings) goes through this package, so they agree on the
// catalogue and always read the current configuration: a settings change applies
// on the next call, without a restart.
package engines

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Kind classifies an engine for display.
type Kind string

const (
	// KindNative engines run Pando itself; they can never be disabled.
	KindNative Kind = "native"
	// KindCLI engines run a third-party CLI agent as a subprocess.
	KindCLI Kind = "cli"
	// KindACP engines talk to a third-party agent over ACP.
	KindACP Kind = "acp"
	// KindCustom engines come from *.template.yaml files in the engines dir.
	KindCustom Kind = "custom"
)

// Engine is one entry of the built-in catalogue.
type Engine struct {
	Name   string
	Label  string
	Kind   Kind
	Binary string // executable looked up on PATH; empty when not applicable
}

// builtin lists the built-in engines in display order. Binaries mirror what the
// spawners in internal/mesnada/agent execute.
var builtin = []Engine{
	{Name: string(models.EnginePando), Label: "Pando", Kind: KindNative},
	{Name: string(models.EngineWarmACP), Label: "Pando (warm instance)", Kind: KindNative},
	{Name: string(models.EngineACPServer), Label: "Pando ACP server", Kind: KindNative},
	{Name: string(models.EngineClaude), Label: "Claude Code CLI", Kind: KindCLI, Binary: "claude"},
	{Name: string(models.EngineCopilot), Label: "GitHub Copilot CLI", Kind: KindCLI, Binary: "copilot"},
	{Name: string(models.EngineGemini), Label: "Gemini CLI", Kind: KindCLI, Binary: "gemini"},
	{Name: string(models.EngineOpenCode), Label: "OpenCode", Kind: KindCLI, Binary: "opencode"},
	{Name: string(models.EngineMistral), Label: "Mistral Vibe", Kind: KindCLI, Binary: "vibe"},
	{Name: string(models.EngineOllamaClaude), Label: "Ollama + Claude Code", Kind: KindCLI, Binary: "claude"},
	{Name: string(models.EngineOllamaOpenCode), Label: "Ollama + OpenCode", Kind: KindCLI, Binary: "opencode"},
	{Name: string(models.EngineACP), Label: "ACP agent (configured)", Kind: KindACP},
	{Name: string(models.EngineACPClaudeCode), Label: "Claude Code (ACP)", Kind: KindACP, Binary: "claude-code-acp"},
	{Name: string(models.EngineACPCodex), Label: "Codex (ACP)", Kind: KindACP, Binary: "codex-acp"},
}

// Builtin returns a copy of the built-in catalogue.
func Builtin() []Engine {
	out := make([]Engine, len(builtin))
	copy(out, builtin)
	return out
}

// Normalize maps engine aliases to their canonical name and trims whitespace.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "claude-code":
		return string(models.EngineClaude)
	case "gemini-cli":
		return string(models.EngineGemini)
	}
	return name
}

// IsNative reports whether name is one of Pando's own engines (including the
// empty/default engine). Native engines are never disableable.
func IsNative(name string) bool {
	switch Normalize(name) {
	case "", string(models.EnginePando), string(models.EngineWarmACP), string(models.EngineACPServer):
		return true
	}
	return false
}

// NormalizeDisabledList canonicalises a disabled-engines list (aliases
// resolved, blanks/duplicates/native engines dropped, sorted). It is the same
// normalisation the config applies on load and save.
func NormalizeDisabledList(list []string) []string {
	return config.NormalizeMesnadaDisabledEngines(list)
}

// Disabled returns the engines disabled in the live configuration.
func Disabled() []string {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	return NormalizeDisabledList(cfg.Mesnada.Orchestrator.DisabledEngines)
}

// IsDisabled reports whether name is disabled in the live configuration.
// Native engines are never disabled.
func IsDisabled(name string) bool {
	n := Normalize(name)
	if IsNative(n) {
		return false
	}
	for _, d := range Disabled() {
		if d == n {
			return true
		}
	}
	return false
}

// DisabledError is returned when a task asks for a disabled engine. The
// message tells the model how to recover.
type DisabledError struct {
	Engine string
}

func (e *DisabledError) Error() string {
	return fmt.Sprintf("engine %q is disabled in Mesnada settings; use engine=%s", e.Engine, models.EnginePando)
}

// CheckEnabled returns a *DisabledError when name is disabled.
func CheckEnabled(name string) error {
	if IsDisabled(name) {
		return &DisabledError{Engine: Normalize(name)}
	}
	return nil
}

// FilterEnabled returns names without the disabled engines, preserving order.
func FilterEnabled(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !IsDisabled(n) {
			out = append(out, n)
		}
	}
	return out
}

// DefaultMaxDepth mirrors the config default for mesnada.delegation.maxDepth.
const DefaultMaxDepth = 3

// MaxDepth returns the live delegation nesting cap (>= 1). It already folds in
// mesnada.delegation.allowNestedSubagents: when that is false the cap is 1.
func MaxDepth() int {
	cfg := config.Get()
	if cfg == nil {
		return DefaultMaxDepth
	}
	return cfg.Mesnada.Delegation.EffectiveMaxDepth()
}

// NestedAllowed reports the live mesnada.delegation.allowNestedSubagents value.
func NestedAllowed() bool {
	cfg := config.Get()
	return cfg == nil || cfg.Mesnada.Delegation.NestedSubagentsAllowed()
}

// DepthError is returned when a spawn would exceed the nesting cap.
type DepthError struct {
	Level int
	Max   int
	// NestedDisabled is true when the cap comes from allowNestedSubagents=false
	// rather than from maxDepth.
	NestedDisabled bool
}

func (e *DepthError) Error() string {
	if e.NestedDisabled {
		return "nested subagents are disabled in Mesnada settings: do this work yourself instead of spawning a subagent"
	}
	return fmt.Sprintf("delegation nesting limit reached (level %d > max %d): do this work yourself instead of spawning a subagent", e.Level, e.Max)
}

// CheckSpawnDepth refuses a task that would run at taskLevel when that exceeds
// the live cap. With MaxDepth=1 (or allowNestedSubagents=false) the main session
// (level 0) may spawn level-1 subagents, but those cannot spawn level-2
// grandchildren.
func CheckSpawnDepth(taskLevel int) error {
	if max := MaxDepth(); taskLevel > max {
		return &DepthError{Level: taskLevel, Max: max, NestedDisabled: !NestedAllowed() && taskLevel > 1}
	}
	return nil
}

// CanDelegate reports whether an agent running at ownLevel may still spawn
// subagents. It drives hiding the spawn/swarm/await tools.
func CanDelegate(ownLevel int) bool {
	return ownLevel < MaxDepth()
}

// Status is one engine as reported to the settings UIs.
type Status struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Kind      Kind   `json:"kind"`
	Binary    string `json:"binary"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Enabled   bool   `json:"enabled"`
	Locked    bool   `json:"locked"`
}

// CustomEngine describes a template engine for detection.
type CustomEngine struct {
	Name    string
	Command string
}

// lookPath is exec.LookPath, swappable in tests.
var lookPath = exec.LookPath

// Detect lists every built-in engine plus the given custom engines with their
// installed state (binary on PATH) and whether disabled lists them. A custom
// template that shadows a built-in name replaces that entry.
func Detect(custom []CustomEngine, disabled []string) []Status {
	off := map[string]bool{}
	for _, d := range NormalizeDisabledList(disabled) {
		off[d] = true
	}

	customByName := map[string]CustomEngine{}
	for _, c := range custom {
		if n := Normalize(c.Name); n != "" {
			customByName[n] = c
		}
	}

	out := make([]Status, 0, len(builtin)+len(custom))
	for _, e := range builtin {
		if c, ok := customByName[e.Name]; ok && e.Kind != KindNative {
			out = append(out, customStatus(c, off))
			delete(customByName, e.Name)
			continue
		}
		s := Status{Name: e.Name, Label: e.Label, Kind: e.Kind, Binary: e.Binary}
		if e.Kind == KindNative {
			s.Locked, s.Enabled, s.Installed = true, true, true
			s.Path = selfPath()
		} else {
			s.Enabled = !off[e.Name]
			s.Installed, s.Path = resolve(e.Binary)
		}
		out = append(out, s)
	}

	names := make([]string, 0, len(customByName))
	for n := range customByName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, customStatus(customByName[n], off))
	}
	return out
}

func customStatus(c CustomEngine, off map[string]bool) Status {
	name := Normalize(c.Name)
	s := Status{Name: name, Label: c.Name, Kind: KindCustom, Binary: c.Command, Enabled: !off[name]}
	s.Installed, s.Path = resolve(c.Command)
	return s
}

func resolve(binary string) (bool, string) {
	if binary == "" {
		return false, ""
	}
	p, err := lookPath(binary)
	if err != nil {
		return false, ""
	}
	if abs, absErr := filepath.Abs(p); absErr == nil {
		p = abs
	}
	return true, p
}

func selfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
