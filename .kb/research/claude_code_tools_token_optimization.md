<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# in Claude Code when a tool returns more than 15k tokens or words it returns to a file indicating it has been dumped to a temporary file to be read, I've also seen that for long files it generates temporary files where it numbers each line. Can you explain to me and investigate why it does this and how that optimization mechanism works?

Here is a detailed explanation of the context optimization mechanism used by Claude Code:

## The underlying problem: the context window

Claude Code operates within a finite context window (200k tokens in standard models) [^1_1]. Every time a tool returns data —Bash command output, file contents, search results— those data are consumed directly from the session's token budget. Without optimization, a single `cat large_file.log` could collapse the context before writing a single line of code [^1_2].

## 2000-line limit in the `View` tool

The `View` tool (file reading) has a **hard-coded limit of 2000 lines per read** [^1_3]. As can be seen in Claude Code's internal prompt, the official tool description literally says:

> *"By default, reads up to 2000 lines starting from the beginning of the file."* [^1_4]

This limit is not configurable by the user [^1_3]. When Claude needs to read a longer file, it does so in **sequential chunks** using line offsets, reading ranges like lines 1–2000, then 2001–4000, etc.

## Why it numbers lines

Claude Code prepends line numbers to file contents as a **precision anchoring** mechanism. This serves two purposes:

- **Surgical edits**: the `Edit` and `Replace` tools need exact line references to make changes without rewriting the entire file.
- **Reduce location errors**: without numbering, Claude tends to miscount lines in diff hunks, pointing to the wrong line with ~40% error; with numbering accuracy rises to ~85% [^1_5].


## Why it dumps large outputs to temporary files

When a tool (typically `Bash`) returns more than a token threshold (~15k), Claude Code uses the **temporary file** mechanism instead of injecting the content directly into the context [^1_6]. The logic is:

1. The output is written to a temporary file on disk (e.g. `/tmp/claude-output-XXXX`)
2. In the context only a **reference message** is inserted indicating the file path
3. Claude can then selectively read sections of the file with `View` + offsets, consuming only the actually needed tokens

This avoids the problem described in GitHub issues where massive tool outputs (like compiler traces) caused direct context overflow [^1_6][^1_7].

## The complete context management cycle

| Situation | Behavior |
| :-- | :-- |
| File ≤ 2000 lines | Reads it completely with line numbers |
| File > 2000 lines | Reads in chunks of 2000 lines with offsets |
| Tool output > ~15k tokens | Dumps to temporary file, inserts reference in context |
| Context at 75% capacity | Triggers **auto-compact** to summarize history [^1_2] |

## Practical implications for your projects

