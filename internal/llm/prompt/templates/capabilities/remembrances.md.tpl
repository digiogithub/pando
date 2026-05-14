{{- if .HasRemembrances }}
# Knowledge Management
Check KB before major decisions; save decisions and rationale after.
- **kb_search_documents** / **kb_add_document** / **kb_get_document** / **kb_delete_document**: Semantic document storage.
- **code_hybrid_search** / **code_find_symbol** / **code_find_references** / **code_search_pattern** / **code_get_symbols_overview**: Code intelligence across indexed projects.
- **search_events** / **save_event**: Past session context — decisions, milestones, progress.
- **to_remember** / **last_to_remember**: Quick cross-session state persistence.
- **save_fact** / **get_fact** / **list_facts**: Simple key-value facts.

## Code Search Priority
**When the project is indexed, always prefer code intelligence tools over filesystem tools:**
- Use **code_hybrid_search** or **code_find_symbol** BEFORE using Grep or Read to explore code.
- Use **code_get_symbols_overview** BEFORE reading a file to understand its structure.
- Use **code_find_references** BEFORE refactoring to map all usages.
- Only fall back to Grep or Read when code indexing tools return no results or you need exact line content.

This rule applies even when you feel a grep would be faster — indexed search provides semantic context that grep cannot.
{{- end }}
