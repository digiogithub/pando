---
id: PANDO-US-0023
type: story
title: "Token provisioning without stdout scraping: PANDO_AGUI_TOKEN and --token-file"
status: backlog
priority: medium
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [agui, ops, security]
estimate: 3
created: 2026-09-13T21:16:01Z
updated: 2026-09-13T21:16:01Z
---

## Description

As an operator writing a systemd unit or a container spec, I want `agui-serve` to take its token from an environment variable or a file, so that I do not have to scrape it from stdout and it never lands in the journal.

Today `cmd/agui_serve.go` accepts `--token`; otherwise it mints a random 32-byte hex at startup (`cmd/agui_serve.go:99-104` and the `randomToken` helper) and **prints it to stdout** (`fmt.Printf("Token:   %s\n", token)` in the startup banner). `--no-token` disables auth with a warning. Worse for a reader of `--help`: `PANDO_AGUI_TOKEN` appears only inside the `Example:` string at `cmd/agui_serve.go:46`, where it is *shell* expansion — the binary never reads it, so the help text implies an env var that does not exist.

Implement, roughly 20 LOC plus tests:

- read `PANDO_AGUI_TOKEN` from the environment;
- add `--token-file <path>`, reading the file and trimming trailing whitespace/newline; a missing or unreadable file is a startup error, not a silent fallback to a generated token;
- precedence: `--token` > `--token-file` > `PANDO_AGUI_TOKEN` > generated;
- reject an empty token from any explicit source rather than falling through;
- never log the token: keep it out of `logging` calls and out of the startup config dump (`internal/agui/runtime.go:77-85` logs the resolved config at startup — verify the token is not in it). Print the token on stdout **only** when it was generated, since that is the only case where the operator has no other way to learn it;
- fix the `--help` text so the `Example:` and the flag descriptions match what the binary actually reads.

Do NOT touch the co-mounted server's token path (`internal/api`'s per-process token and `GET /api/v1/token`) — this story is `agui-serve` only. Do NOT change `--no-token` semantics.

## Acceptance Criteria

- [ ] `PANDO_AGUI_TOKEN=x pando agui-serve` authenticates with `x`; `--token-file` with a file containing `x` (with or without a trailing newline) does the same.
- [ ] Precedence `--token` > `--token-file` > env > generated is covered by a table test in `cmd/`.
- [ ] A `--token-file` pointing at a missing or unreadable path fails startup with a named error instead of generating a token.
- [ ] An explicitly supplied but empty token is a startup error.
- [ ] The token appears in no log line at any level, including the startup config dump — asserted by a test capturing log output; it is printed to stdout only in the generated case.
- [ ] `pando agui-serve --help` documents `--token`, `--token-file` and `PANDO_AGUI_TOKEN` consistently with the implementation, and the `Example:` no longer implies an unread env var.

## Notes

Size: S. Independent of the other stories in PANDO-EP-0004; can ship in any order. Evidence: `report-agui-server.md` §4, Token provisioning. Related but explicitly out of scope: the unauthenticated MCP HTTP transport on :9777, which is its own epic.
