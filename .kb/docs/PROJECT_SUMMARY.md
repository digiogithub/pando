# Pando Project - Code Intelligence Assistant

## Summary
Pando is an AI-powered terminal-based coding assistant built in Go that helps software developers with natural language interactions directly in the CLI/TUI.

## Core Capabilities
- **TUI Interface**: Built on Bubble Tea for smooth terminal experiences  
- **Multi-model Support**: OpenAI, Anthropic Claude, Google Gemini, AWS Bedrock, Groq, Azure, VertexAI, Ollama self-hosted
- **File Editing & Execution**: AI can edit files through remembrances tools (edit, patch)
- **Shell Tool Integration**: Execute commands safely with validation  
- **File Operations**: ls, glob, grep for filesystem navigation  
- **Code Navigation**: Symbol search, reference finding, LSP integration
- **RAG System**: Semantic indexing of code using tree-sitter and embeddings
- **MCP Gateway**: Proxy/management layer for MCP servers across multiple languages/environments

## Architecture Highlights
Pando follows a clean architecture with separation between:
- CLI commands (Cobra)
- Domain services in `internal/`
- TUI components separate from business logic  
- ACP server implementation following the Agent Client Protocol spec

**Key packages:**
go.mod at /www/MCP/Pando/pango includes comprehensive dependencies for LLM providers, SQLite database, and tool handling.
