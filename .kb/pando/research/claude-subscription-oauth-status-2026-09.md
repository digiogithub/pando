---
created_at: 2026-09-08T19:39:21.05175789Z
updated_at: 2026-09-08T19:39:21.05175789Z
tags:
    - research
    - auth
    - claude
---
# Claude subscription OAuth in third-party clients — status (2026-09)

Research date: 2026-09-08. Question: can Pando use a Claude Code Pro/Max
subscription (its OAuth token) directly, instead of wrapping the Claude Code
CLI as an agent?

## Timeline (post-leak era)

| Date | Event |
|------|-------|
| 2026-01-09 | Anthropic deploys server-side blocks. Error: "This credential is only authorized for use with Claude Code and cannot be used for other API requests." Account bans (some reversed) hit OpenCode/OpenClaw users. |
| 2026-02-18 | Legal docs updated: using OAuth tokens from Free/Pro/Max in any other product, tool or service **including the Agent SDK** is a Consumer ToS violation. |
| 2026-03-31 | Source map leak: v2.1.88 npm package shipped `cli.js.map` (59.8 MB, ~512k lines TS). Anthropic afterwards reworked internals; obfuscated names/constants in our KB doc (based on v2.1.76) are likely stale. |
| 2026-04-04 | Full enforcement. Subscriptions no longer cover third-party harnesses. One-month credit offered; "extra usage bundles" (API credits) as alternative. |
| 2026-08/09 | Detection hardened into **wire-fingerprint checks**: user-agent + Claude Code version, Stainless headers, `anthropic-beta` set, system-block layout, tool-name prefixing, token clamp, and a billing attestation hash. Upstream also gates models on minimum Claude Code version (e.g. `claude_code_version_too_old` 400 if mimicked version < required). |

## What is allowed / blocked

- Blocked: subscription OAuth token (`sk-ant-oat…`) used by anything that is
  not the genuine Claude Code binary — including our own
  `internal/auth/claude.go` token against `/v1/messages`. Detection + ban risk
  is real; bans documented for OpenCode users.
- Allowed: official Claude Code CLI (incl. headless `-p`, remote via SSH),
  Claude.ai/Desktop, and **API keys** (`sk-ant-api…`, pay-as-you-go) in any
  third-party tool. Claude Code also gained `--bare` mode: skips OAuth/keychain,
  requires `ANTHROPIC_API_KEY`/apiKeyHelper; positioned as the future default
  for scripted `-p` use — Anthropic is pushing all programmatic use to API billing.

## How the underground works (informational, NOT recommended for Pando)

Projects like `pi-sub-anthropic` (Pi extension), `sub2api`, `dario`,
CLIProxyAPI make subscription tokens work by reproducing Claude Code's wire
fingerprint byte-for-byte: UA, beta headers, system-block layout, token clamp,
tool naming, billing attestation hash. Consequences observed:
- Fingerprint is pinned; breaks whenever Anthropic changes server-side
  (sub2api issue #6563: hardcoded 2.1.220 fingerprint rejected by models
  requiring >= 2.1.251).
- Explicit ban warnings from the projects themselves; account at risk is the
  user's main Claude subscription account.
- Shipping impersonation code in an MIT-licensed open project like Pando adds
  legal/reputational exposure.

## Options for Pando

1. **API key provider (clean, supported)**: keep `anthropic` provider with
   `ANTHROPIC_API_KEY`. This is the officially sanctioned path for third-party
   clients and survives the crackdown.
2. **Claude Code as subprocess/ACP agent (what we do today)**: legitimate use
   of the subscription because the official binary performs inference. The user
   wants to move away from this, but it remains the only ToS-compliant way to
   spend subscription quota.
3. **Do not implement fingerprint impersonation**: technically feasible (our
   KB already documents the OAuth endpoints/constants), but ToS violation, ban
   risk, and high maintenance (fingerprint + min-version churn post-leak).
4. Watch items: any official Anthropic third-party partner program; "extra
   usage bundles" pricing; whether `--bare` becomes mandatory for `-p`.

## Maintenance note

`pando/auth/claude-oauth-implementation.md` and `research/claude-code-api.md`
were audited against cli.js **v2.1.76** (2026-03-16), i.e. **before the leak**.
Any re-implementation must re-audit constants (client ID, scopes, endpoints,
beta header) against a current cli.js, and account for the fingerprint/version
gating described above.

Sources: claudefa.st leak write-up (Sep 2026), KERSAI ban guide (Apr 2026),
pi.dev pi-sub-anthropic README (Aug 2026), sub2api #6563 (Sep 2026), HN
47844269 (May 2026, --bare docs quote), LinkedIn/Aaron Xie recap (Aug 2026).
