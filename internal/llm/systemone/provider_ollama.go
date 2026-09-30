package systemone

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	minOllamaMajor, minOllamaMinor = 0, 35
	defaultOllamaKeepAlive         = "30m"
	defaultOllamaNumCtx            = 2048
	budgetTTL                      = 5 * time.Minute
)

type ollamaProvider struct {
	client *Client

	mu      sync.Mutex
	budgets map[string]budgetEntry
}

type budgetEntry struct {
	tokens int
	at     time.Time
}

func newOllamaProvider(o Options) *ollamaProvider {
	if o.KeepAlive == "" {
		o.KeepAlive = defaultOllamaKeepAlive
	}
	return &ollamaProvider{client: NewClient(o), budgets: map[string]budgetEntry{}}
}

func (p *ollamaProvider) Kind() ProviderKind { return KindOllama }
func (p *ollamaProvider) Client() *Client    { return p.client }

type ollamaTag struct {
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
	Details      struct {
		ContextLength int `json:"context_length"`
	} `json:"details"`
}

func (t ollamaTag) id() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Model
}

func (t ollamaTag) hasCap(c string) bool {
	for _, x := range t.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

func (p *ollamaProvider) tags(ctx context.Context) ([]ollamaTag, error) {
	data, status, err := p.client.get(ctx, "/api/tags")
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, p.client.statusError(status, data)
	}
	var v struct {
		Models []ollamaTag `json:"models"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, newAPIError(status, ErrMalformedResponse, "invalid /api/tags JSON")
	}
	return v.Models, nil
}

func isCloud(name string) bool {
	return strings.HasSuffix(name, ":cloud") || strings.HasSuffix(name, "-cloud")
}

func (p *ollamaProvider) ListDecisionModels(ctx context.Context, showAll bool) ([]DecisionModel, ListStatus, error) {
	tags, err := p.tags(ctx)
	if err != nil {
		return nil, ListUnsupported, err
	}
	anyCaps := false
	for _, t := range tags {
		if len(t.Capabilities) > 0 {
			anyCaps = true
			break
		}
	}
	out := []DecisionModel{}
	for _, t := range tags {
		if isCloud(t.id()) {
			continue
		}
		if !showAll && anyCaps && !t.hasCap("decision") {
			continue
		}
		out = append(out, DecisionModel{ID: t.id(), Name: t.id(), ContextWindow: t.Details.ContextLength, Capabilities: t.Capabilities})
	}
	if !anyCaps && len(tags) > 0 {
		// Old Ollama: no capability metadata; the caller should show an upgrade hint.
		return out, ListUnsupported, nil
	}
	return out, ListFiltered, nil
}

// parseVersion parses "0.35.0" / "v0.35.0-rc1" into major, minor.
func parseVersion(v string) (int, int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return maj, min, true
}

func sameModel(a, b string) bool {
	if a == b {
		return true
	}
	return strings.TrimSuffix(a, ":latest") == strings.TrimSuffix(b, ":latest")
}

func (p *ollamaProvider) Health(ctx context.Context, model string) HealthReport {
	r := &HealthReport{Kind: KindOllama, Model: model, Authorized: true}

	data, status, err := p.client.get(ctx, "/api/version")
	if err != nil {
		r.Problems = append(r.Problems, fmt.Sprintf("Ollama is not reachable at %s", p.client.BaseURL()))
		return finishReport(r)
	}
	r.Reachable = true
	if status == 401 || status == 403 {
		r.Authorized = false
		r.Problems = append(r.Problems, "Ollama rejected the credentials")
		return finishReport(r)
	}
	var ver struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(data, &ver)
	r.Version = ver.Version
	maj, min, ok := parseVersion(ver.Version)
	r.VersionOK = ok && (maj > minOllamaMajor || (maj == minOllamaMajor && min >= minOllamaMinor))
	if !r.VersionOK {
		r.Problems = append(r.Problems, fmt.Sprintf("Upgrade Ollama to ≥ 0.35 (found %q)", ver.Version))
		return finishReport(r)
	}

	if model == "" {
		r.Problems = append(r.Problems, "No router model selected")
		return finishReport(r)
	}
	tags, err := p.tags(ctx)
	if err != nil {
		r.Problems = append(r.Problems, "Cannot list Ollama models: "+err.Error())
		return finishReport(r)
	}
	anyCaps := false
	for _, t := range tags {
		if len(t.Capabilities) > 0 {
			anyCaps = true
		}
		if sameModel(t.id(), model) {
			r.ModelFound = true
			r.IsDecision = t.hasCap("decision")
		}
	}
	switch {
	case !r.ModelFound:
		r.Problems = append(r.Problems, fmt.Sprintf("Model %q is not installed (try: ollama pull %s)", model, model))
		return finishReport(r)
	case !anyCaps:
		r.Problems = append(r.Problems, "Ollama does not report model capabilities; upgrade to ≥ 0.35")
		return finishReport(r)
	case !r.IsDecision:
		r.Problems = append(r.Problems, fmt.Sprintf("Model %q does not have the \"decision\" capability", model))
		return finishReport(r)
	}

	probeStart := time.Now()
	if _, err := p.client.Decide(ctx, probeRequest(model, p.client.opts.KeepAlive)); err != nil {
		r.Problems = append(r.Problems, "Probe failed: "+err.Error())
		return finishReport(r)
	}
	r.LatencyMs = time.Since(probeStart).Milliseconds()
	return finishReport(r)
}

// ContextBudget reads num_ctx from /api/show parameters, falling back to
// model_info *.context_length, then to 2048.
func (p *ollamaProvider) ContextBudget(ctx context.Context, model string) int {
	p.mu.Lock()
	if e, ok := p.budgets[model]; ok && time.Since(e.at) < budgetTTL {
		p.mu.Unlock()
		return e.tokens
	}
	p.mu.Unlock()

	data, status, err := p.client.postJSON(ctx, "/api/show", map[string]string{"model": model})
	if err != nil || status < 200 || status > 299 {
		return defaultOllamaNumCtx
	}
	var v struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if json.Unmarshal(data, &v) != nil {
		return defaultOllamaNumCtx
	}
	n := 0
	for _, line := range strings.Split(v.Parameters, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "num_ctx" {
			n, _ = strconv.Atoi(f[1])
		}
	}
	if n <= 0 {
		for k, val := range v.ModelInfo {
			if strings.HasSuffix(k, ".context_length") {
				if f, ok := val.(float64); ok && f > 0 {
					n = int(f)
				}
			}
		}
	}
	if n <= 0 {
		n = defaultOllamaNumCtx
	}
	p.mu.Lock()
	p.budgets[model] = budgetEntry{tokens: n, at: time.Now()}
	p.mu.Unlock()
	return n
}
