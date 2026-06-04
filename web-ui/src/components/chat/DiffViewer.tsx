import { useCallback, useEffect } from 'react'
import { DiffEditor, type BeforeMount } from '@monaco-editor/react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faTimes, faFileCode } from '@fortawesome/free-solid-svg-icons'
import type { FileChange } from '@/stores/fileChangesStore'

interface DiffViewerProps {
  file: FileChange
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

export default function DiffViewer({ file, onClose }: DiffViewerProps) {
  const language = detectLanguage(file.filePath)

  // Build the original and modified content by replaying edits sequentially.
  // The first edit's oldString is the starting point; each subsequent edit
  // transforms the result of the previous one.
  const { original, modified } = buildDiffContent(file)

  const handleBeforeMount: BeforeMount = useCallback((monaco) => {
    defineTheme(monaco)
  }, [])

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
            {file.filePath}
          </span>
          <span style={{ fontSize: 11, color: 'var(--fg-muted)', display: 'flex', gap: 8 }}>
            {file.additions > 0 && <span style={{ color: '#a6e3a1' }}>+{file.additions}</span>}
            {file.removals > 0 && <span style={{ color: '#f38ba8' }}>-{file.removals}</span>}
            <span>{file.edits.length} edit{file.edits.length !== 1 ? 's' : ''}</span>
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

      {/* Diff editor */}
      <div style={{ flex: 1, overflow: 'hidden' }}>
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
      </div>

      {/* Keyboard hint */}
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
        Press <kbd style={{ margin: '0 4px', padding: '1px 5px', border: '1px solid var(--border)', borderRadius: 3, fontSize: 10 }}>Esc</kbd> or click X to close
      </div>

      {/* Esc key handler */}
      <EscHandler onClose={onClose} />
    </div>
  )
}

/** Replay edits to produce original vs modified content for the diff viewer. */
function buildDiffContent(file: FileChange): { original: string; modified: string } {
  if (file.edits.length === 0) {
    return { original: '', modified: '' }
  }

  // For a single edit, just use old/new directly
  if (file.edits.length === 1) {
    return {
      original: file.edits[0].oldString,
      modified: file.edits[0].newString,
    }
  }

  // For multiple edits on the same file, the first edit's oldString is the
  // original file state. We replay each edit by replacing old->new in sequence
  // to build the final modified content.
  const original = file.edits[0].oldString
  let current = original
  for (const edit of file.edits) {
    if (edit.oldString) {
      const idx = current.indexOf(edit.oldString)
      if (idx >= 0) {
        current = current.slice(0, idx) + edit.newString + current.slice(idx + edit.oldString.length)
      } else {
        // If exact match not found, append the new content
        current += '\n' + edit.newString
      }
    } else {
      // New file / write: newString is the entire content
      current = edit.newString
    }
  }

  return { original, modified: current }
}

/** Component that listens for Escape key to close the viewer. */
function EscHandler({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  return null
}
