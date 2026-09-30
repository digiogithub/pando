# Remote diagnostics (telemetry)

Pando can ship its logs as JSON to a Better Stack source so you can share diagnostics with
the maintainers when something goes wrong. **Off by default** — nothing is ever sent unless
you turn it on.

Enable it from any of these:

- TUI: **Settings → General → Remote Telemetry**
- Web UI / Desktop: **Settings → General → Remote Diagnostics**
- CLI: `pando telemetry enable`
- Agent tool: `pando_setup telemetry enable` (the agent will only do this if you ask it to)

On first enable, Pando generates a random 16-digit **debug ID** (displayed grouped, e.g.
`1234-5678-9012-3456`) and shows it next to the toggle. It is kept across disable/enable so
you can quote the same ID across a whole troubleshooting session; **Regenerate** replaces it
with a fresh one at any time. The ID identifies your records in Better Stack — it is random
and is never derived from your username, hostname, machine ID or any other identifying value.

**Exactly what is sent**, per log record: `dt` (timestamp), `level`, `message`, `debug_id`,
`app` (`version`, `variant`, `os`, `arch`, `go`, `mode`), `source`, `session_id`, `attrs` (a
small map of structured fields), and `dropped` (a counter, when records had to be discarded
because the network was down). At the default `info` level, a session's per-turn tool
activity is shipped as a short summary, not the full tool input/output. Debug-level records
are only shipped when Pando's own `--debug`/`Debug` flag is also on. Every string field is
truncated (a few KiB per field, a little more per whole record) before it leaves the process.

**What is redacted before anything is sent:** any attribute whose key looks like a secret
(`*key`, `*token`, `*secret`, `*password`, `Authorization`, `Cookie`, …) is replaced with
`[REDACTED]`; values that look like a bearer token, an API key, a JWT, a URL with embedded
credentials, etc. are scrubbed the same way `pando_setup config` masks its output; and any
absolute path under your home directory is rewritten so `$HOME` becomes `~`.

**What is never sent, under any settings:** the full request/response bodies Pando logs to
disk per session (`.pando/data/**` message dumps), file contents, or anything that would let
a maintainer reconstruct your code or conversation — only the redacted, summarized log stream
described above.

```bash
pando telemetry            # same as "status"
pando telemetry status     # available / enabled / debug id / min level / endpoint host
pando telemetry status --json
pando telemetry enable     # turns it on, generates a debug id on first use, and prints it
pando telemetry disable    # turns it off, keeps the debug id
pando telemetry id         # print only the debug id (script-friendly; fails if none exists)
pando telemetry regenerate # replace the debug id with a new one
pando telemetry level warn # minimum level shipped: debug, info, warn or error
```

When reporting an issue, paste the output of `pando telemetry id` (or the ID shown in
Settings → General) into the bug report so a maintainer can find your records without you
having to paste any logs.

**Build notes** — telemetry needs an ingest token to be available at all:

- Official release binaries have the Better Stack ingest token linked in at build time via
  `-ldflags`, so telemetry is available out of the box.
- A plain `go build`/`go install`, `make build-fast`, or a fork carries no token, so
  `pando telemetry status` reports `available: no` and every toggle stays disabled/refused —
  this is expected, not a bug.
- Maintainers building locally with the real token:
  `PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build`.
- Anyone can point Pando at a **self-hosted** (or local test) ingest sink instead, without a
  custom build, via `PANDO_TELEMETRY_TOKEN` (required) and `PANDO_TELEMETRY_ENDPOINT`
  (defaults to the Better Stack host when unset). Setting `PANDO_TELEMETRY_ENDPOINT` always
  requires `PANDO_TELEMETRY_TOKEN` to be set explicitly too — the build-time token is only
  ever used against the default Better Stack endpoint, never against a custom one. A plain
  `http://` endpoint is only accepted when it points at loopback (`127.0.0.1`/`localhost`),
  which is enough to test against a local mock server; anything else must be `https://`.
