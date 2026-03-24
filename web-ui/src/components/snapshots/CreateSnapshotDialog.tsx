import { useState } from 'react'
import { TextInput } from '@/components/shared/FormInput'
import { useSnapshotsStore } from '@/stores/snapshotsStore'

export default function CreateSnapshotDialog() {
  const [name, setName] = useState('')
  const { creating, createSnapshot, setCreateDialogOpen } = useSnapshotsStore()

  const handleCreate = async () => {
    const trimmed = name.trim()
    if (!trimmed) return
    await createSnapshot(trimmed)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleCreate()
    if (e.key === 'Escape') setCreateDialogOpen(false)
  }

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={() => setCreateDialogOpen(false)}
    >
      <div
        style={{
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: 400,
          maxWidth: '90vw',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <h3
          style={{
            fontSize: 16,
            fontWeight: 700,
            color: 'var(--fg)',
            marginBottom: '1.25rem',
          }}
        >
          Create Snapshot
        </h3>

        <div style={{ marginBottom: '1.5rem' }}>
          <TextInput
            label="Snapshot Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. before-refactor"
            autoFocus
            disabled={creating}
          />
        </div>

        <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
          <button
            onClick={() => setCreateDialogOpen(false)}
            disabled={creating}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg)',
              fontSize: 14,
              cursor: 'pointer',
              fontFamily: 'inherit',
              opacity: creating ? 0.5 : 1,
            }}
          >
            Cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !name.trim()}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 14,
              fontWeight: 600,
              cursor: creating || !name.trim() ? 'not-allowed' : 'pointer',
              fontFamily: 'inherit',
              opacity: creating || !name.trim() ? 0.5 : 1,
            }}
          >
            {creating ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  )
}
