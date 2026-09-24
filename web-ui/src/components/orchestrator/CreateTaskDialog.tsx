import { useState } from 'react'
import { useOrchestratorStore } from '@pando/client/stores/orchestratorStore'
import ModelCombobox from '@/components/shared/ModelCombobox'
import { Button, Dialog, Input, Textarea } from '@/components/ui'
import api from '@pando/client/services/api'

export default function CreateTaskDialog() {
  const { setCreateDialogOpen, fetchTasks } = useOrchestratorStore()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [model, setModel] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      await api.post('/api/v1/orchestrator/tasks', {
        name: name.trim(),
        description: description.trim(),
        model,
      })
      await fetchTasks()
      setCreateDialogOpen(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create task')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open
      onClose={() => setCreateDialogOpen(false)}
      title="Create Orchestrator Task"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={() => setCreateDialogOpen(false)}>
            Cancel
          </Button>
          <Button type="submit" form="create-task-form" variant="primary" loading={submitting} disabled={!name.trim()}>
            {submitting ? 'Creating…' : 'Create Task'}
          </Button>
        </>
      }
    >
      <form onSubmit={handleSubmit} id="create-task-form">
        <div className="flex flex-col gap-4">
          {/* Name */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold uppercase tracking-wide text-muted">Task Name *</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Refactor auth module"
              required
              data-autofocus
            />
          </div>

          {/* Description / prompt */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold uppercase tracking-wide text-muted">Description / Prompt</label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Describe what the agent should do..."
              rows={4}
            />
          </div>

          {/* Model */}
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold uppercase tracking-wide text-muted">Model</label>
            <ModelCombobox value={model} onChange={setModel} placeholder="Default model" />
          </div>

          {/* Error */}
          {error && <div className="rounded-sm bg-danger-soft px-3 py-2 text-xs text-danger">{error}</div>}
        </div>
      </form>
    </Dialog>
  )
}
