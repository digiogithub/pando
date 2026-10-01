import { useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useProjectTabsStore, type ProjectTabActionResult } from '@pando/client/stores/projectTabsStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import { Button, EmptyState, Spinner } from '@/components/ui'
import { ArrowLeft, CircleAlert, FolderOpen, RefreshCw, X } from '@/components/ui/icons'

function translateNotice(
  t: (key: string, options?: Record<string, unknown>) => string,
  result: ProjectTabActionResult,
) {
  if (!result.notice) return
  const message = result.error ? `${t(result.notice.key)}: ${result.error}` : t(result.notice.key)
  useToastStore.getState().addToast(message, result.notice.type)
}

export default function ProjectWorkspace() {
  const navigate = useNavigate()
  const { t } = useTranslation()
  const { id = '' } = useParams<{ id: string }>()
  const project = useProjectStore((state) => state.projects.find((entry) => entry.id === id) ?? null)
  const fetchProjects = useProjectStore((state) => state.fetchProjects)
  const tab = useProjectTabsStore((state) => state.tabs.find((entry) => entry.projectId === id) ?? null)

  useEffect(() => {
    if (!id || project || tab) return
    void fetchProjects()
  }, [fetchProjects, id, project, tab])

  if (!id || (!project && !tab)) {
    return (
      <div className="project-workspace" data-project-id={id || undefined}>
        <div className="project-workspace-overlay">
          <EmptyState
            icon={<FolderOpen size={20} />}
            title={t('projects.workspace.unknownTitle')}
            description={t('projects.workspace.unknownDescription')}
            action={(
              <Button
                variant="secondary"
                icon={<ArrowLeft size={14} />}
                onClick={() => navigate('/projects')}
              >
                {t('projects.workspace.backToProjects')}
              </Button>
            )}
          />
        </div>
      </div>
    )
  }

  if (!tab) {
    return (
      <div className="project-workspace" data-project-id={id} data-state="starting">
        <div className="project-workspace-overlay">
          <EmptyState
            icon={<Spinner size={20} label={t('projects.workspace.startingTitle')} />}
            title={t('projects.workspace.startingTitle')}
            description={t('projects.workspace.startingDescription', {
              path: project?.path || id,
            })}
          />
        </div>
      </div>
    )
  }

  if (tab.state === 'running') {
    return <div className="project-workspace" data-project-id={id} data-state="running" />
  }

  if (tab.state === 'starting') {
    return (
      <div className="project-workspace" data-project-id={id} data-state="starting">
        <div className="project-workspace-overlay">
          <EmptyState
            icon={<Spinner size={20} label={t('projects.workspace.startingTitle')} />}
            title={t('projects.workspace.startingTitle')}
            description={t('projects.workspace.startingDescription', {
              path: tab.path || project?.path || tab.name,
            })}
          />
        </div>
      </div>
    )
  }

  if (tab.state === 'stopped') {
    return (
      <div className="project-workspace" data-project-id={id} data-state="stopped">
        <div className="project-workspace-overlay">
          <EmptyState
            icon={<FolderOpen size={20} />}
            title={t('projects.workspace.stoppedTitle', { name: tab.name })}
            description={t('projects.workspace.stoppedDescription')}
            action={(
              <Button
                variant="primary"
                icon={<RefreshCw size={14} />}
                onClick={async () => {
                  const result = await useProjectTabsStore.getState().restartTab(id)
                  translateNotice(t, result)
                }}
              >
                {t('projects.workspace.reopen')}
              </Button>
            )}
          />
        </div>
      </div>
    )
  }

  return (
    <div className="project-workspace" data-project-id={id} data-state="error">
      <div className="project-workspace-overlay">
        <div className="project-workspace-panel">
          <EmptyState
            icon={<CircleAlert size={20} />}
            title={t('projects.workspace.errorTitle', { name: tab.name })}
            description={t('projects.workspace.errorDescription')}
          />
          {tab.error && <pre className="project-workspace-error">{tab.error}</pre>}
          <div className="project-workspace-actions">
            <Button
              variant="primary"
              icon={<RefreshCw size={14} />}
              onClick={async () => {
                const result = await useProjectTabsStore.getState().restartTab(id)
                translateNotice(t, result)
              }}
            >
              {t('projects.workspace.restart')}
            </Button>
            <Button
              variant="secondary"
              icon={<X size={14} />}
              onClick={async () => {
                await useProjectTabsStore.getState().closeTab(id)
                navigate('/projects')
              }}
            >
              {t('projects.workspace.close')}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
