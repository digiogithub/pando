import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { DiffEditor, type BeforeMount } from '@monaco-editor/react'
import api from '@pando/client/services/api'
import type { FileChange } from '@pando/client/stores/fileChangesStore'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { IconButton, Kbd } from '@/components/ui'
import { FileCode, X } from '@/components/ui/icons'
import { useTheme } from '@/hooks/useTheme'
import { cssVarHex } from '@/lib/cssColor'

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

/** Reads a colour token from the document as strict hex (Monaco rejects `#fff`, rgb(), …). */
const token = cssVarHex

const THEME_NAME = 'pando-diff'

/** Monaco theme built from the live Pando tokens (re-defined on each mount). */
const defineTheme = (monacoInstance: Parameters<BeforeMount>[0], dark: boolean) => {
  const bg = token('--bg', dark ? '#111113' : '#ffffff')
  const fg = token('--fg', dark ? '#ececee' : '#18181b')
  const faint = token('--fg-faint', '#85858c')
  const raised = token('--bg-raised', dark ? '#252528' : '#efeff1')
  const success = token('--success', '#15803d')
  const danger = token('--danger', '#dc2626')
  const accent = token('--accent', '#8a6516')
  monacoInstance.editor.defineTheme(THEME_NAME, {
    base: dark ? 'vs-dark' : 'vs',
    inherit: true,
    rules: [{ token: 'comment', foreground: faint.slice(1), fontStyle: 'italic' }],
    colors: {
      'editor.background': bg,
      'editor.foreground': fg,
      'editor.lineHighlightBackground': raised,
      'editor.selectionBackground': `${accent}40`,
      'editorCursor.foreground': accent,
      'editorLineNumber.foreground': faint,
      'editorLineNumber.activeForeground': fg,
      'diffEditor.insertedTextBackground': `${success}2e`,
      'diffEditor.removedTextBackground': `${danger}2e`,
      'diffEditor.insertedLineBackground': `${success}17`,
      'diffEditor.removedLineBackground': `${danger}17`,
    },
  })
}

export default function DiffViewer({ file, onClose }: DiffViewerProps) {
  const { t } = useTranslation()
  const { resolvedMode, family, accent } = useTheme()
  const dark = resolvedMode === 'dark'
  const language = detectLanguage(file.filePath)

  const sessionId = useSessionStore((s) => s.activeSessionId)
  const cwd = useProjectStore((s) => s.workspace?.cwd ?? '')
  const fetchWorkspace = useProjectStore((s) => s.fetchWorkspace)
  const [content, setContent] = useState<DiffContent | null>(null)

  useEffect(() => {
    if (!cwd) void fetchWorkspace()
  }, [cwd, fetchWorkspace])

  // Load the whole file (not only the edited snippets): agent-vcs blobs first,
  // then the file on disk with the edits reverted, then the raw snippets.
  useEffect(() => {
    let cancelled = false
    setContent(null)
    loadDiffContent(file, sessionId, cwd).then((c) => {
      if (!cancelled) setContent(c)
    })
    return () => { cancelled = true }
  }, [file, sessionId, cwd])

  const handleBeforeMount: BeforeMount = useCallback((monaco) => {
    defineTheme(monaco, dark)
  }, [dark])

  // Portalled to <body>: the chat lives inside `.shell-main-inner`, a stacking
  // context below the title bar, which would otherwise cover the header.
  return createPortal(
    <div className="chat-diff" role="dialog" aria-label={file.filePath}>
      <div className="chat-diff-head">
        <FileCode size={16} />
        <span className="chat-diff-path" title={file.filePath}>{file.filePath}</span>
        <span className="chat-diff-stats">
          {file.additions > 0 && <span className="chat-add">+{file.additions}</span>}
          {file.removals > 0 && <span className="chat-del">-{file.removals}</span>}
          <span>{t('chat.diff.edits', { count: file.edits.length })}</span>
        </span>
        <span className="chat-spacer" />
        <IconButton aria-label={t('chat.diff.close')} tooltip icon={<X />} onClick={onClose} />
      </div>

      {/* Diff editor — remounted when the theme changes so colours follow it. */}
      <div className="chat-diff-body">
        {content === null ? (
          <div className="chat-diff-loading">{t('common.loading')}</div>
        ) : (
        <DiffEditor
          key={`${family}-${resolvedMode}-${accent ?? ''}`}
          height="100%"
          theme={THEME_NAME}
          language={language}
          original={content.original}
          modified={content.modified}
          beforeMount={handleBeforeMount}
          options={{
            readOnly: true,
            fontSize: 13,
            fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', monospace",
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

      <div className="chat-diff-foot">
        <Kbd>Esc</Kbd> {t('chat.diff.escToClose')}
      </div>

      {/* Esc key handler */}
      <EscHandler onClose={onClose} />
    </div>,
    document.body,
  )
}

interface DiffContent {
  original: string
  modified: string
}

interface VcsDiffEntry {
  path: string
  old_hash?: string
  new_hash?: string
}

/** Path relative to the workspace root, or null when it lies outside it. */
function toRelativePath(filePath: string, cwd: string): string | null {
  if (!filePath.startsWith('/')) return filePath.replace(/^\.\//, '')
  const root = cwd.replace(/\/+$/, '')
  if (root && filePath.startsWith(root + '/')) return filePath.slice(root.length + 1)
  return null
}

/**
 * Resolve the full before/after content of a changed file. The edit list only
 * holds the replaced snippets (and nothing at all for patch or agent-vcs-only
 * entries), so the viewer would otherwise show empty or partial panes.
 */
async function loadDiffContent(file: FileChange, sessionId: string | null, cwd: string): Promise<DiffContent> {
  const rel = toRelativePath(file.filePath, cwd)

  // 1. agent-vcs: session baseline vs HEAD, exact full content on both sides.
  if (sessionId) {
    try {
      const res = await api.get<{ diff?: VcsDiffEntry[] }>(`/api/v1/agentvcs/sessions/${sessionId}/diff`)
      const entry = (res.diff ?? []).find(
        (e) => e.path === rel || e.path === file.filePath || file.filePath.endsWith('/' + e.path),
      )
      if (entry) {
        const blob = (hash?: string) =>
          hash ? api.get<string>(`/api/v1/agentvcs/blobs/${hash}`).then(String) : Promise.resolve('')
        const [original, modified] = await Promise.all([blob(entry.old_hash), blob(entry.new_hash)])
        return { original, modified }
      }
    } catch {
      // agent-vcs disabled or unreachable — fall through.
    }
  }

  // 2. Current file on disk, with the recorded edits reverted for the original.
  if (rel) {
    try {
      const res = await api.get<{ content?: string }>(`/api/v1/files/${rel}`)
      const modified = res.content ?? ''
      return { original: revertEdits(modified, file.edits), modified }
    } catch {
      // Deleted or unreadable — fall through.
    }
  }

  // 3. Only the snippets the tool calls carried.
  return buildDiffContent(file)
}

/** Undo the edits (newest first) on the current content to recover the original. */
function revertEdits(current: string, edits: FileChange['edits']): string {
  let text = current
  for (let i = edits.length - 1; i >= 0; i--) {
    const { oldString, newString } = edits[i]
    if (!oldString) {
      // write: the whole file was replaced; the prior content is unknown.
      if (newString === text) text = ''
      continue
    }
    const idx = newString ? text.lastIndexOf(newString) : -1
    if (idx >= 0) text = text.slice(0, idx) + oldString + text.slice(idx + newString.length)
  }
  return text
}

/** Replay edits to produce original vs modified content for the diff viewer. */
function buildDiffContent(file: FileChange): DiffContent {
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
