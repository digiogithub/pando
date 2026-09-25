import { useEffect, useState } from 'react'
import { Plus, Clock, Play, Trash2, X, Check } from '@/components/ui/icons'
import { Button, IconButton, Input, Spinner, Switch, Textarea } from '@/components/ui'
import { useCronJobsStore } from '@pando/client/stores/cronJobsStore'
import type { CronJobCreate } from '@pando/client/types'
import EmptyState from '@/components/shared/EmptyState'
import { useDialogs } from '@/components/shared/useDialogs'

const POLL_INTERVAL = 30_000

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
  const { confirm, dialogs } = useDialogs()

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
    const ok = await confirm({
      title: 'Delete cronjob',
      message: `Delete cronjob "${name}"?`,
      confirmLabel: 'Delete',
      dangerous: true,
    })
    if (!ok) return
    await deleteJob(name)
  }

  return (
    <div className="view">
      {/* Toolbar */}
      <div className="view-header">
        <div className="view-header-text">
          <div className="view-title">
            Scheduled CronJobs
            {loading && <Spinner size={14} />}
          </div>
        </div>
        <div className="view-header-actions">
          <Button
            variant={showForm ? 'secondary' : 'primary'}
            icon={showForm ? <X size={13} /> : <Plus size={13} />}
            onClick={() => setShowForm((v) => !v)}
          >
            {showForm ? 'Cancel' : 'New CronJob'}
          </Button>
        </div>
      </div>

      {/* Create form */}
      {showForm && (
        <form onSubmit={(e) => void handleSubmit(e)} className="inline-form">
          <div className="inline-form-grid">
            <div className="inline-form-field">
              <label className="inline-form-label">Name *</label>
              <Input
                required
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="e.g. daily-summary"
              />
            </div>

            <div className="inline-form-field">
              <label className="inline-form-label">
                Schedule * <span className="normal-case font-normal">(cron: min hr dom mon dow)</span>
              </label>
              <Input
                required
                value={form.schedule}
                onChange={(e) => setForm((f) => ({ ...f, schedule: e.target.value }))}
                placeholder="e.g. 0 9 * * 1-5"
              />
            </div>

            <div className="inline-form-field" style={{ gridColumn: '1 / -1' }}>
              <label className="inline-form-label">Prompt *</label>
              <Textarea
                required
                rows={3}
                value={form.prompt}
                onChange={(e) => setForm((f) => ({ ...f, prompt: e.target.value }))}
                placeholder="Describe what the agent should do when this job fires..."
              />
            </div>

            <div className="inline-form-field">
              <label className="inline-form-label">Engine</label>
              <Input
                value={form.engine ?? ''}
                onChange={(e) => setForm((f) => ({ ...f, engine: e.target.value }))}
                placeholder="e.g. claude"
              />
            </div>

            <div className="inline-form-field">
              <label className="inline-form-label">Model</label>
              <Input
                value={form.model ?? ''}
                onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
                placeholder="e.g. sonnet"
              />
            </div>

            <div className="inline-form-field">
              <label className="inline-form-label">Timeout</label>
              <Input
                value={form.timeout ?? ''}
                onChange={(e) => setForm((f) => ({ ...f, timeout: e.target.value }))}
                placeholder="e.g. 5m"
              />
            </div>

            <div className="flex items-center gap-2.5">
              <Switch
                id="enabled-check"
                checked={form.enabled}
                onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
              />
              <label htmlFor="enabled-check" className="text-sm text-fg">
                Enabled
              </label>
            </div>

            <div className="inline-form-actions">
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setShowForm(false)
                  setForm(EMPTY_FORM)
                }}
              >
                Cancel
              </Button>
              <Button type="submit" variant="primary" icon={<Check size={13} />} loading={submitting}>
                {submitting ? 'Creating…' : 'Create'}
              </Button>
            </div>
          </div>
        </form>
      )}

      {/* Table */}
      <div className="view-body">
        {jobs.length === 0 && !loading ? (
          <EmptyState
            icon={<Clock size={22} />}
            title="No cronjobs configured"
            description="Create your first scheduled job to run prompts automatically on a cron schedule."
            action={
              <Button variant="primary" icon={<Plus size={13} />} onClick={() => setShowForm(true)}>
                New CronJob
              </Button>
            }
          />
        ) : (
          <div className="view-table-wrap">
            <table className="view-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Schedule</th>
                  <th>Enabled</th>
                  <th>Engine / Model</th>
                  <th>Next Run</th>
                  <th className="is-numeric">Actions</th>
                </tr>
              </thead>
              <tbody>
                {jobs.map((job) => (
                  <tr key={job.name}>
                    <td>
                      <span className="font-semibold">{job.name}</span>
                      {job.prompt && (
                        <div className="is-ellipsis mt-0.5 text-xs text-muted" style={{ maxWidth: 300 }} title={job.prompt}>
                          {job.prompt}
                        </div>
                      )}
                    </td>
                    <td>
                      <code className="rounded bg-raised px-1.5 py-0.5 text-xs">{job.schedule}</code>
                    </td>
                    <td>
                      <Switch
                        checked={job.enabled}
                        onCheckedChange={() => void toggleEnabled(job.name)}
                        aria-label={job.enabled ? 'Disable' : 'Enable'}
                      />
                    </td>
                    <td className="is-muted">{[job.engine, job.model].filter(Boolean).join(' / ') || '—'}</td>
                    <td className="is-muted">{formatNextRun(job.nextRun)}</td>
                    <td className="is-numeric">
                      <div className="flex justify-end gap-1.5">
                        <IconButton
                          aria-label="Run now"
                          tooltip
                          icon={<Play size={13} />}
                          size="sm"
                          loading={runningJobs.has(job.name)}
                          onClick={() => void handleRun(job.name)}
                        />
                        <IconButton
                          aria-label="Delete"
                          tooltip
                          icon={<Trash2 size={13} />}
                          size="sm"
                          variant="danger"
                          onClick={() => void handleDelete(job.name)}
                        />
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {dialogs}
    </div>
  )
}