If you work with large files in Go/Rust, Claude will use `grep`/`head`/`tail` via Bash as a more efficient alternative to chunked `View`, since it allows extracting only relevant lines without saturating the context [^1_8]. For your own tool outputs (MCP servers), if you want to avoid dumping to temporary and have Claude see the content directly, keep responses below that ~15k token threshold or structure the output with explicit pagination.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_16][^1_17][^1_18][^1_19][^1_20][^1_21][^1_22][^1_23][^1_24][^1_25][^1_26][^1_27][^1_28][^1_29][^1_30][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://www.datastudios.org/post/claude-long-context-window-and-handling-of-very-large-documents-context-size-practical-strategies

[^1_2]: https://hyperdev.matsuoka.com/p/how-claude-code-got-better-by-protecting

[^1_3]: https://www.reddit.com/r/ClaudeAI/comments/1lg17nc/how_to_allow_claude_code_to_read_large_files/

[^1_4]: https://kirshatrov.com/posts/claude-code-internals

[^1_5]: https://www.reddit.com/r/ClaudeAI/comments/1qfrb48/getting_claude_to_output_accurate_line_numbers_in/

[^1_6]: https://github.com/anthropics/claude-code/issues/805

[^1_7]: https://github.com/anthropics/claude-code/issues/12054

[^1_8]: https://www.reddit.com/r/ClaudeAI/comments/1lmxzhp/how_are_you_handling_projects_that_are_too_big/

[^1_9]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/

[^1_10]: https://www.youtube.com/watch?v=TcTKbF6rWBw

[^1_11]: https://github.com/anthropics/claude-code/issues/14723

[^1_12]: https://www.reddit.com/r/ClaudeAI/comments/1n39xro/claude_code_truncates_long_maven_outputs_whats/

[^1_13]: https://platform.claude.com/docs/en/agents-and-tools/tool-use/implement-tool-use

[^1_14]: https://github.com/openai/codex/issues/5913

[^1_15]: https://github.com/anthropics/claude-code/issues/11348

[^1_16]: https://forum.langchain.com/t/filemanagement-toolkits-file-write-doesnt-work-with-claude-sonnet-3-7-4-for-slightly-big-files/1077

[^1_17]: https://www.reddit.com/r/ClaudeAI/comments/1lypm28/claude_codes_tiny_context_window_is_driving_me/

[^1_18]: https://github.com/anthropics/claude-code/issues/2638

[^1_19]: https://github.com/anthropics/claude-code/issues/10448

[^1_20]: https://code.claude.com/docs/en/settings

[^1_21]: https://sankalp.bearblog.dev/my-experience-with-claude-code-20-and-how-to-get-better-at-using-coding-agents/

[^1_22]: https://www.scribd.com/document/953839474/Claude-Code-CLI-Large-Document-Solutions

[^1_23]: https://www.datastudios.org/post/claude-ai-file-upload-and-reading-supported-formats-context-handling-structured-comprehension

[^1_24]: https://www.youtube.com/watch?v=S3SnmD0YEhU

[^1_25]: https://www.youtube.com/watch?v=5Khch9rlg4A

[^1_26]: https://code.claude.com/docs/en/cli-reference

[^1_27]: https://gist.github.com/bgauryy/0cdb9aa337d01ae5bd0c803943aa36bd

[^1_28]: https://www.producttalk.org/how-to-use-claude-code-safely/

[^1_29]: https://news.ycombinator.com/item?id=46098838

[^1_30]: https://github.com/anthropics/claude-code/issues/16130


---

# are there more reviews of internal tools in Claude Code's system prompt that have optimization details?

Yes. In those leaked/gists of the internal prompt there are **quite a few** additional hints about optimization, not just the temporary file dump. The clearest thing is that Claude Code is designed to **save context, reduce unnecessary exploration and parallelize work** whenever possible. [^2_1]

## Clear hints

In the `Bash` block it appears that, if the output exceeds **30000 characters**, it's truncated before being returned to the model, confirming an explicit context protection limit in that tool. [^2_1] It's also insisted on not using `cat`, `head`, `tail`, `grep`, `find`, `sed`, `awk` or `echo` when dedicated tools exist, because those tools seem to be more controlled and return more compact and predictable results. [^2_1]

In `Read` another strong optimization is documented: by default it reads only **2000 lines**, truncates lines over **2000 characters**, and returns content in `cat -n` format, i.e., with line numbers. [^2_1] This reduces raw volume, allows partial reads with `offset` and `limit`, and improves subsequent `Edit` and `MultiEdit` precision, which depend on exact matches. [^2_1]

## Search and exploration

The `Glob` and `Grep` tools are described as "fast" tools that work well even with large codebases, and it's recommended to use them instead of equivalent shell commands. [^2_1] Additionally, in another version of the prompt there's strong push for using `Task` or `Explore` type agents for open searches, with the explicit reason of **reducing context usage** and delegating iterative exploration out of the main thread. [^2_1]

There are also instructions to make **parallel** calls when there are no dependencies between them. [^2_1] This doesn't save tokens directly, but reduces agent-tool rounds and prevents the model from having to "remember" for too many turns searches that could be resolved at once. [^2_1]

## Cache and background

`WebFetch` mentions a **15-minute self-cleaning cache**, designed for repeated access to the same resource without redoing all the work. [^2_1] In the variant with `Task` and `TaskOutput`, another optimization pattern is also seen: allowing **background** agents with an `output_file`, to not keep putting all execution and its output continuously in the main context. [^2_1]

This fits very well with what you saw about temporary files: the system externalizes large results to disk and then only re-incorporates fragments or summaries when needed. [^2_1]

## What this tells us about the design

The general pattern seems to be: **minimum main context, deferred access to detail, and specialized tools with hard limits**. [^2_1] That's why we see line-based chunking, character truncation, line numbering, explorer agents, background execution, temporary cache and preference for structured tools over raw Bash. [^2_1]

A practical example: instead of putting 50k lines of log in the prompt, Claude Code prefers to save the output, show the model only a reference or a part, and then let it request the specific ranges it needs. [^2_1]

## What's less certain

What **does** seem well supported by the prompt is the `Read` limit, `Bash` truncation, preference for specific tools, use of agents for open exploration and `WebFetch` cache. [^2_1] What's harder to assert with absolute certainty from just these sources is the exact **token** threshold for the temporary file dump, because in the accessible texts what's better seen is the **character** limit and the general strategy than the exact internal implementation. [^2_2][^2_1]

If you want, in the next step I'll make you a **tool-by-tool table** with all the internal optimizations inferable from Claude Code's prompt.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15][^2_16][^2_17][^2_3][^2_4][^2_5][^2_6][^2_7][^2_8][^2_9]</span>

