package page

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	oauthantigravity "github.com/digiogithub/pando/internal/oauth/antigravity"
)

// antigravityLoginCommand starts the Antigravity Google OAuth login flow.
// It spins up a temporary local HTTP server to receive Google's redirect,
// opens the browser for the user to authenticate, and waits for the callback.
func antigravityLoginCommand(accountID string) tea.Cmd {
	return tea.Batch(
		// Immediate feedback while the browser flow is in progress.
		func() tea.Msg {
			return providerAccountActionMsg{info: fmt.Sprintf("Opening browser for Google login (%s)... Complete authentication in your browser.", accountID)}
		},
		// Blocking OAuth flow.
		func() tea.Msg {
			acc, ok := config.GetProviderAccount(accountID)
			if !ok {
				return providerAccountActionMsg{err: fmt.Errorf("provider account %q not found", accountID)}
			}
			if acc.Type != models.ProviderAntigravity {
				return providerAccountActionMsg{err: fmt.Errorf("provider account %q is not an Antigravity account", accountID)}
			}

			// Find a free port for the temporary callback server.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to find free port: %w", err)}
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()

			redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

			// Create OAuth session with PKCE.
			session, err := oauthantigravity.NewSession(redirectURI)
			if err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to create OAuth session: %w", err)}
			}
			authURL, err := session.AuthURL()
			if err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to build auth URL: %w", err)}
			}

			// Persist OAuth session state in the provider account.
			updated := *acc
			updated.UseOAuth = true
			updated.OAuthState = session.State
			updated.OAuthCodeVerifier = session.CodeVerifier
			updated.OAuthRedirectURI = redirectURI
			if err := config.UpdateProviderAccount(accountID, updated); err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to save OAuth session: %w", err)}
			}

			// Channel to receive the authorization code from the callback.
			type callbackResult struct {
				code string
				err  error
			}
			resultCh := make(chan callbackResult, 1)

			mux := http.NewServeMux()
			mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
				if errParam := r.URL.Query().Get("error"); errParam != "" {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2><p>%s</p><p>You can close this window.</p></body></html>", html.EscapeString(errParam))
					resultCh <- callbackResult{err: fmt.Errorf("OAuth error: %s", errParam)}
					return
				}
				code := r.URL.Query().Get("code")
				state := r.URL.Query().Get("state")
				if code == "" || state == "" {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					fmt.Fprint(w, "<html><body><h2>Authentication failed</h2><p>Missing code or state.</p><p>You can close this window.</p></body></html>")
					resultCh <- callbackResult{err: fmt.Errorf("missing code or state in callback")}
					return
				}
				if state != session.State {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					fmt.Fprint(w, "<html><body><h2>Authentication failed</h2><p>Invalid state parameter.</p><p>You can close this window.</p></body></html>")
					resultCh <- callbackResult{err: fmt.Errorf("invalid OAuth state")}
					return
				}

				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You can close this window and return to Pando.</p></body></html>")
				resultCh <- callbackResult{code: code}
			})

			srv := &http.Server{
				Addr:    fmt.Sprintf("127.0.0.1:%d", port),
				Handler: mux,
			}

			go func() { _ = srv.ListenAndServe() }()
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()

			// Open browser.
			if err := auth.OpenBrowser(authURL); err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to open browser: %w\nManually open: %s", err, authURL)}
			}

			// Wait for callback (5 minute timeout).
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			var code string
			select {
			case res := <-resultCh:
				if res.err != nil {
					return providerAccountActionMsg{err: res.err}
				}
				code = res.code
			case <-ctx.Done():
				return providerAccountActionMsg{err: fmt.Errorf("login timed out — no response from Google within 5 minutes")}
			}

			// Exchange code for tokens.
			token, err := oauthantigravity.ExchangeCode(nil, code, session.CodeVerifier, redirectURI)
			if err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to exchange code: %w", err)}
			}

			// Re-read account in case it changed during the flow.
			acc, ok = config.GetProviderAccount(accountID)
			if !ok {
				return providerAccountActionMsg{err: fmt.Errorf("provider account %q disappeared during login", accountID)}
			}
			if strings.TrimSpace(token.RefreshToken) == "" && strings.TrimSpace(acc.OAuthRefreshToken) != "" {
				token.RefreshToken = acc.OAuthRefreshToken
			}

			// Hydrate account: fetch user info and project ID.
			hydrated, err := hydrateAntigravityAccount(*acc, *token)
			if err != nil {
				return providerAccountActionMsg{err: err}
			}
			if err := config.UpdateProviderAccount(accountID, hydrated); err != nil {
				return providerAccountActionMsg{err: fmt.Errorf("failed to save account: %w", err)}
			}

			return providerAccountActionMsg{info: fmt.Sprintf("Antigravity login successful for %s (%s)", accountID, hydrated.Email)}
		},
	)
}

// antigravityRefreshCommand refreshes the OAuth access token for an Antigravity account.
func antigravityRefreshCommand(accountID string) tea.Cmd {
	return func() tea.Msg {
		acc, ok := config.GetProviderAccount(accountID)
		if !ok {
			return providerAccountActionMsg{err: fmt.Errorf("provider account %q not found", accountID)}
		}
		if acc.Type != models.ProviderAntigravity {
			return providerAccountActionMsg{err: fmt.Errorf("provider account %q is not an Antigravity account", accountID)}
		}
		if strings.TrimSpace(acc.OAuthRefreshToken) == "" {
			return providerAccountActionMsg{err: fmt.Errorf("provider account %q has no refresh token — please login first", accountID)}
		}

		token, err := oauthantigravity.RefreshToken(nil, acc.OAuthRefreshToken)
		if err != nil {
			return providerAccountActionMsg{err: fmt.Errorf("failed to refresh token: %w", err)}
		}
		if strings.TrimSpace(token.RefreshToken) == "" {
			token.RefreshToken = acc.OAuthRefreshToken
		}

		hydrated, err := hydrateAntigravityAccount(*acc, *token)
		if err != nil {
			return providerAccountActionMsg{err: err}
		}
		if err := config.UpdateProviderAccount(accountID, hydrated); err != nil {
			return providerAccountActionMsg{err: fmt.Errorf("failed to save account: %w", err)}
		}

		return providerAccountActionMsg{info: fmt.Sprintf("Token refreshed for %s (%s)", accountID, hydrated.Email)}
	}
}

// hydrateAntigravityAccount fetches user info and project ID and updates the account fields.
func hydrateAntigravityAccount(account config.ProviderAccount, token oauthantigravity.Token) (config.ProviderAccount, error) {
	userInfo, err := oauthantigravity.FetchUserInfo(nil, token.AccessToken)
	if err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to fetch Google account email: %w", err)
	}
	projectID, err := oauthantigravity.FetchProjectID(nil, token.AccessToken)
	if err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to resolve Antigravity project ID: %w", err)
	}

	updated := account
	updated.Type = models.ProviderAntigravity
	updated.UseOAuth = true
	updated.OAuthAccessToken = token.AccessToken
	updated.OAuthRefreshToken = token.RefreshToken
	updated.OAuthExpiry = token.Expiry
	updated.Email = userInfo.Email
	updated.ProjectID = projectID
	updated.OAuthState = ""
	updated.OAuthCodeVerifier = ""
	updated.ReauthRequired = false
	return updated, nil
}
