import { useState, useCallback, useEffect } from 'react'
import clsx from 'clsx'
import type { FileNode } from '@pando/client/types'
import { useEditorStore } from '@pando/client/stores/editorStore'
import api from '@pando/client/services/api'
import { useDialogs } from '@/components/shared/useDialogs'
import { IconButton, Input } from '@/components/ui'
import {
  Folder,
  FolderOpen,
  FileIcon,
  FileCode,
  FileText,
  ImageIcon,
  Plus,
  X,
} from '@/components/ui/icons'

interface FileExplorerProps {
  files: FileNode[]
  /** Bumped by the parent on every tree refresh; expanded directories re-read
   *  their children whenever it changes. */
  treeVersion?: number
  onRefresh: () => void
  onClose?: () => void
}

interface ContextMenuState {
  visible: boolean
  x: number
  y: number
  node: FileNode | null
}

const CODE_EXTS = new Set(['go', 'ts', 'tsx', 'js', 'jsx', 'py', 'css', 'html', 'sh', 'bash', 'rs', 'sql', 'lua', 'toml'])
const TEXT_EXTS = new Set(['md', 'txt', 'yaml', 'yml', 'json'])
const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'])

/** Icon shape only — no per-language colour coding, to keep one restrained accent. */
function FileTreeIcon({ name, isDir, isOpen }: { name: string; isDir: boolean; isOpen: boolean }) {
  if (isDir) return isOpen ? <FolderOpen size={15} /> : <Folder size={15} />
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  if (IMAGE_EXTS.has(ext)) return <ImageIcon size={15} />
  if (CODE_EXTS.has(ext)) return <FileCode size={15} />
  if (TEXT_EXTS.has(ext)) return <FileText size={15} />
  return <FileIcon size={15} />
}

interface TreeNodeProps {
  node: FileNode
  depth: number
  filter: string
  treeVersion: number
  onContextMenu: (e: React.MouseEvent, node: FileNode) => void
}

