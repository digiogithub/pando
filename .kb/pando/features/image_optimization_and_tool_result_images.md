---
created_at: 2026-07-23T08:59:37.906319861Z
updated_at: 2026-07-23T10:34:04.744240297Z
tags:
    - feature
    - images
    - vision
    - providers
    - tui
    - webui
    - anthropic-beta
    - files-api
---
# Feature: Optimized image reading/sending + tool-result images to the model + Anthropic Files API

Date: 2026-07-23. Plan: `/home/sevir/.claude/plans/structured-crunching-pnueli.md`.

## Problem
1. Images sent to LLMs were never optimized: user attachments -> `message.BinaryContent` -> each provider embedded FULL base64 every turn (no resize to model vision tier, no recompression, no dimension cap; only a 5 MB file-size gate). 4K photo bloated payload/latency, zero detail gain.
2. Tool-result images broken: `ToolResponse.Type == ToolResponseTypeImage` (browser_screenshot) was discarded in agent.go, base64 landed as text in `ToolResult.Content`, then `ShouldOmitFromPrompt("browser_screenshot")` blanked it -> model never saw screenshots. TUI dumped raw base64.

## Delivered (ALL fases 0-7 done)

### `internal/imageopt` (Fase 0)
`Normalize` (decode png/jpeg/webp/gif, resize Lanczos, recompress w/ progressive shrink to base64 budget, fast-path if within limits), `DetectMIME` (magic bytes), `ToDataURI`, `Crop`, `TileImage`, `ImageDimensions`. Tests green.

### Model vision limits (Fase 1)
`models.Model.ImageMaxLongSidePx` + `ImageLongSideLimit()` (1568; high-tier 2576 for opus-4.8/sonnet-5/fable-5/mythos-5 via regex).

### Config `[Image]` (Fase 2)
`config.ImageConfig{AutoResize(true),MaxWidth/MaxHeight(2000),MaxBase64Bytes(5MB),Quality(85),UseFilesAPI(false)}`. Defaults in setDefaults. Setters `UpdateImageAutoResize`, `UpdateImageUseFilesAPI`.

### Attachment optimization (Fase 3)
agent.go `imageOptionsForModel` + `optimizeAttachment`; both BinaryContent sites route through it.

### Tool-result images to the model (Fase 4)
`message.ToolResult.Images []BinaryContent` (json `images`). Removed browser_screenshot omit hack (`SanitizedForPrompt` now identity). agent.go `buildImageToolResult` (base64 Content -> Normalize -> Images + `[image WxH fmt]` placeholder), wired at success branch (skips cache interception). Provider Tool-role conversion gated on `SupportsAttachments`: Anthropic native tool_result image block; OpenAI/Copilot(chat+responses)/Gemini synthetic user message.

### Render, no base64 dump (Fase 5)
TUI `renderToolResultImages` (placeholder + `internal/tui/image.ToString` preview). WebUI: SSE `tool_result` payload includes `images` data URIs (handlers_chat.go); `SSEToolResult.images` type, sse.ts passthrough, MessageList passes `tc.result.images`, MessageBubble `ToolContent`/`EventRow` render `<img>`.

### Crop/zoom + tiling (Fase 7)
`internal/llm/tools/image_crop.go` `image_crop(path,x,y,width,height)` -> native-res region as ToolResponseTypeImage (through buildImageToolResult). Registered in base tools. `imageopt.TileImage` library helper.

### Anthropic Files API via beta Messages API (Fase 6 — DONE, opt-in)
Opt-in toggle `[Image] UseFilesAPI` (default false): when true AND provider is direct Anthropic (not bedrock), Pando routes through the **beta Messages API** and uploads images to the **Files API** once, referencing them by `file_id` across turns (small payload every turn). When false -> classic non-beta base64 path (unchanged).
- `internal/llm/provider/anthropic_beta.go`: `useBetaAPI()` gate; `uploadImage` (sha256 content-hash -> file_id cache `fileIDCache`/`fileIDMu` on anthropicClient; `client.Beta.Files.Upload` with `anthropic.File(reader,name,mime)` + beta header `files-api-2025-04-14`); `betaImageBlock` (prefers `BetaFileImageSourceParam{FileID}`, falls back to `BetaBase64ImageSourceParam`); `convertMessagesBeta`/`convertToolsBeta`/`preparedMessagesBeta`/`sendBeta`/`streamBeta`/`toolCallsBeta`/`usageBeta` — beta mirror of the non-beta path (same retry/thinking/cache logic, reuses shouldRetry/finishReason/parseContextOverflowError). `send`/`stream` dispatch to the beta variants when `useBetaAPI()`.
- Beta streaming uses `BetaRaw*` events; input_json delta via `event.Delta.PartialJSON`; tool-use input via `variant.JSON.Input.Raw()`.
- The non-beta `ImageBlockParamSourceUnion` only supports base64/URL, which is WHY file_id requires the beta path.
- Bedrock and non-Anthropic providers ignore `useBetaAPI` (method is anthropic-only).

### Settings UI (opt-in surface)
API `SettingsResponse`/`SettingsUpdateRequest` gain `image_auto_resize`, `image_use_files_api` (handlers_settings.go). WebUI: types + settingsStore defaults + two toggles in GeneralSettings ("Optimize images", "Anthropic Files API (beta)").

## Verification
`go build ./...` clean. `go test ./internal/imageopt ./internal/llm/agent ./internal/api ./internal/llm/provider ./internal/llm/tools ./internal/config ./internal/llm/models` all ok (added TestUseBetaAPIGating, TestPreparedMessagesBetaHasSystemAndThinking, crop/tile tests). WebUI `tsc --noEmit` clean.

## Aside (not fixed)
`internal/message/message.go` unmarshallParts `imageURLType` branch does not `append` (preexisting ImageURLContent bug) — left untouched.
