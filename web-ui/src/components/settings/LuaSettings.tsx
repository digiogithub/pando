import { useEffect } from 'react'
import { useExtensionsStore } from '@pando/client/stores/extensionsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { Button, Input, SettingsRow, SettingsSection, Switch, Tooltip } from '@/components/ui'
import { Info } from '@/components/ui/icons'

// Parse a duration string like "30s" → 30, "5m" → 300, "1h" → 3600.
// Returns the raw seconds value, or NaN if unparseable.
function parseDurationSecs(s: string): number {
  if (!s) return NaN
  const match = s.match(/^(\d+(?:\.\d+)?)(s|m|h)?$/)
  if (!match) return NaN
  const n = parseFloat(match[1])
  const unit = match[2] ?? 's'
  if (unit === 'm') return n * 60
  if (unit === 'h') return n * 3600
  return n
}

function formatDurationSecs(secs: number): string {
  if (!Number.isFinite(secs) || secs < 0) return '30s'
  return `${Math.round(secs)}s`
}

export default function LuaSettings() {
  const {
    extensions,
    extensionsDirty,
    extensionsLoading,
    extensionsSaving,
    extensionsError,
    fetchExtensions,
    updateExtensions,
    saveExtensions,
    resetExtensions,
  } = useExtensionsStore()
  useUnsavedChangesGuard({
    id: 'lua',
    dirty: extensionsDirty,
    save: async () => {
      await saveExtensions()
      return !useExtensionsStore.getState().extensionsError
    },
    discard: resetExtensions,
  })

  useEffect(() => {
    fetchExtensions()
  }, [fetchExtensions])

  const lua = extensions.lua

  const update = (patch: Partial<typeof lua>) => {
    updateExtensions({ lua: { ...lua, ...patch } })
  }

  const timeoutSecs = parseDurationSecs(lua.timeout)

  if (extensionsLoading) {
    return <div className="settings-loading">Loading Lua settings…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Lua Engine</h2>
      </header>

      <SettingsSection>
        <SettingsRow
          label="Enabled"
          description="Activate the Lua scripting engine for custom hooks and filters"
          htmlFor="lua-enabled"
        >
          <Switch id="lua-enabled" checked={lua.enabled} onCheckedChange={(v) => update({ enabled: v })} />
        </SettingsRow>
        <SettingsRow label="Script path" htmlFor="lua-script-path">
          <Input
            id="lua-script-path"
            placeholder="/path/to/hooks.lua"
            value={lua.script_path}
            onChange={(e) => update({ script_path: e.target.value })}
          />
        </SettingsRow>
        <SettingsRow label="Timeout (seconds)" htmlFor="lua-timeout">
          <Input
            id="lua-timeout"
            type="number"
            min={1}
            max={3600}
            value={Number.isFinite(timeoutSecs) ? timeoutSecs : 30}
            onChange={(e) => update({ timeout: formatDurationSecs(parseInt(e.target.value, 10)) })}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection>
        <SettingsRow label="Strict mode" description="Treat Lua errors as fatal and halt execution" htmlFor="lua-strict">
          <Switch id="lua-strict" checked={lua.strict_mode} onCheckedChange={(v) => update({ strict_mode: v })} />
        </SettingsRow>
        <SettingsRow
          label={
            <span className="inline-flex items-center gap-1.5">
              Hot reload
              <Tooltip content="When enabled, Pando watches the script file and reloads it without restarting. Integrates with the config hot-reload system.">
                <Info size={13} className="text-faint" aria-hidden />
              </Tooltip>
            </span>
          }
          description="Automatically reload Lua scripts when the file changes"
          htmlFor="lua-hot-reload"
        >
          <Switch id="lua-hot-reload" checked={lua.hot_reload} onCheckedChange={(v) => update({ hot_reload: v })} />
        </SettingsRow>
        <SettingsRow
          label="Log filtered data"
          description="Log data that was filtered or blocked by Lua scripts"
          htmlFor="lua-log-filtered"
        >
          <Switch id="lua-log-filtered" checked={lua.log_filtered_data} onCheckedChange={(v) => update({ log_filtered_data: v })} />
        </SettingsRow>
      </SettingsSection>

      {extensionsError && <div className="settings-banner settings-banner--danger" role="alert">{extensionsError}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveExtensions} disabled={!extensionsDirty || extensionsSaving} loading={extensionsSaving}>
          {extensionsSaving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetExtensions} disabled={!extensionsDirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
