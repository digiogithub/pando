# Output Compression Filters

Pando shrinks verbose command output before it reaches the model, cutting token
usage on noisy tools (test runners, builds, installers, linters) by roughly
60-90%. This is an RTK-inspired ([rtk-ai/rtk](https://github.com/rtk-ai/rtk))
mechanism that runs at the `bash` tool output boundary.

Guarantees:

- **Fail-safe** — any parse/regex error returns the raw output unchanged. A
  filter can never block a command or swallow its result.
- **Exit-code preserving** — only the text shown to the model is altered, never
  exit codes or control flow.
- **Conservative** — built-in filters strip guidance/noise and summarise success
  states, but never drop error, failure or warning lines.
- **On by default** — it is additive and fail-safe, so no prompting changes are
  needed. Opt out with `Bash.OutputFilterDisabled = true` or the `Bash → Output
  Filter` toggle in the TUI/WebUI settings.

## Two tiers

Output is processed by two complementary mechanisms, in this order:

1. **Native structured parsers** (Go code, first tier) for commands whose output
   is not line-oriented. Each applies RTK's 3-tier degradation: **Full**
   (structured parse → failure-focused summary) → **Degraded** (regex grep) →
   **Passthrough** (raw). Built-ins:
   - `go-test-json` — `go test … -json` (NDJSON → only failing tests + build
     errors + a `SUMMARY:` line). Plain `go test` (no `-json`) is handled by the
     TOML `go-test` filter instead.
   - `golangci-lint` — JSON `Issues` grouped per linter.
   - `tsc` — TypeScript diagnostics (`file(line,col): error TSxxxx`) grouped per
     file.
   A parser that matches a command but can only passthrough reports
   "not applied" and does **not** fall through to the TOML filters.

2. **Declarative TOML filters** (second tier) — an 8-stage line pipeline matched
   to a command by regex. This is the tier you extend without writing Go.

## Filter sources & precedence

Loaded high-precedence first; the **first** filter whose `match_command` matches
a command wins:

1. Project-local `.pando/filters.toml` (relative to the working directory).
2. User-global files listed in `Bash.OutputFilterPaths`.
3. Embedded built-in defaults (15 filters across git, docker, cargo, go,
   gradle/maven, npm/pnpm/yarn, bun, deno, swift, pip, pytest).

So a project or user filter that matches a command overrides the built-in for
that command.

## Filter schema

Each filter is a `[filters.<name>]` table. **Pattern fields are regexes** — use
TOML literal strings (`'...'`) so backslashes need no escaping.

| Field | Type | Purpose |
|-------|------|---------|
| `description` | string | Human note (not used at runtime). |
| `match_command` | regex (**required**) | Tested against the full command string; the filter applies on a match. |
| `strip_ansi` | bool | Remove ANSI escape sequences first. |
| `replace` | array of `{pattern, replacement}` | Chained per-line regex substitutions. |
| `match_output` | array of `{pattern, message, unless?}` | Whole-blob short-circuit: if `pattern` matches (and `unless` does not), return `message` and stop. |
| `strip_lines_matching` | array of regex | Drop lines matching any pattern. |
| `keep_lines_matching` | array of regex | Keep **only** lines matching any pattern. Mutually exclusive with `strip_lines_matching`. |
| `truncate_lines_at` | int | Cap each line's length (characters). |
| `head_lines` | int | Keep the first N lines. |
| `tail_lines` | int | Keep the last N lines. |
| `max_lines` | int | Absolute cap on total lines. |
| `on_empty` | string | Message to emit if the output became empty. |

### Pipeline order (per matched filter)

1. `strip_ansi`
2. `replace[]`
3. `match_output[]` (short-circuit)
4. `strip_lines_matching[]` / `keep_lines_matching[]`
5. `truncate_lines_at`
6. `head_lines` / `tail_lines`
7. `max_lines`
8. `on_empty`

## Inline self-tests

Embed `[[filters.<name>.tests]]` cases so your filter is verifiable. Each case
declares an `input` and the `expected` compressed output (trailing newlines are
ignored in the comparison):

```toml
[filters.echo-demo]
description = "demo: drop blank lines from echo output"
match_command = '^echo\b'
strip_lines_matching = ['^\s*$']

[[filters.echo-demo.tests]]
name = "drops blank lines"
input = "hello\n\nworld\n"
expected = "hello\nworld"
```

Validate them with the CLI:

```bash
pando filter test .pando/filters.toml   # your authoring file
pando filter test                       # the built-in defaults
```

The command runs every filter's inline tests through its pipeline, prints
`PASS`/`FAIL` per case with a got/want diff on failure, and exits non-zero if any
case fails — suitable for CI.

## A worked example

The built-in `git-status` filter collapses a clean tree to one line and drops the
`(use "git …")` hints:

```toml
[filters.git-status]
description = "Compact git status."
match_command = '(^|\s)git\s+status(\s|$)'
strip_ansi = true
match_output = [
  { pattern = 'nothing to commit, working tree clean', message = "git status: clean (working tree clean, nothing to commit)" },
]
strip_lines_matching = [
  '^\s*\(use "git ',
  '^\s*$',
]
```

## Tips

- Be **conservative**: never strip lines that could carry an error, failure or
  warning. Prefer dropping known-noise patterns over `keep_lines_matching` unless
  you are sure what to keep.
- Anchor `match_command` carefully (`(^|\s)tool\b`) so you don't accidentally
  claim unrelated commands.
- Always ship inline `[[tests]]` and run `pando filter test` — a filter without
  tests is easy to get subtly wrong.
- Changes to `.pando/filters.toml` and `Bash.OutputFilterPaths` are picked up on
  the next engine load; the on/off toggle (`OutputFilterDisabled`) takes effect
  immediately.
