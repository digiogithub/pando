<!-- pando:agents-md:begin -->
## MANDATORY operating rules for AI agents

These rules are **MANDATORY**. They are not suggestions or best-effort guidance:
an AI agent working in this repository **MUST** follow every rule below on every
task, even small or one-line changes. When a rule conflicts with a habit or a
shortcut, the rule wins.

The rules assume the agent has access to the project's tool suite: a knowledge
base (KB), a code-indexing/remembrance subsystem, web search, the Context7
library-docs tools, and a headless browser. Use the exact tool names listed
below; if a tool is unavailable in the current environment, fall back to the
closest equivalent and say so explicitly.

### 1. MANDATORY: gather context BEFORE starting any task

Before writing code, designing, fixing, or planning anything that builds on prior
work, you **MUST** recover the relevant context first. Never start from a blank
slate when prior knowledge exists. In order:

1. **Knowledge base first.** Search the KB with `kb_search_documents` (or
   `hybrid_search_remembrances` when you also want indexed sessions and code in
   the same query). This returns BOTH the stored documentation AND everything
   saved via the memory subsystem (`remember`) in a single semantic + full-text
   query, so it is the primary entry point for recovering prior decisions, plans,
   and past fixes. Search **without** a `tags` filter for broad context recovery;
   only add `tags` when you deliberately want to narrow to one document type.
2. **Then the code index.** Use the code-remembrance tools to understand
   structure and prior decisions in the codebase before editing:
   `code_get_symbols_overview` (file/package shape), `code_find_symbol` (locate a
   specific function/type), `code_hybrid_search` (semantic search across the
   codebase and related indexed projects), and `code_search_pattern` (literal or
   regex matches). Prefer these over blind file reads when locating code.
3. **Recall only when you know the key.** Use `recall` only when you already know
   roughly which short fact/key you are after. Do not use it as a substitute for
   the KB search above.

If, after searching, no relevant context exists, state that briefly and proceed.

### 2. MANDATORY: external research when the answer is not in the repo

When the task needs knowledge that is not in the repository or the KB, you
**MUST** research it instead of guessing:

- **Library / framework / API usage** → use the Context7 tools: first
  `c7_resolve_library_id` to resolve the library name to its ID, then
  `c7_get_library_docs` to pull current, version-accurate documentation and usage
  patterns. Prefer Context7 over memory for any third-party API surface.
- **General/unknown facts, current events, error messages, release notes** → use
  web search (`google_search`, `brave_search`, or `exa_search`) and fetch the
  most relevant sources (`fetch`) to read them. Cross-check more than one source
  for anything load-bearing.
- **Frontend / web UI work** (rendering, layout, DOM, console errors, visual
  verification) → use the browser tools: `browser_navigate`, `browser_get_content`,
  `browser_evaluate`, `browser_click`, `browser_fill`, `browser_screenshot`,
  `browser_console_logs`, and `browser_network`. Verify UI behavior by actually
  driving the page, not by assuming.

### 3. MANDATORY: plan before non-trivial work

For anything larger than a trivial change you **MUST** produce a written plan
before implementing:

- Break the work into **phases**, each independently testable.
- Save the plan to the KB with `kb_add_document` under a clear `file_path` (e.g.
  `<project>/plans/<short-slug>_plan.md`) so it survives the session and can be
  recovered later with `kb_search_documents`.
- If you are unsure whether a plan already exists, search for it first and
  confirm with the user before diverging from it.
- Keep the plan updated as phases complete.

### 4. MANDATORY: implement in small, verified increments

- Write code in small, testable increments. After each increment, **run the
  tests / build** to confirm the change works before moving on.
- Match the surrounding code: naming, style, comment density, and idioms. Follow
  the language's and project's established conventions.
- Add or update tests for new behavior. Place tests where the project expects
  them.
- Never report something as done unless you verified it (tests passed, build
  succeeded, or the behavior was observed). If a step was skipped or a test
  failed, say so plainly with the evidence.

### 5. MANDATORY: document every change in the knowledge base

Every time you modify, implement, fix, or refactor anything, you **MUST** record
a summary with `kb_add_document` once the change is done. This keeps a living,
active documentation of the project and is **not optional** — it applies even to
small or one-line changes.

The summary **MUST** capture at least:

- **What changed** — a concise description of the behavior/code change.
- **Files & symbols touched** — the concrete paths and functions/types.
- **Why** — the motivation or the bug being fixed.
- **How it was verified** — tests run, build status, manual checks.

Store it under a clear `file_path`: `<project>/changes/<slug>.md`, or the matching
`<project>/fixes/<slug>.md` / `<project>/features/<slug>.md`. If a related
document already exists, **update it** instead of creating a duplicate.

> Plan-mode note: while a harness "plan mode" is active you may only edit the
> plan file, so defer the `kb_add_document` write until the plan is approved and
> writes are allowed again — but do not skip it.

### 6. Choosing the right memory tool

- **`remember` / `recall`** — ONLY for short, durable facts identified by a known
  key (e.g. `project.test_command`, `user.preferred_lang`). Call `remember` to
  upsert; call `recall` when you know roughly what key you want. Never use
  `remember` for long or structured content.
- **`kb_add_document` / `kb_search_documents`** — for any extensive or ordered
  information: plans, analyses, design notes, multi-step decisions, references.
  This is also what the mandatory pre-task search queries.

### 7. General conduct

- Use **English** for code, comments, and documentation.
- Parallelize independent work when possible, but never at the cost of losing
  context. When delegating to sub-agents, give them clear, self-contained
  instructions and the context they need.
- For actions that are hard to reverse or outward-facing, confirm first unless
  durably authorized.
<!-- pando:agents-md:end -->
