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
