## Pando is an AI assistant designed to improve the workflow of software developers

Whis project based in golang, is a fork of a archived project called "openproject" (different to OpenProject software based in Nodejs) and continued by Charmbracelet Crush changing the licence. This fork continues with MIT license and is open to contributions.

The project is indexed with the latest code remembrance tools under the name "pando." You also have other indexed projects that will be used as a basis for improvements.

## Get the most performance using the tools

You have remembrance tools for storing and retrieving context and information about the project, its structure, and previous decisions. Use the code remembrance tools (`code_find_symbol`, `code_get_symbols_overview`, `code_hybrid_search`, `code_search_pattern`) to search symbols, functions, and run hybrid (semantic) search across the codebase and related projects that pando reimplements or is based on. For the remembrance tools, always use "pando" as the user_id or project_id to store and retrieve relevant information for this project.

### MANDATORY: search before any task that needs prior context

Before starting any task that requires additional context or previous knowledge (a feature, fix, decision, or anything that builds on earlier work), you MUST first search the knowledge base with `kb_search_documents` (or `hybrid_search_remembrances` when you also want indexed sessions/code). The KB hybrid search retrieves BOTH the stored documentation AND everything saved with the memory subsystem (`remember`) in a single semantic + full-text query, so it is the primary entry point for recovering prior context. Do not rely on `recall` alone for this — use the KB search. For this context recovery, search WITHOUT a `tags` filter so any related document can surface; the `tags` argument is only for narrowing a search to a specific document type when you deliberately want to segment results.

### MANDATORY: document after any modification or implementation task

Every time you modify, implement, fix, or refactor anything in the software, you MUST record a summary of the work with `kb_add_document` once the change is done (and after exiting plan mode, when file/system writes are allowed again). This is required to keep generating active, living documentation of the project — it is not optional and applies even to small or one-line changes.

The summary document must capture at least: what was changed, the files/symbols touched, the reason/motivation, and how it was verified (tests, build, manual checks). Store it under a clear `file_path` (e.g. `pando/changes/<short-slug>.md` or the matching `pando/fixes/…` / `pando/features/…` path). If a related document already exists, update it instead of creating a duplicate. Use `remember` only for short key-identified facts, never as a substitute for this summary.

Note on plan mode: while the harness plan mode is active you may only edit the plan file, so defer the `kb_add_document` write until the plan is approved and you are back in normal mode — but do not skip it.

### When to store, and with which tool

- **`remember` / `recall`** — use ONLY for short, durable facts identified by a specific known key (e.g. `user.preferred_lang`, `project.test_command`). Call `remember` with that `key` to upsert; call `recall` only when you already know roughly what key/fact you are after. Do not use `remember` for long or structured content.
- **`kb_add_document` / `kb_search_documents`** — use whenever you need to remember more extensive and ordered information: plans, analyses, design notes, multi-step decisions, references. Store it as a KB document (chunked + embedded) under a clear `file_path`; `tags` are optional and only useful to later segment a search by document type — they do not need to be set for the document to be found by an untagged context search. This is also what the mandatory pre-task search above queries.

Parallelize tasks when possible, but always ensure that you are not losing context or important information. Use the tools of `spawn_agent` and the rest of mesnada tools to delegate tasks when needed, but always ensure that you are providing clear instructions and context to the agents you are delegating to. When use the engine "claude", use the model "sonnet" for programming tasks and the model "opus" for planning and very harder tasks that require more reasoning and understanding of the context. Use preferently the engine "copilot" for programming tasks with the model "gpt-5.4" and "gpt-5-mini" for translations and very simple tasks.

### Development Workflow

- **Language**: Use always english for code, comments, and documentation.
- **Context Awareness**: Always be aware of the current context of the project. Before working on anything that builds on prior knowledge, retrieve it with `kb_search_documents` / `hybrid_search_remembrances` (these return both KB documentation and stored memories), and use the code indexing tools (`code_find_symbol`, `code_hybrid_search`, etc.) for the project's structure and previous decisions.
- **Planning**: If you are unsure about whether you are following a plan, find the current plan with `kb_search_documents` and confirm with the user if needed, before proceeding. If you don't have a plan, create one, split into phases, and save it as a document in the kb with `kb_add_document`.
- **Implementation**: Write code in small, testable increments. After each increment, run tests to ensure functionality. When the change is complete, you MUST save a summary of the work with `kb_add_document` (see "MANDATORY: document after any modification or implementation task").
- **Code Style**: Follow Go best practices and project-specific coding standards
- **Testing**: Create tests in `tests/` folder (Python files, not in root)
- **Verified commands**:
  - Agent/API targeted tests: `go test ./internal/llm/agent ./internal/api`
- **Documentation**: Update documentation as needed, especially if you are adding new features or making significant changes. Use always the kb tools (`kb_add_document`) to save any extensive or structured information about the project, the changes you are making, and the reasons behind those changes; reserve `remember` for short key-identified facts.

### External Research

- Use web search (google/brave) for additional information when needed
- Use Context7 for API documentation and library usage patterns
