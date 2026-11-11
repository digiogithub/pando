# Knowledge Base

## Document conversion in the Knowledge Base

Pando converts rich documents to Markdown using the pure-Go
[`conductor-oss/markitdown`](https://github.com/conductor-oss/markitdown) library (no CGO; PDF
via PDFium compiled to WebAssembly). Beyond the `pando convert` command, any supported document
dropped inside the Knowledge Base directory (`[Remembrances] KBPath`) is **converted on the fly
and indexed** with its Markdown chunks, while the indexed document keeps referencing the
**original file** (its `source_path`, `source_format` and a `converted` flag are stored in
metadata). Supported KB document formats: `.pdf .docx .pptx .xlsx .xls .epub .ipynb .csv .html
.htm .rss .atom`. Plain `.md` files are still indexed verbatim.

Configure it under `[Remembrances]`:

```toml
[Remembrances]
KBConvertDocuments = true            # default; convert documents in the KB folder
# KBConvertExtensions = ["docx", "pdf", "xlsx"]   # optional: override the curated set
```

## Document dates in the Knowledge Base

Every KB file carries `created_at` and `updated_at` in its YAML front matter, and the file is
the record of a document's history. Indexing it (the startup import, the watcher, a reindex,
or a fresh index after the database was lost) stores those dates as they are, converted to
UTC; touching the file, or a git checkout or pull that only changes its mtime, does not
re-date it. A file with no `created_at` keeps the creation time already in the index (now for
a new document); a file with no `updated_at` gets the import time; `updated_at` is never
stored earlier than `created_at`.

`kb_add_document` rewrites a document's file from the file's own front matter: it keeps
`created_at`, `aliases` and any host-defined keys (`status`, `project`, ...), replaces the
tags only when new ones are given, and sets `updated_at` to now. It records the file it wrote
in the index, so the next startup import leaves that file alone.

## Filtering injected context with the decision model

Context enrichment (KB, events and code snippets added to the user message) and memory
injection pick snippets by embedding score. Two opt-in switches under `[Remembrances]` let the
shared [decision model](decision-model.md) additionally drop snippets that are similar but not
useful for the current prompt:

```toml
[Remembrances]
ContextEnrichmentDecisionFilterEnabled = false       # KB / events / code snippets (default off)
MemoryContextDecisionFilterEnabled     = false       # injected memories (default off)
ContextEnrichmentDecisionFilterThreshold = 0.60      # keep when p(useful) >= threshold
ContextEnrichmentDecisionFilterMaxCandidates = 32    # candidates judged per turn
ContextEnrichmentDecisionFilterMaxCandidateChars = 400
ContextEnrichmentDecisionFilterAllowHosted = false   # false: only local (Ollama) providers see snippets
```

The filter is fail-open (any problem keeps the unfiltered result), never drops memories from
`MemoryPinnedScopes`, keeps candidates it did not ask about and reports a notice only when it
dropped something. Measured quality and a threshold recommendation are in
[tests/decision_model/REPORT.md](../tests/decision_model/REPORT.md); details in
[decision-model.md](decision-model.md#context-relevance-filter). In the WebUI the toggles live under
Settings > Remembrances.

## Wiki links in the Knowledge Base

KB documents link each other with `[[wiki links]]`, turning the knowledge base into a
navigable graph instead of a pile of loose files. Write `[[concept]]` or
`[[concept|display label]]` anywhere in a document's body; the target may be a full path
(`[[pando/plans/foo.md]]`), a bare name (`[[foo]]`) or an alias declared in the document's
front matter (`aliases: [...]`). Occurrences inside code fences and inline code are ignored,
so documentation that merely *shows* the syntax does not pollute the graph.

Links are resolved when they are read, not when they are written, so a link to a document
that does not exist yet is **valid on purpose**: it records a concept worth documenting
later (a "wanted concept"), and it starts resolving by itself the day that document is
created. The KB tools expose the graph:

- `kb_get_document` returns the document's outgoing links and its **backlinks**.
- `kb_search_documents` reports how connected each hit is and lists the neighbours of the
  best match, so the agent can hop instead of searching again.
- `kb_related_documents` navigates the graph — with a `file_path` it returns links,
  backlinks and scored related documents; with no arguments it lists the **wanted
  concepts**, i.e. what the knowledge base refers to but never explains.
- `kb_add_document` reports how many links it indexed and which targets are still
  undocumented.

Documents stored before the graph existed are backfilled in the background at startup;
`pando kb relink [--force]` rebuilds it on demand (it costs no embeddings and never rewrites
your markdown). Toggle the feature from the TUI/WebUI settings (`Remembrances → Wiki Links`)
or in config:

```toml
[Remembrances]
KBWikiLinks = true                   # default; index [[wiki links]] as a document graph
```

Turning it off is safe and reversible: nothing new is indexed and the tools answer exactly as
they did before the graph existed, but the links already stored survive and light up again
when you turn it back on.
