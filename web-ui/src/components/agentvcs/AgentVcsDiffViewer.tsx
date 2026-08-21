import { useState, useEffect, useCallback } from 'react'
import { DiffEditor, type BeforeMount } from '@monaco-editor/react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes, faFileCode, faSpinner } from '@fortawesome/free-solid-svg-icons'
import { useAgentVcsStore, type DiffEntry } from '@pando/client/stores/agentVcsStore'

interface AgentVcsDiffViewerProps {
  entry: DiffEntry
  commitId: string
  onClose: () => void
}

function detectLanguage(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    go: 'go', ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
    py: 'python', md: 'markdown', json: 'json', yaml: 'yaml', yml: 'yaml',
    css: 'css', html: 'html', sh: 'shell', bash: 'shell', rs: 'rust',
    toml: 'toml', sql: 'sql', lua: 'lua', vue: 'html', svelte: 'html',
  }
  return map[ext] ?? 'plaintext'
}

const defineTheme: BeforeMount = (monacoInstance) => {
  monacoInstance.editor.defineTheme('pando-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: 'comment', foreground: '6c7086', fontStyle: 'italic' },
      { token: 'keyword', foreground: 'cba6f7', fontStyle: 'bold' },
      { token: 'string', foreground: 'a6e3a1' },
      { token: 'number', foreground: 'fab387' },
      { token: 'type', foreground: 'f9e2af' },
      { token: 'variable', foreground: 'cdd6f4' },
      { token: 'function', foreground: '89b4fa' },
      { token: 'operator', foreground: '89dceb' },
    ],
    colors: {
      'editor.background': '#1e1e2e',
      'editor.foreground': '#cdd6f4',
      'editor.lineHighlightBackground': '#2a2a3d',
      'editor.selectionBackground': '#3d5985',
      'editorCursor.foreground': '#f5c2e7',
      'editorLineNumber.foreground': '#45475a',
      'editorLineNumber.activeForeground': '#cdd6f4',
      'diffEditor.insertedTextBackground': '#a6e3a120',
      'diffEditor.removedTextBackground': '#f38ba820',
      'diffEditor.insertedLineBackground': '#a6e3a110',
      'diffEditor.removedLineBackground': '#f38ba810',
    },
  })
}

function diffTypeLabel(type: string): { text: string; color: string } {
  switch (type) {
    case 'added': return { text: 'ADDED', color: '#a6e3a1' }
    case 'deleted': return { text: 'DELETED', color: '#f38ba8' }
    default: return { text: 'MODIFIED', color: '#fab387' }
  }
}

export default function AgentVcsDiffViewer({ entry, commitId, onClose }: AgentVcsDiffViewerProps) {
  const language = detectLanguage(entry.path)
  const { fetchBlobContent } = useAgentVcsStore()
  const [original, setOriginal] = useState<string>('')
  const [modified, setModified] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)

    const load = async () => {
      try {
        const [oldContent, newContent] = await Promise.all([
          entry.old_hash ? fetchBlobContent(entry.old_hash) : Promise.resolve(''),
          entry.new_hash ? fetchBlobContent(entry.new_hash) : Promise.resolve(''),
        ])
        if (cancelled) return
        setOriginal(oldContent)
        setModified(newContent)
      } catch (e) {
        if (cancelled) return
        setError(e instanceof Error ? e.message : 'Failed to load file content')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    load()
    return () => { cancelled = true }
  }, [entry.old_hash, entry.new_hash, fetchBlobContent])

  const handleBeforeMount: BeforeMount = useCallback((monaco) => {
    defineTheme(monaco)
  }, [])

  // Esc key handler
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  const badge = diffTypeLabel(entry.type)

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--bg, #1e1e2e)',
      }}
    >
      {/* Header */}
      <div
        style={{
          height: 44,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 16px',
          borderBottom: '1px solid var(--border)',
          background: 'var(--sidebar-bg, #181825)',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <FontAwesomeIcon icon={faFileCode} style={{ fontSize: 14, color: 'var(--primary)' }} />
          <span
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--fg)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {entry.path}
          </span>
          <span
            style={{
              fontSize: 10,
              padding: '2px 6px',
              borderRadius: 3,
              background: `${badge.color}15`,
              color: badge.color,
              fontWeight: 700,
            }}
          >
            {badge.text}
          </span>
          <span style={{ fontSize: 11, color: 'var(--fg-dim)', fontFamily: "'JetBrains Mono', monospace" }}>
            commit {commitId.slice(0, 12)}
          </span>
        </div>
        <button
          onClick={onClose}
          title="Close diff viewer"
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            borderRadius: 'var(--radius-sm, 4px)',
            border: '1px solid var(--border)',
            background: 'transparent',
            color: 'var(--fg-muted)',
            cursor: 'pointer',
            fontSize: 14,
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = 'var(--error, #f38ba8)'
            e.currentTarget.style.color = '#fff'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = 'transparent'
            e.currentTarget.style.color = 'var(--fg-muted)'
          }}
        >
          <FontAwesomeIcon icon={faTimes} />
        </button>
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'hidden' }}>
        {loading ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: 12,
              color: 'var(--fg-muted)',
              fontSize: 13,
            }}
          >
            <FontAwesomeIcon icon={faSpinner} spin style={{ fontSize: 24, color: 'var(--primary)' }} />
            Loading file content...
          </div>
        ) : error ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: 'var(--error, #f38ba8)',
              fontSize: 13,
            }}
          >
            {error}
          </div>
        ) : (
          <DiffEditor
            height="100%"
            theme="pando-dark"
            language={language}
            original={original}
            modified={modified}
            beforeMount={handleBeforeMount}
            options={{
              readOnly: true,
              fontSize: 13,
              fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
              fontLigatures: true,
              renderSideBySide: window.innerWidth >= 768,
              minimap: { enabled: false },
              scrollBeyondLastLine: false,
              automaticLayout: true,
              lineNumbers: 'on',
              renderOverviewRuler: true,
              padding: { top: 8, bottom: 8 },
              smoothScrolling: true,
            }}
          />
        )}
      </div>

      {/* Footer hint */}
      <div
        style={{
          height: 28,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderTop: '1px solid var(--border)',
          background: 'var(--sidebar-bg, #181825)',
          fontSize: 11,
          color: 'var(--fg-dim)',
        }}
      >
        Press{' '}
        <kbd
          style={{
            margin: '0 4px',
            padding: '1px 5px',
            border: '1px solid var(--border)',
            borderRadius: 3,
            fontSize: 10,
          }}
        >
          Esc
        </kbd>{' '}
        or click X to close
      </div>
    </div>
  )
}
