package modelrouter

import (
	"context"
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

// ProviderFor builds the decision provider described by the router block.
func ProviderFor(r config.DecisionRouterConfig, timeout time.Duration) (systemone.DecisionProvider, error) {
	return systemone.NewProvider(systemone.ProviderKind(r.EffectiveProvider()), systemone.Options{
		BaseURL:   r.EffectiveBaseURL(),
		APIKey:    r.EffectiveAPIKey(),
		Headers:   r.Headers,
		Timeout:   timeout,
		KeepAlive: r.KeepAlive,
	})
}

// RouterHealth returns the (cached) health report for the configured router.
func RouterHealth(ctx context.Context, cfg config.ModelAutoModeConfig) (systemone.HealthReport, error) {
	p, err := ProviderFor(cfg.Router, cfg.EffectiveTimeout())
	if err != nil {
		return systemone.HealthReport{}, err
	}
	return sharedHealth.Get(ctx, p, cfg.Router.Model), nil
}

var warmupOnce sync.Once

// StartWarmupOnReload subscribes to the config bus (once per process) and, on
// every "modelAutoMode" change, drops the cached health reports and warms the
// router model up in the background when auto mode is enabled.
func StartWarmupOnReload() {
	warmupOnce.Do(func() {
		ch := make(chan config.ConfigChangeEvent, 16)
		config.Bus.Subscribe(ch)
		go func() {
			for ev := range ch {
				if ev.Section != "modelAutoMode" {
					continue
				}
				warmupFromConfig()
			}
		}()
	})
}

func warmupFromConfig() {
	sharedHealth.Invalidate()
	c := config.Get()
	if c == nil || !c.ModelAutoMode.Enabled || c.ModelAutoMode.Router.Model == "" {
		return
	}
	cfg := c.ModelAutoMode
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), warmupTimeout)
		defer cancel()
		p, err := ProviderFor(cfg.Router, warmupTimeout)
		if err != nil {
			return
		}
		_ = systemone.Warmup(ctx, p, cfg.Router.Model)
	}()
}
