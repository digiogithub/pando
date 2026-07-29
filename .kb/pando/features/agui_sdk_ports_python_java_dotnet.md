---
created_at: 2026-07-29T12:08:11.131338056Z
updated_at: 2026-07-29T12:08:11.131338056Z
tags:
    - feature
    - agui
    - sdk
    - python
    - java
    - dotnet
    - genui
    - copilotkit
---

# Feature: AG-UI / GenUI client in the Python, Java and .NET SDKs

**Date:** 2026-07-29
**Builds on:** [[pando/features/agui_adapter_p6_p7.md]], [[pando/features/agui_adapter_p0_p1.md]],
[[pando/analysis/copilotkit_agui_integration_analysis.md]],
[[pando/fixes/agui_tool_calls_missing_from_event_stream.md]]

## Motivation

The TypeScript SDK gained the AG-UI subpath (`@pando-ai/sdk/agui`, commit `5db927b`
in the nested `sdk/typescript` repo: `src/agui/{client,types,index,copilotkit}.ts` +
`tests/agui.test.ts`). The other three SDKs had no way to talk to
`pando agui-serve` / `pando serve --agui-port` at all. The original analysis said
"Python/Java/.NET: nothing required *for CopilotKit*" — true, since CopilotKit is a
JS runtime — but the protocol client itself is language-agnostic and is what those
SDKs were missing. This change ports it, feature for feature, minus the
CopilotKit-runtime glue (`copilotkit.ts`), which has no equivalent outside JS.

## What was implemented

Parity target for each language: `PandoAguiClient` (run + streamed events +
`runText` + `/info` discovery), the full protocol type set, the shared-state
document, SSE parsing that tolerates chunk boundaries / malformed frames /
`[DONE]` / a missing trailing blank line, bearer-token auth, and error mapping
onto a `PandoAguiError`-equivalent carrying the HTTP status.

### Python — `sdk/python/src/pando/agui/`

- `types.py`: `AguiEvent` (payload kept whole; attribute access accepts both
  `toolCallName` and `tool_call_name`), `AguiMessage`, `AguiToolCall`, `AguiTool`,
  `AguiContext`, `RunAgentInput`, `JsonPatchOperation`, `PandoState` +
  `PandoModelState` / `PandoTokenUsageState` / `PandoFileState` /
  `PandoSubAgentState`, `AguiInfo` / `AguiAgentDescriptor` / `AguiCapabilities`,
  `iter_state_deltas`.
- `client.py`: `PandoAguiClient` with **sync** (`run_sync`, `run_text_sync`,
  `info_sync`) built on `urllib` — standard library only — and **async** (`run`,
  `run_text`, `info`) on `httpx`; `parse_sse`, `parse_frame`, `random_id`,
  `PandoAguiError`, `PERMISSION_TOOL_NAME`, `PandoPermissionRequest/Answer`.
- `pyproject.toml`: new `agui` extra (`httpx`), needed only for the async half.
- Subpackage is separate: `import pando` pulls in none of it.

### Java — `sdk/java/src/main/java/io/pando/sdk/agui/`

`PandoAguiClient` (builder; `run(RunOptions, Consumer<AguiEvent>)`,
`stream(...)` returning a `Flow.Publisher` like `PandoHttpClient`, `runText`,
`info`, static `parseSse`, injectable `HttpClient` for tests), `AguiEvent`
(with `snapshot()` / `delta()`), `RunOptions` + `RunAgentInput`, `AguiMessage`
(+ `user` / `assistant` / `toolResult` factories), `AguiToolCall`, `AguiTool`,
`AguiContext`, `PandoState` and friends, `AguiInfo` / `AguiAgentDescriptor` /
`AguiCapabilities`, `JsonPatchOperation`, `PandoPermissionRequest`, package-private
`Wire` readers, and `io.pando.sdk.exception.PandoAguiException`.
No new dependency: `java.net.http` + Jackson.

### .NET — `sdk/dotnet/src/Pando.Sdk/Agui/`

`PandoAguiClient` (`RunAsync` as `IAsyncEnumerable<AguiEvent>`, `RunTextAsync`,
`InfoAsync`, static `ParseSseAsync`, `IAsyncDisposable`, plus an overload taking a
caller-owned `HttpClient` for `IHttpClientFactory`/tests), `AguiEvent`
(`GetString`, `GetAs<T>`, `Snapshot()`, `Delta()`), `AguiTypes.cs` with the whole
type set + `AguiEventTypes` / `RunOutcomes` constants, `PandoAguiClientOptions` and
`RunOptions`, and `Exceptions/PandoAguiException.cs`. `HttpClient` +
`System.Text.Json` only. SSE reading uses `ReadLineAsync`, never `EndOfStream`,
which would block on a read-ahead between events of a live stream.

### Docs

AG-UI sections added to `sdk/python/README.md`, `sdk/java/README.md` and
`sdk/dotnet/README.md`, mirroring the TypeScript "Mode 4" section: discovery,
streaming, `runText`, frontend tools / interrupt-resume, permission prompts, and an
export table.

## Verification

- Python: `pytest` — 15 new tests in `sdk/python/tests/test_agui.py`; whole suite
  **91 passed** (76 pre-existing + 15). Tests stub `urllib.request.urlopen`, so they
  run without `httpx`.
- Java: `sdk/java/src/test/java/io/pando/sdk/agui/PandoAguiClientTest.java` — 15 tests
  against a real loopback `com.sun.net.httpserver.HttpServer` (so the SSE path is
  exercised end to end). Compiled with JDK 21 + Jackson 2.17.1 and run through the
  JUnit 5 launcher: **15/15 pass**. (`mvn` is not installed on this machine; the
  classpath was assembled from `~/.m2`.)
- .NET: `sdk/dotnet/tests/Pando.Sdk.Tests/AguiClientTests.cs` — 14 xUnit tests over a
  stub `HttpMessageHandler`. **Not compiled or run here: no `dotnet` SDK is installed
  on this machine.** Needs `dotnet test` on a machine that has it.

## Deliberately out of scope

`copilotkit.ts` (`createPandoAgent`, `discoverPandoAgents`,
`registerPandoCopilotKit`) is glue for the CopilotKit *JS* runtime and has no
counterpart in the other languages; `/info` discovery covers the useful half of it
(enumerate agents and their URLs) in all three ports.