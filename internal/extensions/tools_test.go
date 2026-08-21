package extensions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/pkg/extension"
)

// fakeCoreTool is a minimal tools.BaseTool standing in for a core tool.
type fakeCoreTool struct {
	name string
	out  string
}

func (t fakeCoreTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: t.name, Description: "core " + t.name}
}

func (t fakeCoreTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse(t.out), nil
}

// fakeExtTool is a minimal extension.Tool.
type fakeExtTool struct {
	name string
	out  string
	err  error
}

func (t fakeExtTool) Info() extension.ToolInfo {
	return extension.ToolInfo{Name: t.name, Description: "ext " + t.name}
}

func (t fakeExtTool) Run(context.Context, extension.ToolCall) (extension.ToolResponse, error) {
	if t.err != nil {
		return extension.ToolResponse{}, t.err
	}
	return extension.NewTextResponse(t.out), nil
}

// Capabilities are separate types on purpose: a single struct with every
// method would implement every interface, and the manager would then hand
// ApplyTools a "filter" whose filter function is nil.
type baseExt struct {
	id       extension.ID
	priority int
}

func (e baseExt) info(self extension.Extension) extension.Info {
	return extension.Info{ID: e.id, Version: "1.0.0", New: func() extension.Extension { return self }}
}
func (e baseExt) Priority() int { return e.priority }

type provExt struct {
	baseExt
	tools []extension.Tool
}

func (e *provExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *provExt) Tools() []extension.Tool       { return e.tools }

type filterExt struct {
	baseExt
	filter func([]extension.Tool) []extension.Tool
}

func (e *filterExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *filterExt) FilterTools(in []extension.Tool) []extension.Tool {
	return e.filter(in)
}

type interExt struct {
	baseExt
	intercep func(context.Context, extension.ToolCall, extension.ToolFunc) (extension.ToolResponse, error)
}

func (e *interExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *interExt) InterceptTool(ctx context.Context, call extension.ToolCall, next extension.ToolFunc) (extension.ToolResponse, error) {
	return e.intercep(ctx, call, next)
}

// managerWith loads exts into an isolated manager, so tests never touch the
// package-level registry.
func managerWith(t *testing.T, exts ...extension.Extension) *extension.Manager {
	t.Helper()
	reg := extension.NewRegistry()
	for _, e := range exts {
		reg.Register(e)
	}
	mgr := extension.NewManager(extension.Options{Registry: reg})
	if err := mgr.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(mgr.Cleanup)
	return mgr
}

func toolNames(ts []tools.BaseTool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Info().Name
	}
	return out
}

func findTool(ts []tools.BaseTool, name string) tools.BaseTool {
	for _, t := range ts {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}

func TestApplyToolsNilManagerIsIdentity(t *testing.T) {
	core := []tools.BaseTool{fakeCoreTool{name: "bash"}}
	got := ApplyTools(nil, core)
	if len(got) != 1 || got[0].Info().Name != "bash" {
		t.Fatalf("got %v", toolNames(got))
	}
}

func TestApplyToolsAddsProviderTools(t *testing.T) {
	mgr := managerWith(t, &provExt{baseExt: baseExt{id: "tools.acme"}, tools: []extension.Tool{fakeExtTool{name: "acme_jira", out: "ok"}}})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}})

	jira := findTool(got, "acme_jira")
	if jira == nil {
		t.Fatalf("extension tool missing: %v", toolNames(got))
	}
	resp, err := jira.Run(context.Background(), tools.ToolCall{Name: "acme_jira"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q", resp.Content)
	}
}

// A core tool must always win a name clash: an extension that could replace
// bash could silently take over the agent.
func TestApplyToolsCoreToolWinsNameClash(t *testing.T) {
	mgr := managerWith(t, &provExt{baseExt: baseExt{id: "tools.acme"}, tools: []extension.Tool{fakeExtTool{name: "bash", out: "hijacked"}}})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash", out: "real"}})

	if len(got) != 1 {
		t.Fatalf("got %v", toolNames(got))
	}
	resp, _ := got[0].Run(context.Background(), tools.ToolCall{Name: "bash"})
	if resp.Content != "real" {
		t.Errorf("core tool was replaced: %q", resp.Content)
	}
}

func TestApplyToolsFilterSeesCoreTools(t *testing.T) {
	var seen []string
	mgr := managerWith(t, &filterExt{
		baseExt: baseExt{id: "policy.acme"},
		filter: func(in []extension.Tool) []extension.Tool {
			var out []extension.Tool
			for _, tool := range in {
				seen = append(seen, tool.Info().Name)
				if tool.Info().Name != "bash" {
					out = append(out, tool)
				}
			}
			return out
		},
	})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}, fakeCoreTool{name: "view"}})

	if len(seen) != 2 {
		t.Errorf("filter saw %v, want both core tools", seen)
	}
	if findTool(got, "bash") != nil {
		t.Errorf("bash was not filtered out: %v", toolNames(got))
	}
	if findTool(got, "view") == nil {
		t.Errorf("view was dropped: %v", toolNames(got))
	}
}

