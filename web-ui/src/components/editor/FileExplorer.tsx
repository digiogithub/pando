import { useState, useCallback, useEffect } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faFolder,
  faFolderOpen,
  faFile,
  faCode,
  faFileAlt,
  faFileCode,
  faPalette,
  faSearch,
  faPlus,
  faFolderPlus,
  faXmark,
} from '@fortawesome/free-solid-svg-icons'
import type { FileNode } from '@/types'
import { useEditorStore } from '@/stores/editorStore'
import api from '@/services/api'

interface FileExplorerProps {
  files: FileNode[]
  onRefresh: () => void
  onClose?: () => void
}

interface ContextMenuState {
  visible: boolean
  x: number
  y: number
  node: FileNode | null
}

function getFileIcon(name: string, isDir: boolean, isOpen: boolean) {
  if (isDir) {
    return {
      icon: isOpen ? faFolderOpen : faFolder,
      color: 'var(--warning)',
    }
  }
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  switch (ext) {
    case 'go':
      return { icon: faCode, color: '#7dcfff' }
    case 'ts':
    case 'tsx':
      return { icon: faCode, color: '#4fc1ff' }
    case 'js':
    case 'jsx':
      return { icon: faCode, color: '#f9c74f' }
    case 'py':
      return { icon: faCode, color: '#a6e3a1' }
    case 'md':
      return { icon: faFileAlt, color: '#a0a0b0' }
    case 'json':
      return { icon: faFileCode, color: '#fab387' }
    case 'yaml':
    case 'yml':
      return { icon: faFileCode, color: '#fab387' }
    case 'css':
      return { icon: faPalette, color: '#cba6f7' }
    case 'html':
      return { icon: faFileCode, color: '#f38ba8' }
    case 'sh':
    case 'bash':
      return { icon: faCode, color: '#a6e3a1' }
    default:
      return { icon: faFile, color: 'var(--fg-muted, #a0a0b0)' }
  }
}

interface TreeNodeProps {
  node: FileNode
  depth: number
  filter: string
  onContextMenu: (e: React.MouseEvent, node: FileNode) => void
}

