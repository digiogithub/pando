import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import type { TerminalEntry } from '@/stores/terminalStore'

interface TerminalOutputProps {
  entries: TerminalEntry[]
  shell?: string
  cwd?: string
}

export default function TerminalOutput({ entries, shell, cwd }: TerminalOutputProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

  useEffect(() => {
    if (!containerRef.current || terminalRef.current) return

    const terminal = new Terminal({
      convertEol: true,
      cursorBlink: true,
      disableStdin: true,
      fontFamily: 'Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.35,
      theme: {
        background: '#0d1117',
        foreground: '#e6edf3',
        cursor: '#58a6ff',
        selectionBackground: '#264f78',
      },
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(containerRef.current)
    fitAddon.fit()

    const resizeObserver = new ResizeObserver(() => fitAddon.fit())
    resizeObserver.observe(containerRef.current)

    terminalRef.current = terminal
    fitAddonRef.current = fitAddon

    return () => {
      resizeObserver.disconnect()
      terminal.dispose()
      terminalRef.current = null
      fitAddonRef.current = null
    }
  }, [])

  useEffect(() => {
    const terminal = terminalRef.current
    const fitAddon = fitAddonRef.current
    if (!terminal) return

    terminal.reset()
    terminal.clear()
    terminal.writeln('\x1b[90mPando Terminal\x1b[0m')
    if (shell || cwd) {
      terminal.writeln(`\x1b[90m${shell ?? 'shell'}${cwd ? `  ${cwd}` : ''}\x1b[0m`)
    }
    if (entries.length === 0) {
      terminal.writeln('\x1b[90mtype a command to get started\x1b[0m')
      fitAddon?.fit()
      return
    }

    for (const entry of entries) {
      if (entry.type === 'command') {
        terminal.writeln(`\x1b[32m$\x1b[0m ${entry.text}`)
        continue
      }
      terminal.write(entry.text.replace(/\n$/, ''))
      terminal.writeln('')
    }
    fitAddon?.fit()
    terminal.scrollToBottom()
  }, [entries, shell, cwd])

  return (
    <div
      style={{ flex: 1, minHeight: 0, overflow: 'hidden', padding: '0.5rem 0.75rem', display: 'flex' }}
    >
      <div ref={containerRef} style={{ flex: 1, minHeight: 0, overflow: 'hidden' }} />
    </div>
  )
}
