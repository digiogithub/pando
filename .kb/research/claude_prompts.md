Index your code with Devin

[DeepWiki](/)

Index your code with

Devin

Share

[anthropics/claude-code](/anthropics/claude-code)

Como funcionan los diferentes modos de prompts de sistema y resumen y luego muestramelos

Fast

View as codemap

Los diferentes modos de prompts de sistema y resumen en Claude Code funcionan a través de varios mecanismos que modifican el comportamiento del agente. Aquí te explico cómo funcionan y te muestro los ejemplos:

## Modos de Prompt del Sistema

### 1. Output Style Plugins

Los plugins de estilo de salida modifican el comportamiento del agente inyectando instrucciones al inicio de cada sesión mediante hooks `SessionStart` README.md:3-4 .

**Plugin Explanatory:**  
Inyecta instrucciones para proporcionar insights educativos sobre decisiones de implementación README.md:14-16 :

```
`★ Insight ─────────────────────────────────────`  
[2-3 key educational points]  
`─────────────────────────────────────────────────`  
```

**Plugin Learning:**  
Combina funcionalidad explicativa con aprendizaje interactivo, solicitando contribuciones de código del usuario en puntos de decisión README.md:31-36 .

### 2. SessionStart Hooks

Los hooks `SessionStart` ejecutan scripts que inyectan contexto adicional JSON en la sesión del agente :

```

#!/usr/bin/env bash  
cat << 'EOF'  
{  
  "hookSpecificOutput": {  
    "hookEventName": "SessionStart",  
    "additionalContext": "Instructions text that will be injected..."  
  }  
}  
EOF
```

## Modos de Resumen

### 1. Auto-Compaction

Claude Code gestiona automáticamente los límites de tokens mediante compactación proactiva CHANGELOG.md:34-45 :

- **Token Tracking**: Monitorea la longitud de la conversación
- **Proactive Compaction**: Se activa automáticamente al acercarse al límite
- **Payload Stripping**: Elimina contenido pesado antes de compactar
- **Manual Control**: Comando `/compact` para compactación manual

### 2. Thinking Mode

Modo de pensamiento que permite al modelo mostrar su razonamiento interno. Se puede activar con palabras clave como "think", "think harder" o "ultrathink" CHANGELOG.md:2024 .

### 3. Plan Mode

Modo de planificación que utiliza un subagente Plan para estructurar el trabajo antes de ejecutarlo CHANGELOG.md:1218 .

## Ejemplos de Configuración

### Plugin Manifest

```

{  
  "name": "explanatory-output-style",  
  "version": "1.0.0",  
  "description": "Adds educational insights about implementation choices...",  
  "author": {  
    "name": "Dickson Tsai",  
    "email": "dickson@anthropic.com"  
  }  
}
```

### Hook Definition

```

{  
  "description": "Explanatory hook that adds educational insights",  
  "hooks": {  
    "SessionStart": [  
      {  
        "hooks": [  
          {  
            "type": "command",  
            "command": "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/session-start.sh"  
          }  
        ]  
      }  
    ]  
  }  
}
```

## Comandos Relacionados

- `/model` - Cambiar modelo o nivel de esfuerzo CHANGELOG.md:111
- `/compact` - Compactar manualmente el historial de conversación CHANGELOG.md:501
- `/context` - Visualizar contexto y uso de tokens CHANGELOG.md:906
- `/memory` - Gestionar memoria automática CHANGELOG.md:458

## Notas

Los modos de prompt del sistema y resumen en Claude Code están diseñados para ser flexibles y extensibles mediante el sistema de plugins, permitiendo personalización sin modificar el código base README.md:55-56 .

Wiki pages you might want to explore:

- [Claude Code Overview (anthropics/claude-code)](/anthropics/claude-code/1-claude-code-overview)
- [Output Style Plugins (anthropics/claude-code)](/anthropics/claude-code/4.4-output-style-plugins)

