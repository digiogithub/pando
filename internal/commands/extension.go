package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/pkg/extension"
)

// Slash commands contributed by extensions are resolved through this file so
// that every surface — ACP, TUI, WebUI, completions — sees the same set without
// each one having to know the extension system exists. The manager is injected
// (internal/app owns it) rather than imported, because internal/app already
// depends on this package.

var (
	extMu      sync.RWMutex
	extManager *extension.Manager
)

// SetExtensionManager wires the loaded extension manager into the slash-command
// registry. Passing nil detaches it.
func SetExtensionManager(mgr *extension.Manager) {
	extMu.Lock()
	defer extMu.Unlock()
	extManager = mgr
}

func extensionManager() *extension.Manager {
	extMu.RLock()
	defer extMu.RUnlock()
	return extManager
}

// ExtensionCommands returns the slash commands contributed by loaded
// extensions, in provider order.
//
// A command whose name collides with a built-in is dropped: an extension must
// not be able to redefine /compact, and resolving the collision in the
// extension's favour would change core behaviour depending on which binary the
// user is running.
func ExtensionCommands() []SlashCommand {
	mgr := extensionManager()
	if mgr == nil {
		return nil
	}
	reserved := make(map[string]bool)
	for _, c := range BuiltinCommands() {
		reserved[c.Name] = true
	}

	var out []SlashCommand
	for _, p := range extension.Capability[extension.SlashCommandProvider](mgr) {
		for _, c := range p.SlashCommands() {
			name := strings.ToLower(strings.TrimSpace(c.Name))
			if name == "" || reserved[name] {
				continue
			}
			reserved[name] = true
			out = append(out, SlashCommand{
				Name:        name,
				Description: c.Description,
				AcceptsArgs: c.AcceptsArgs,
			})
		}
	}
	return out
}

// IsExtensionCommand reports whether name is served by an extension.
func IsExtensionCommand(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, c := range ExtensionCommands() {
		if c.Name == name {
			return true
		}
	}
	return false
}

// ExtensionResult is what running an extension slash command produced. Prompt,
// when non-empty, is text the caller should send to the model as if the user
// had typed it; Output is text to show directly without a model turn.
type ExtensionResult struct {
	Prompt string
	Output string
}

// RunExtension executes an extension slash command. It reports false when no
// extension owns the name, which lets a caller fall through to its own
// handling.
//
// A panicking extension is contained: a broken command must not take down the
// session it was typed into.
func RunExtension(ctx context.Context, name, args string) (res ExtensionResult, handled bool, err error) {
	mgr := extensionManager()
	if mgr == nil {
		return ExtensionResult{}, false, nil
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ExtensionResult{}, false, nil
	}
	// Built-ins win, exactly as in ExtensionCommands: an extension that
	// declares /compact never gets to run it.
	for _, c := range BuiltinCommands() {
		if c.Name == name {
			return ExtensionResult{}, false, nil
		}
	}

	for _, p := range extension.Capability[extension.SlashCommandProvider](mgr) {
		owns := false
		for _, c := range p.SlashCommands() {
			if strings.EqualFold(strings.TrimSpace(c.Name), name) {
				owns = true
				break
			}
		}
		if !owns {
			continue
		}
		out, runErr := runSlashSafely(ctx, p, name, args)
		if runErr != nil {
			return ExtensionResult{}, true, runErr
		}
		return ExtensionResult{Prompt: out.Prompt, Output: out.Output}, true, nil
	}
	return ExtensionResult{}, false, nil
}

// runSlashSafely turns a panic into an error so the surface can report it like
// any other command failure.
func runSlashSafely(ctx context.Context, p extension.SlashCommandProvider, name, args string) (res extension.SlashResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("extension %s panicked running /%s: %v", p.ExtensionInfo().ID, name, r)
		}
	}()
	return p.RunSlashCommand(ctx, name, args)
}