<div align="center">⁂</div>

[^2_1]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/

[^2_2]: https://github.com/anthropics/claude-code/issues/805

[^2_3]: https://gist.github.com/chigkim/1f37bb2be98d97c952fd79cbb3efb1c6

[^2_4]: https://arize.com/blog/claude-md-best-practices-learned-from-optimizing-claude-code-with-prompt-learning/

[^2_5]: https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices

[^2_6]: https://www.reddit.com/r/ClaudeAI/comments/1e39tvj/sonnet_35_coding_system_prompt_v2_with_explainer/

[^2_7]: https://www.reddit.com/r/ClaudeAI/comments/1o66m77/understanding_claude_codes_3_system_prompt/

[^2_8]: https://gist.github.com/wong2/e0f34aac66caf890a332f7b6f9e2ba8f

[^2_9]: https://simonwillison.net/2025/May/25/claude-4-system-prompt/

[^2_10]: https://mikhail.io/2025/09/sonnet-4-5-system-prompt-changes/

[^2_11]: https://weaxsey.org/en/articles/2025-10-12/

[^2_12]: https://code.claude.com/docs/en/best-practices

[^2_13]: https://generativeai.pub/mind-reading-claude-ais-complete-system-prompt-bb498276fe3d

[^2_14]: https://claudefa.st/blog/guide/mechanics/interactive-mode

[^2_15]: https://dejan.ai/blog/claude-system-internals/

[^2_16]: https://www.reddit.com/r/ClaudeAI/comments/1r9tmct/claude_code_works_because_of_bash_noncoding/

[^2_17]: https://www.anthropic.com/engineering/advanced-tool-use


---

# go ahead, make me a table

Yes; from the internal prompt several quite concrete optimizations can be extracted. [^3_1]

## Table

