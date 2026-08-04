---
created_at: 2026-08-04T15:18:28.119324549Z
updated_at: 2026-08-04T15:18:28.119324549Z
tags:
    - analysis
    - copilot
    - byok
    - openrouter
    - tools
---
# Copilot BYOK (OpenRouter) 400 `strict` null on tool calls

## Symptom

Chatting with an org BYOK model routed through OpenRouter fails:

```
POST "https://api.business.githubcopilot.com/chat/completions": 400 Bad Request
{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,
 \"metadata\":{\"raw\":\"{\\\"error\\\":{\\\"code\\\":\\\"400\\\",\\\"message\\\":
 \\\"[{'type': 'bool_type', 'loc': ('body', 'tools', 0, 'function', 'strict'),
 'msg': 'Input should be a valid boolean', 'input': None}, ...]\\\"
 ,\\\"provider_name\\\":\\\"Xiaomi\\\",\\\"is_byok\\\":false}}"
```

One entry per tool in the request. Native Copilot models and the direct
OpenRouter provider both work; only the Copilot-proxied BYOK path breaks.

## Root cause: NOT a Pando bug

The GitHub Copilot proxy rewrites the tool definitions before forwarding them to
OpenRouter and always emits `"strict": null` in each `function` object. The
upstream provider behind `xiaomi/mimo-v2.5` validates `strict` as a strict
boolean and rejects `null`.

Verified by direct curl/urllib probes against
`https://api.business.githubcopilot.com/chat/completions` with the exchanged
Copilot API token (see [[fix-copilot-byok-custom-models-token-exchange]]),
sending the same tool three ways:

| `strict` sent by client | result |
|---|---|
| omitted | 400, `'strict' ... input: None` |
| `false` | 400, same |
| `true` | 400, same |

The client value is discarded by the proxy, so no client-side change can fix it.

## Scope: one model only

Same probe, `ping` prompt, with and without a single `get_weather` tool:

| model | no tools | with tools |
|---|---|---|
| `madeindigio/OpenRouter/xiaomi%2Fmimo-v2.5` | OK | **400 strict-null** |
| `madeindigio/OpenRouter/xiaomi%2Fmimo-v2.5-pro` | OK | OK |
| `madeindigio/OpenRouter/z-ai%2Fglm-5.2` | OK | OK |
| `madeindigio/OpenRouter/qwen%2Fqwen3.7-max` | OK | OK |
| `madeindigio/OpenRouter/moonshotai%2Fkimi-k3` | OK | OK |
| `madeindigio/OpenRouter/deepseek%2Fdeepseek-v4-pro` | OK | OK |
| `madeindigio/OpenRouter/minimax%2Fminimax-m3` | OK | OK |

Only `mimo-v2.5` (non-pro) is affected, and only when tools are present. Since
Pando always sends its tool set, the model is unusable as a coder/agent model.

## Code notes

- `copilotClient.convertTools` (`internal/llm/provider/copilot.go:362`) builds
  `openai.ChatCompletionToolParam` without `Strict`. In openai-go
  v0.1.0-beta.2, `FunctionDefinitionParam.Strict` is
  `param.Opt[bool]` with `json:"strict,omitzero"`, so Pando omits the field
  entirely — it never sends `null`.
- The `/responses` route (`convertToolsToResponses`,
  `internal/llm/provider/copilot.go:780`) already sends `strict: false` via
  `responses.ToolParamOfFunction(name, params, false)`, because
  `responses.FunctionToolParam.Strict` is `json:"strict,required"`.

## Resolution

No code change. Workaround: use `xiaomi/mimo-v2.5-pro` (or any other BYOK model
in the table). The fix belongs to GitHub (stop injecting `strict: null`) or to
the Xiaomi endpoint on OpenRouter (accept `null`).

Related: [[fix-copilot-byok-custom-models-token-exchange]],
[[fix_copilot_endpoint_metadata_routing]].
