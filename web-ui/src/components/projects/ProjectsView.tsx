import { useEffect, useRef, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus, faTimes, faFolderOpen, faSpinner } from '@fortawesome/free-solid-svg-icons'
import { useTranslation } from 'react-i18next'
import { useProjectStore } from '@/stores/projectStore'
import type { Project } from '@/types'
import ProjectInitWizard from './ProjectInitWizard'

/** Replace leading /home/<user> or /Users/<user> with ~. */
function shortenPath(path: string): string {
  return path
    .replace(/^\/home\/[^/]+/, '~')
    .replace(/^\/Users\/[^/]+/, '~')
}

function statusColor(status: Project['status']): string {
  switch (status) {
    case 'running': return 'var(--primary)'
    case 'stopped': return 'var(--fg-muted)'
    case 'error': return '#e55'
    case 'initializing': return '#fa0'
    case 'missing': return 'var(--fg-muted)'
    default: return 'var(--fg-muted)'
  }
}

function StatusBadge({ status }: { status: Project['status'] }) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '0.3rem',
        fontSize: 11,
        color: statusColor(status),
        background: 'var(--sidebar-bg)',
        borderRadius: 'var(--radius-sm)',
        padding: '0.2rem 0.5rem',
        border: `1px solid ${statusColor(status)}`,
        whiteSpace: 'nowrap',
      }}
    >
      <span
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: statusColor(status),
          display: 'inline-block',
          flexShrink: 0,
        }}
      />
      {status}
    </span>
  )
}

