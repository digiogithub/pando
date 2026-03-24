import { useState } from 'react'
import { useOrchestratorStore } from '@/stores/orchestratorStore'
import api from '@/services/api'

const COMMON_MODELS = [
  'claude-sonnet-4-5',
  'claude-opus-4',
  'gpt-4o',
  'gpt-4o-mini',
  'gpt-5',
  'gpt-5-mini',
]

export default function CreateTaskDialog() {
  const { setCreateDialogOpen, fetchTasks } = useOrchestratorStore()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [model, setModel] = useState(COMMON_MODELS[0])
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

  const inputStyle: React.CSSProperties = {
    width: '100%',
    padding: '0.5rem 0.75rem',
    background: 'var(--bg)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
    color: 'var(--fg)',
    fontSize: 13,
    outline: 'none',
    boxSizing: 'border-box',
  }

  const labelStyle: React.CSSProperties = {
    display: 'block',
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--fg-muted)',
    marginBottom: '0.375rem',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
  }

  return (
    /* Overlay backdrop */
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={() => setCreateDialogOpen(false)}
    >
      {/* Dialog panel */}
      <div
        style={{
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius)',
          padding: '1.5rem',
          width: 440,
          maxWidth: '90vw',
          boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
          Create Orchestrator Task
        </h3>

        <form onSubmit={handleSubmit}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            {/* Name */}
            <div>
              <label style={labelStyle}>Task Name *</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Refactor auth module"
                required
                style={inputStyle}
                autoFocus
              />
            </div>

            {/* Description / prompt */}
            <div>
              <label style={labelStyle}>Description / Prompt</label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Describe what the agent should do..."
                rows={4}
                style={{ ...inputStyle, resize: 'vertical', fontFamily: 'inherit' }}
              />
            </div>

            {/* Model */}
            <div>
              <label style={labelStyle}>Model</label>
              <select
                value={model}
                onChange={(e) => setModel(e.target.value)}
                style={{ ...inputStyle, cursor: 'pointer' }}
              >
                {COMMON_MODELS.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </div>

            {/* Error */}
            {error && (
              <div style={{ fontSize: 12, color: 'var(--error)', background: 'color-mix(in srgb, var(--error) 10%, transparent)', padding: '0.5rem 0.75rem', borderRadius: 'var(--radius-sm)' }}>
                {error}
              </div>
            )}

            {/* Actions */}
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '0.5rem' }}>
              <button
                type="button"
                onClick={() => setCreateDialogOpen(false)}
                style={{
                  padding: '0.5rem 1rem',
                  background: 'none',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--radius-sm)',
                  cursor: 'pointer',
                  color: 'var(--fg)',
                  fontSize: 13,
                }}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={submitting || !name.trim()}
                style={{
                  padding: '0.5rem 1rem',
                  background: 'var(--primary)',
                  border: 'none',
                  borderRadius: 'var(--radius-sm)',
                  cursor: submitting ? 'not-allowed' : 'pointer',
                  color: 'white',
                  fontSize: 13,
                  fontWeight: 600,
                  opacity: submitting || !name.trim() ? 0.6 : 1,
                }}
              >
                {submitting ? 'Creating…' : 'Create Task'}
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  )
}
