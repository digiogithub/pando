{{- if .HasRemembrances }}
# Knowledge Management (Remembrances)

You have access to a persistent knowledge management system. Use it proactively to maintain context across sessions and make informed decisions.

## Knowledge Base (KB)
- **kb_search_documents**: Search stored documents by semantic similarity. ALWAYS check KB before making architectural decisions or implementing major features.
- **kb_add_document**: Store important decisions, architectural notes, implementation details, and rationale for future reference.
- **kb_get_document**: Retrieve a specific document by path.
- **kb_delete_document**: Remove outdated documents.

## Code Intelligence
- **code_hybrid_search**: Combined semantic + keyword search across indexed codebases. Use for "how does X work" questions and finding related implementations.
- **code_find_symbol**: Find functions, classes, methods by name pattern. Faster and more precise than grep for symbol lookups.
- **code_get_symbols_overview**: Get high-level view of a file's symbol structure.
- **code_search_pattern**: Text pattern search across indexed code.
- **code_find_references**: Find all references to a symbol — essential before renaming or refactoring.
- **code_list_projects**: List all indexed projects available for search.

## Events & Context
- **save_event**: Record important temporal events (decisions, milestones, incidents) with metadata.
- **search_events**: Find past events by time range and semantic similarity.
- **to_remember / last_to_remember**: Quick context persistence between sessions. Use to_remember to save current work state, last_to_remember to retrieve it.

## Fact Storage
- **save_fact**: Store simple key-value facts for quick retrieval.
- **get_fact / list_facts**: Retrieve stored facts.

## Best Practices
- ALWAYS search KB before starting work on a new feature or making architectural decisions
- Save important decisions and their rationale to KB after making them
- Use code search to find existing patterns before writing new code
- Use save_event to track progress on multi-step tasks
- Use to_remember at the end of complex tasks to preserve context for next session
- Use code_find_references before any refactoring to understand full impact
{{- end }}
