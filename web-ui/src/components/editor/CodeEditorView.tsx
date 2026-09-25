import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useEditorStore } from '@pando/client/stores/editorStore'
import api from '@pando/client/services/api'
import { Button, IconButton } from '@/components/ui'
import { ArrowLeft, FileCode, FolderTree, Plus, Save } from '@/components/ui/icons'
import FileExplorer from './FileExplorer'
import EditorTabs from './EditorTabs'
import CodeEditor from './CodeEditor'
import EditorStatusBar from './EditorStatusBar'
import { useDialogs } from '@/components/shared/useDialogs'
import '@/styles/editor.css'
import type { FileNode } from '@pando/client/types'

interface FilesResponse {
  path: string
  files: Array<{
    name: string
    path: string
    isDir: boolean
    size: number
  }>
}

async function buildFileTree(dirPath: string): Promise<FileNode[]> {
  const data = await api.get<FilesResponse>(`/api/v1/files?path=${encodeURIComponent(dirPath)}`)
  const nodes: FileNode[] = []

  for (const file of data.files ?? []) {
    const node: FileNode = {
      name: file.name,
      path: file.path,
      is_dir: file.isDir,
      size: file.size,
    }
    if (file.isDir) {
      // Lazily load children only when expanded — for now return empty children
      node.children = []
    }
    nodes.push(node)
  }

  return nodes
}

// How often the explorer re-reads the working directory so files the agent
// creates or deletes show up without a manual refresh. Polling pauses while the
// tab is hidden.
const TREE_REFRESH_INTERVAL_MS = 3000

