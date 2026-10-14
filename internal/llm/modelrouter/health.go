package modelrouter

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
)

// HealthTTL is the lifetime of a cached router health report.
const HealthTTL = 60 * time.Second

const warmupTimeout = 30 * time.Second

var sharedHealth = systemone.NewHealthCache(HealthTTL)

// SharedHealth returns the process-wide router health cache.
func SharedHealth() *systemone.HealthCache { return sharedHealth }

// ProviderFor builds the decision provider described by the decision model,
// bounded by dec.EffectiveTimeout().
func ProviderFor(dec config.DecisionModelConfig) (systemone.DecisionProvider, error) {
	return ProviderWithTimeout(dec.Router, dec.EffectiveTimeout())
}

// ProviderWithTimeout is ProviderFor with an explicit per-call bound (warm-up
// needs a longer one: the first call loads the model).
func ProviderWithTimeout(r config.DecisionRouterConfig, timeout time.Duration) (systemone.DecisionProvider, error) {
	return systemone.NewProvider(systemone.ProviderKind(r.EffectiveProvider()), systemone.Options{
		BaseURL:   r.EffectiveBaseURL(),
		APIKey:    r.EffectiveAPIKey(),
		Headers:   r.Headers,
		Timeout:   timeout,
		KeepAlive: r.KeepAlive,
	})
}

// RouterHealth returns the (cached) health report for the decision model.
func RouterHealth(ctx context.Context, dec config.DecisionModelConfig) (systemone.HealthReport, error) {
	p, err := ProviderFor(dec)
	if err != nil {
		return systemone.HealthReport{}, err
	}
	return sharedHealth.Get(ctx, p, dec.Router.Model), nil
}

var warmupOnce sync.Once

// warmupSections are the config sections whose change can alter the decision
// model or whether any consumer of it is on: the decision block itself, the
// auto mode policy, the agents (persona selector useDecisionModel) and the
// remembrances block (context relevance filter flags).
var warmupSections = map[string]bool{
	"decisionModel": true,
	"modelAutoMode": true,
	"agents":        true,
	"remembrances":  true,
}

// StartWarmupOnReload subscribes to the config bus (once per process) and, on
// every change of a section in warmupSections, drops the cached health reports
// and warms the decision model up in the background when it is configured and
// at least one consumer is enabled.
//
// The listener is synchronous (config.Bus.SubscribeFunc): the configuration is
// read on the goroutine that published the change, right after it wrote it,
// and the background warm-up only gets copies. A channel subscriber reading
// config.Get() later on its own goroutine raced with every later writer
// (config handlers, Reload, tests swapping the configuration).
func StartWarmupOnReload() {
	warmupOnce.Do(func() {
		config.Bus.SubscribeFunc(func(ev config.ConfigChangeEvent) {
			if !warmupSections[ev.Section] && ev.Event != config.EventConfigReloaded {
				return
			}
			warmupFromConfig()
		})
	})
}

func warmupFromConfig() { startWarmup(config.Get()) }

// startWarmup drops the cached health reports and, when the decision model is
// configured and at least one consumer is on, warms it up in the background.
// The returned channel closes when the warm-up ends; it is nil when none ran.
//
// Everything read from c (and from the global configuration, which
// EffectiveBaseURL consults for the Ollama provider) is resolved before the
// goroutine starts, so the warm-up never touches configuration that a later
// change may be rewriting.
func startWarmup(c *config.Config) <-chan struct{} {
	sharedHealth.Invalidate()
	if c == nil || c.DecisionModel.Router.Model == "" || !c.AnyDecisionConsumerEnabled() {
		return nil
	}
	router := c.DecisionModel.Router
	router.Headers = maps.Clone(router.Headers)
	model := router.Model
	p, err := ProviderWithTimeout(router, warmupTimeout)
	if err != nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), warmupTimeout)
		defer cancel()
		_ = systemone.Warmup(ctx, p, model)
	}()
	return done
}
