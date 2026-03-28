import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faArrowLeft, faFileMedical, faFileCode,
  faFloppyDisk, faFolderTree,
} from '@fortawesome/free-solid-svg-icons'
import type { FileNode } from '@/types'
import { useEditorStore } from '@/stores/editorStore'
import api from '@/services/api'
import FileExplorer from './FileExplorer'
import EditorTabs from './EditorTabs'
import CodeEditor from './CodeEditor'
import EditorStatusBar from './EditorStatusBar'

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

export default function CodeEditorView() {
  const navigate = useNavigate()
  const { openFiles, activeFilePath, markFileSaved } = useEditorStore()
  const [files, setFiles] = useState<FileNode[]>([])
  const [gitBranch, setGitBranch] = useState('main')
  const [explorerOpen, setExplorerOpen] = useState(() => window.innerWidth >= 768)
  const [saving, setSaving] = useState(false)

  const activeFile = activeFilePath ? openFiles.find((f) => f.path === activeFilePath) : null

  const fetchFiles = useCallback(async () => {
    try {
      const tree = await buildFileTree('.')
      setFiles(tree)
    } catch (err) {
      console.error('Failed to fetch file tree:', err)
    }
  }, [])

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
    const name = window.prompt('New file name:')
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: '' })
      fetchFiles()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
  }, [fetchFiles])

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
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100vh',
        background: 'var(--bg)',
        overflow: 'hidden',
      }}
    >
      {/* Header bar */}
      <div
        style={{
          height: 40,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 16px',
          borderBottom: '1px solid var(--border)',
          background: 'var(--sidebar-bg)',
          flexShrink: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {/* Back button */}
          <button
            onClick={() => navigate('/chat')}
            title="Back to Chat"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-muted, #a0a0b0)',
              fontSize: 13,
              padding: '4px 8px',
              borderRadius: 'var(--radius-sm)',
              fontFamily: 'inherit',
            }}
            onMouseEnter={(e) => {
              ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--hover-bg)'
              ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)'
            }}
            onMouseLeave={(e) => {
              ;(e.currentTarget as HTMLButtonElement).style.background = 'transparent'
              ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted, #a0a0b0)'
            }}
          >
            <FontAwesomeIcon icon={faArrowLeft} style={{ fontSize: 12 }} />
            <span className="editor-back-label">Back</span>
          </button>

          {/* Explorer toggle */}
          <button
            onClick={() => setExplorerOpen((v) => !v)}
            title={explorerOpen ? 'Hide file explorer' : 'Show file explorer'}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              background: explorerOpen ? 'var(--selected)' : 'transparent',
              border: '1px solid var(--border)',
              cursor: 'pointer',
              color: explorerOpen ? 'var(--primary)' : 'var(--fg-muted, #a0a0b0)',
              fontSize: 13,
              padding: '4px 8px',
              borderRadius: 'var(--radius-sm)',
              fontFamily: 'inherit',
            }}
          >
            <FontAwesomeIcon icon={faFolderTree} style={{ fontSize: 12 }} />
          </button>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <FontAwesomeIcon icon={faFileCode} style={{ fontSize: 14, color: 'var(--primary)' }} />
            <span className="editor-title-label" style={{ fontSize: 14, fontWeight: 600, color: 'var(--fg)' }}>
              Code Editor
            </span>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {/* Save button */}
          {activeFile && (
            <button
              onClick={handleSave}
              disabled={saving || !activeFile.isDirty}
              title={activeFile.isDirty ? 'Save file (Ctrl+S)' : 'No unsaved changes'}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                padding: '5px 12px',
                borderRadius: 'var(--radius-sm)',
                border: '1px solid var(--border)',
                background: activeFile.isDirty ? 'var(--primary)' : 'var(--bg)',
                color: activeFile.isDirty ? 'white' : 'var(--fg-muted)',
                fontSize: 13,
                fontWeight: 600,
                cursor: activeFile.isDirty ? 'pointer' : 'default',
                fontFamily: 'inherit',
                opacity: saving ? 0.6 : 1,
                transition: 'background 0.15s, color 0.15s',
              }}
            >
              <FontAwesomeIcon icon={faFloppyDisk} style={{ fontSize: 11 }} />
              <span className="editor-save-label">{saving ? 'Saving…' : 'Save'}</span>
            </button>
          )}

          {/* New file button */}
          <button
            onClick={handleNewFile}
            title="New file"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              padding: '5px 12px',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            <FontAwesomeIcon icon={faFileMedical} style={{ fontSize: 11 }} />
            <span className="editor-new-label">New File</span>
          </button>
        </div>
      </div>

      {/* Main content: file explorer + editor */}
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0, position: 'relative' }}>
        {/* File explorer — hidden on mobile by default, overlay when open */}
        {explorerOpen && (
          <>
            <div className="editor-explorer-backdrop" onClick={() => setExplorerOpen(false)} />
            <div className="editor-explorer-panel">
              <FileExplorer
                files={files}
                onRefresh={fetchFiles}
                onClose={() => setExplorerOpen(false)}
              />
            </div>
          </>
        )}

        {/* Editor area */}
        <div
          style={{
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            overflow: 'hidden',
            minWidth: 0,
          }}
        >
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

      <style>{`
        .editor-explorer-backdrop { display: none; }
        .editor-explorer-panel { display: contents; }
        @media (max-width: 768px) {
          .editor-explorer-backdrop {
            display: block;
            position: fixed;
            inset: 0;
            background: rgba(0,0,0,0.45);
            z-index: 49;
          }
          .editor-explorer-panel {
            display: block;
            position: fixed;
            top: 40px;
            left: 0;
            bottom: 0;
            z-index: 50;
            width: 260px;
          }
          .editor-explorer-panel > * { height: 100%; }
          .editor-back-label { display: none; }
          .editor-title-label { display: none; }
          .editor-save-label { display: none; }
          .editor-new-label { display: none; }
        }
      `}</style>
    </div>
  )
}

function EmptyEditorState() {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#1e1e2e',
        color: '#6c7086',
      }}
    >
      <FontAwesomeIcon icon={faFileCode} style={{ fontSize: 48, marginBottom: 16, opacity: 0.3 }} />
      <p style={{ fontSize: 16, fontWeight: 500, marginBottom: 8, color: '#585b70' }}>
        Open a file from the tree
      </p>
      <p style={{ fontSize: 13, color: '#45475a' }}>
        Select a file in the explorer to start editing
      </p>
    </div>
  )
}

function ImageViewer({ path }: { path: string }) {
  const src = `/api/v1/files/raw/${path}`
  return (
    <div
      style={{
        flex: 1,
        overflow: 'auto',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#1e1e2e',
        padding: 24,
        gap: 12,
      }}
    >
      <img
        src={src}
        alt={path.split('/').pop()}
        style={{
          maxWidth: '100%',
          maxHeight: 'calc(100% - 40px)',
          objectFit: 'contain',
          borderRadius: 4,
          boxShadow: '0 4px 24px rgba(0,0,0,0.5)',
          // Checkerboard for transparent images
          background:
            'repeating-conic-gradient(#3a3a4a 0% 25%, #2a2a3a 0% 50%) 0 0 / 16px 16px',
        }}
      />
      <span style={{ fontSize: 12, color: '#45475a' }}>{path}</span>
    </div>
  )
}

function PdfViewer({ path }: { path: string }) {
  const src = `/api/v1/files/raw/${path}`
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <iframe
        src={src}
        title={path.split('/').pop()}
        style={{
          flex: 1,
          width: '100%',
          border: 'none',
          background: '#fff',
        }}
      />
    </div>
  )
}
