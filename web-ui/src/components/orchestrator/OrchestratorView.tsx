import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Network, Clock } from '@/components/ui/icons'
import { Badge, Button, Spinner, Tabs, type TabItem } from '@/components/ui'
import { SectionHero } from '@/components/brand'
import { useOrchestratorStore } from '@pando/client/stores/orchestratorStore'
import type { DelegationMetrics } from '@pando/client/types'
import TaskRow from './TaskRow'
import TaskDetail from './TaskDetail'
import CreateTaskDialog from './CreateTaskDialog'
import CronJobsPanel from './CronJobsPanel'
import EmptyState from '@/components/shared/EmptyState'

const POLL_INTERVAL = 5000

type Tab = 'tasks' | 'cronjobs'

export default function OrchestratorView() {
  const { t } = useTranslation()
  const {
    tasks,
    loading,
    selectedTask,
    createDialogOpen,
    delegationMetrics,
    fetchTasks,
    fetchDelegationMetrics,
    setSelectedTask,
    setCreateDialogOpen,
  } = useOrchestratorStore()

  const [activeTab, setActiveTab] = useState<Tab>('tasks')

  // Initial fetch + polling (only when tasks tab is active)
  useEffect(() => {
    if (activeTab !== 'tasks') return
    fetchTasks()
    fetchDelegationMetrics()
    const timer = setInterval(() => {
      fetchTasks()
      fetchDelegationMetrics()
    }, POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [fetchTasks, fetchDelegationMetrics, activeTab])

  const hasRunning = tasks.some((t) => t.status === 'running')
  const runningCount = tasks.filter((t) => t.status === 'running').length

  const tabItems: TabItem<Tab>[] = [
    { value: 'tasks', label: 'Mesnada Tasks', icon: <Network size={13} /> },
    { value: 'cronjobs', label: 'CronJobs', icon: <Clock size={13} /> },
  ]

  return (
    <div className="view">
      <SectionHero
        className="px-6 pt-5"
        variant="mesnada"
        title={t('orchestrator.hero.title')}
        tagline={t('orchestrator.hero.tagline')}
      />

      {/* Tab bar */}
      <div className="border-b border-border bg-bg px-6">
        <Tabs items={tabItems} value={activeTab} onChange={setActiveTab} aria-label="Orchestrator sections" />
      </div>

      {/* Tasks tab */}
      {activeTab === 'tasks' && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {/* Toolbar */}
          <div className="view-header">
            <div className="view-header-text">
              <div className="view-title">
                Mesnada Tasks
                {hasRunning && (
                  <Badge tone="success" dot>
                    {runningCount} running
                  </Badge>
                )}
                {loading && <Spinner size={14} />}
              </div>
            </div>
            <div className="view-header-actions">
              <Button variant="primary" icon={<Plus size={13} />} onClick={() => setCreateDialogOpen(true)}>
                Create Task
              </Button>
            </div>
          </div>

          {/* Delegation metrics strip (item E1) — shown once there is any
              warm-routing or resurrection activity. */}
          <DelegationMetricsBar metrics={delegationMetrics} />

          {/* Main area: table + optional detail panel */}
          <div className="split-pane">
            {/* Task table */}
            <div className="min-w-0 flex-1 overflow-auto">
              {tasks.length === 0 && !loading ? (
                <EmptyState
                  icon={<Network size={22} />}
                  title="No tasks yet"
                  description="Create your first orchestrator task to delegate work to agents."
                  action={
                    <Button variant="primary" icon={<Plus size={13} />} onClick={() => setCreateDialogOpen(true)}>
                      Create Task
                    </Button>
                  }
                />
              ) : (
                <div className="view-table-wrap">
                  <table className="view-table">
                    <thead>
                      <tr>
                        <th>Status</th>
                        <th>Name</th>
                        <th>Agent</th>
                        <th>Model</th>
                        <th style={{ minWidth: 130 }}>Progress</th>
                        <th className="is-numeric">Tokens</th>
                        <th>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {tasks.map((task) => (
                        <TaskRow
                          key={task.id}
                          task={task}
                          selected={selectedTask?.id === task.id}
                          onClick={() =>
                            setSelectedTask(selectedTask?.id === task.id ? null : task)
                          }
                        />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>

            {/* Detail panel */}
            {selectedTask && (
              <TaskDetail task={selectedTask} onClose={() => setSelectedTask(null)} />
            )}
          </div>

          {/* Create dialog */}
          {createDialogOpen && <CreateTaskDialog />}
        </div>
      )}

      {/* CronJobs tab */}
      {activeTab === 'cronjobs' && <CronJobsPanel />}
    </div>
  )
}

function DelegationMetricsBar({ metrics }: { metrics: DelegationMetrics | null }) {
  const { t } = useTranslation()

  // Only surface once delegation has actually done something, so the strip stays
  // out of the way for users who never enable warm reuse / resurrection.
  if (
    !metrics ||
    (metrics.warm_attempts === 0 &&
      metrics.resurrections === 0 &&
      metrics.live_injections === 0)
  ) {
    return null
  }

  const hitRate = Math.round((metrics.warm_hit_rate ?? 0) * 100)

  return (
    <div className="flex flex-shrink-0 flex-wrap items-center gap-5 border-b border-border px-6 py-2 text-xs">
      <Metric label={t('orchestrator.delegationMetrics.warmHitRate')} value={`${hitRate}%`} accent />
      <Metric label={t('orchestrator.delegationMetrics.warmHits')} value={metrics.warm_hits} />
      {metrics.external_hits > 0 && (
        <Metric label={t('orchestrator.delegationMetrics.externalHits')} value={metrics.external_hits} />
      )}
      <Metric label={t('orchestrator.delegationMetrics.warmFailures')} value={metrics.warm_failures} />
      <Metric label={t('orchestrator.delegationMetrics.coldFallbacks')} value={metrics.cold_fallbacks} />
      <Metric label={t('orchestrator.delegationMetrics.capRejections')} value={metrics.cap_rejections} />
      <Metric label={t('orchestrator.delegationMetrics.resurrections')} value={metrics.resurrections} />
      <Metric label={t('orchestrator.delegationMetrics.liveInjections')} value={metrics.live_injections} />
      {metrics.external_reattach_recovered > 0 && (
        <Metric label={t('orchestrator.delegationMetrics.reattachRecovered')} value={metrics.external_reattach_recovered} />
      )}
      {metrics.external_reattach_failed > 0 && (
        <Metric label={t('orchestrator.delegationMetrics.reattachFailed')} value={metrics.external_reattach_failed} />
      )}
    </div>
  )
}

function Metric({
  label,
  value,
  accent,
}: {
  label: string
  value: string | number
  accent?: boolean
}) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="uppercase tracking-wide text-muted">{label}</span>
      <span className={`font-semibold ${accent ? 'text-success' : 'text-fg'}`}>{value}</span>
    </div>
  )
}