[anthropics/claude-code](https://github.com/anthropics/claude-code)[plugins/explanatory-output-style/README.md](https://github.com/anthropics/claude-code/blob/9582ad48/plugins/explanatory-output-style/README.md#L3-L4)

```

1# Explanatory Output Style Plugin
2 
```

```

3This plugin recreates the deprecated Explanatory output style as a SessionStart
4hook.
```

```

5 
6WARNING: Do not install this plugin unless you are fine with incurring the token
7cost of this plugin's additional instructions and output.
```

```

11When enabled, this plugin automatically adds instructions at the start of each
12session that encourage Claude to:
13 
```

```

141. Provide educational insights about implementation choices
152. Explain codebase patterns and decisions
163. Balance task completion with learning opportunities
```

```

17 
18## How it works
19 
```

```

52 
53You can now achieve the same behavior by installing this plugin instead.
54 
```

```

55More generally, this SessionStart hook pattern is roughly equivalent to
56CLAUDE.md, but it is more flexible and allows for distribution through plugins.
```

```

57 
58Note: Output styles that involve tasks besides software development, are better
59expressed as
```

[anthropics/claude-code](https://github.com/anthropics/claude-code)[plugins/learning-output-style/README.md](https://github.com/anthropics/claude-code/blob/9582ad48/plugins/learning-output-style/README.md#L31-L36)

```

28## When Claude requests contributions
29 
30Claude will ask you to write code for:
```

```

31- Business logic with multiple valid approaches
32- Error handling strategies
33- Algorithm implementation choices
34- Data structure decisions
35- User experience decisions
36- Design patterns and architecture choices
```

```

37 
38## When Claude won't request contributions
39 
```

[anthropics/claude-code](https://github.com/anthropics/claude-code)[CHANGELOG.md](https://github.com/anthropics/claude-code/blob/9582ad48/CHANGELOG.md#L34-L45)

```

31- Fixed ghost dotfiles (`.bashrc`, `HEAD`, etc.) appearing as untracked files in the working directory after sandboxed Bash commands on Linux
32- Fixed Shift+Enter printing `[27;2;13~` instead of inserting a newline in Ghostty over SSH
33- Fixed stash (Ctrl+S) being cleared when submitting a message while Claude is working
```

```

34- Fixed ctrl+o (transcript toggle) freezing for many seconds in long sessions with lots of file edits
35- Fixed plan mode feedback input not supporting multi-line text entry (backslash+Enter and Shift+Enter now insert newlines)
36- Fixed cursor not moving down into blank lines at the top of the input box
37- Fixed `/stats` crash when transcript files contain entries with missing or malformed timestamps
38- Fixed a brief hang after a streaming error on long sessions (the transcript was being fully rewritten to drop one line; it is now truncated in place)
39- Fixed `--setting-sources user` not blocking dynamically discovered project skills
40- Fixed duplicate CLAUDE.md, slash commands, agents, and rules when running from a worktree nested inside its main repo (e.g. `claude -w`)
41- Fixed plugin Stop/SessionEnd/etc hooks not firing after any `/plugin` operation
42- Fixed plugin hooks being silently dropped when two plugins use the same `${CLAUDE_PLUGIN_ROOT}/...` command template
43- Fixed memory leak in long-running SDK/CCR sessions where conversation messages were retained unnecessarily
44- Fixed API 400 errors in forked agents (autocompact, summarization) when resuming sessions that were interrupted mid-tool-batch
45- Fixed "unexpected tool_use_id found in tool_result blocks" error when resuming conversations that start with an orphaned tool result
```

```

46- Fixed teammates accidentally spawning nested teammates via the Agent tool's `name` parameter
47- Fixed `CLAUDE_CODE_MAX_OUTPUT_TOKENS` being ignored during conversation compaction
48- Fixed `/compact` summary rendering as a user bubble in SDK consumers (Claude Code Remote web UI, VSCode extension)
```

```

108 
109## 2.1.68
110 
```

```

111- Opus 4.6 now defaults to medium effort for Max and Team subscribers. Medium effort works well for most tasks — it's the sweet spot between speed and thoroughness. You can change this anytime with `/model`
```

```

112- Re-introduced the "ultrathink" keyword to enable high effort for the next turn
113- Removed Opus 4 and 4.1 from Claude Code on the first-party API — users with these models pinned are automatically moved to Opus 4.6
114 
```

```

455 
456- Claude Opus 4.6 is now available!
457- Added research preview agent teams feature for multi-agent collaboration (token-intensive feature, requires setting CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1)
```

```

458- Claude now automatically records and recalls memories as it works
```

```

459- Added "Summarize from here" to the message selector, allowing partial conversation summarization.
460- Skills defined in `.claude/skills/` within additional directories (`--add-dir`) are now loaded automatically.
461- Fixed `@` file completion showing incorrect relative paths when running from a subdirectory
```

```

498- Fixed a regression where Windows users with a `.bashrc` file could not run bash commands
499- Improved memory usage for `--resume` (68% reduction for users with many sessions) by replacing the session index with lightweight stat-based loading and progressive enrichment
500- Improved `TaskStop` tool to display the stopped command/task description in the result line instead of a generic "Task stopped" message
```

```

501- Changed `/model` to execute immediately instead of being queued
```

```

502- [VSCode] Added multiline input support to the "Other" text input in question dialogs (use Shift+Enter for new lines)
503- [VSCode] Fixed duplicate sessions appearing in the session list when starting a new conversation
504 
```

```

903- Fixed a potential crash when syntax highlighting isn't initialized correctly
904- Fixed visual bug in `/plugins discover` where list selection indicator showed while search box was focused
905- Fixed macOS keyboard shortcuts to display 'opt' instead of 'alt'
```

```

906- Improved `/context` command visualization with grouped skills and agents by source, slash commands, and sorted token count
```

```

907- [Windows] Fixed issue with improper rendering
908- [VSCode] Added gift tag pictogram for year-end promotion message
909 
```

```

1215 
1216## 2.0.28
1217 
```

```

1218- Plan mode: introduced new Plan subagent
```

```

1219- Subagents: claude can now choose to resume subagents
1220- Subagents: claude can dynamically choose the model used by its subagents
1221- SDK: added --max-budget-usd flag
```

```

2021 
2022## 0.2.44
2023 
```

```

2024- Ask Claude to make a plan with thinking mode: just say 'think' or 'think harder' or even 'ultrathink'
```

```

2025 
2026## 0.2.41
2027 
```

Search | DeepWiki