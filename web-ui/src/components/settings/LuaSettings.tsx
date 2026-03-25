import { useEffect } from 'react'
import { useExtensionsStore } from '@/stores/extensionsStore'
import { Toggle, TextInput } from '@/components/shared/FormInput'
import Tooltip from '@/components/shared/Tooltip'

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

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

  useEffect(() => {
    fetchExtensions()
  }, [fetchExtensions])

  const lua = extensions.lua

  const update = (patch: Partial<typeof lua>) => {
    updateExtensions({ lua: { ...lua, ...patch } })
  }

  const timeoutSecs = parseDurationSecs(lua.timeout)

  if (extensionsLoading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading Lua settings…
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
        Lua Engine
      </h2>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <Toggle
          label="Enabled"
          description="Activate the Lua scripting engine for custom hooks and filters"
          checked={lua.enabled}
          onChange={(v) => update({ enabled: v })}
        />

        <TextInput
          label="Script Path"
          placeholder="/path/to/hooks.lua"
          value={lua.script_path}
          onChange={(e) => update({ script_path: e.target.value })}
        />

        {/* Timeout */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label
            style={{
              fontSize: 12,
              fontWeight: 600,
              color: 'var(--fg-muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
            }}
          >
            Timeout (seconds)
          </label>
          <input
            type="number"
            min={1}
            max={3600}
            value={Number.isFinite(timeoutSecs) ? timeoutSecs : 30}
            onChange={(e) => update({ timeout: formatDurationSecs(parseInt(e.target.value, 10)) })}
            style={{
              background: 'var(--input-bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--fg)',
              fontSize: 14,
              padding: '0.5rem 0.75rem',
              outline: 'none',
              width: '100%',
              fontFamily: 'inherit',
              boxSizing: 'border-box',
            }}
            onFocus={(e) => {
              e.target.style.borderColor = 'var(--border-focus)'
            }}
            onBlur={(e) => {
              e.target.style.borderColor = 'var(--border)'
            }}
          />
        </div>

        <div style={dividerStyle} />

        <Toggle
          label="Strict Mode"
          description="Treat Lua errors as fatal and halt execution"
          checked={lua.strict_mode}
          onChange={(v) => update({ strict_mode: v })}
        />

        {/* Hot Reload with tooltip */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <div style={{ flex: 1 }}>
            <Toggle
              label="Hot Reload"
              description="Automatically reload Lua scripts when the file changes"
              checked={lua.hot_reload}
              onChange={(v) => update({ hot_reload: v })}
            />
          </div>
          <Tooltip
            content="When enabled, Pando watches the script file and reloads it without restarting. Integrates with the config hot-reload system."
            position="left"
          >
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 18,
                height: 18,
                borderRadius: '50%',
                background: 'var(--border)',
                color: 'var(--fg-muted)',
                fontSize: 11,
                fontWeight: 700,
                cursor: 'help',
                flexShrink: 0,
                userSelect: 'none',
              }}
            >
              ?
            </span>
          </Tooltip>
        </div>

        <Toggle
          label="Log Filtered Data"
          description="Log data that was filtered or blocked by Lua scripts"
          checked={lua.log_filtered_data}
          onChange={(v) => update({ log_filtered_data: v })}
        />
      </div>

      <div style={dividerStyle} />

      {extensionsError && (
        <div
          style={{
            marginBottom: '1rem',
            padding: '0.625rem 0.875rem',
            background: 'var(--error)',
            color: 'var(--primary-fg)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
          }}
        >
          {extensionsError}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveExtensions}
          disabled={!extensionsDirty || extensionsSaving}
          style={{
            padding: '0.5rem 1.5rem',
            background: !extensionsDirty || extensionsSaving ? 'var(--border)' : 'var(--primary)',
            color: !extensionsDirty || extensionsSaving ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !extensionsDirty || extensionsSaving ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {extensionsSaving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetExtensions}
          disabled={!extensionsDirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !extensionsDirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !extensionsDirty ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
