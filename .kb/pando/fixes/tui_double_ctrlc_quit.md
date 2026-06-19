---
created_at: 2026-06-19T05:29:07.686361149Z
updated_at: 2026-06-19T05:29:07.686361149Z
tags:
    - fix
    - tui
    - keybindings
---
# Fix: salir del TUI con doble Ctrl+C

Fecha: 2026-06-19

## Qué se cambió
En el modo TUI, pulsar `Ctrl+C` muestra el diálogo de confirmación de salida
(`quitDialogCmp`). Antes el handler hacía un *toggle* de `a.showQuit`, por lo que
una segunda pulsación de `Ctrl+C` solo cerraba el diálogo en vez de salir. Ahora
la primera pulsación muestra el diálogo y la segunda confirma la salida
directamente (sin esperar interacción del usuario).

## Archivos/símbolos tocados
- `internal/tui/tui.go`, en el `case key.Matches(msg, a.keys.Global.Quit) && !(a.tabBar != nil && a.tabBar.IsActiveEditable())` (handler de teclado de `appModel.Update`).
  - Se sustituyó `a.showQuit = !a.showQuit` por:
    ```go
    if a.showQuit {
        // Second Ctrl+C confirms the quit dialog without waiting for interaction.
        return a, tea.Quit
    }
    a.showQuit = true
    ```
  - El resto del bloque (cierre de los demás diálogos: showHelp, showSessionDialog,
    showCommandDialog, showFilepicker, showModelDialog, showMultiArgumentsDialog,
    showInfoDialog) se mantiene y solo se ejecuta en la rama del primer Ctrl+C.

## Motivo
El usuario quería que doble Ctrl+C actúe como confirmación implícita de la
pantalla de salida, agilizando el cierre.

## Notas de diseño
- `tea.Quit` ya estaba en uso (es el mismo comando que devuelve `quitDialogCmp.Update`
  al confirmar); no hicieron falta imports nuevos.
- No se usa ventana temporal: como el diálogo permanece visible hasta descartarse,
  "diálogo visible + Ctrl+C" equivale semánticamente a "doble Ctrl+C".
- Se preservan las excepciones previas: editor editable activo (`IsActiveEditable`)
  y goal en ejecución en ChatPage (`HasRunningGoal`) mantienen su prioridad.
- El diálogo sigue respondiendo a y/n, enter/space, flechas/tab como antes.

## Verificación
- `go build ./internal/tui/` pasa sin errores.
- Manual: primer Ctrl+C muestra el diálogo; segundo Ctrl+C sale; con el diálogo
  abierto, `n` lo cancela; en editor editable Ctrl+C no dispara salida; con goal en
  ejecución Ctrl+C se delega a la página.