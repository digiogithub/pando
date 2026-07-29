---
created_at: 2026-07-29T13:03:21.585686685Z
updated_at: 2026-07-29T13:22:15.214421385Z
tags:
    - fix
    - acp
    - images
    - transport
    - stdio
    - acp-go-sdk
---
# Fix: ACP hangs when the client attaches an image / screenshot (2026-07-29)

## Symptom

Attaching an image or screenshot in an ACP client (Zed, etc.) made Pando hang:
the prompt never got a response and the process had to be restarted.

## Root causes (both reproduced with a scripted stdio client)

1. **Oversized JSON-RPC line kills the stdin reader.**
   `interceptStdin` (`internal/mesnada/acp/transport_stdio.go`) used a
   `bufio.Scanner` capped at 10 MiB, and `acp-go-sdk@v0.15.0`
   `Connection.receive` used the same 10 MiB cap. Inline images are base64
   (+33%), so a 4K screenshot crosses it. Reproduced with an 8 MB PNG (10.9 MB
   base64): `bufio.Scanner: token too long` → interceptor loop exits →
   `fwd.Close()` → SDK sees EOF → `Connection closed`, `session/prompt` never
   answered = the reported hang.
2. **Attachment-only prompts rejected** with `no text content in prompt`
   (`-32603`), so a pasted screenshot without a caption always failed.
3. **`resource_link` and `resource` blocks ignored** — per the ACP content spec
   clients may attach files by reference (file:// URI) or as an embedded
   resource (text or base64 blob); those attachments were silently dropped.

## Changes — Pando

- New `internal/mesnada/acp/prompt_payload.go`:
  - `readJSONRPCLine` — `bufio.Reader` line reader with no Scanner cap; an
    oversized line (> `acpMaxLineBytes` = 128 MiB) is discarded **without**
    terminating the loop and the stream stays in sync.
  - `peekRequestID` — recovers the JSON-RPC id from a truncated prefix so the
    client is answered instead of hanging.
  - `shrinkPromptImages` — inline `image` blocks above
    `promptShrinkThresholdBytes` (10 MiB) are re-encoded through
    `imageopt.Normalize` (long side 1568 px, per-image budget) and the payload
    re-marshalled. Since SDK v0.15.1 this is an optimization (bandwidth,
    latency, tokens) plus a safety net for older SDKs — a failed shrink now
    forwards the payload as-is instead of rejecting it.
- `transport_stdio.go`: `interceptStdin` uses the new reader, shrinks large
  payloads, replies `-32600` only for lines above the hard ceiling, and never
  tears down the connection.
- `prompt_handler.go` `extractPromptContent`: attachment-only prompts accepted
  (`imageOnlyPromptText`); `resource_link` local images inlined via
  `attachmentFromFile` (cap `maxInlineAttachmentBytes` = 32 MiB) with a textual
  path mention as fallback; embedded `resource` text appended as context and
  image blobs attached; `uriToPath` for `file://`; `image` blocks with `uri`
  now carry `FilePath`/`FileName`.

## Changes — acp-go-sdk (upstream, v0.15.1)

Repo `madeindigio/acp-go-sdk`, commit `bed2104`, tag `v0.15.1` (pushed):
`Connection.receive` no longer uses `bufio.Scanner`.

- `readMessage` reads a newline-delimited message with no token cap; a message
  above the limit is skipped (rest of line discarded, stream stays in sync) and
  reported as `errMessageTooLarge`.
- Oversized **requests** are answered `-32600` using the id recovered from the
  message prefix (`peekMessageID`), so the peer never waits forever.
- Limit configurable via `Connection.SetMaxMessageBytes` / `MaxMessageBytes`,
  default 256 MiB (was an unexported 10 MiB constant).
- This also protects the **client** side (`NewClientSideConnection`, used by
  Pando's child ACP instances in Projects), which the transport-side fix does
  not cover.
- Tests: `connection_message_size_test.go` (20 MiB message, too-large skip +
  resync, id peek, end-to-end "connection survives an oversized message",
  setter defaults). Full SDK suite green with `-race`.

Pando bumped to `acp-go-sdk v0.15.1` in `go.mod`.

## Verification

- `go build ./...`, `go vet ./internal/mesnada/acp` clean.
- `go test ./internal/mesnada/acp ./internal/llm/agent ./internal/api` → ok.
- New Pando tests: `prompt_payload_test.go`, `prompt_content_blocks_test.go`.
- End-to-end with a scripted stdio ACP client:
  - 8 MB PNG (10.9 MB base64): before = connection closed / no response; after
    = `end_turn` in ~10 s, agent ALIVE on a follow-up `session/list`.
  - image-only prompt: before = `-32603 no text content in prompt`; after =
    `end_turn`, model describes the image.

Related: [[feature_image_optimization_tool_results]], [[acp_implementation_plan]]
