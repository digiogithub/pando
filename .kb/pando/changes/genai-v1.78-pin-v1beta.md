---
created_at: 2026-11-18T06:04:48.783994533Z
updated_at: 2026-11-18T06:04:48.783994533Z
tags:
    - change
    - gemini
---
# genai SDK v1.78.0 upgrade, API version pinned to v1beta (2026-11-18)

Google notice: google.golang.org/genai v1.78.0 changes the default API version for API-key clients (BackendGeminiAPI) from v1beta to v1; "*-latest" aliases do not exist in v1.

## Findings
- Pando uses the SDK only in `internal/llm/provider/gemini.go` (API key) and `vertexai.go` (Vertex, unaffected). Model listing (`internal/llm/models/fetcher.go`) already uses an explicit `/v1beta/models` URL. No `-latest` Gemini aliases in code/catalog.

## Changes
- go.mod/go.sum: genai v1.51.0 -> v1.78.0.
- `gemini.go`: `geminiAPIVersion()` pins `HTTPOptions.APIVersion = "v1beta"` unless `GOOGLE_GENAI_API_VERSION` is set (then the SDK reads the env itself, so users can opt into v1).
- `gemini_test.go`: `TestGeminiAPIVersionPinsV1beta`.

## Verification
`go build ./...`, `go test ./internal/llm/...` pass. Not committed at time of writing.