export default function CodeEditorView() {
  const navigate = useNavigate()
  const { openFiles, activeFilePath, markFileSaved } = useEditorStore()
  const [files, setFiles] = useState<FileNode[]>([])
  const [treeVersion, setTreeVersion] = useState(0)
  const [gitBranch, setGitBranch] = useState('main')
  const { prompt, dialogs } = useDialogs()
  const [explorerOpen, setExplorerOpen] = useState(() => window.innerWidth >= 768)
  const [saving, setSaving] = useState(false)

  const activeFile = activeFilePath ? openFiles.find((f) => f.path === activeFilePath) : null

  const fetchFiles = useCallback(async () => {
    try {
      const tree = await buildFileTree('.')
      setFiles(tree)
      // Bumping the version makes every expanded directory re-read its children.
      setTreeVersion((v) => v + 1)
    } catch (err) {
      console.error('Failed to fetch file tree:', err)
    }
  }, [])

  // Poll the tree while the explorer is open and the tab is visible, and refresh
  // immediately when the tab regains focus after being hidden.
  useEffect(() => {
    if (!explorerOpen) return

    const refreshIfVisible = () => {
      if (document.hidden) return
      fetchFiles()
    }

    const interval = window.setInterval(refreshIfVisible, TREE_REFRESH_INTERVAL_MS)
    document.addEventListener('visibilitychange', refreshIfVisible)
    window.addEventListener('focus', refreshIfVisible)
    return () => {
      window.clearInterval(interval)
      document.removeEventListener('visibilitychange', refreshIfVisible)
      window.removeEventListener('focus', refreshIfVisible)
    }
  }, [explorerOpen, fetchFiles])

  useEffect(() => {
    fetchFiles()

    // Try to detect git branch from terminal or just keep default
    const detectBranch = async () => {
      try {
        const result = await api.post<{ stdout: string; stderr: string }>(
          '/api/v1/terminal/exec',
          { command: 'git rev-parse --abbrev-ref HEAD' }
        )
        const branch = result.stdout?.trim()
        if (branch && branch !== 'HEAD') {
          setGitBranch(branch)
        }
      } catch {
        // Keep default 'main'
      }
    }
    detectBranch()
  }, [fetchFiles])

  const handleNewFile = useCallback(async () => {
    const name = await prompt({ title: 'New file', label: 'File name', confirmLabel: 'Create' })
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: '' })
      fetchFiles()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
  }, [fetchFiles, prompt])

  const handleSave = useCallback(async () => {
    if (!activeFile) return
    setSaving(true)
    try {
      await api.put(`/api/v1/files/${activeFile.path}`, { content: activeFile.content })
      markFileSaved(activeFile.path)
    } catch (err) {
      console.error('Failed to save:', err)
    } finally {
      setSaving(false)
    }
  }, [activeFile, markFileSaved])

  return (
    <div className="editor-shell">
      {/* Header bar */}
      <div className="editor-header">
        <div className="editor-header-group">
          <Button size="sm" variant="ghost" icon={<ArrowLeft size={14} />} onClick={() => navigate('/chat')} title="Back to Chat">
            <span className="editor-back-label">Back</span>
          </Button>

          <IconButton
            aria-label={explorerOpen ? 'Hide file explorer' : 'Show file explorer'}
            tooltip
            icon={<FolderTree size={15} />}
            variant={explorerOpen ? 'secondary' : 'ghost'}
            active={explorerOpen}
            onClick={() => setExplorerOpen((v) => !v)}
          />

          <div className="editor-header-title">
            <FileCode size={15} />
            <span className="editor-title-label">Code Editor</span>
          </div>
        </div>

        <div className="editor-header-group">
          {activeFile && (
            <Button
              size="sm"
              variant={activeFile.isDirty ? 'primary' : 'secondary'}
              icon={<Save size={13} />}
              onClick={handleSave}
              disabled={saving || !activeFile.isDirty}
              loading={saving}
              title={activeFile.isDirty ? 'Save file (Ctrl+S)' : 'No unsaved changes'}
            >
              <span className="editor-save-label">{saving ? 'Saving…' : 'Save'}</span>
            </Button>
          )}

          <Button size="sm" variant="ghost" icon={<Plus size={13} />} onClick={handleNewFile} title="New file">
            <span className="editor-new-label">New File</span>
          </Button>
        </div>
      </div>

      {/* Main content: file explorer + editor */}
      <div style={{ display: 'flex', flex: 1, overflow: 'clip', minHeight: 0, position: 'relative' }}>
        {/* File explorer — hidden on mobile by default, overlay when open */}
        {explorerOpen && (
          <>
            <div className="editor-explorer-backdrop" onClick={() => setExplorerOpen(false)} />
            <div className="editor-explorer-panel">
              <FileExplorer
                files={files}
                treeVersion={treeVersion}
                onRefresh={fetchFiles}
                onClose={() => setExplorerOpen(false)}
              />
            </div>
          </>
        )}

        {/* Editor area */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', minWidth: 0 }}>
          {/* Tabs */}
          <EditorTabs />

          {/* Editor, viewer, or empty state */}
          {activeFile ? (
            activeFile.fileType === 'image' ? (
              <ImageViewer path={activeFile.path} />
            ) : activeFile.fileType === 'pdf' ? (
              <PdfViewer path={activeFile.path} />
            ) : (
              <CodeEditor
                filePath={activeFile.path}
                content={activeFile.content}
                language={activeFile.language}
              />
            )
          ) : (
            <EmptyEditorState />
          )}
        </div>
      </div>

      {/* Status bar */}
      <EditorStatusBar gitBranch={gitBranch} />
      {dialogs}
    </div>
  )
}

function EmptyEditorState() {
  return (
    <div className="editor-empty">
      <FileCode size={44} strokeWidth={1.5} />
      <div className="editor-empty-title">Open a file from the tree</div>
      <div className="editor-empty-desc">Select a file in the explorer to start editing</div>
    </div>
  )
}

function ImageViewer({ path }: { path: string }) {
  const token = api.getToken()
  const src = `/api/v1/files/raw/${path}${token ? `?token=${encodeURIComponent(token)}` : ''}`
  return (
    <div className="editor-viewer">
      <img
        src={src}
        alt={path.split('/').pop()}
        className="editor-viewer-img"
        // A machine with no headless browser answers 503 here; the card must
        // still be usable, so the broken image simply disappears.
        onError={(e) => {
          e.currentTarget.style.display = 'none'
        }}
      />
      <span className="editor-viewer-path">{path}</span>
    </div>
  )
}

function PdfViewer({ path }: { path: string }) {
  const token = api.getToken()
  const src = `/api/v1/files/raw/${path}${token ? `?token=${encodeURIComponent(token)}` : ''}`
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <iframe src={src} title={path.split('/').pop()} className="editor-viewer-frame" />
    </div>
  )
}
