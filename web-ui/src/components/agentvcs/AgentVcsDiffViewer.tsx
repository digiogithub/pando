import { useState, useEffect, useCallback } from 'react'
import { DiffEditor, type BeforeMount } from '@monaco-editor/react'
import type * as monacoTypes from 'monaco-editor'
import { useAgentVcsStore, type DiffEntry } from '@pando/client/stores/agentVcsStore'
import { IconButton, Kbd, Spinner } from '@/components/ui'
import { FileCode, X } from '@/components/ui/icons'
import { definePandoMonacoTheme, watchMonacoTheme, PANDO_MONACO_THEME } from '@/components/editor/monacoTheme'
import '@/styles/agentvcs.css'

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

function diffTypeLabel(type: string): string {
  switch (type) {
    case 'added': return 'ADDED'
    case 'deleted': return 'DELETED'
    default: return 'MODIFIED'
  }
}

export default function AgentVcsDiffViewer({ entry, commitId, onClose }: AgentVcsDiffViewerProps) {
  const language = detectLanguage(entry.path)
  const { fetchBlobContent } = useAgentVcsStore()
  const [original, setOriginal] = useState<string>('')
  const [modified, setModified] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [monacoInstance, setMonacoInstance] = useState<typeof monacoTypes | null>(null)

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
    definePandoMonacoTheme(monaco, document.documentElement.getAttribute('data-theme') === 'dark')
    setMonacoInstance(monaco)
  }, [])

  // Keep the Monaco theme in sync with the app theme (family / mode / accent).
  useEffect(() => {
    if (!monacoInstance) return
    return watchMonacoTheme(monacoInstance, (m, dark) => {
      definePandoMonacoTheme(m, dark)
      m.editor.setTheme(PANDO_MONACO_THEME)
    })
  }, [monacoInstance])

  // Esc key handler
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  const badgeLabel = diffTypeLabel(entry.type)
  const badgeTone = entry.type === 'added' ? 'success' : entry.type === 'deleted' ? 'danger' : 'warning'

  return (
    <div className="agentvcs-diffviewer">
      {/* Header */}
      <div className="agentvcs-diffviewer-header">
        <div className="agentvcs-diffviewer-title">
          <FileCode size={15} />
          <span className="agentvcs-diffviewer-path">{entry.path}</span>
          <span className={`ui-badge ui-badge--${badgeTone}`}>{badgeLabel}</span>
          <span className="agentvcs-diffviewer-commit">commit {commitId.slice(0, 12)}</span>
        </div>
        <IconButton aria-label="Close diff viewer" tooltip icon={<X size={16} />} onClick={onClose} />
      </div>

      {/* Content */}
      <div className="agentvcs-diffviewer-body">
        {loading ? (
          <div className="agentvcs-diffviewer-status">
            <Spinner size={22} />
            Loading file content...
          </div>
        ) : error ? (
          <div className="agentvcs-diffviewer-status" style={{ color: 'var(--danger)' }}>
            {error}
          </div>
        ) : (
          <DiffEditor
            height="100%"
            theme={PANDO_MONACO_THEME}
            language={language}
            original={original}
            modified={modified}
            beforeMount={handleBeforeMount}
            options={{
              readOnly: true,
              fontSize: 13,
              fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
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
      <div className="agentvcs-diffviewer-footer">
        Press <Kbd>Esc</Kbd> or click X to close
      </div>
    </div>
  )
}
