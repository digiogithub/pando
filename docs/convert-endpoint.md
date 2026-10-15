# Document conversion endpoint (`POST /v1/convert`)

Pando converts rich documents to Markdown with the pure-Go markitdown library
(`internal/convert`). Besides the `pando convert` command and the Knowledge
Base importer, the converter is available over HTTP on the LLM proxy, for
services that receive documents (mail attachments, uploads) and need their
text.

## Enabling it

The endpoint is **off by default**. Turn it on with `--convert`:

```bash
pando llm-proxy --host 0.0.0.0 --port 11434 --api-key "$PANDO_TOKEN" --convert
```

| Flag | Default | Meaning |
|---|---|---|
| `--convert` | `false` | mount `POST /v1/convert` |
| `--convert-max-bytes` | `20971520` (20 MiB) | largest document accepted (413 above) |
| `--convert-max-output-bytes` | `8388608` (8 MiB) | largest Markdown returned (422 above) |
| `--convert-timeout` | `60s` | one conversion, including the wait for a free slot (504 / 503) |
| `--convert-max-concurrent` | `2` | conversions running at once; the rest wait for a slot |
| `--convert-memory-limit` | `1GiB` | `GOMEMLIMIT` of each conversion child (soft limit; empty for none) |
| `--convert-in-process` | `false` | convert inside the proxy instead of a child process (trusted input only) |

`--convert` on a non-loopback `--host` **requires `--api-key`**: the proxy
refuses to start otherwise, because the endpoint parses untrusted files.

## Authentication

The same as every other llm-proxy route: `Authorization: Bearer <api-key>`
or `x-api-key: <api-key>`. Without the right key the answer is 401. The
listener is also covered by the sandbox port guard, like the rest of the
proxy.

## Request

Either form:

1. **Raw body** (preferred): the document bytes as the body, its media type in
   `Content-Type`, an optional file name in `X-Filename`.
2. **Multipart** (`multipart/form-data`): the document in the field `file`;
   the part's `Content-Type` and file name are used. Other fields are
   ignored.

The format is resolved from the media type when it is a known one; when the
media type is absent or `application/octet-stream`, from the file name's
extension. Any other media type is 415. Accepted:

| Media type | Format |
|---|---|
| `application/pdf` | pdf |
| `application/vnd.openxmlformats-officedocument.wordprocessingml.document` | docx |
| `application/vnd.openxmlformats-officedocument.presentationml.presentation` | pptx |
| `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` | xlsx |
| `application/vnd.ms-excel` | xls |
| `text/html`, `application/xhtml+xml` | html |
| `text/csv` | csv |
| `application/epub+zip` | epub |
| `application/x-ipynb+json` | ipynb |
| `application/xml`, `text/xml`, `application/rss+xml`, `application/atom+xml` | xml, rss, atom |
| `application/json`, `application/x-ndjson`, `application/jsonl` | json, jsonl |
| `text/plain`, `text/markdown` | txt, md |

Not accepted: **`.zip`** (the library converts archives recursively, an
archive bomb over HTTP) and the formats markitdown does not read at all:
legacy binary Word and PowerPoint (`.doc`, `.ppt`), OpenDocument
(`.odt`, `.ods`, `.odp`) and RTF. They answer 415.

## Response

`200` with the Markdown. Content negotiation:

- default (`Accept` absent, `*/*`, `text/markdown`, `text/plain`):
  `Content-Type: text/markdown; charset=utf-8`, the Markdown as the body;
- `Accept: application/json` (without `text/markdown`): JSON

```json
{
  "markdown": "### 1 Introduction\n…",
  "metadata": {
    "filename": "newsletter.pdf",
    "format": "pdf",
    "media_type": "application/pdf",
    "input_bytes": 92971,
    "markdown_bytes": 5221,
    "duration_ms": 869
  }
}
```

Both forms carry `X-Pando-Convert-Format`, `X-Pando-Convert-Input-Bytes`,
`X-Pando-Convert-Duration-Ms` and `Cache-Control: no-store`. The Markdown is
valid UTF-8 (invalid sequences become U+FFFD). A document with no
extractable text (for example a scanned PDF; there is no OCR) is `200` with
an empty body.

Errors are JSON `{"error":{"message","type","code"}}`:

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_request`, `empty_document` | malformed multipart, no `file` field, empty body |
| 401 | — | missing or wrong API key |
| 405 | `method_not_allowed` | not POST |
| 413 | `too_large` | document above `--convert-max-bytes` |
| 415 | `unsupported_format` | format not accepted (message lists the accepted extensions) |
| 422 | `conversion_failed` | the converter could not read the document (broken, encrypted, wrong type) |
| 422 | `output_too_large` | Markdown above `--convert-max-output-bytes` |
| 503 | `busy` (+ `Retry-After`) | no slot became free within the timeout |
| 504 | `timeout` | conversion exceeded `--convert-timeout`; the child was killed |
| 500 | `converter_error` | the child could not be started |

Error messages never quote the document; the converter's own error is only
logged at debug level.

## Untrusted input

- **Process isolation.** Each conversion runs in a child process,
  `<pando binary> convert --stdin-format <ext>`, started without a shell. The
  only variable argument is the canonical extension chosen from the table
  above, never client text. The document goes through stdin and the Markdown
  comes back on stdout, so **nothing is written to disk** (no temp files to
  clean). The child is killed on timeout, on client disconnect and as soon as
  its output passes the limit.
- **Minimal environment.** The child gets only `PATH`, `TMPDIR`, `HOME`
  (the temp dir) and `GOMEMLIMIT`; provider keys and tokens in the proxy's
  environment do not reach it. Its working directory is the temp dir.
- **Limits.** Body size (`http.MaxBytesReader`, checked against
  `Content-Length` first), output size, time, concurrency. `GOMEMLIMIT` is a
  soft limit: put a hard memory limit on the container (cgroup) as well.
- **No network.** The conversion path never fetches URLs (only
  `pando convert <url>` does, and the endpoint does not use it).
- **File names** are metadata only: base name, control characters removed,
  at most 200 bytes, never used as a path.

`--convert-in-process` skips the child process and so the per-conversion
start-up of a child (mostly PDFium WebAssembly initialisation; a 90 KB PDF
takes about 0.9 s end to end through a child). In-process
conversions cannot be interrupted: a timeout answers 504 but the conversion
keeps running and holds the converter lock until it ends. Use it only for
trusted input.

## Code

- `internal/convert/http.go` — `NewHTTPHandler`, `HTTPOptions`, format
  resolution, limits, responses.
- `internal/convert/runner.go` — `Runner`, `ProcessRunner` (default),
  `InProcessRunner`.
- `cmd/convert.go` — `pando convert --stdin-format <ext>` (also usable by
  hand: `cat a.pdf | pando convert --stdin-format pdf`).
- `cmd/llm_proxy.go`, `internal/llmproxy/{config,routes}.go` — the flags and
  the route, behind the proxy's auth middleware.

## Verify

```bash
go test ./internal/convert ./internal/llmproxy -run 'Convert|ProcessRunner|SanitizeFilename'

pando llm-proxy --port 11434 --api-key k --convert &
curl -s -D - -X POST http://localhost:11434/v1/convert \
  -H 'Authorization: Bearer k' -H 'Content-Type: application/pdf' \
  -H 'X-Filename: newsletter.pdf' --data-binary @newsletter.pdf
curl -s -X POST http://localhost:11434/v1/convert -H 'Authorization: Bearer k' \
  -H 'Accept: application/json' -F 'file=@report.docx'
```
