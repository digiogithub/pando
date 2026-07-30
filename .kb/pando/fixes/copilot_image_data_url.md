---
created_at: 2026-07-30T05:49:12.158736311Z
updated_at: 2026-07-30T05:49:12.158736311Z
tags:
    - fix
    - copilot
    - images
    - vision
    - acp
    - responses-api
---
# Fix: Copilot rejects attached images — "the provided image URL is invalid" (2026-07-30)

## Symptom

Attaching an image in an ACP client (Zed) against a GitHub Copilot model failed with:

```
POST "https://api.githubcopilot.com/responses": 400 Bad Request
{"message":"validating vision content in responses input: validating responses image content: the provided image URL is invalid","code":"invalid_request_body"}
```

Follow-up of [[acp_image_attachment_hang]]: the transport hang was fixed, so the
image finally reached the provider — and the provider payload turned out to be
malformed.

## Root cause

`BinaryContent.String(provider)` in `internal/message/content.go` only emitted a
`data:<mime>;base64,...` URL for `models.ProviderOpenAI`; every other provider
got a bare base64 blob (correct for Anthropic/Gemini/Bedrock, which carry the
media type in a separate field).

The Copilot client passes that string straight into OpenAI-shaped image fields:

- `internal/llm/provider/copilot.go:231,283` — chat completions
  `ChatCompletionContentPartImageImageURLParam.URL`
- `internal/llm/provider/copilot.go:656,707` — Responses API
  `responses.ResponseInputImageParam.ImageURL`

Both require a full data URL (or an http(s) URL), so Copilot rejected the bare
base64. Affected user attachments and tool-result images alike.

## Changes

`internal/message/content.go`:

- `BinaryContent.String` now returns a data URL for the OpenAI-compatible
  providers: `ProviderOpenAI`, `ProviderAzure`, `ProviderCopilot`,
  `ProviderOpenAICompatible`. Other providers keep raw base64.
- New `BinaryContent.mimeTypeOrDefault()` — an empty `MIMEType` would otherwise
  produce the malformed `data:;base64,...`; defaults to `image/png`.

`internal/message/content_test.go` (new): data-URL providers, raw-base64
providers, and the empty-MIME default.

## Verification

- `go build ./...`
- `go test ./internal/message/ ./internal/llm/provider/` — pass.
