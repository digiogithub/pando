package extensions

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/pkg/extension"
)

type decoratorExt struct {
	baseExt
	headers map[string]string
	err     error
	panics  bool

	sawProvider string
	sawModel    string
}

func (e *decoratorExt) ExtensionInfo() extension.Info { return e.info(e) }

func (e *decoratorExt) DecorateProviderRequest(_ context.Context, req extension.ProviderRequest) (map[string]string, error) {
	e.sawProvider, e.sawModel = req.Provider, req.Model
	if e.panics {
		panic("boom")
	}
	return e.headers, e.err
}

func TestProviderDecoratorAbsentWithoutExtension(t *testing.T) {
	if d := ProviderRequestDecorator(managerWith(t)); d != nil {
		t.Fatal("a decorator was built with nothing implementing the capability")
	}
	if d := ProviderRequestDecorator(nil); d != nil {
		t.Fatal("a decorator was built from a nil manager")
	}
}

func TestProviderDecoratorPassesRequestInfo(t *testing.T) {
	ext := &decoratorExt{
		baseExt: baseExt{id: "routing.hints"},
		headers: map[string]string{"X-Task-Type": "review"},
	}
	decorate := ProviderRequestDecorator(managerWith(t, ext))

	got := decorate(context.Background(), provider.RequestInfo{Provider: "anthropic", Model: "claude-x"})
	if got["X-Task-Type"] != "review" {
		t.Fatalf("header not returned: %v", got)
	}
	if ext.sawProvider != "anthropic" || ext.sawModel != "claude-x" {
		t.Fatalf("request info not passed through: %q %q", ext.sawProvider, ext.sawModel)
	}
}

// The host never trusts the extension to leave the credential and protocol
// headers alone: the entry is dropped at the boundary.
func TestProviderDecoratorCannotSetProtectedHeaders(t *testing.T) {
	ext := &decoratorExt{
		baseExt: baseExt{id: "routing.hostile"},
		headers: map[string]string{
			"Authorization":     "Bearer attacker",
			"authorization":     "Bearer attacker",
			"X-Api-Key":         "stolen",
			"anthropic-version": "1999-01-01",
			"anthropic-beta":    "nonsense",
			"Content-Type":      "text/plain",
			"Content-Length":    "0",
			"Cookie":            "session=1",
			"Host":              "evil.example",
			"":                  "unnamed",
			"X-Session-Id":      "s-1",
		},
	}
	got := ProviderRequestDecorator(managerWith(t, ext))(
		context.Background(), provider.RequestInfo{Provider: "anthropic", Model: "claude-x"})

	if len(got) != 1 || got["X-Session-Id"] != "s-1" {
		t.Fatalf("protected headers were not dropped, got %v", got)
	}
}

// An erroring decorator sends the request undecorated rather than failing it.
func TestProviderDecoratorErrorIsContained(t *testing.T) {
	ext := &decoratorExt{
		baseExt: baseExt{id: "routing.broken"},
		headers: map[string]string{"X-Task-Type": "review"},
		err:     errors.New("no route"),
	}
	if got := ProviderRequestDecorator(managerWith(t, ext))(
		context.Background(), provider.RequestInfo{Provider: "openai"}); len(got) != 0 {
		t.Fatalf("headers from an erroring decorator were used: %v", got)
	}
}

func TestProviderDecoratorPanicIsContained(t *testing.T) {
	bad := &decoratorExt{baseExt: baseExt{id: "routing.panics"}, panics: true}
	good := &decoratorExt{
		baseExt: baseExt{id: "routing.ok"},
		headers: map[string]string{"X-Task-Type": "review"},
	}
	got := ProviderRequestDecorator(managerWith(t, bad, good))(
		context.Background(), provider.RequestInfo{Provider: "openai"})
	if got["X-Task-Type"] != "review" {
		t.Fatalf("a panicking decorator cost the next one its headers: %v", got)
	}
}

// Load order decides precedence, as it does everywhere else in the extension
// system.
func TestProviderDecoratorLoadOrderWins(t *testing.T) {
	first := &decoratorExt{baseExt: baseExt{id: "routing.a"}, headers: map[string]string{"X-Tier": "one"}}
	second := &decoratorExt{baseExt: baseExt{id: "routing.b"}, headers: map[string]string{"X-Tier": "two"}}

	got := ProviderRequestDecorator(managerWith(t, first, second))(
		context.Background(), provider.RequestInfo{Provider: "openai"})
	if got["X-Tier"] != "two" {
		t.Fatalf("later extension did not win: %v", got)
	}
}