function TreeNode({ node, depth, filter, onContextMenu }: TreeNodeProps) {
  const { fileTreeExpanded, toggleTreeNode, openFile, setActiveFile } = useEditorStore()
  const isOpen = fileTreeExpanded[node.path] ?? false
  const { icon, color } = getFileIcon(node.name, node.is_dir, isOpen)
  const [children, setChildren] = useState<FileNode[]>([])
  const [childrenLoaded, setChildrenLoaded] = useState(false)

  // Load children when directory is first expanded
  useEffect(() => {
    if (!node.is_dir || !isOpen || childrenLoaded) return
    api
      .get<{ path: string; files: Array<{ name: string; path: string; isDir: boolean; size: number }> }>(
        `/api/v1/files?path=${encodeURIComponent(node.path)}`
      )
      .then((data) => {
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
      .catch((err) => console.error('Failed to load children:', err))
  }, [node.is_dir, node.path, isOpen, childrenLoaded])

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
      const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif']
      if (imageExts.includes(ext)) {
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
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          paddingLeft: depth * 16 + 8,
          paddingRight: 8,
          paddingTop: 4,
          paddingBottom: 4,
          cursor: 'pointer',
          borderRadius: 4,
          fontSize: 13,
          color: 'var(--fg)',
          userSelect: 'none',
          transition: 'background 0.1s',
        }}
        onMouseEnter={(e) => {
          ;(e.currentTarget as HTMLDivElement).style.background = 'var(--hover-bg)'
        }}
        onMouseLeave={(e) => {
          ;(e.currentTarget as HTMLDivElement).style.background = 'transparent'
        }}
      >
        <FontAwesomeIcon icon={icon} style={{ fontSize: 12, color, flexShrink: 0 }} />
        <span
          style={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            flex: 1,
          }}
        >
          {node.name}
        </span>
      </div>
      {node.is_dir && isOpen && (
        <div>
          {children.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              filter={filter}
              onContextMenu={onContextMenu}
            />
          ))}
          {childrenLoaded && children.length === 0 && (
            <div
              style={{
                paddingLeft: (depth + 1) * 16 + 8,
                paddingTop: 3,
                paddingBottom: 3,
                fontSize: 12,
                color: 'var(--fg-muted, #a0a0b0)',
                fontStyle: 'italic',
              }}
            >
              Empty folder
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default function FileExplorer({ files, onRefresh, onClose }: FileExplorerProps) {
  const [filter, setFilter] = useState('')
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
    const parentPath = node?.is_dir ? node.path : (node?.path.split('/').slice(0, -1).join('/') ?? '.')
    const name = window.prompt('New file name:')
    if (!name) return
    const newPath = parentPath === '.' ? name : `${parentPath}/${name}`
    try {
      await api.post('/api/v1/files', { path: newPath, content: '' })
      onRefresh()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
    closeContextMenu()
  }, [contextMenu.node, onRefresh, closeContextMenu])

  const handleNewFolder = useCallback(async () => {
    const node = contextMenu.node
    const parentPath = node?.is_dir ? node.path : (node?.path.split('/').slice(0, -1).join('/') ?? '.')
    const name = window.prompt('New folder name:')
    if (!name) return
    const newPath = parentPath === '.' ? name : `${parentPath}/${name}`
    try {
      await api.post('/api/v1/files', { path: newPath, content: null, isDir: true })
      onRefresh()
    } catch (err) {
      console.error('Failed to create folder:', err)
    }
    closeContextMenu()
  }, [contextMenu.node, onRefresh, closeContextMenu])

  const handleRename = useCallback(async () => {
    const node = contextMenu.node
    if (!node) return
    const newName = window.prompt('Rename to:', node.name)
    if (!newName || newName === node.name) return
    const parentPath = node.path.split('/').slice(0, -1).join('/') || '.'
    const newPath = parentPath === '.' ? newName : `${parentPath}/${newName}`
    try {
      await api.post('/api/v1/files/rename', { oldPath: node.path, newPath })
      onRefresh()
    } catch (err) {
      console.error('Failed to rename:', err)
    }
    closeContextMenu()
  }, [contextMenu.node, onRefresh, closeContextMenu])

  const handleDelete = useCallback(async () => {
    const node = contextMenu.node
    if (!node) return
    const confirmed = window.confirm(`Delete "${node.name}"?`)
    if (!confirmed) return
    try {
      await api.delete(`/api/v1/files/${node.path}`)
      onRefresh()
    } catch (err) {
      console.error('Failed to delete:', err)
    }
    closeContextMenu()
  }, [contextMenu.node, onRefresh, closeContextMenu])

  return (
    <div
      style={{
        width: 250,
        flexShrink: 0,
        background: 'var(--sidebar-bg)',
        borderRight: '1px solid var(--border)',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
      onClick={contextMenu.visible ? closeContextMenu : undefined}
    >
      {/* Search input */}
      <div
        style={{
          padding: '8px 10px',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            padding: '4px 8px',
          }}
        >
          <FontAwesomeIcon icon={faSearch} style={{ fontSize: 11, color: 'var(--fg-muted, #a0a0b0)', flexShrink: 0 }} />
          <input
            type="text"
            placeholder="Filter files..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            style={{
              flex: 1,
              background: 'transparent',
              border: 'none',
              outline: 'none',
              color: 'var(--fg)',
              fontSize: 12,
              fontFamily: 'inherit',
            }}
          />
        </div>
      </div>

      {/* Tree header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '6px 10px 4px',
          flexShrink: 0,
        }}
      >
        <span style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted, #a0a0b0)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
          Explorer
        </span>
        <div style={{ display: 'flex', gap: 4 }}>
          <button
            title="New File"
            onClick={() => handleNewFileFromHeader()}
            style={{
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-muted, #a0a0b0)',
              padding: 2,
              borderRadius: 3,
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted, #a0a0b0)' }}
          >
            <FontAwesomeIcon icon={faPlus} style={{ fontSize: 12 }} />
          </button>
          <button
            title="New Folder"
            onClick={() => handleNewFolderFromHeader()}
            style={{
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              color: 'var(--fg-muted, #a0a0b0)',
              padding: 2,
              borderRadius: 3,
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted, #a0a0b0)' }}
          >
            <FontAwesomeIcon icon={faFolderPlus} style={{ fontSize: 12 }} />
          </button>
          {onClose && (
            <button
              title="Hide explorer"
              onClick={onClose}
              style={{
                background: 'transparent',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--fg-muted, #a0a0b0)',
                padding: 2,
                borderRadius: 3,
              }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg)' }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.color = 'var(--fg-muted, #a0a0b0)' }}
            >
              <FontAwesomeIcon icon={faXmark} style={{ fontSize: 12 }} />
            </button>
          )}
        </div>
      </div>

      {/* File tree */}
      <div style={{ flex: 1, overflowY: 'auto', overflowX: 'hidden', padding: '0 4px 8px' }}>
        {files.map((node) => (
          <TreeNode
            key={node.path}
            node={node}
            depth={0}
            filter={filter}
            onContextMenu={handleContextMenu}
          />
        ))}
        {files.length === 0 && (
          <div
            style={{
              padding: '24px 12px',
              textAlign: 'center',
              color: 'var(--fg-muted, #a0a0b0)',
              fontSize: 12,
            }}
          >
            No files in working directory
          </div>
        )}
      </div>

      {/* Context menu */}
      {contextMenu.visible && (
        <div
          style={{
            position: 'fixed',
            top: contextMenu.y,
            left: contextMenu.x,
            background: 'var(--card-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            boxShadow: '0 4px 16px rgba(0,0,0,0.3)',
            zIndex: 1000,
            minWidth: 160,
            overflow: 'hidden',
          }}
          onClick={(e) => e.stopPropagation()}
        >
          {[
            { label: 'New File', action: handleNewFile },
            { label: 'New Folder', action: handleNewFolder },
            { label: 'Rename', action: handleRename },
            { label: 'Delete', action: handleDelete },
          ].map(({ label, action }) => (
            <button
              key={label}
              onClick={action}
              style={{
                display: 'block',
                width: '100%',
                padding: '8px 14px',
                textAlign: 'left',
                background: 'transparent',
                border: 'none',
                color: label === 'Delete' ? 'var(--error)' : 'var(--fg)',
                fontSize: 13,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--hover-bg)' }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  )

  async function handleNewFileFromHeader() {
    const name = window.prompt('New file name:')
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: '' })
      onRefresh()
    } catch (err) {
      console.error('Failed to create file:', err)
    }
  }

  async function handleNewFolderFromHeader() {
    const name = window.prompt('New folder name:')
    if (!name) return
    try {
      await api.post('/api/v1/files', { path: name, content: null, isDir: true })
      onRefresh()
    } catch (err) {
      console.error('Failed to create folder:', err)
    }
  }
}
