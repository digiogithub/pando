import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, X, FolderOpen, Folder, Lock, Bot, LoaderCircle, Play, CircleStop } from '@/components/ui/icons'
import { Badge, Button, IconButton, Input, Spinner, type BadgeTone } from '@/components/ui'
import { useProjectStore } from '@pando/client/stores/projectStore'
import type { Project } from '@pando/client/types'
import ProjectInitWizard from './ProjectInitWizard'
import DirBrowserDialog from '@/components/shared/DirBrowserDialog'
import EmptyState from '@/components/shared/EmptyState'

/** Replace leading /home/<user> or /Users/<user> with ~. */
function shortenPath(path: string): string {
  return path
    .replace(/^\/home\/[^/]+/, '~')
    .replace(/^\/Users\/[^/]+/, '~')
}

function statusTone(status: Project['status']): BadgeTone {
  switch (status) {
    case 'running': return 'success'
    case 'stopped': return 'neutral'
    case 'error': return 'danger'
    case 'initializing': return 'warning'
    case 'missing': return 'neutral'
    default: return 'neutral'
  }
}

function StatusBadge({ status }: { status: Project['status'] }) {
  return (
    <Badge tone={statusTone(status)} dot outline>
      {status}
    </Badge>
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
    stopProject,
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
  const [showBrowser, setShowBrowser] = useState(false)

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

  // Toggle a project's instance: a running instance is stopped (or, when it was
  // launched externally, the backend rejects it and the store shows a message);
  // a stopped instance is started/activated.
  const handleToggle = async (proj: Project) => {
    if (proj.status === 'running') {
      await stopProject(proj.id)
      return
    }
    await activateProject(proj.id)
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
    <div className="view">
      {/* Header */}
      <div className="view-header">
        <div className="view-header-text">
          <div className="view-title">
            <FolderOpen size={16} className="text-muted" />
            {t('nav.projects')} <span className="view-title-count">({projects.length})</span>
          </div>
        </div>

        <div className="view-header-actions">
          {activeProjectId && (
            <Button variant="secondary" onClick={deactivateProject}>
              Deactivate
            </Button>
          )}
          <Button variant="primary" icon={<Plus size={13} />} onClick={() => setShowAddForm(!showAddForm)}>
            Add Project
          </Button>
        </div>
      </div>

      {/* Inline add form */}
      {showAddForm && (
        <div className="flex flex-wrap items-center gap-2 border-b border-border bg-shell px-6 py-3">
          <Input
            type="text"
            placeholder="Path (e.g. ~/code/myapp)"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            className="min-w-[140px] flex-[2]"
            autoFocus
          />
          <IconButton aria-label="Browse for directory" tooltip icon={<Folder size={14} />} onClick={() => setShowBrowser(true)} />
          <Input
            type="text"
            placeholder="Name (optional)"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            className="min-w-[120px] flex-1"
          />
          <Button variant="primary" loading={adding} disabled={adding || !newPath.trim()} onClick={() => void handleAdd()}>
            Add
          </Button>
          <IconButton
            aria-label="Cancel"
            icon={<X size={14} />}
            onClick={() => { setShowAddForm(false); setNewPath(''); setNewName('') }}
          />
        </div>
      )}

      {/* Project list */}
      <div className="view-body">
        {loading && projects.length === 0 ? (
          <div className="flex h-full items-center justify-center gap-2 text-sm text-muted">
            <Spinner size={16} /> Loading projects…
          </div>
        ) : projects.length === 0 ? (
          <EmptyState icon={<FolderOpen size={22} />} title="No projects yet" description="Add one to get started." />
        ) : (
          <div className="view-table-wrap">
            <table className="view-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Path</th>
                  <th>Status</th>
                  <th style={{ width: 90 }} className="is-numeric">Actions</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((proj) => {
                  const isActive = proj.id === activeProjectId
                  const isRunning = proj.status === 'running'
                  const rowTitle = proj.external
                    ? 'Launched externally — close it from the application that started it'
                    : isRunning
                      ? 'Click to stop this instance'
                      : 'Click to start this instance'
                  return (
                    <tr
                      key={proj.id}
                      title={rowTitle}
                      onClick={() => void handleToggle(proj)}
                      data-clickable="true"
                      data-selected={isActive || undefined}
                    >
                      <td className={isActive ? 'font-semibold' : undefined}>{proj.name}</td>
                      <td className="is-mono is-muted">{shortenPath(proj.path)}</td>
                      <td>
                        <span className="inline-flex flex-wrap items-center gap-1.5">
                          <StatusBadge status={proj.status} />
                          {proj.external && (
                            <Badge outline icon={<Lock size={10} />} title="Launched externally (e.g. from an editor in ACP mode)">
                              external
                            </Badge>
                          )}
                          {proj.delegation_spawned && (
                            <Badge outline icon={<Bot size={10} />} title="Auto-started by the delegation router to run delegated agent loops">
                              auto
                            </Badge>
                          )}
                          {!!proj.delegations && proj.delegations > 0 && (
                            <Badge
                              tone="warning"
                              icon={<LoaderCircle size={10} className="animate-spin" />}
                              title={`${proj.delegations} delegated agent loop${proj.delegations === 1 ? '' : 's'} running inside this instance`}
                            >
                              {proj.delegations} {proj.delegations === 1 ? 'loop' : 'loops'}
                            </Badge>
                          )}
                        </span>
                      </td>
                      <td className="is-numeric" onClick={(e) => e.stopPropagation()}>
                        <div className="flex justify-end gap-1.5">
                          <IconButton
                            aria-label={proj.external ? 'Launched externally — cannot be stopped here' : isRunning ? 'Stop instance' : 'Start instance'}
                            tooltip
                            icon={proj.external ? <Lock size={13} /> : isRunning ? <CircleStop size={13} /> : <Play size={13} />}
                            size="sm"
                            variant={isRunning && !proj.external ? 'danger' : 'ghost'}
                            onClick={() => void handleToggle(proj)}
                          />
                          {pendingDelete === proj.id ? (
                            <Button variant="danger" size="sm" onClick={() => void handleDelete(proj.id)}>
                              Confirm
                            </Button>
                          ) : (
                            <IconButton
                              aria-label="Remove project"
                              tooltip
                              icon={<X size={13} />}
                              size="sm"
                              onClick={() => void handleDelete(proj.id)}
                            />
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Directory browser modal */}
      {showBrowser && (
        <DirBrowserDialog
          initialPath={newPath || '~'}
          onSelect={(path) => setNewPath(path)}
          onClose={() => setShowBrowser(false)}
        />
      )}

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
