# LLM proxy

`pando llm-proxy` serves every configured provider through an OpenAI- and
Anthropic-compatible HTTP API (default `localhost:11434`).

## API key

Authentication is optional. When a key is configured, every route except
`/health` requires `Authorization: Bearer <key>` or `x-api-key: <key>`. The
key is compared in constant time (both sides are hashed with SHA-256 first, so
neither content nor length leaks through timing).

The key comes from the first of these that is set:

1. `--api-key <key>`
2. `--api-key-file <path>`: read once at start, one trailing newline trimmed;
   an unreadable or empty file is a startup error.
3. Environment variable `PANDO_LLM_PROXY_API_KEY`.

Passing both `--api-key` and `--api-key-file` is an error. A key given with
`--api-key` is visible to other local users in the process arguments (`ps`,
`/proc/<pid>/cmdline`); prefer the file or the environment variable.

```sh
pando llm-proxy --api-key-file /run/secrets/proxy_key
PANDO_LLM_PROXY_API_KEY="$KEY" pando llm-proxy
```