| Tool / mechanism | Observed detail | What it actually optimizes | Design signal |
| :-- | :-- | :-- | :-- |
| `Bash` | Output is **truncated if it exceeds 30000 characters**. [^3_1] | Prevents huge logs, builds or command dumps from filling the context in one go. [^3_1] | There's a hard limit on output serialization. [^3_1] |
| `Bash` vs dedicated tools | The prompt says to **avoid** `find`, `grep`, `cat`, `head`, `tail`, `ls` and use `Glob`, `Grep`, `Read`, `LS`. [^3_1] | Reduces noise, makes outputs more structured and cheaper for the model to consume. [^3_1] | They prioritize tools with controlled semantics over free shell. [^3_1] |
| `Read` | By default reads only **2000 lines**, allows `offset` and `limit`. [^3_1] | Native chunking of long files to read only the necessary portion. [^3_1] | Incremental access instead of complete ingestion. [^3_1] |
| `Read` | Lines over **2000 characters** are truncated. [^3_1] | Protects against minified files, giant JSON, blobs or pathological lines. [^3_1] | There are defenses both by line count and line length. [^3_1] |
| `Read` | Returns in `cat -n` format, with **line numbers**. [^3_1] | Facilitates precise edits and robust references in `Edit`/`MultiEdit`. [^3_1] | The system is optimized for locating and patching, not just reading. [^3_1] |
| `Edit` | Requires having done `Read` before, and demands exact text match. [^3_1] | Prevents "blind" edits and reduces replacement errors from insufficient context. [^3_1] | The flow is designed as read-then-patch. [^3_1] |
| `MultiEdit` | Multiple edits in a single operation, applied sequentially and **atomically**. [^3_1] | Fewer tool/model rounds and fewer re-reads of the same file. [^3_1] | Tries to compact multiple changes into a single transaction. [^3_1] |
| `Glob` | Defined as **fast** pattern search that works with codebases of any size. [^3_1] | Find candidates by name without manually traversing the tree with shell. [^3_1] | Indexed or at least more optimized search than generic Bash. [^3_1] |
| `Grep` | Defined as **fast** content search over large codebases. [^3_1] | Discover relevant files without opening them all or filling context. [^3_1] | Favors path retrieval over content dumping. [^3_1] |
| `Agent` / `Task` | For open searches recommends delegating to a separate agent and launching them **concurrently**. [^3_1] | Takes iterative exploration out of the main thread and reduces local context rounds. [^3_1] | Externalization of exploratory work and parallelization. [^3_1] |
| `Agent` / `Task` | Each agent is **stateless** and returns a single final report. [^3_1] | Prevents long histories per subagent and forces compact results. [^3_1] | Design oriented toward context compression per subtask. [^3_1] |
| `WebFetch` | Can **summarize** results if content is very large. [^3_1] | Doesn't put whole pages in context when a guided extraction suffices. [^3_1] | Reading mediated by a small/fast model. [^3_1] |
| `WebFetch` | Has a **15-minute self-cleaning cache**. [^3_1] | Avoids recomputing fetch + parse + summary on repeated access. [^3_1] | There's temporal optimization in addition to contextual. [^3_1] |
| `TodoRead` / `TodoWrite` | The prompt insists on using them very frequently. [^3_1] | Externalizes plan and work state outside the free conversational text. [^3_1] | Part of the operational memory is moved to a summarized structure. [^3_1] |
| Response style | The prompt orders **minimize tokens**, respond short and avoid preambles. [^3_1] | Less context window spending on the model's own messages. [^3_1] | Optimization isn't just in tools; also in output policy. [^3_1] |

## Technical reading

The common pattern is that Claude Code tries to work with **references, ranges, matches and summaries**, not complete blobs. [^3_1] This explains why you see output truncation, partial reading, line numbering, exact editing and delegation to agents for broad searches. [^3_1]

It's also seen that there are two levels of optimization: one of **structured I/O** (`Read`, `Glob`, `Grep`, `MultiEdit`, `WebFetch`) and another of **conversational discipline** (short responses, stateless agents, use of todos for operational memory). [^3_1]

## What to infer with caution

The table above captures things that do appear quite directly in the published prompt. [^3_1] What's more inferential is whether behind there are local indexes, mmap memory, temporary files per specific tool or exact tokenized thresholds different from the character/line limits visible in the prompt. [^3_1]

If you want, the next step I'll make as an **inferred internal architecture map**: `Bash/Read -> truncation -> temporary reference -> selective re-read -> exact edit`.

<div align="center">⁂</div>

[^3_1]: https://www.reddit.com/r/ClaudeAI/comments/1ohjjwq/claude_now_truncates_output_when_working_with/
