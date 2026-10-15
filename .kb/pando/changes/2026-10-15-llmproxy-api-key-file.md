---
created_at: 2026-10-15T17:06:21.768196418Z
updated_at: 2026-10-15T17:06:21.768196418Z
tags:
    - change
    - llmproxy
---
# llm-proxy: API key from file/env, constant-time compare

Bookmark feat/llmproxy-api-key-file (base main ceb7afd0). Label: enterprise-hook.

What: `pando llm-proxy` accepts `--api-key-file` and env PANDO_LLM_PROXY_API_KEY besides `--api-key` (precedence flag > file > env; flag+file is an error; empty or unreadable file is an error). Auth middleware compares SHA-256 digests with subtle.ConstantTimeCompare.

Files and symbols: cmd/llm_proxy.go, internal/llmproxy/apikey.go (ResolveAPIKey, keyMatches), internal/llmproxy/middleware.go (authMiddleware), internal/llmproxy/apikey_test.go, docs/llm-proxy.md.

Why: a key in argv is readable via ps and /proc/<pid>/cmdline; the old != comparison was not constant time.

Verified: go build ./..., go vet and go test for ./cmd/... and ./internal/llmproxy/... pass.
