package extensions

import (
	"context"
	"sort"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// This file is the bridge between the public tool contract (pkg/extension) and
// the agent's internal tool interface (internal/llm/tools). Both directions are
// needed: extension tools have to become BaseTools to reach the model, and core
// tools have to look like extension.Tool so that middleware can see the whole
// set — a policy extension that could only filter other extensions' tools would
// be useless.

// coreTool presents an extension.Tool as a tools.BaseTool.
type coreTool struct {
	inner extension.Tool
}

func (t coreTool) Info() tools.ToolInfo {
	info, ok := guardValue("Tool.Info", "", t.inner.Info)
	if !ok {
		return tools.ToolInfo{}
	}
	return tools.ToolInfo{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
		Required:    info.Required,
	}
}

// Run calls the extension's tool. A panic becomes a tool error rather than a
// crash: the model can be told a tool failed, but nothing can be told anything
// once the process is gone. No deadline is imposed here — see guard.go.
func (t coreTool) Run(ctx context.Context, params tools.ToolCall) (resp tools.ToolResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension tool panicked",
				"tool", params.Name, "panic", r)
			resp = tools.NewTextErrorResponse("tool failed")
			err = nil
		}
	}()
	out, err := t.inner.Run(ctx, extension.ToolCall{ID: params.ID, Name: params.Name, Input: params.Input})
	if err != nil {
		return tools.ToolResponse{}, err
	}
	return toCoreResponse(out), nil
}

// extTool presents a tools.BaseTool as an extension.Tool.
type extTool struct {
	inner tools.BaseTool
}

func (t extTool) Info() extension.ToolInfo {
	info := t.inner.Info()
	return extension.ToolInfo{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
		Required:    info.Required,
	}
}

func (t extTool) Run(ctx context.Context, call extension.ToolCall) (extension.ToolResponse, error) {
	resp, err := t.inner.Run(ctx, tools.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input})
	if err != nil {
		return extension.ToolResponse{}, err
	}
	return extension.ToolResponse{
		Content:  resp.Content,
		Metadata: resp.Metadata,
		IsError:  resp.IsError,
	}, nil
}

// toCoreResponse converts a contract response, preserving the response type
// core assigns. The contract has no Type field on purpose (it would pin an
// internal enum into the public API), so a converted response is always text;
// image results stay an internal-only capability.
func toCoreResponse(resp extension.ToolResponse) tools.ToolResponse {
	out := tools.NewTextResponse(resp.Content)
	out.Metadata = resp.Metadata
	out.IsError = resp.IsError
	return out
}

// asExtensionTool wraps a core tool for middleware, unwrapping the round trip
// when the tool came from an extension in the first place. Without this a tool
// would gain a layer of adapters on every rebuild of the tool set.
func asExtensionTool(t tools.BaseTool) extension.Tool {
	if ct, ok := t.(coreTool); ok {
		return ct.inner
	}
	return extTool{inner: t}
}

// asCoreTool is the inverse of asExtensionTool.
func asCoreTool(t extension.Tool) tools.BaseTool {
	if et, ok := t.(extTool); ok {
		return et.inner
	}
	return coreTool{inner: t}
}

// middlewareOrder sorts middleware by ascending priority, breaking ties on
// extension ID so the order is identical in every process for a given build.
func middlewareOrder[T extension.ToolMiddleware](mws []T) []T {
	out := make([]T, len(mws))
	copy(out, mws)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].Priority(), out[j].Priority()
		if pi != pj {
			return pi < pj
		}
		return out[i].ExtensionInfo().ID < out[j].ExtensionInfo().ID
	})
	return out
}

