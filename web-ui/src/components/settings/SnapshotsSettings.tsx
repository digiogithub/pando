import { useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import TagListEditor from '@/components/shared/TagListEditor'
import api from '@pando/client/services/api'
import { Button, Input, SettingsRow, SettingsSection, Switch } from '@/components/ui'

export default function SnapshotsSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateSnapshots, saveServices, resetServices } =
    useServicesSettingsStore()
  useUnsavedChangesGuard({
    id: 'snapshots',
    dirty,
    save: async () => {
      await saveServices()
      return !useServicesSettingsStore.getState().error
    },
    discard: resetServices,
  })

  const [snapshotCount, setSnapshotCount] = useState<number | null>(null)

  useEffect(() => {
    fetchServices()
    // Fetch current snapshot count
    api.get<{ count: number }>('/api/v1/snapshots/count')
      .then((data) => setSnapshotCount(data.count))
      .catch(() => setSnapshotCount(null))
  }, [fetchServices])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
  }

  const snaps = config.snapshots

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Snapshots</h2>
      </header>

      {snapshotCount !== null && (
        <div className="settings-banner">
          <span>
            Current snapshots: <strong className="text-fg">{snapshotCount}</strong>
          </span>
        </div>
      )}

      <SettingsSection>
        <SettingsRow label="Enabled" description="Enable session snapshot system" htmlFor="snapshots-enabled">
          <Switch id="snapshots-enabled" checked={snaps.enabled} onCheckedChange={(v) => updateSnapshots('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Max snapshots" htmlFor="snapshots-max">
          <Input
            id="snapshots-max"
            type="number"
            value={String(snaps.maxSnapshots)}
            onChange={(e) => updateSnapshots('maxSnapshots', Number(e.target.value))}
            placeholder="50"
          />
        </SettingsRow>
        <SettingsRow label="Max file size" description="e.g. 10MB, 500KB" htmlFor="snapshots-max-size">
          <Input
            id="snapshots-max-size"
            value={snaps.maxFileSize}
            onChange={(e) => updateSnapshots('maxFileSize', e.target.value)}
            placeholder="10MB"
          />
        </SettingsRow>
        <SettingsRow
          label="Auto cleanup"
          description="Automatically delete old snapshots after this many days"
          htmlFor="snapshots-auto-cleanup"
        >
          <Switch
            id="snapshots-auto-cleanup"
            checked={snaps.autoCleanupDays > 0}
            onCheckedChange={(v) => updateSnapshots('autoCleanupDays', v ? 30 : 0)}
          />
          {snaps.autoCleanupDays > 0 && (
            <Input
              type="number"
              min={1}
              value={snaps.autoCleanupDays}
              onChange={(e) => updateSnapshots('autoCleanupDays', Number(e.target.value))}
              className="w-20"
              aria-label="Auto cleanup days"
            />
          )}
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Exclude patterns" description="Files matching these patterns are never included in a snapshot.">
        <div className="p-4">
          <TagListEditor
            items={snaps.excludePatterns ?? []}
            onChange={(items) => updateSnapshots('excludePatterns', items)}
            placeholder="e.g. *.log, node_modules/"
          />
        </div>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveServices} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetServices} disabled={!dirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
