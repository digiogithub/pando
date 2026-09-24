import { useState } from 'react'
import { Button, Dialog } from '@/components/ui'
import { TextInput } from '@/components/shared/FormInput'
import { useSnapshotsStore } from '@pando/client/stores/snapshotsStore'

export default function CreateSnapshotDialog() {
  const [name, setName] = useState('')
  const { creating, createSnapshot, setCreateDialogOpen } = useSnapshotsStore()

  const handleCreate = async () => {
    const trimmed = name.trim()
    if (!trimmed) return
    await createSnapshot(trimmed)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') void handleCreate()
    if (e.key === 'Escape') setCreateDialogOpen(false)
  }

  return (
    <Dialog
      open
      onClose={() => setCreateDialogOpen(false)}
      title="Create Snapshot"
      size="sm"
      footer={
        <>
          <Button variant="secondary" disabled={creating} onClick={() => setCreateDialogOpen(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={creating} disabled={!name.trim()} onClick={() => void handleCreate()}>
            {creating ? 'Creating…' : 'Create'}
          </Button>
        </>
      }
    >
      <div onKeyDown={handleKeyDown}>
        <TextInput
          label="Snapshot Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. before-refactor"
          data-autofocus
          disabled={creating}
        />
      </div>
    </Dialog>
  )
}
