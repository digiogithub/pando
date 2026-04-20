import { useEffect, useState } from 'react'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faPlus, faClock, faPlay, faTrash, faToggleOn, faToggleOff, faTimes, faCheck,
} from '@fortawesome/free-solid-svg-icons'
import { useCronJobsStore } from '@/stores/cronJobsStore'
import type { CronJobCreate } from '@/types'
import LoadingSpinner from '@/components/shared/LoadingSpinner'
import EmptyState from '@/components/shared/EmptyState'

const POLL_INTERVAL = 30_000

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

const TD_STYLE: React.CSSProperties = {
  padding: '0.5rem 0.75rem',
  fontSize: 13,
  color: 'var(--fg)',
  borderBottom: '1px solid var(--border)',
  verticalAlign: 'middle',
}

const EMPTY_FORM: CronJobCreate = {
  name: '',
  schedule: '',
  prompt: '',
  enabled: true,
  engine: '',
  model: '',
  timeout: '',
}

function formatNextRun(nextRun?: string): string {
  if (!nextRun) return '—'
  const d = new Date(nextRun)
  if (isNaN(d.getTime()) || d.getFullYear() <= 1970) return '—'
  return d.toLocaleString()
}

export default function CronJobsPanel() {
  const { jobs, loading, fetchJobs, runJob, toggleEnabled, createJob, deleteJob } =
    useCronJobsStore()

  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState<CronJobCreate>(EMPTY_FORM)
  const [submitting, setSubmitting] = useState(false)
  const [runningJobs, setRunningJobs] = useState<Set<string>>(new Set())

  useEffect(() => {
    void fetchJobs()
    const timer = setInterval(() => void fetchJobs(), POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [fetchJobs])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    try {
      await createJob(form)
      setForm(EMPTY_FORM)
      setShowForm(false)
    } catch {
      // toast already shown by store
    } finally {
      setSubmitting(false)
    }
  }

  const handleRun = async (name: string) => {
    setRunningJobs((s) => new Set(s).add(name))
    try {
      await runJob(name)
    } finally {
      setRunningJobs((s) => {
        const next = new Set(s)
        next.delete(name)
        return next
      })
    }
  }

  const handleDelete = async (name: string) => {
    if (!confirm(`Delete cronjob "${name}"?`)) return
    await deleteJob(name)
  }

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
            Scheduled CronJobs
          </h2>
          {loading && <LoadingSpinner size={14} />}
        </div>

        <button
          onClick={() => setShowForm((v) => !v)}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.375rem',
            padding: '0.45rem 0.875rem',
            background: showForm ? 'var(--fg-muted)' : 'var(--primary)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            cursor: 'pointer',
            color: 'white',
            fontSize: 13,
            fontWeight: 600,
          }}
        >
          <FontAwesomeIcon icon={showForm ? faTimes : faPlus} style={{ fontSize: 11 }} />
          {showForm ? 'Cancel' : 'New CronJob'}
        </button>
      </div>

      {/* Create form */}
      {showForm && (
        <form
          onSubmit={(e) => void handleSubmit(e)}
          style={{
            padding: '1rem 1.25rem',
            borderBottom: '1px solid var(--border)',
            background: 'var(--surface)',
            display: 'grid',
            gridTemplateColumns: '1fr 1fr',
            gap: '0.75rem',
            flexShrink: 0,
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Name *
            </label>
            <input
              required
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="e.g. daily-summary"
              style={inputStyle}
            />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Schedule * <span style={{ fontWeight: 400, textTransform: 'none' }}>(cron: min hr dom mon dow)</span>
            </label>
            <input
              required
              value={form.schedule}
              onChange={(e) => setForm((f) => ({ ...f, schedule: e.target.value }))}
              placeholder="e.g. 0 9 * * 1-5"
              style={inputStyle}
            />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', gridColumn: '1 / -1' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Prompt *
            </label>
            <textarea
              required
              rows={3}
              value={form.prompt}
              onChange={(e) => setForm((f) => ({ ...f, prompt: e.target.value }))}
              placeholder="Describe what the agent should do when this job fires..."
              style={{ ...inputStyle, resize: 'vertical', fontFamily: 'inherit' }}
            />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Engine
            </label>
            <input
              value={form.engine ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, engine: e.target.value }))}
              placeholder="e.g. claude"
              style={inputStyle}
            />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Model
            </label>
            <input
              value={form.model ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
              placeholder="e.g. sonnet"
              style={inputStyle}
            />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            <label style={{ fontSize: 11, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase' }}>
              Timeout
            </label>
            <input
              value={form.timeout ?? ''}
              onChange={(e) => setForm((f) => ({ ...f, timeout: e.target.value }))}
              placeholder="e.g. 5m"
              style={inputStyle}
            />
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <input
              type="checkbox"
              id="enabled-check"
              checked={form.enabled}
              onChange={(e) => setForm((f) => ({ ...f, enabled: e.target.checked }))}
            />
            <label htmlFor="enabled-check" style={{ fontSize: 13, color: 'var(--fg)' }}>
              Enabled
            </label>
          </div>

          <div style={{ gridColumn: '1 / -1', display: 'flex', justifyContent: 'flex-end', gap: '0.5rem' }}>
            <button
              type="button"
              onClick={() => { setShowForm(false); setForm(EMPTY_FORM) }}
              style={{
                padding: '0.45rem 0.875rem',
                background: 'transparent',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                cursor: 'pointer',
                color: 'var(--fg-muted)',
                fontSize: 13,
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.375rem',
                padding: '0.45rem 0.875rem',
                background: 'var(--primary)',
                border: 'none',
                borderRadius: 'var(--radius-sm)',
                cursor: submitting ? 'not-allowed' : 'pointer',
                color: 'white',
                fontSize: 13,
                fontWeight: 600,
                opacity: submitting ? 0.7 : 1,
              }}
            >
              <FontAwesomeIcon icon={faCheck} style={{ fontSize: 11 }} />
              {submitting ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      )}

      {/* Table */}
      <div style={{ flex: 1, overflow: 'auto', minHeight: 0 }}>
        {jobs.length === 0 && !loading ? (
          <EmptyState
            icon={<FontAwesomeIcon icon={faClock} />}
            title="No cronjobs configured"
            description="Create your first scheduled job to run prompts automatically on a cron schedule."
            action={
              <button
                onClick={() => setShowForm(true)}
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
                New CronJob
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
                <th style={TH_STYLE}>Name</th>
                <th style={TH_STYLE}>Schedule</th>
                <th style={TH_STYLE}>Enabled</th>
                <th style={TH_STYLE}>Engine / Model</th>
                <th style={TH_STYLE}>Next Run</th>
                <th style={{ ...TH_STYLE, textAlign: 'right' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.name}>
                  <td style={TD_STYLE}>
                    <span style={{ fontWeight: 600 }}>{job.name}</span>
                    {job.prompt && (
                      <div
                        style={{
                          fontSize: 11,
                          color: 'var(--fg-muted)',
                          marginTop: 2,
                          maxWidth: 300,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                        title={job.prompt}
                      >
                        {job.prompt}
                      </div>
                    )}
                  </td>
                  <td style={TD_STYLE}>
                    <code
                      style={{
                        fontSize: 12,
                        background: 'var(--code-bg, color-mix(in srgb, var(--fg) 8%, transparent))',
                        padding: '0.1rem 0.35rem',
                        borderRadius: 4,
                      }}
                    >
                      {job.schedule}
                    </code>
                  </td>
                  <td style={TD_STYLE}>
                    <button
                      onClick={() => void toggleEnabled(job.name)}
                      title={job.enabled ? 'Disable' : 'Enable'}
                      style={{
                        background: 'none',
                        border: 'none',
                        cursor: 'pointer',
                        color: job.enabled ? 'var(--success)' : 'var(--fg-dim)',
                        fontSize: 18,
                        padding: 0,
                        lineHeight: 1,
                      }}
                    >
                      <FontAwesomeIcon icon={job.enabled ? faToggleOn : faToggleOff} />
                    </button>
                  </td>
                  <td style={TD_STYLE}>
                    <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
                      {[job.engine, job.model].filter(Boolean).join(' / ') || '—'}
                    </span>
                  </td>
                  <td style={TD_STYLE}>
                    <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
                      {formatNextRun(job.nextRun)}
                    </span>
                  </td>
                  <td style={{ ...TD_STYLE, textAlign: 'right' }}>
                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.375rem' }}>
                      <ActionButton
                        icon={faPlay}
                        title="Run now"
                        loading={runningJobs.has(job.name)}
                        onClick={() => void handleRun(job.name)}
                        color="var(--primary)"
                      />
                      <ActionButton
                        icon={faTrash}
                        title="Delete"
                        onClick={() => void handleDelete(job.name)}
                        color="var(--error)"
                      />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

const inputStyle: React.CSSProperties = {
  padding: '0.45rem 0.625rem',
  background: 'var(--input-bg, var(--bg))',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 13,
  width: '100%',
  boxSizing: 'border-box',
}

function ActionButton({
  icon,
  title,
  onClick,
  color,
  loading = false,
}: {
  icon: typeof faPlay
  title: string
  onClick: () => void
  color: string
  loading?: boolean
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      disabled={loading}
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 28,
        height: 28,
        background: `color-mix(in srgb, ${color} 12%, transparent)`,
        border: `1px solid color-mix(in srgb, ${color} 30%, transparent)`,
        borderRadius: 'var(--radius-sm)',
        cursor: loading ? 'not-allowed' : 'pointer',
        color,
        fontSize: 12,
        opacity: loading ? 0.6 : 1,
      }}
    >
      {loading ? <LoadingSpinner size={10} /> : <FontAwesomeIcon icon={icon} />}
    </button>
  )
}
