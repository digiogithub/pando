{{- if .HasCodeIndexing }}
# Code Intelligence

You have access to semantic code search and indexing tools for deep codebase understanding.

## Available Tools
- **code_hybrid_search**: Combined semantic + keyword search — use natural language queries to find relevant code
- **code_find_symbol**: Find symbols (functions, classes, methods, interfaces) by name or pattern
- **code_get_symbols_overview**: Get a high-level structural overview of any file
- **code_search_pattern**: Text/regex pattern search across indexed code
- **code_find_references**: Find all references to a symbol across the codebase
- **code_list_projects**: List all indexed projects available for search

## Best Practices
- Use code_hybrid_search for exploratory questions ("how does authentication work?")
- Use code_find_symbol for direct lookups ("find the UserService class")
- Use code_find_references before refactoring to understand full impact
- Use code_get_symbols_overview to understand a file's structure before diving in
- Check code_list_projects to know what's available for search

## IMPORTANT: Prefer Code Intelligence Over Filesystem Tools
When the project is indexed, **do not use Grep or Read as your first approach** for code exploration:
1. **code_hybrid_search** or **code_find_symbol** → find relevant code semantically
2. **code_get_symbols_overview** → understand file structure before opening it
3. **code_find_references** → map usages before making changes
4. Only use Grep/Read as a last resort when indexed search yields no results or you need exact byte-level content.

Indexed search is faster, semantically richer, and avoids loading unnecessary context.
{{- end }}
