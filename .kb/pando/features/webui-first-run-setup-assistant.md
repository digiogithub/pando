---
created_at: 2026-09-28T21:24:51.249086721Z
updated_at: 2026-09-28T21:24:51.249086721Z
tags:
    - feature
    - webui
    - setup
    - onboarding
    - copilot
    - remembrances
    - ollama
---
# Feature: WebUI/Desktop first-run setup assistant

Date: 2026-09-28. Surfaces: WebUI + Wails desktop only (TUI unchanged, by user decision).
Related: [[copilot_auto_login_on_add_provider]], [[no-hardcoded-provider-and-agent-model-defaults]], [[init-config-fails-to-start-without-providers]], [[change_remove_oauth_providers]].

## What it does
A modal assistant (`SetupWizard`) that opens by itself when Pando starts in a directory with no
project config and nothing is configured yet, or on demand from the config banner ("Setup assistant").
Steps: Scope → Provider → Models → Remembrances → Done. "Cancel assistant" (or Esc/X) at any step
closes it and leaves the original screens (ConfigInitBanner, Settings) as they were; cancel lasts for
the page session only.

1. **Scope**: global settings (default, recommended) or this directory only. Disabled "project" in $HOME.
2. **Provider**: shows existing accounts ("Use these accounts" / "Add another provider") or a picker
   (Copilot, Anthropic, OpenAI, Gemini, OpenRouter, Groq, xAI, Ollama, OpenAI-compatible) with per-provider
   guidance + "Get an API key" link. Anthropic is API-key only (OAuth providers were removed the same day).
   **Copilot**: creates the account (reuses an existing copilot account), checks
   `/api/v1/auth/providers/copilot/status` (editor tokens skip login), otherwise starts the device flow
   and shows the user code big and centred with Copy code / Open GitHub, polls status every `interval`
   seconds until authenticated, then advances automatically. Expiry handled.
3. **Models**: main model (coder) + fast/cheap secondary model (all other agents: summarizer, task,
   title, cli-assist, persona-selector, context-enricher). Suggestions from
   `GET /api/v1/setup/suggested-models?provider=&refresh=1` (refreshes dynamic models first so a
   just-authenticated Copilot has its list). Pickers reuse `ModelCombobox`.
4. **Remembrances**: detects Ollama (binary + `/api/version`). Not installed → install options per OS:
   download app (recommended on macOS/Windows), official script `curl -fsSL https://ollama.com/install.sh | sh`
   (recommended on Linux), Homebrew / winget, Docker last. "Run it for me" only when runnable
   (Linux needs root or passwordless sudo; brew/winget on PATH) and after an inline confirmation;
   output streamed as a job. Installed but stopped → "Start Ollama" (`open -a Ollama` on macOS app,
   else `ollama serve` detached via procgroup). Running → pull rows for the document model
   (`nomic-embed-text`) and the code model (`hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M`, editable),
   each with its `ollama pull …` command (copyable) and a Download button with a progress bar
   (Ollama HTTP `/api/pull` stream). "Enable Remembrances" saves provider ollama + both models.
5. **Done**: summary; Finish writes the completed marker.

## Auto-open rule (`config.GetSetupStatus`)
`needed = !hasLocalConfig && !completed && (explicitProviderAccounts == 0 || !coderModelValid)`.
- Auto-detected local runtimes (Ollama/llama.cpp answering on the default port surface as bare
  accounts with no base URL/key) do NOT count as explicit accounts — otherwise a fresh machine with
  Ollama running would never see the assistant (verified: Load auto-detects Ollama and even writes
  `~/.pando.json`).
- `completed` = marker `<os.UserConfigDir()>/pando/setup-completed` (outside any config file).

## Scope mechanics (`config.PrepareSetupScope`)
No write API takes a scope: `updateCfgFile` writes the project file when one applies, otherwise the
global file. So:
- `project` → `InitializeProjectAt(cwd)` (full `.pando.toml` + `.pando/` layout), refused in $HOME.
- `global` → refused if a project file already applies; if no global file resolves, creates
  `~/.config/pando/.pando.toml` (comment header) so writes don't fall back to a fresh `~/.pando.json`.
- Then `config.Reload()` so viper registers the new file; every later `UpdateXxx` lands in it.

## Files
Backend:
- `internal/config/setup.go` (new): `SetupStatus`, `GetSetupStatus`, `PrepareSetupScope`,
  `SuggestSetupModels`, `SecondaryAgentNames`, `ApplySetupRemembrances`, `SetupCompleted`,
  `MarkSetupCompleted`, `countExplicitProviderAccounts`, defaults consts.
- `internal/ollamasetup/ollamasetup.go` (new): `Manager` (Detect, ListModels, Start, WaitRunning,
  Pull → job, Install → job, Get), install options per OS, model-name regex (no shell for pulls;
  install commands are fixed server-side, chosen by id).
- `internal/api/handlers_setup.go` (new) + routes in `internal/api/routes.go`:
  `GET /api/v1/setup/status`, `POST /setup/scope`, `GET /setup/suggested-models`, `POST /setup/models`
  (coder via `s.setCoderModel` so the live agent rebuilds its provider; others via `UpdateAgentModel`),
  `GET /setup/ollama/status`, `POST /setup/ollama/install`, `POST /setup/ollama/start`,
  `POST /setup/ollama/pull`, `GET /setup/jobs/{id}`, `POST /setup/remembrances`, `POST /setup/complete`.
Frontend:
- `web-ui/packages/pando-client/src/stores/setupWizardStore.ts` (new).
- `web-ui/src/components/setup/{SetupWizard,ProviderStep,ModelsStep,RemembrancesStep}.tsx`,
  `setupShared.tsx` (CommandLine, ChoiceCard, Notice), `setupUtils.ts` (new).
- Mounted in `web-ui/src/components/layout/MainLayout.tsx`; "Setup assistant" button in
  `web-ui/src/components/overlays/ConfigInitBanner.tsx`.
- i18n `setup.*` keys in `en.json` and `es.json` (other locales fall back to English).

## Verification
- `go test ./internal/config` (new `setup_test.go`: status, global/project scope writes, marker,
  auto-detected accounts) and `go test ./internal/ollamasetup` (fake Ollama: detect, pull progress,
  pull error, invalid names, unknown install option) — pass. `go build ./...` OK.
- `tsc -p tsconfig.app.json --noEmit` + eslint on the new files — clean.
- Manual: `pando serve` under an isolated $HOME + vite dev proxy; drove the wizard in a headless
  browser: auto-open, scope, provider picker, Copilot code screen (real device code), models step,
  Remembrances with a real local Ollama; API flow (scope project, models, real pull job, remembrances,
  complete) checked with curl and the resulting `.pando.toml`.

## Known limits / follow-ups
- Fast-model heuristic uses name markers (mini/flash/haiku…); for Ollama it may pick a `:cloud` model.
- `internal/api` test package did not compile at the time because of a concurrent OAuth-removal refactor
  (handlers_provider_accounts_test.go imports the deleted antigravity package) — unrelated to this feature.
- Translations other than en/es pending.
