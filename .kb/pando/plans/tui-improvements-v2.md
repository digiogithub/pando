# Plan de Implementación: Mejoras TUI Pando v2

## Resumen
4 fases de mejora para Pando TUI:
1. **Editor inline en pestaña** (Ctrl+i) con syntax highlighting
2. **Corrección modelos duplicados + filtro búsqueda** en selector de modelos
3. **Soporte Ollama** - fix modelos no visibles
4. **Terminal embebido** con pestañas usando bubbleterm

## Fase 1: Editor Inline en Pestaña
- Reemplazar editor externo ($EDITOR/nvim) por editor en pestaña dentro de Pando
- Basado en https://github.com/satya-sudo/editgo como referencia
- Reutilizar Highlighter de internal/tui/components/editor/highlight.go (chroma)
- Integrar con TabBar existente de internal/tui/components/editor/tabs.go
- Archivos clave: editor.go (chat), viewer.go, highlight.go, tabs.go

## Fase 2: Modelos Duplicados + Filtro Búsqueda
- Problema: modelos estáticos ("GitHub Copilot GPT-4o") + dinámicos ("Copilot: gpt-4o") generan duplicados visuales
- Deduplicar por APIModel field en registry.go
- Añadir textinput de búsqueda fuzzy al diálogo de modelos (como commands.go)
- Archivos clave: models.go (dialog), registry.go, fetcher.go, copilot.go

## Fase 3: Fix Ollama
- fetchOllamaModels usa /v1/models (OpenAI compat) - verificar formato respuesta
- Verificar que provider Ollama esté habilitado en config
- Añadir fallback a /api/tags (API nativa Ollama)
- Archivos clave: ollama.go, fetcher.go, config.go, registry.go

## Fase 4: Terminal Embebido
- Integrar github.com/taigrr/bubbleterm
- Crear componente terminal con sistema de pestañas (reutilizar patrón TabBar)
- Comando "Open Terminal Emulator Embedded" en command dialog
- Panel inferior, ancho completo, múltiples instancias
- Archivos clave: tui.go, commands.go, chat.go (page)

## Dependencias entre fases
- Fases 1-4 son independientes, pueden implementarse en paralelo
- Fase 2 y 3 comparten archivos de modelos pero cambios no conflictivos
