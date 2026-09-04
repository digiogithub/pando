package extensions

import (
	"context"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the prompt-running service.
//
// The direction of the dependency is the whole reason this file exists.
// Running a prompt needs the agent, the session store and the permission
// service, all of which live in internal/app, and internal/app imports this
// package. So internal/app registers its runner here before it loads the
// extensions, exactly as it registers the extension manager with the agent,
// and this package hands the registered function to extensions as the public
// interface.
//
// A host with no agent registers nothing: HostServices.Prompts stays nil and
// an extension that needs it says so instead of calling into a half-built
// process.

// PromptRunFunc performs one non-interactive turn. internal/app implements it.
type PromptRunFunc func(ctx context.Context, req extension.PromptRequest) (extension.PromptResult, error)

// promptRunner holds the registered function. It is read when a manager is
// built, so registration has to happen before Load.
var promptRunner atomic.Pointer[PromptRunFunc]

// SetPromptRunner registers the host's prompt runner. Passing nil unregisters
// it, which is what tests and teardown do.
func SetPromptRunner(fn PromptRunFunc) {
	if fn == nil {
		promptRunner.Store(nil)
		return
	}
	promptRunner.Store(&fn)
}

// currentPromptRunner returns the service to publish on HostServices, or nil
// when the host has no agent to run anything with.
func currentPromptRunner() extension.PromptRunner {
	if fn := promptRunner.Load(); fn != nil {
		return promptService{run: *fn}
	}
	return nil
}

// promptService adapts the registered function to the public interface and is
// where the guarantees the contract makes are actually enforced.
type promptService struct {
	run PromptRunFunc
}

// RunPrompt validates the request and runs it. The progress callback belongs
// to the extension, so it is wrapped in panic containment before core hands
// its event loop to it: a broken callback must break its own extension, not
// the turn and not the host.
func (p promptService) RunPrompt(ctx context.Context, req extension.PromptRequest) (extension.PromptResult, error) {
	if req.Prompt == "" {
		return extension.PromptResult{}, errEmptyPrompt
	}
	if cb := req.OnProgress; cb != nil {
		req.OnProgress = func(pr extension.PromptProgress) {
			defer func() {
				if r := recover(); r != nil {
					logging.Error("Extension prompt progress callback panicked, ignoring it", "panic", r)
				}
			}()
			cb(pr)
		}
	}
	return p.run(ctx, req)
}

// errEmptyPrompt is returned rather than sent to a model: an empty turn costs
// money and answers nothing.
var errEmptyPrompt = promptError("extension: RunPrompt needs a prompt")

// promptError is a string error, so the package adds no error type to its
// public surface.
type promptError string

func (e promptError) Error() string { return string(e) }