function TreeNode({ node, depth, filter, treeVersion, onContextMenu }: TreeNodeProps) {
  const { fileTreeExpanded, toggleTreeNode, openFile, openBinaryFile, setActiveFile } = useEditorStore()
  const isOpen = fileTreeExpanded[node.path] ?? false
  const [children, setChildren] = useState<FileNode[]>([])
  const [childrenLoaded, setChildrenLoaded] = useState(false)

  // Load children when the directory is expanded, and re-read them on every
  // tree refresh so files created or deleted outside the UI appear here too.
  useEffect(() => {
    if (!node.is_dir || !isOpen) return
    let cancelled = false
    api
      .get<{ path: string; files: Array<{ name: string; path: string; isDir: boolean; size: number }> }>(
        `/api/v1/files?path=${encodeURIComponent(node.path)}`
      )
      .then((data) => {
        if (cancelled) return
        const kids: FileNode[] = (data.files ?? []).map((f) => ({
          name: f.name,
          path: f.path,
          is_dir: f.isDir,
          size: f.size,
          children: f.isDir ? [] : undefined,
        }))
        setChildren(kids)
        setChildrenLoaded(true)
      })
      .catch((err) => {
        if (!cancelled) console.error('Failed to load children:', err)
      })
    return () => {
      cancelled = true
    }
  }, [node.is_dir, node.path, isOpen, treeVersion])

  const matchesFilter =
    filter === '' || node.name.toLowerCase().includes(filter.toLowerCase())

  const hasMatchingChildren = (n: FileNode, kids: FileNode[]): boolean => {
    if (filter === '') return true
    if (n.name.toLowerCase().includes(filter.toLowerCase())) return true
    return kids.some((c) => hasMatchingChildren(c, []))
  }

  if (!matchesFilter && !hasMatchingChildren(node, children)) return null

  const handleClick = async () => {
    if (node.is_dir) {
      toggleTreeNode(node.path)
    } else {
      const ext = node.name.split('.').pop()?.toLowerCase() ?? ''
      if (IMAGE_EXTS.has(ext)) {
        openBinaryFile(node.path, 'image')
        setActiveFile(node.path)
        return
      }
      if (ext === 'pdf') {
        openBinaryFile(node.path, 'pdf')
        setActiveFile(node.path)
        return
      }
      try {
        const data = await api.get<{ path: string; content: string }>(`/api/v1/files/${node.path}`)
        openFile(data.path, data.content)
        setActiveFile(data.path)
      } catch (err) {
        console.error('Failed to open file:', err)
      }
    }
  }

  return (
    <div>
      <div
        onClick={handleClick}
        onContextMenu={(e) => onContextMenu(e, node)}
        className="editor-tree-row"
        style={{ paddingLeft: depth * 16 + 8 }}
      >
        <span className="editor-tree-row-icon">
          <FileTreeIcon name={node.name} isDir={node.is_dir} isOpen={isOpen} />
        </span>
        <span className="editor-tree-row-label">{node.name}</span>
      </div>
      {node.is_dir && isOpen && (
        <div>
          {children.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              filter={filter}
              treeVersion={treeVersion}
              onContextMenu={onContextMenu}
            />
          ))}
          {childrenLoaded && children.length === 0 && (
            <div className="editor-tree-empty-child" style={{ paddingLeft: (depth + 1) * 16 + 8 }}>
              Empty folder
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default function FileExplorer({ files, treeVersion = 0, onRefresh, onClose }: FileExplorerProps) {
  const [filter, setFilter] = useState('')
  const closeFile = useEditorStore((s) => s.closeFile)
  const { confirm, prompt, dialogs } = useDialogs()
  const [contextMenu, setContextMenu] = useState<ContextMenuState>({
    visible: false,
    x: 0,
    y: 0,
    node: null,
  })

  const handleContextMenu = useCallback((e: React.MouseEvent, node: FileNode) => {
    e.preventDefault()
    setContextMenu({ visible: true, x: e.clientX, y: e.clientY, node })
  }, [])

  const closeContextMenu = useCallback(() => {
    setContextMenu((s) => ({ ...s, visible: false }))
  }, [])

  const handleNewFile = useCallback(async () => {
    const node = contextMenu.node
    closeContextMenu()
    const parentPath = node?.is_dir ? node.path : (node?.path.split('/').slice(0, -1).join('/') || '.')
    const name = await prompt({ title: 'New file', label: 'File name', confirmLabel: 'Create' })
    if (!name) return
    const newPath = parentPath === '.' ? name : `${parentPath}/${name}`
    try {
      await api.post('/api/v1/files', { path: newPath, content: '' })
      onRefresh()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
  }, [contextMenu.node, onRefresh, closeContextMenu, prompt])

  const handleNewFolder = useCallback(async () => {
    const node = contextMenu.node
    closeContextMenu()
    const parentPath = node?.is_dir ? node.path : (node?.path.split('/').slice(0, -1).join('/') || '.')
    const name = await prompt({ title: 'New folder', label: 'Folder name', confirmLabel: 'Create' })
    if (!name) return
    const newPath = parentPath === '.' ? name : `${parentPath}/${name}`
    try {
      await api.post('/api/v1/files', { path: newPath, content: null, isDir: true })
      onRefresh()
    } catch (err) {
      console.error('Failed to create folder:', err)
    }
  }, [contextMenu.node, onRefresh, closeContextMenu, prompt])

  const handleRename = useCallback(async () => {
    const node = contextMenu.node
    closeContextMenu()
    if (!node) return
    const newName = await prompt({ title: `Rename "${node.name}"`, label: 'New name', defaultValue: node.name, confirmLabel: 'Rename' })
    if (!newName || newName === node.name) return
    const parentPath = node.path.split('/').slice(0, -1).join('/') || '.'
    const newPath = parentPath === '.' ? newName : `${parentPath}/${newName}`
    try {
      await api.post('/api/v1/files/rename', { oldPath: node.path, newPath })
      onRefresh()
    } catch (err) {
      console.error('Failed to rename:', err)
    }
  }, [contextMenu.node, onRefresh, closeContextMenu, prompt])

  const handleDelete = useCallback(async () => {
    const node = contextMenu.node
    closeContextMenu()
    if (!node) return
    const confirmed = await confirm({
      title: node.is_dir ? 'Delete folder' : 'Delete file',
      message: node.is_dir
        ? `Delete "${node.name}" and everything inside it? This cannot be undone.`
        : `Delete "${node.name}"? This cannot be undone.`,
      confirmLabel: 'Delete',
      dangerous: true,
    })
    if (!confirmed) return
    try {
      await api.delete(`/api/v1/files/${node.path.split('/').map(encodeURIComponent).join('/')}`)
      if (!node.is_dir) closeFile(node.path)
      onRefresh()
    } catch (err) {
      console.error('Failed to delete:', err)
    }
  }, [contextMenu.node, onRefresh, closeContextMenu, confirm, closeFile])

  async function handleNewFileFromHeader() {
    const name = await prompt({ title: 'New file', label: 'File name', confirmLabel: 'Create' })
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: '' })
      onRefresh()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
  }

  async function handleNewFolderFromHeader() {
    const name = await prompt({ title: 'New folder', label: 'Folder name', confirmLabel: 'Create' })
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: null, isDir: true })
      onRefresh()
    } catch (err) {
      console.error('Failed to create folder:', err)
    }
  }

  return (
    <div className="editor-explorer" onClick={contextMenu.visible ? closeContextMenu : undefined}>
      {/* Search input */}
      <div className="editor-explorer-search">
        <Input
          size="sm"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter files…"
          aria-label="Filter files"
        />
      </div>

      {/* Tree header */}
      <div className="editor-explorer-header">
        <span className="editor-explorer-label">Explorer</span>
        <div className="editor-explorer-actions">
          <IconButton aria-label="New file" tooltip icon={<Plus size={13} />} size="sm" onClick={() => void handleNewFileFromHeader()} />
          <IconButton aria-label="New folder" tooltip icon={<Folder size={13} />} size="sm" onClick={() => void handleNewFolderFromHeader()} />
          {onClose && <IconButton aria-label="Hide explorer" tooltip icon={<X size={13} />} size="sm" onClick={onClose} />}
        </div>
      </div>

      {/* File tree */}
      <div className="editor-tree">
        {files.map((node) => (
          <TreeNode
            key={node.path}
            node={node}
            depth={0}
            filter={filter}
            treeVersion={treeVersion}
            onContextMenu={handleContextMenu}
          />
        ))}
        {files.length === 0 && <div className="editor-tree-empty">No files in working directory</div>}
      </div>

      {/* Context menu */}
      {contextMenu.visible && (
        <div
          className="editor-context-menu"
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onClick={(e) => e.stopPropagation()}
        >
          {[
            { label: 'New File', action: handleNewFile, danger: false },
            { label: 'New Folder', action: handleNewFolder, danger: false },
            { label: 'Rename', action: handleRename, danger: false },
            { label: 'Delete', action: handleDelete, danger: true },
          ].map(({ label, action, danger }) => (
            <button
              key={label}
              type="button"
              onClick={action}
              className={clsx('ui-menu-item', danger && 'ui-menu-item--danger')}
            >
              <span className="ui-menu-item-label">{label}</span>
            </button>
          ))}
        </div>
      )}
      {dialogs}
    </div>
  )
}
