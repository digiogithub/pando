import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import {
  connectPty,
  registerPtyConnection,
  unregisterPtyConnection,
  type PtyConnection,
} from '@/services/terminalPty'
import { useTerminalStore, type TerminalTab } from '@/stores/terminalStore'

interface TerminalPtyPaneProps {
  tab: TerminalTab
  active: boolean
}

const XTERM_THEME = {
  background: '#0d1117',
  foreground: '#e6edf3',
  cursor: '#58a6ff',
  selectionBackground: '#264f78',
}

/**
 * An interactive shell pane: xterm.js wired to a real PTY over a websocket.
 *
 * The pane stays mounted while its tab is in the background (hidden with CSS)
 * so the shell keeps running and its screen state survives tab switches.
 */
export default function TerminalPtyPane({ tab, active }: TerminalPtyPaneProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const connectionRef = useRef<PtyConnection | null>(null)

  const attachPtySession = useTerminalStore((state) => state.attachPtySession)
  const markPtyExited = useTerminalStore((state) => state.markPtyExited)
  const fallbackToExec = useTerminalStore((state) => state.fallbackToExec)

  // The tab id is the identity of this pane; everything else is read through
  // refs or the store so the shell is never torn down by an unrelated re-render.
  const tabId = tab.id
  // Captured once: reconnecting must target the session this pane opened with,
  // and later store updates to ptySessionId must not restart the shell.
  const [initialSessionId] = useState(tab.ptySessionId)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: 'Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 10000,
      theme: XTERM_THEME,
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(container)

    terminalRef.current = terminal
    fitAddonRef.current = fitAddon

    // Fit before connecting so the shell starts with the real window size.
    try {
      fitAddon.fit()
    } catch {
      // The container may not be laid out yet; the ResizeObserver will fit later.
    }

    let ready = false
    const decoder = new TextDecoder()

    const connection = connectPty(
      { sessionId: initialSessionId, cols: terminal.cols, rows: terminal.rows },
      {
        onData: (chunk) => terminal.write(decoder.decode(chunk, { stream: true })),
        onReady: (info) => {
          ready = true
          attachPtySession(tabId, info)
        },
        onExit: (code) => {
          terminal.writeln(`\r\n\x1b[90m[process exited with code ${code}]\x1b[0m`)
          markPtyExited(tabId, code)
        },
        onClose: (err) => {
          if (!ready) {
            // Never reached a working shell: the server may predate the PTY
            // endpoint, or a proxy is stripping the upgrade. Use the /exec path.
            fallbackToExec(tabId, err?.message ?? 'terminal connection closed')
            return
          }
          terminal.writeln('\r\n\x1b[90m[disconnected]\x1b[0m')
        },
      },
    )
    connectionRef.current = connection
    registerPtyConnection(tabId, connection)

    const dataSub = terminal.onData((data) => connection.send(data))
    const resizeSub = terminal.onResize(({ cols, rows }) => connection.resize(cols, rows))

    const resizeObserver = new ResizeObserver(() => {
      // fit() throws if the element is hidden (zero size) — normal for a
      // background tab, which gets re-fitted when it becomes active again.
      try {
        fitAddon.fit()
      } catch {
        /* ignore */
      }
    })
    resizeObserver.observe(container)

    return () => {
      resizeObserver.disconnect()
      dataSub.dispose()
      resizeSub.dispose()
      unregisterPtyConnection(tabId)
      // Detach, don't terminate: closing the pane (e.g. navigating away) leaves
      // the shell alive so it can be reattached. closeTab() terminates it.
      connection.detach()
      terminal.dispose()
      terminalRef.current = null
      fitAddonRef.current = null
      connectionRef.current = null
    }
  }, [tabId, initialSessionId, attachPtySession, markPtyExited, fallbackToExec])

  // Re-fit and focus when this tab comes to the foreground: a hidden container
  // has no size, so xterm could not measure itself while it was in background.
  useEffect(() => {
    if (!active) return
    const terminal = terminalRef.current
    const fitAddon = fitAddonRef.current
    if (!terminal || !fitAddon) return

    const id = window.setTimeout(() => {
      try {
        fitAddon.fit()
      } catch {
        /* ignore */
      }
      terminal.focus()
    }, 0)
    return () => window.clearTimeout(id)
  }, [active])

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        overflow: 'hidden',
        padding: '0.5rem 0.75rem',
        display: active ? 'flex' : 'none',
      }}
    >
      <div ref={containerRef} style={{ flex: 1, minHeight: 0, overflow: 'hidden' }} />
    </div>
  )
}
