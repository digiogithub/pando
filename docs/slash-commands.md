# Slash commands

## Built-in Slash Commands

Available in the TUI, the Web UI and over ACP (editors like Zed or VS Code):

| Command | What it does |
| --- | --- |
| `/goal <objective>` (alias `/autopilot`) | Start goal mode with a persistent objective; `/goal-status`, `/goal-cancel` |
| `/compact` (alias `/summarize`) | Summarize and compact the current session |
| `/db-compact` | Explains how to compact the database: `pando db compact` with every instance closed (see [database.md](database.md)) |
| `/ponytail [lite\|full\|ultra\|off]` | Toggle "lazy senior developer" mode (build less, keep the diff short) |
| `/caveman [lite\|full\|ultra]` | Answer with fewer words to spend fewer output tokens (see below) |
| `/caveman-finish` | Return the session to normal output length |
| `/superpowers [objective]` | Enable the disciplined development workflow (see below) |
| `/superpowers-finish` | Verify, report, and return to normal mode |
| `/learning [focus]` | Enable learner mode: read the KB more, document discoveries, ask questions, keep docs current (see below) |
| `/learning-finish` | Consolidate what was learned into the KB/memory and return to normal mode |
| `/improve-agents-md` | Create or reinforce AGENTS.md with the mandatory AI-agent operating rules |

### Caveman output brevity (opt-in)

`/caveman` asks Pando to say the same thing in fewer words: no greetings, no restatement of
your question, no generic transitions, no summary of what you just read. The goal is to spend
fewer **output** tokens on prose you did not ask for.

It constrains *expression*, never *work*. Code, commands, file paths, URLs, JSON/YAML/TOML,
error text, API signatures, test output, security warnings and approval questions are always
reproduced exactly — the mode may not abbreviate or paraphrase them. It may not skip
root-cause evidence, test commands and their results, or safety caveats in order to be
shorter, and it does not reduce reasoning, tool use or verification. If you ask for a detailed
explanation, you get one: a direct request always beats the brevity preference.

Three levels, from mild to extreme:

| Level | What it does |
| --- | --- |
| `lite` | Normal sentences, fewer of them: filler and restatement removed |
| `full` | Fragments over sentences: the answer, then a few lines of what matters (a bare `/caveman` picks this) |
| `ultra` | Telegraphic: the answer and nothing around it |

Replies stay in your language.

Set a global default for sessions that have not chosen a level, from the TUI/Web UI settings
(`Token Optimization → Caveman Output Brevity`) or in config:

```toml
[Caveman]
DefaultMode = ''   # default (off); or 'lite', 'full', 'ultra'
```

Scope and precedence, from strongest to weakest: **your direct instructions and project rules**
→ **the session's explicit choice** (`/caveman <level>`, or `/caveman-finish` for explicit off)
→ **`Caveman.DefaultMode`** → **off**. A session that ran `/caveman-finish` therefore stays
verbose even if the global default is on, and changing the global default never overrides a
session that already made a choice. Like Ponytail and Superpowers, the session override lives in
memory only: it does not survive a restart, while the TOML default does.

What it does *not* do: it does not reduce **input** or reasoning tokens, and the policy it
injects into the prompt has a small input cost of its own — on work whose output is already
terse, total session savings can be small or even negative. How much you save depends on the
model and the task, so Pando ships no percentage claim; measure it on your own workload.

The style levels follow [caveman](https://github.com/juliusbrussee/caveman) by Julius Brussee
(MIT), reimplemented natively in Pando as a prompt policy — no hooks, no telemetry, no stats
collection, no MCP middleware.

### Superpowers mode (opt-in)

`/superpowers` turns on a workflow policy for the current session. Long or risky work is then
routed through explicit gates instead of jumping straight to code: understand the context, present
a design and get approval, write a prioritized plan for anything multi-file (phases ordered by
risk and dependency, each with its exit criteria and verification command), implement test-first in
small increments, reproduce bugs before fixing them, and verify with real command output rather
than claims.

`/superpowers-finish` runs a closing turn that verifies the work, summarizes what changed, states
what is *not* done, and then returns the session to normal mode.

Worth knowing:

- **Opt-in and inert by default.** A session that never runs `/superpowers` behaves exactly as
  before — nothing is injected into the prompt.
- **Your instructions win.** The policy explicitly yields to direct user instructions, AGENTS.md,
  and the permission system, and it does not apply its gates to trivial or read-only requests.
- **No automatic git side effects.** Neither the mode nor the finish command will ever commit,
  merge, push, open a pull request, touch branches or worktrees, or discard work. Git stays
  user-directed.
- **Ephemeral (v1).** The mode is per-session and in-memory: it is cleared by `/superpowers-finish`
  and does not survive a restart.

It is inspired by the workflow principles of the [Superpowers](https://github.com/obra/superpowers)
plugin by Jesse Vincent (MIT), reimplemented natively in Pando — no plugin runtime, no telemetry,
no forced subagents.

### Learning mode (opt-in)

`/learning` turns on a knowledge-capture policy for the current session. With it active, Pando
treats the knowledge base and memory as a first-class part of the work rather than an afterthought:

- **Recover context first.** Before building on prior work it searches the KB (`kb_search_documents`,
  `hybrid_search_remembrances`) and reads relevant memories, instead of re-deriving what was already
  decided.
- **Ask what matters.** When a decision is genuinely yours to make, it asks — through the
  `AskUserQuestion` tool — rather than guessing.
- **Document discoveries.** Non-obvious findings are written back with `kb_add_document` (plans,
  analyses, design notes) and short durable facts with `remember`, so the next session starts ahead.
- **Keep docs honest.** When a plan, feature note, or fix write-up has been superseded, it marks the
  stale document outdated with `kb_mark_outdated` (excluded from default searches, still retrievable)
  and adds the up-to-date one, instead of leaving contradictory docs behind.

`/learning [focus]` takes an optional focus to steer what to learn; `/learning-finish` runs a closing
turn that consolidates what was learned into the KB/memory and returns the session to normal mode.

Worth knowing:

- **Opt-in and inert by default.** A session that never runs `/learning` behaves exactly as before —
  nothing is injected into the prompt.
- **Depth, not verbosity.** Learning governs how much Pando *documents and asks*, which is
  independent from output brevity: it composes cleanly with `/caveman`, which only shortens chat
  prose. You can run both at once.
- **Your instructions win.** The policy yields to direct user instructions, AGENTS.md, and the
  permission system.
- **No automatic git side effects.** Neither the mode nor the finish command commits, pushes, or
  otherwise touches git.
- **Ephemeral.** The mode is per-session and in-memory: it is cleared by `/learning-finish` and does
  not survive a restart.

## Custom Commands

Custom commands are predefined prompts stored as Markdown files:

1. **User Commands** (prefixed with `user:`): `$XDG_CONFIG_HOME/pando/commands/` or `$HOME/.pando/commands/`
2. **Project Commands** (prefixed with `project:`): `<PROJECT DIR>/.pando/commands/`
