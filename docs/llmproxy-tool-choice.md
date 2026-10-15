# LLM proxy: `tool_choice`

`pando llm-proxy` forwards the `tool_choice` field of a request to the
upstream provider, translated into that provider's own form. Before this
change the proxy's request type had no such field, so a client asking for a
forced tool call silently got the model's free choice.

## Accepted input

`POST /v1/chat/completions` (OpenAI format):

| `tool_choice` | Meaning |
|---|---|
| absent or `null` | provider default (auto); nothing is sent upstream |
| `"auto"` | the model chooses between text and tool calls |
| `"none"` | no tool call, even though tools are declared |
| `"required"` | at least one call to any of the declared tools |
| `{"type":"function","function":{"name":"classify"}}` | a call to `classify` |
| `{"type":"function","name":"classify"}` | same (Responses API shape, accepted for convenience) |

`POST /v1/messages` (Anthropic format) accepts `{"type":"auto"}`,
`{"type":"none"}`, `{"type":"any"}` (= required) and
`{"type":"tool","name":"classify"}`.

The proxy answers **400** (`code: invalid_tool_choice` on the OpenAI route)
when the value is malformed, when `"required"` or a named function comes with
no tools, or when the named function is not among the request's tools. A bad
request never reaches the provider.

## Provider mapping

| Provider type | Support | Upstream form |
|---|---|---|
| `openai`, `azure`, `groq`, `openrouter`, `xai` | native | OpenAI `tool_choice`, verbatim |
| `copilot` (Chat Completions) | native | OpenAI `tool_choice` |
| `copilot` (Responses API models) | native | Responses `tool_choice`: `"auto"/"none"/"required"` or `{"type":"function","name":…}` |
| `anthropic`, `bedrock` (Anthropic models) | native | `{"type":"auto"}`, `{"type":"none"}`, `{"type":"any"}`, `{"type":"tool","name":…}` (stable and beta Messages API) |
| `gemini`, `vertexai` | native | `toolConfig.functionCallingConfig`: mode `AUTO`, `NONE`, `ANY`; a named function is `ANY` with `allowedFunctionNames: [name]` |
| `openai-compatible`, `llama-cpp`, `local` | forwarded | OpenAI `tool_choice`, verbatim. Honoured only if the server implements it; support varies by server and version (vLLM, LiteLLM and llama.cpp with `--jinja` implement at least part of it) |
| `ollama` | emulated | Ollama's OpenAI layer documents `tool_choice` as unsupported and ignores it, so the field is **not** sent. `"none"` is emulated exactly by sending no tools. A named function sends **only that tool**; `"required"` sends all tools. Neither can force the call: the model may still answer with text |

The tool choice is only sent when the request declares tools; providers
reject a tool choice without tools.

**Anthropic and extended thinking.** Anthropic rejects a forced tool choice
(`any` or `tool`) together with extended thinking. When the choice forces a
call, the Anthropic client turns thinking off for that request (debug log
"Disabling extended thinking for a forced tool choice"). `auto` and `none`
keep thinking as configured.

## Knowing what happened: `X-Pando-Tool-Choice`

Every response to a request that carried `tool_choice` has the header
`X-Pando-Tool-Choice`:

| Value | Meaning |
|---|---|
| `native` | the provider received the equivalent parameter |
| `forwarded` | sent verbatim to an OpenAI-compatible server that may ignore it |
| `emulated` | the provider has no tool choice; Pando emulated it exactly (`none` on Ollama) |
| `degraded` | the provider has no tool choice and the forced call could not be enforced; the proxy also logs a warning `llm-proxy: provider cannot enforce tool_choice; forced tool call degraded` with provider, model and choice |

A client that needs a guaranteed tool call should treat `degraded` (and,
depending on the server, `forwarded`) as "check that a tool call came back",
and validate the arguments as usual.

## Code

- `internal/llm/provider/tool_choice.go` — `ToolChoice`, `WithToolChoice`,
  `ToolChoiceSupportFor`, `EmulateToolChoice` and the per-provider mappers.
  The agent never sets a tool choice, so its requests are unchanged.
- `internal/llm/provider/{openai,copilot,anthropic,anthropic_beta,gemini}.go`
  — set the parameter when tools are present.
- `internal/llmproxy/tool_choice.go` — parsing, validation, emulation and the
  header; used by `handlers_chat.go` and `handlers_messages.go`.

## Verify

```bash
go test ./internal/llm/provider -run 'ToolChoice|GeminiToolConfig'
go test ./internal/llmproxy -run 'ToolChoice|ChatCompletions|AnthropicMessages'
```

Against a running proxy (any configured model with tool support):

```bash
curl -s -D - http://localhost:11434/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -d '{
  "model": "anthropic.claude-sonnet-4-5",
  "messages": [{"role":"user","content":"Classify: new VAT rules for 2027"}],
  "tools": [{"type":"function","function":{"name":"classify",
    "parameters":{"type":"object","properties":{"topic":{"type":"string"}},"required":["topic"]}}}],
  "tool_choice": {"type":"function","function":{"name":"classify"}}
}'
```

The answer has `finish_reason: "tool_calls"`, one call to `classify` and the
header `X-Pando-Tool-Choice: native`.