// Filters run in ascending priority order, and the order must not depend on
// registration order.
func TestApplyToolsFilterOrderFollowsPriority(t *testing.T) {
	var order []string
	mk := func(id extension.ID, prio int) *filterExt {
		return &filterExt{baseExt: baseExt{id: id, priority: prio}, filter: func(in []extension.Tool) []extension.Tool {
			order = append(order, string(id))
			return in
		}}
	}
	mgr := managerWith(t, mk("policy.late", 10), mk("policy.early", -5))
	ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}})

	if len(order) != 2 || order[0] != "policy.early" || order[1] != "policy.late" {
		t.Errorf("filter order = %v", order)
	}
}

func TestApplyToolsInterceptorWrapsEveryTool(t *testing.T) {
	var calls []string
	mgr := managerWith(t, &interExt{
		baseExt: baseExt{id: "audit.acme"},
		intercep: func(ctx context.Context, call extension.ToolCall, next extension.ToolFunc) (extension.ToolResponse, error) {
			calls = append(calls, call.Name)
			resp, err := next(ctx, call)
			resp.Content = "[audited] " + resp.Content
			return resp, err
		},
	})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash", out: "hi"}})

	resp, err := got[0].Run(context.Background(), tools.ToolCall{Name: "bash"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if resp.Content != "[audited] hi" {
		t.Errorf("content = %q", resp.Content)
	}
	if len(calls) != 1 || calls[0] != "bash" {
		t.Errorf("interceptor calls = %v", calls)
	}
}

// An interceptor may refuse a call outright; next must then never run.
func TestApplyToolsInterceptorCanRefuse(t *testing.T) {
	mgr := managerWith(t, &interExt{
		baseExt: baseExt{id: "policy.acme"},
		intercep: func(context.Context, extension.ToolCall, extension.ToolFunc) (extension.ToolResponse, error) {
			return extension.NewErrorResponse("not allowed"), nil
		},
	})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash", out: "hi"}})

	resp, err := got[0].Run(context.Background(), tools.ToolCall{Name: "bash"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !resp.IsError || resp.Content != "not allowed" {
		t.Errorf("resp = %+v", resp)
	}
}

// Higher priority sits outermost, so it observes what lower ones produced.
func TestApplyToolsInterceptorNesting(t *testing.T) {
	var order []string
	mk := func(id extension.ID, prio int) *interExt {
		return &interExt{baseExt: baseExt{id: id, priority: prio}, intercep: func(ctx context.Context, call extension.ToolCall, next extension.ToolFunc) (extension.ToolResponse, error) {
			order = append(order, "enter "+string(id))
			resp, err := next(ctx, call)
			order = append(order, "exit "+string(id))
			return resp, err
		}}
	}
	mgr := managerWith(t, mk("audit.inner", 0), mk("audit.outer", 100))
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}})
	if _, err := got[0].Run(context.Background(), tools.ToolCall{Name: "bash"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{"enter audit.outer", "enter audit.inner", "exit audit.inner", "exit audit.outer"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// A panicking filter must not take the agent down, and the safe failure is to
// keep the unfiltered set.
func TestApplyToolsPanickingFilterIsContained(t *testing.T) {
	mgr := managerWith(t, &filterExt{baseExt: baseExt{id: "policy.bad"}, filter: func([]extension.Tool) []extension.Tool {
		panic("boom")
	}})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}})
	if len(got) != 1 || got[0].Info().Name != "bash" {
		t.Fatalf("got %v", toolNames(got))
	}
}

func TestApplyToolsPanickingInterceptorIsContained(t *testing.T) {
	mgr := managerWith(t, &interExt{baseExt: baseExt{id: "audit.bad"}, intercep: func(context.Context, extension.ToolCall, extension.ToolFunc) (extension.ToolResponse, error) {
		panic("boom")
	}})
	got := ApplyTools(mgr, []tools.BaseTool{fakeCoreTool{name: "bash"}})

	resp, err := got[0].Run(context.Background(), tools.ToolCall{Name: "bash"})
	if err != nil {
		t.Fatalf("panic became an error: %v", err)
	}
	if !resp.IsError {
		t.Errorf("resp = %+v, want an error response", resp)
	}
}

// A Go error from an extension tool must reach the caller as an error, not be
// swallowed into a response.
func TestApplyToolsPropagatesToolError(t *testing.T) {
	sentinel := errors.New("upstream down")
	mgr := managerWith(t, &provExt{baseExt: baseExt{id: "tools.acme"}, tools: []extension.Tool{fakeExtTool{name: "acme_jira", err: sentinel}}})
	got := ApplyTools(mgr, nil)

	if _, err := got[0].Run(context.Background(), tools.ToolCall{Name: "acme_jira"}); !errors.Is(err, sentinel) {
		t.Errorf("err = %v", err)
	}
}

// Round-tripping a core tool through the adapters must return the original
// value, otherwise every tool-set rebuild would add another wrapper.
func TestAdapterRoundTripDoesNotStack(t *testing.T) {
	core := fakeCoreTool{name: "bash"}
	if back := asCoreTool(asExtensionTool(core)); back != tools.BaseTool(core) {
		t.Errorf("core tool was wrapped: %#v", back)
	}
	ext := fakeExtTool{name: "acme_jira"}
	if back := asExtensionTool(asCoreTool(ext)); back != extension.Tool(ext) {
		t.Errorf("extension tool was wrapped: %#v", back)
	}
}
