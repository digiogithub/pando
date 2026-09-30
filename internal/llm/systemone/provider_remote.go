package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultRemoteContextBudget = 32768

// remoteHints are the substrings that mark a model id as a decision model on
// mixed catalogues (OpenRouter, LiteLLM, ...).
var remoteHints = []string{"jev", "systemone", "decision", "tev", "nimble", "kev"}

type remoteProvider struct {
	kind   ProviderKind
	client *Client
}

func newRemoteProvider(kind ProviderKind, o Options) *remoteProvider {
	return &remoteProvider{kind: kind, client: NewClient(o)}
}

func (p *remoteProvider) Kind() ProviderKind { return p.kind }
func (p *remoteProvider) Client() *Client    { return p.client }

func matchesHint(id string) bool {
	l := strings.ToLower(id)
	for _, h := range remoteHints {
		if strings.Contains(l, h) {
			return true
		}
	}
	return false
}

// parseModelList accepts {"models":[...]} (strings or objects with id/name/model)
// and {"data":[{"id":...}]}.
func parseModelList(data []byte) ([]DecisionModel, error) {
	var v struct {
		Models []json.RawMessage `json:"models"`
		Data   []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	raw := v.Models
	if len(raw) == 0 {
		raw = v.Data
	}
	out := []DecisionModel{}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			if s != "" {
				out = append(out, DecisionModel{ID: s, Name: s})
			}
			continue
		}
		var o struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		}
		if json.Unmarshal(r, &o) != nil {
			continue
		}
		id := firstNonEmpty(o.ID, o.Model, o.Name)
		if id == "" {
			continue
		}
		out = append(out, DecisionModel{ID: id, Name: firstNonEmpty(o.Name, id), ContextWindow: o.ContextLength})
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (p *remoteProvider) ListDecisionModels(ctx context.Context, showAll bool) ([]DecisionModel, ListStatus, error) {
	data, status, err := p.client.get(ctx, "/v1/models")
	if err != nil {
		return nil, ListUnsupported, err
	}
	switch {
	case status == 404 || status == 405 || status == 501:
		return nil, ListUnsupported, nil
	case status < 200 || status > 299:
		return nil, ListUnsupported, p.client.statusError(status, data)
	}
	all, err := parseModelList(data)
	if err != nil {
		return nil, ListUnsupported, newAPIError(status, ErrMalformedResponse, "invalid /v1/models JSON")
	}
	if p.kind == KindTypeSafe {
		return all, ListFiltered, nil
	}
	if showAll {
		return all, ListUnfiltered, nil
	}
	matched := []DecisionModel{}
	for _, m := range all {
		if matchesHint(m.ID) {
			matched = append(matched, m)
		}
	}
	if len(matched) == 0 {
		return all, ListUnfiltered, nil
	}
	return matched, ListUnfiltered, nil
}

func (p *remoteProvider) Health(ctx context.Context, model string) HealthReport {
	r := &HealthReport{Kind: p.kind, Model: model, Remote: true, Authorized: true, VersionOK: true}

	listed := false
	data, status, err := p.client.get(ctx, "/v1/models")
	switch {
	case err != nil:
		if errors.Is(err, ErrTimeout) {
			r.Problems = append(r.Problems, "Timed out reaching "+p.client.BaseURL())
		} else {
			r.Problems = append(r.Problems, "Cannot reach "+p.client.BaseURL())
		}
		return finishReport(r)
	case status == 401 || status == 403:
		r.Reachable = true
		r.Authorized = false
		r.Problems = append(r.Problems, "The provider rejected the API key")
		return finishReport(r)
	case status >= 200 && status <= 299:
		r.Reachable = true
		if models, perr := parseModelList(data); perr == nil && len(models) > 0 {
			listed = true
			for _, m := range models {
				if m.ID == model {
					r.ModelFound = true
				}
			}
		}
	default:
		// 404 and friends: no catalogue; fall through to the probe.
		r.Reachable = true
	}

	if model == "" {
		r.Problems = append(r.Problems, "No router model selected")
		return finishReport(r)
	}

	probeStart := time.Now()
	_, err = p.client.Decide(ctx, probeRequest(model, ""))
	r.LatencyMs = time.Since(probeStart).Milliseconds()
	switch {
	case err == nil:
		r.ModelFound = true
		r.IsDecision = true
	case errors.Is(err, ErrUnauthorized):
		r.Authorized = false
		r.Problems = append(r.Problems, "The provider rejected the API key")
	case errors.Is(err, ErrModelNotFound):
		r.ModelFound = false
		r.Problems = append(r.Problems, fmt.Sprintf("Model %q was not found", model))
	default:
		if listed && !r.ModelFound {
			r.Problems = append(r.Problems, fmt.Sprintf("Model %q is not in the provider catalogue", model))
		}
		r.Problems = append(r.Problems, "Probe failed: "+err.Error())
	}
	return finishReport(r)
}

func (p *remoteProvider) ContextBudget(_ context.Context, _ string) int {
	if p.client.opts.ContextBudget > 0 {
		return p.client.opts.ContextBudget
	}
	return defaultRemoteContextBudget
}
