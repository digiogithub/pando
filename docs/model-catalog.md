# Model pricing and capabilities (models.dev)

Most providers do not report per-token pricing (and often not the real context window) in
their model-listing API, so the session **Cost** shown in the TUI and WebUI sidebars stayed
empty for those models. Pando completes that metadata from the community catalog
[models.dev](https://github.com/anomalyco/models.dev): input/output/cache prices, context and
output limits, reasoning and attachment support, description and training cutoff.

- The catalog is downloaded **once per instance**, lazily, and cached in
  `~/.pando_modelsdev.json` for 24 h; a stale cache is reused when the network is unavailable.
- It **only fills what is missing**. Anything the provider (or Pando's curated catalogue)
  already reported stays authoritative, and capability flags are only ever raised.
- Any failure — offline, HTTP error, unknown provider or model — is silent: models keep exactly
  the behaviour they had before, with no cost displayed.
- Local runtimes (Ollama, llama.cpp) are deliberately excluded: they cost nothing to run, and
  importing a hosted price for a same-named open-weights model would invent a cost.

The model selectors (TUI dialog and WebUI switcher/combobox) show the resulting
`200K ctx · $3/$15 per 1M · cutoff 2025-01` line, and the badges are derived from the real
price instead of guessing from the model name.

Toggle it in **Settings → General → Model Catalog (models.dev)** (TUI and WebUI), or in the
config file:

```toml
[ModelsDev]
Enabled = true   # default; set to false to never contact models.dev
```

The switch is live: turning it off stops the catalog from being consulted immediately, and
turning it back on lets the next model refresh load it without restarting Pando.