// ApplyTools returns the tool set the model should see: the core tools passed
// in, plus every tool contributed by a loaded ToolProvider, with every
// ToolFilter applied and every ToolInterceptor wrapped around each tool.
//
// A nil manager (no extensions in the build, or a code path that never loaded
// them) returns coreTools untouched, so callers do not need to guard.
func ApplyTools(mgr *extension.Manager, coreTools []tools.BaseTool) []tools.BaseTool {
	if mgr == nil {
		return coreTools
	}

	providers := extension.Capability[extension.ToolProvider](mgr)
	filters := middlewareOrder(extension.Capability[extension.ToolFilter](mgr))
	interceptors := middlewareOrder(extension.Capability[extension.ToolInterceptor](mgr))
	if len(providers) == 0 && len(filters) == 0 && len(interceptors) == 0 {
		return coreTools
	}

	// Build the combined set in the contract's terms.
	set := make([]extension.Tool, 0, len(coreTools))
	for _, t := range coreTools {
		set = append(set, asExtensionTool(t))
	}
	seen := make(map[string]bool, len(set))
	for _, t := range set {
		seen[t.Info().Name] = true
	}
	for _, p := range providers {
		id := p.ExtensionInfo().ID
		declared, ok := guardDeclarative(context.Background(), "ToolProvider.Tools", id,
			func(context.Context) []extension.Tool { return p.Tools() })
		if !ok {
			continue
		}
		for _, t := range declared {
			if t == nil {
				continue
			}
			info, infoOK := guardValue("Tool.Info", id, t.Info)
			if !infoOK {
				continue
			}
			name := info.Name
			if name == "" {
				logging.Warn("Extension tool without a name ignored", "extension", id)
				continue
			}
			// A core tool always wins a name clash: an extension must never be
			// able to silently replace bash or edit. Shadowing is a mistake
			// worth reporting, not a feature.
			if seen[name] {
				logging.Warn("Extension tool name already taken, ignored",
					"extension", id, "tool", name)
				continue
			}
			seen[name] = true
			set = append(set, t)
		}
	}

	for _, f := range filters {
		filtered := safeFilter(f, set)
		set = filtered
	}

	if len(interceptors) > 0 {
		for i, t := range set {
			set[i] = wrapInterceptors(t, interceptors)
		}
	}

	out := make([]tools.BaseTool, 0, len(set))
	for _, t := range set {
		if t == nil {
			continue
		}
		out = append(out, asCoreTool(t))
	}
	return out
}

// safeFilter runs one filter, containing a panic: a broken policy extension
// must not take the agent down, and dropping its opinion is the safe failure
// (the unfiltered set is what core would have offered anyway).
func safeFilter(f extension.ToolFilter, in []extension.Tool) (out []extension.Tool) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension tool filter panicked, ignoring it",
				"extension", f.ExtensionInfo().ID, "panic", r)
			out = in
		}
	}()
	return f.FilterTools(in)
}

// interceptedTool applies an interceptor chain around a single tool.
type interceptedTool struct {
	inner extension.Tool
	chain extension.ToolFunc
}

func (t interceptedTool) Info() extension.ToolInfo { return t.inner.Info() }

func (t interceptedTool) Run(ctx context.Context, call extension.ToolCall) (extension.ToolResponse, error) {
	return t.chain(ctx, call)
}

// wrapInterceptors builds the call chain for one tool. Interceptors are applied
// innermost-first in priority order, so the highest priority ends up outermost
// and sees every call the others make.
func wrapInterceptors(t extension.Tool, interceptors []extension.ToolInterceptor) extension.Tool {
	next := t.Run
	for _, ic := range interceptors {
		next = chainLink(ic, next)
	}
	return interceptedTool{inner: t, chain: next}
}

// chainLink captures one interceptor and its successor. It is a function rather
// than an inline closure so that the loop variable capture is explicit, and it
// contains panics for the same reason safeFilter does.
func chainLink(ic extension.ToolInterceptor, next extension.ToolFunc) extension.ToolFunc {
	return func(ctx context.Context, call extension.ToolCall) (resp extension.ToolResponse, err error) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("Extension tool interceptor panicked",
					"extension", ic.ExtensionInfo().ID, "tool", call.Name, "panic", r)
				resp = extension.NewErrorResponse("tool interceptor failed")
				err = nil
			}
		}()
		return ic.InterceptTool(ctx, call, next)
	}
}
