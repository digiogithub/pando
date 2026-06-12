# Implementation Plan: Pando TUI Improvements v2

## Summary
4 improvement phases for Pando TUI:
1. **Inline editor in tab** (Ctrl+i) with syntax highlighting
2. **Duplicate models fix + search filter** in model selector
3. **Ollama support** - fix invisible models
4. **Embedded terminal** with tabs using bubbleterm

## Phase 1: Inline Editor in Tab
- Replace external editor ($EDITOR/nvim) with in-tab editor inside Pando
- Based on https://github.com/satya-sudo/editgo as reference
- Reuse Highlighter from internal/tui/components/editor/highlight.go (chroma)
- Integrate with existing TabBar from internal/tui/components/editor/tabs.go
- Key files: editor.go (chat), viewer.go, highlight.go, tabs.go

## Phase 2: Duplicate Models + Search Filter
- Problem: static models ("GitHub Copilot GPT-4o") + dynamic ("Copilot: gpt-4o") generate visual duplicates
- Deduplicate by APIModel field in registry.go
- Add fuzzy search textinput to models dialog (like commands.go)
- Key files: models.go (dialog), registry.go, fetcher.go, copilot.go

## Phase 3: Ollama Fix
- fetchOllamaModels uses /v1/models (OpenAI compat) - verify response format
- Verify Ollama provider is enabled in config
- Add fallback to /api/tags (Ollama native API)
- Key files: ollama.go, fetcher.go, config.go, registry.go

## Phase 4: Embedded Terminal
- Integrate github.com/taigrr/bubbleterm
- Create terminal component with tab system (reuse TabBar pattern)
- "Open Terminal Emulator Embedded" command in command dialog
- Bottom panel, full width, multiple instances
- Key files: tui.go, commands.go, chat.go (page)

## Phase dependencies
- Phases 1-4 are independent, can be implemented in parallel
- Phases 2 and 3 share model files but changes are non-conflicting
