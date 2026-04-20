import { useEffect, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPlus, faNetworkWired, faClock } from '@fortawesome/free-solid-svg-icons'
import { useOrchestratorStore } from '@/stores/orchestratorStore'
import TaskRow from './TaskRow'
import TaskDetail from './TaskDetail'
import CreateTaskDialog from './CreateTaskDialog'
import CronJobsPanel from './CronJobsPanel'
import EmptyState from '@/components/shared/EmptyState'
import LoadingSpinner from '@/components/shared/LoadingSpinner'

const POLL_INTERVAL = 5000

const TH_STYLE: React.CSSProperties = {
  padding: '0.5rem 0.75rem',
  fontSize: 11,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.05em',
  textAlign: 'left',
  borderBottom: '1px solid var(--border)',
  whiteSpace: 'nowrap',
  background: 'var(--surface)',
}

type Tab = 'tasks' | 'cronjobs'

export default function OrchestratorView() {
  const {
    tasks,
    loading,
    selectedTask,
    createDialogOpen,
    fetchTasks,
    setSelectedTask,
    setCreateDialogOpen,
  } = useOrchestratorStore()

  const [activeTab, setActiveTab] = useState<Tab>('tasks')

  // Initial fetch + polling (only when tasks tab is active)
  useEffect(() => {
    if (activeTab !== 'tasks') return
    fetchTasks()
    const timer = setInterval(fetchTasks, POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [fetchTasks, activeTab])

  const hasRunning = tasks.some((t) => t.status === 'running')

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
        background: 'var(--bg)',
      }}
    >
      {/* Tab bar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          borderBottom: '1px solid var(--border)',
          background: 'var(--surface)',
          padding: '0 1.25rem',
          flexShrink: 0,
          gap: '0.25rem',
        }}
      >
        <TabButton
          label="Mesnada Tasks"
          icon={faNetworkWired}
          active={activeTab === 'tasks'}
          onClick={() => setActiveTab('tasks')}
          badge={hasRunning ? tasks.filter((t) => t.status === 'running').length : undefined}
        />
        <TabButton
          label="CronJobs"
          icon={faClock}
          active={activeTab === 'cronjobs'}
          onClick={() => setActiveTab('cronjobs')}
        />
      </div>

      {/* Tasks tab */}
      {activeTab === 'tasks' && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            flex: 1,
            overflow: 'hidden',
          }}
        >
          {/* Toolbar */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              padding: '0.875rem 1.25rem',
              borderBottom: '1px solid var(--border)',
              background: 'var(--surface)',
              flexShrink: 0,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.625rem' }}>
              <h2 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
                Mesnada Tasks
              </h2>
              {hasRunning && (
                <span
                  style={{
                    fontSize: 11,
                    fontWeight: 600,
                    color: 'var(--success)',
                    background: 'color-mix(in srgb, var(--success) 15%, transparent)',
                    padding: '0.125rem 0.5rem',
                    borderRadius: 9999,
                  }}
                >
                  {tasks.filter((t) => t.status === 'running').length} running
                </span>
              )}
              {loading && <LoadingSpinner size={14} />}
            </div>

            <button
              onClick={() => setCreateDialogOpen(true)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.375rem',
                padding: '0.45rem 0.875rem',
                background: 'var(--primary)',
                border: 'none',
                borderRadius: 'var(--radius-sm)',
                cursor: 'pointer',
                color: 'white',
                fontSize: 13,
                fontWeight: 600,
              }}
            >
              <FontAwesomeIcon icon={faPlus} style={{ fontSize: 11 }} />
              Create Task
            </button>
          </div>

          {/* Main area: table + optional detail panel */}
          <div style={{ flex: 1, display: 'flex', overflow: 'hidden', minHeight: 0 }}>
            {/* Task table */}
            <div style={{ flex: 1, overflow: 'auto', minWidth: 0 }}>
              {tasks.length === 0 && !loading ? (
                <EmptyState
                  icon={<FontAwesomeIcon icon={faNetworkWired} />}
                  title="No tasks yet"
                  description="Create your first orchestrator task to delegate work to agents."
                  action={
                    <button
                      onClick={() => setCreateDialogOpen(true)}
                      style={{
                        padding: '0.5rem 1rem',
                        background: 'var(--primary)',
                        border: 'none',
                        borderRadius: 'var(--radius-sm)',
                        cursor: 'pointer',
                        color: 'white',
                        fontSize: 13,
                        fontWeight: 600,
                      }}
                    >
                      <FontAwesomeIcon icon={faPlus} style={{ marginRight: '0.375rem', fontSize: 11 }} />
                      Create Task
                    </button>
                  }
                />
              ) : (
                <table
                  style={{
                    width: '100%',
                    borderCollapse: 'collapse',
                    tableLayout: 'auto',
                  }}
                >
                  <thead>
                    <tr>
                      <th style={TH_STYLE}>Status</th>
                      <th style={TH_STYLE}>Name</th>
                      <th style={TH_STYLE}>Agent</th>
                      <th style={TH_STYLE}>Model</th>
                      <th style={{ ...TH_STYLE, minWidth: 130 }}>Progress</th>
                      <th style={{ ...TH_STYLE, textAlign: 'right' }}>Tokens</th>
                      <th style={TH_STYLE}>Actions</th>
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

function TabButton({
  label,
  icon,
  active,
  onClick,
  badge,
}: {
  label: string
  icon: typeof faNetworkWired
  active: boolean
  onClick: () => void
  badge?: number
}) {
  return (
    <button
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '0.4rem',
        padding: '0.625rem 0.875rem',
        background: 'none',
        border: 'none',
        borderBottom: active ? '2px solid var(--primary)' : '2px solid transparent',
        cursor: 'pointer',
        color: active ? 'var(--primary)' : 'var(--fg-muted)',
        fontSize: 13,
        fontWeight: active ? 600 : 400,
        marginBottom: -1,
        whiteSpace: 'nowrap',
      }}
    >
      <FontAwesomeIcon icon={icon} style={{ fontSize: 12 }} />
      {label}
      {badge !== undefined && badge > 0 && (
        <span
          style={{
            fontSize: 10,
            fontWeight: 700,
            color: 'var(--success)',
            background: 'color-mix(in srgb, var(--success) 15%, transparent)',
            padding: '0.1rem 0.4rem',
            borderRadius: 9999,
            lineHeight: '1.4',
          }}
        >
          {badge}
        </span>
      )}
    </button>
  )
}
