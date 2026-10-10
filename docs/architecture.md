# Architecture

- **cmd**: Command-line interface using Cobra
- **internal/app**: Core application services
- **internal/config**: Configuration management
- **internal/db**: Database engine (multi-writer SQLite, see [database.md](database.md)), queries and migrations
- **internal/llm**: LLM providers and tools integration
- **internal/tui**: Terminal UI components and layouts
- **internal/logging**: Logging infrastructure
- **internal/message**: Message handling
- **internal/session**: Session management
- **internal/lsp**: Language Server Protocol integration
