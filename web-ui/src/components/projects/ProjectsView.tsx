import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, X, FolderOpen, Folder, Lock, Bot, LoaderCircle, CircleStop, ExternalLink, Pencil } from '@/components/ui/icons'
import { Badge, Button, IconButton, Input, Spinner, Tooltip, type BadgeTone } from '@/components/ui'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore, type ProjectTabActionResult } from '@pando/client/stores/projectTabsStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import type { Project } from '@pando/client/types'
import ProjectInitWizard from './ProjectInitWizard'
import DirBrowserDialog from '@/components/shared/DirBrowserDialog'
import EmptyState from '@/components/shared/EmptyState'
import { useDialogs } from '@/components/shared/useDialogs'
import { isDesktop } from '@/services/desktop'

/** Replace leading /home/<user> or /Users/<user> with ~. */
function shortenPath(path: string): string {
  return path
    .replace(/^\/home\/[^/]+/, '~')
    .replace(/^\/Users\/[^/]+/, '~')
}

function statusTone(status: Project['web_state'] | undefined): BadgeTone {
  switch (status) {
    case 'running': return 'success'
    case 'error': return 'danger'
    case 'starting': return 'warning'
    case 'stopped':
    default: return 'neutral'
  }
}

function translateTabNotice(
  t: (key: string, options?: Record<string, unknown>) => string,
  result: ProjectTabActionResult,
) {
  if (!result.notice || result.code === 'already_open') return
  const message = result.error
    ? `${t(result.notice.key, result.notice.values)}: ${result.error}`
    : t(result.notice.key, result.notice.values)
  useToastStore.getState().addToast(message, result.notice.type)
}

function workspaceState(project: Project): NonNullable<Project['web_state']> {
  return project.web_state ?? 'stopped'
}