export default function ProjectsView() {
  const { t } = useTranslation()
  const {
    projects,
    activeProjectId,
    loading,
    initDialogProject,
    fetchProjects,
    fetchActive,
    addProject,
    activateProject,
    deactivateProject,
    initProject,
    removeProject,
    setInitDialogProject,
    connectEvents,
    disconnectEvents,
  } = useProjectStore()

  const [showAddForm, setShowAddForm] = useState(false)
  const [newPath, setNewPath] = useState('')
  const [newName, setNewName] = useState('')
  const [adding, setAdding] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  const mountedRef = useRef(false)

  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true
      void fetchProjects()
      void fetchActive()
      connectEvents()
    }
    return () => {
      disconnectEvents()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleAdd = async () => {
    if (!newPath.trim()) return
    setAdding(true)
    await addProject(newPath.trim(), newName.trim() || undefined)
    setNewPath('')
    setNewName('')
    setShowAddForm(false)
    setAdding(false)
  }

  const handleActivate = async (id: string) => {
    await activateProject(id)
  }

  const handleDelete = async (id: string) => {
    if (pendingDelete !== id) {
      setPendingDelete(id)
      return
    }
    setPendingDelete(null)
    await removeProject(id)
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--bg)',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '1rem 1.5rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
          flexWrap: 'wrap',
          gap: '0.5rem',
        }}
      >
        <div>
          <h2 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
            <FontAwesomeIcon icon={faFolderOpen} style={{ marginRight: '0.5rem', color: 'var(--primary)' }} />
            {t('nav.projects')}{' '}
            <span style={{ fontSize: 13, fontWeight: 400, color: 'var(--fg-muted)' }}>
              ({projects.length})
            </span>
          </h2>
        </div>

        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          {activeProjectId && (
            <button
              onClick={deactivateProject}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.375rem',
                padding: '0.5rem 0.875rem',
                borderRadius: 'var(--radius-sm)',
                border: '1px solid var(--border)',
                background: 'transparent',
                color: 'var(--fg-muted)',
                fontSize: 13,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
            >
              Deactivate
            </button>
          )}
          <button
            onClick={() => setShowAddForm(!showAddForm)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '0.375rem',
              padding: '0.5rem 1rem',
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
            <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
            Add Project
          </button>
        </div>
      </div>

      {/* Inline add form */}
      {showAddForm && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
            padding: '0.75rem 1.5rem',
            borderBottom: '1px solid var(--border)',
            background: 'var(--sidebar-bg)',
            flexWrap: 'wrap',
          }}
        >
          <input
            type="text"
            placeholder="Path (e.g. ~/code/myapp)"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            style={{
              flex: 2,
              minWidth: 180,
              padding: '0.4rem 0.6rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'var(--bg)',
              color: 'var(--fg)',
              fontSize: 13,
              fontFamily: 'inherit',
            }}
            autoFocus
          />
          <input
            type="text"
            placeholder="Name (optional)"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            style={{
              flex: 1,
              minWidth: 120,
              padding: '0.4rem 0.6rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'var(--bg)',
              color: 'var(--fg)',
              fontSize: 13,
              fontFamily: 'inherit',
            }}
          />
          <button
            onClick={handleAdd}
            disabled={adding || !newPath.trim()}
            style={{
              padding: '0.4rem 0.875rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: adding || !newPath.trim() ? 'not-allowed' : 'pointer',
              opacity: adding || !newPath.trim() ? 0.6 : 1,
              fontFamily: 'inherit',
            }}
          >
            {adding ? <FontAwesomeIcon icon={faSpinner} spin /> : 'Add'}
          </button>
          <button
            onClick={() => { setShowAddForm(false); setNewPath(''); setNewName('') }}
            style={{
              padding: '0.4rem 0.5rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg-muted)',
              fontSize: 13,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            <FontAwesomeIcon icon={faTimes} />
          </button>
        </div>
      )}

      {/* Project list */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        {loading && projects.length === 0 ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: 'var(--fg-muted)',
              gap: '0.5rem',
            }}
          >
            <FontAwesomeIcon icon={faSpinner} spin />
            Loading projects…
          </div>
        ) : projects.length === 0 ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              color: 'var(--fg-muted)',
              gap: '0.75rem',
            }}
          >
            <FontAwesomeIcon icon={faFolderOpen} style={{ fontSize: 32, opacity: 0.3 }} />
            <p style={{ margin: 0, fontSize: 14 }}>No projects yet. Add one to get started.</p>
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                <th style={{ padding: '0.5rem 1.5rem', textAlign: 'left', fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Name</th>
                <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Path</th>
                <th style={{ padding: '0.5rem 1rem', textAlign: 'left', fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Status</th>
                <th style={{ padding: '0.5rem 1rem', width: 40 }} />
              </tr>
            </thead>
            <tbody>
              {projects.map((proj) => {
                const isActive = proj.id === activeProjectId
                return (
                  <tr
                    key={proj.id}
                    onClick={() => void handleActivate(proj.id)}
                    style={{
                      background: isActive ? 'var(--selected)' : 'transparent',
                      borderBottom: '1px solid var(--border)',
                      cursor: 'pointer',
                      transition: 'background 0.1s',
                    }}
                    onMouseEnter={(e) => {
                      if (!isActive) (e.currentTarget as HTMLTableRowElement).style.background = 'var(--sidebar-bg)'
                    }}
                    onMouseLeave={(e) => {
                      if (!isActive) (e.currentTarget as HTMLTableRowElement).style.background = 'transparent'
                    }}
                  >
                    <td style={{ padding: '0.75rem 1.5rem', fontSize: 13, fontWeight: isActive ? 600 : 400, color: 'var(--fg)' }}>
                      {proj.name}
                    </td>
                    <td style={{ padding: '0.75rem 1rem', fontSize: 12, color: 'var(--fg-muted)', fontFamily: 'monospace' }}>
                      {shortenPath(proj.path)}
                    </td>
                    <td style={{ padding: '0.75rem 1rem' }}>
                      <StatusBadge status={proj.status} />
                    </td>
                    <td
                      style={{ padding: '0.75rem 1rem', textAlign: 'right' }}
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        title={pendingDelete === proj.id ? 'Click again to confirm' : 'Remove project'}
                        onClick={() => void handleDelete(proj.id)}
                        style={{
                          background: pendingDelete === proj.id ? '#e55' : 'transparent',
                          border: pendingDelete === proj.id ? 'none' : '1px solid var(--border)',
                          borderRadius: 'var(--radius-sm)',
                          color: pendingDelete === proj.id ? 'white' : 'var(--fg-muted)',
                          cursor: 'pointer',
                          padding: '0.25rem 0.4rem',
                          fontSize: 11,
                          lineHeight: 1,
                          fontFamily: 'inherit',
                        }}
                      >
                        {pendingDelete === proj.id ? 'Confirm' : <FontAwesomeIcon icon={faTimes} />}
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      {/* Init wizard modal */}
      {initDialogProject && (
        <ProjectInitWizard
          project={initDialogProject}
          onConfirm={async () => {
            await initProject(initDialogProject.id)
            setInitDialogProject(null)
          }}
          onCancel={() => setInitDialogProject(null)}
        />
      )}
    </div>
  )
}
