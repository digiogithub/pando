import { useEffect } from 'react'
import { useBashStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import TagListEditor from '@/components/shared/TagListEditor'
import { Button, SettingsSection } from '@/components/ui'

export default function BashSettings() {
  const { config, dirty, loading, saving, error, fetchBash, updateField, saveBash, resetBash } =
    useBashStore()
  useUnsavedChangesGuard({
    id: 'bash',
    dirty,
    save: async () => {
      await saveBash()
      return !useBashStore.getState().error
    },
    discard: resetBash,
  })

  useEffect(() => {
    fetchBash()
  }, [fetchBash])

  if (loading) {
    return <div className="settings-loading">Loading bash configuration…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Bash Settings</h2>
        <p className="settings-page-description">
          <strong className="text-fg">Banned commands</strong> are always blocked and cannot be executed, even with
          user confirmation. <strong className="text-fg">Allowed commands</strong> run without requiring
          confirmation — useful for safe, frequently-used commands you trust unconditionally.
        </p>
      </header>

      <SettingsSection
        title="Banned commands"
        description="Commands listed here are always blocked, regardless of user confirmation. Pando's built-in defaults apply when this list is empty."
      >
        <div className="p-4">
          <TagListEditor
            items={config.bannedCommands ?? []}
            onChange={(items) => updateField('bannedCommands', items)}
            placeholder="Add a command to ban…"
          />
        </div>
      </SettingsSection>

      <SettingsSection
        title="Allowed commands"
        description={
          <>Commands listed here skip the confirmation prompt and run immediately. Use this for read-only or trusted commands like <code>ls</code>, <code>cat</code>, <code>git status</code>.</>
        }
      >
        <div className="p-4">
          <TagListEditor
            items={config.allowedCommands ?? []}
            onChange={(items) => updateField('allowedCommands', items)}
            placeholder="Add a command to allow…"
          />
        </div>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveBash} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetBash} disabled={!dirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