function StatusBadge({ project }: { project: Project }) {
  const { t } = useTranslation()
  const state = workspaceState(project)
  const label = state === 'running' && project.web_port
    ? t('projects.view.workspaceState.runningWithPort', { port: project.web_port })
    : t(`projects.view.workspaceState.${state}`)

  return (
    <Badge tone={statusTone(state)} dot outline>
      {label}
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
    stopProject,
    openProjectDesktop,
    deactivateProject,
    initProject,
    renameProject,
    removeProject,
    setInitDialogProject,
    connectEvents,
    disconnectEvents,
  } = useProjectStore()
  const tabs = useProjectTabsStore((state) => state.tabs)
  const { confirm, prompt, dialogs } = useDialogs()

  const [showAddForm, setShowAddForm] = useState(false)
  const [newPath, setNewPath] = useState('')
  const [newName, setNewName] = useState('')
  const [adding, setAdding] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [showBrowser, setShowBrowser] = useState(false)

  const mountedRef = useRef(false)
  const activeProject = projects.find((project) => project.id === activeProjectId) ?? null

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

  const handleOpenTab = async (projectId: string) => {
    const result = await useProjectTabsStore.getState().openTab(projectId)
    translateTabNotice(t, result)
  }

  const handleFocusOrOpen = async (proj: Project) => {
    const existingTab = tabs.find((tab) => tab.projectId === proj.id)
    if (existingTab && (existingTab.state === 'running' || existingTab.state === 'starting')) {
      useProjectTabsStore.getState().focusTab(proj.id)
      return
    }
    await handleOpenTab(proj.id)
  }

  const handleRowClick = (proj: Project) => {
    void handleOpenTab(proj.id)
  }

  const handleStop = async (proj: Project) => {
    if ((proj.delegations ?? 0) > 0) {
      const accepted = await confirm({
        title: t('projects.view.stopConfirmTitle'),
        message: t('projects.view.stopConfirmMessage', { count: proj.delegations ?? 0 }),
        confirmLabel: t('projects.view.actions.stop'),
        cancelLabel: t('projects.view.common.cancel'),
        dangerous: true,
      })
      if (!accepted) return
    }

    const running = proj.status === 'running' || ['starting', 'running', 'error'].includes(workspaceState(proj))
    if (running) {
      const stopped = await stopProject(proj.id)
      if (!stopped) return
    }

    const openTab = tabs.find((tab) => tab.projectId === proj.id)
    if (openTab) {
      const result = await useProjectTabsStore.getState().closeTab(proj.id)
      translateTabNotice(t, result)
    }
  }

  const handleRename = async (proj: Project) => {
    const name = await prompt({
      title: t('projects.view.renameDialog.title', { name: proj.name }),
      label: t('projects.view.renameDialog.label'),
      defaultValue: proj.name,
      confirmLabel: t('projects.view.renameDialog.confirm'),
      cancelLabel: t('projects.view.common.cancel'),
    })
    if (!name || name === proj.name) return
    await renameProject(proj.id, name)
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
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted">
            <Tooltip content={t('projects.view.delegationTargetTooltip')}>
              <Badge outline>{t('projects.view.delegationTargetLabel')}</Badge>
            </Tooltip>
            <span>
              {activeProject
                ? t('projects.view.delegationTargetValue', { name: activeProject.name })
                : t('projects.view.delegationTargetUnset')}
            </span>
          </div>
        </div>

        <div className="view-header-actions">
          {activeProjectId && (
            <Button variant="secondary" onClick={() => void deactivateProject()}>
              {t('projects.view.clearDelegationTarget')}
            </Button>
          )}
          <Button variant="primary" icon={<Plus size={13} />} onClick={() => setShowAddForm(!showAddForm)}>
            {t('projects.view.addProject')}
          </Button>
        </div>
      </div>

      {/* Inline add form */}
      {showAddForm && (
        <div className="flex flex-wrap items-center gap-2 border-b border-border bg-shell px-6 py-3">
          <Input
            type="text"
            placeholder={t('projects.view.addForm.pathPlaceholder')}
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            className="min-w-[140px] flex-[2]"
            autoFocus
          />
          <IconButton
            aria-label={t('projects.view.addForm.browse')}
            tooltip
            icon={<Folder size={14} />}
            onClick={() => setShowBrowser(true)}
          />
          <Input
            type="text"
            placeholder={t('projects.view.addForm.namePlaceholder')}
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleAdd() }}
            className="min-w-[120px] flex-1"
          />
          <Button variant="primary" loading={adding} disabled={adding || !newPath.trim()} onClick={() => void handleAdd()}>
            {t('projects.view.addForm.add')}
          </Button>
          <IconButton
            aria-label={t('projects.view.common.cancel')}
            icon={<X size={14} />}
            onClick={() => { setShowAddForm(false); setNewPath(''); setNewName('') }}
          />
        </div>
      )}

      {/* Project list */}
      <div className="view-body">
        {loading && projects.length === 0 ? (
          <div className="flex h-full items-center justify-center gap-2 text-sm text-muted">
            <Spinner size={16} /> {t('projects.view.loading')}
          </div>
        ) : projects.length === 0 ? (
          <EmptyState
            icon={<FolderOpen size={22} />}
            title={t('projects.view.emptyTitle')}
            description={t('projects.view.emptyDescription')}
          />
        ) : (
          <div className="view-table-wrap">
            <table className="view-table">
              <thead>
                <tr>
                  <th>{t('projects.view.columns.name')}</th>
                  <th>{t('projects.view.columns.path')}</th>
                  <th>{t('projects.view.columns.workspace')}</th>
                  <th className={`is-numeric ${isDesktop ? 'w-[11rem]' : 'w-[8.5rem]'}`}>{t('projects.view.columns.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((proj) => {
                  const isDelegationTarget = proj.id === activeProjectId
                  const workspaceIsOpen = tabs.some((tab) => tab.projectId === proj.id && (tab.state === 'running' || tab.state === 'starting'))
                  const showStopAsEnabled = proj.status === 'running'
                    || ['starting', 'running', 'error'].includes(workspaceState(proj))
                    || tabs.some((tab) => tab.projectId === proj.id)
                  return (
                    <tr
                      key={proj.id}
                      title={workspaceIsOpen ? t('projects.view.rowTitle.focusTab') : t('projects.view.rowTitle.openTab')}
                      onClick={() => handleRowClick(proj)}
                      data-clickable="true"
                      data-selected={isDelegationTarget || undefined}
                    >
                      <td className={isDelegationTarget ? 'font-semibold' : undefined}>
                        <span className="inline-flex flex-wrap items-center gap-1.5">
                          <span>{proj.name}</span>
                          {isDelegationTarget && (
                            <Tooltip content={t('projects.view.delegationTargetTooltip')}>
                              <Badge outline>{t('projects.view.badges.delegationTarget')}</Badge>
                            </Tooltip>
                          )}
                        </span>
                      </td>
                      <td className="is-mono is-muted">{shortenPath(proj.path)}</td>
                      <td>
                        <span className="inline-flex flex-wrap items-center gap-1.5">
                          <StatusBadge project={proj} />
                          {proj.external && (
                            <Badge
                              outline
                              icon={<Lock size={10} />}
                              title={t('projects.view.badgeTitles.external')}
                            >
                              {t('projects.view.badges.external')}
                            </Badge>
                          )}
                          {proj.delegation_spawned && (
                            <Badge
                              outline
                              icon={<Bot size={10} />}
                              title={t('projects.view.badgeTitles.auto')}
                            >
                              {t('projects.view.badges.auto')}
                            </Badge>
                          )}
                          {!!proj.delegations && proj.delegations > 0 && (
                            <Badge
                              tone="warning"
                              icon={<LoaderCircle size={10} className="animate-spin" />}
                              title={t('projects.view.badgeTitles.delegations', { count: proj.delegations })}
                            >
                              {t('projects.view.badges.delegations', { count: proj.delegations })}
                            </Badge>
                          )}
                        </span>
                      </td>
                      <td className="is-numeric" onClick={(e) => e.stopPropagation()}>
                        <div className="flex justify-end gap-1.5">
                          <IconButton
                            aria-label={workspaceIsOpen ? t('projects.view.actions.focusTab') : t('projects.view.actions.openTab')}
                            tooltip
                            icon={<FolderOpen size={13} />}
                            size="sm"
                            onClick={() => void handleFocusOrOpen(proj)}
                          />
                          <IconButton
                            aria-label={
                              showStopAsEnabled
                                ? t('projects.view.actions.stop')
                                : t('projects.view.actions.stopDisabled')
                            }
                            tooltip
                            icon={<CircleStop size={13} />}
                            size="sm"
                            variant={showStopAsEnabled ? 'danger' : 'ghost'}
                            disabled={!showStopAsEnabled}
                            onClick={() => void handleStop(proj)}
                          />
                          {isDesktop && (
                            <IconButton
                              aria-label={t('projects.view.actions.openInNewWindow')}
                              tooltip
                              icon={<ExternalLink size={13} />}
                              size="sm"
                              onClick={() => void openProjectDesktop(proj.id)}
                            />
                          )}
                          <IconButton
                            aria-label={t('projects.view.actions.rename')}
                            tooltip
                            icon={<Pencil size={13} />}
                            size="sm"
                            onClick={() => void handleRename(proj)}
                          />
                          {pendingDelete === proj.id ? (
                            <Button variant="danger" size="sm" onClick={() => void handleDelete(proj.id)}>
                              {t('projects.view.actions.confirmDelete')}
                            </Button>
                          ) : (
                            <IconButton
                              aria-label={t('projects.view.actions.delete')}
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
            const initialized = await initProject(initDialogProject.id)
            if (initialized) {
              setInitDialogProject(null)
            }
          }}
          onCancel={() => setInitDialogProject(null)}
        />
      )}

      {dialogs}
    </div>
  )
}
