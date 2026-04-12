# AGE config encryption plan

Scope: encrypt at rest only internal tools API keys/text params and MCP server text secrets in persisted user config (TOML/legacy), not the rest of config.

Sensitive fields in scope:
- internalTools.googleApiKey
- internalTools.googleSearchEngineId
- internalTools.braveApiKey
- internalTools.perplexityApiKey
- internalTools.exaApiKey
- mcpServers[*].headers values
- mcpServers[*].env entries values after KEY=

Out of scope:
- provider API keys
- remembrances/provider settings
- non-text config params
- DB data unless current MCP/internal tool config is actually persisted there

Implementation:
1. Add internal/config AGE helper to lazily create/load keypair under user pando home (not project).
2. Define encrypted marker/prefix for persisted scalar strings.
3. On config load, decrypt scoped fields transparently into runtime cfg.
4. On config write, clone cfg and encrypt only scoped fields before TOML marshal.
5. Keep API GET masking unchanged; PUT accepts plaintext/masked and persistence encrypts behind the scenes.
6. Add Go tests for encryption round-trip and key material location/creation.
