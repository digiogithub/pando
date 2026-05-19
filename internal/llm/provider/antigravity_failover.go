package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	antigravityRateLimitCooldown = 2 * time.Minute
	antigravityCapacityCooldown  = 30 * time.Second
)

type antigravityHTTPStatusError interface {
	StatusCode() int
}

type antigravityRequestContext struct {
	Account config.ProviderAccount
	Pool    string
	Model   models.Model
}

func classifyAntigravityFailure(err error, pool string) AntigravityFailureAction {
	if err == nil {
		return AntigravityFailureAction{Kind: AntigravityFailureUnknown}
	}

	statusCode := antigravityErrorStatusCode(err)
	msg := strings.ToLower(strings.TrimSpace(err.Error()))

	switch {
	case strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "revoked") || strings.Contains(msg, "token has been expired or revoked"):
		return AntigravityFailureAction{
			Kind:           AntigravityFailureInvalidGrant,
			DisableAccount: true,
			RequireReauth:  true,
		}
	case strings.Contains(msg, "verification required") || strings.Contains(msg, "verify it is you") || strings.Contains(msg, "soft blocked") || strings.Contains(msg, "temporarily blocked"):
		return AntigravityFailureAction{
			Kind:           AntigravityFailureVerification,
			DisableAccount: true,
		}
	case statusCode == http.StatusForbidden && (strings.Contains(msg, "project") || strings.Contains(msg, "permission") || strings.Contains(msg, "access denied") || strings.Contains(msg, "not authorized")):
		return AntigravityFailureAction{
			Kind:             AntigravityFailureProjectAccess,
			DisableGeminiCLI: pool == AntigravityPoolGeminiCLI,
			ExhaustPool:      pool == AntigravityPoolGeminiCLI,
		}
	case strings.Contains(msg, "projectid") || strings.Contains(msg, "project id") || strings.Contains(msg, "missing project") || strings.Contains(msg, "no antigravity projects available"):
		return AntigravityFailureAction{
			Kind:             AntigravityFailureProjectAccess,
			DisableGeminiCLI: true,
			ExhaustPool:      pool == AntigravityPoolGeminiCLI,
		}
	case statusCode == http.StatusTooManyRequests || strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests"):
		return AntigravityFailureAction{
			Kind:         AntigravityFailureRateLimited,
			Cooldown:     antigravityRateLimitCooldown,
			ExhaustPool:  false,
			ExhaustModel: false,
		}
	case statusCode == http.StatusServiceUnavailable || statusCode == http.StatusBadGateway || statusCode == http.StatusGatewayTimeout || strings.Contains(msg, "capacity") || strings.Contains(msg, "overloaded") || strings.Contains(msg, "unavailable"):
		return AntigravityFailureAction{
			Kind:        AntigravityFailureCapacity,
			Cooldown:    antigravityCapacityCooldown,
			ExhaustPool: false,
		}
	case strings.Contains(msg, "quota exhausted") || strings.Contains(msg, "quota exceeded") || strings.Contains(msg, "resource exhausted") || strings.Contains(msg, "billing quota"):
		return AntigravityFailureAction{
			Kind:         AntigravityFailureQuotaExhausted,
			ExhaustPool:  true,
			ExhaustModel: true,
		}
	default:
		return AntigravityFailureAction{Kind: AntigravityFailureUnknown}
	}
}

func applyAntigravityFailure(account config.ProviderAccount, pool string, model models.Model, err error) error {
	action := classifyAntigravityFailure(err, pool)
	if action.Kind == AntigravityFailureUnknown {
		return err
	}

	defaultAntigravityScheduler.ApplyFailureAction(account.ID, pool, model, action)

	if action.DisableGeminiCLI || action.DisableAccount || action.RequireReauth {
		persisted, ok := config.GetProviderAccount(account.ID)
		if ok {
			updated := *persisted
			changed := false
			if action.DisableGeminiCLI && strings.TrimSpace(updated.ProjectID) != "" {
				updated.ProjectID = ""
				changed = true
			}
			if action.DisableAccount && !updated.Disabled {
				updated.Disabled = true
				changed = true
			}
			if action.RequireReauth {
				if !updated.ReauthRequired {
					updated.ReauthRequired = true
					changed = true
				}
			}
			if changed {
				if updateErr := config.UpdateProviderAccount(account.ID, updated); updateErr != nil {
					return fmt.Errorf("%w (state update failed: %v)", err, updateErr)
				}
			}
		}
	}

	return err
}

func antigravityErrorStatusCode(err error) int {
	var statusErr antigravityHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode()
	}
	return 0
}
