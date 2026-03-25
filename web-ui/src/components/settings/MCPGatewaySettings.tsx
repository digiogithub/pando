import { useEffect } from 'react'
import { useMCPGatewayStore } from '@/stores/mcpGatewayStore'
import { Toggle } from '@/components/shared/FormInput'

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

function NumberField({
  label,
  description,
  value,
  onChange,
  min,
}: {
  label: string
  description?: string
  value: number
  onChange: (v: number) => void
  min?: number
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
        {label}
      </label>
      {description && (
        <span style={{ fontSize: 12, color: 'var(--fg-muted)', marginBottom: '0.125rem' }}>{description}</span>
      )}
      <input
        type="number"
        min={min ?? 0}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        style={{
          background: 'var(--input-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--fg)',
          fontSize: 14,
          padding: '0.5rem 0.75rem',
          outline: 'none',
          width: 120,
          fontFamily: 'inherit',
        }}
        onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
        onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
      />
    </div>
  )
}

export default function MCPGatewaySettings() {
  const { config, dirty, loading, saving, error, fetchGateway, updateField, saveGateway, resetGateway } =
    useMCPGatewayStore()

  useEffect(() => {
    fetchGateway()
  }, [fetchGateway])

  if (loading) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>MCP Gateway</h2>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <Toggle
          label="Enable MCP Gateway"
          description="Automatically track and surface frequently used MCP tools"
          checked={config.enabled}
          onChange={(v) => updateField('enabled', v)}
        />

        <div style={dividerStyle} />

        <p style={{ fontSize: 13, color: 'var(--fg-muted)', marginBottom: '0.5rem' }}>
          The gateway tracks tool usage to promote frequently-used MCP servers as favorites.
        </p>

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1.5rem' }}>
          <NumberField
            label="Favorite Threshold"
            description="Min uses to become a favorite"
            value={config.favorite_threshold}
            onChange={(v) => updateField('favorite_threshold', v)}
            min={1}
          />
          <NumberField
            label="Max Favorites"
            description="Maximum number of favorites"
            value={config.max_favorites}
            onChange={(v) => updateField('max_favorites', v)}
            min={1}
          />
          <NumberField
            label="Favorite Window (days)"
            description="Rolling window for usage counting"
            value={config.favorite_window_days}
            onChange={(v) => updateField('favorite_window_days', v)}
            min={1}
          />
          <NumberField
            label="Decay Days"
            description="Days until unused favorites are demoted"
            value={config.decay_days}
            onChange={(v) => updateField('decay_days', v)}
            min={1}
          />
        </div>
      </div>

      <div style={dividerStyle} />

      {error && (
        <div style={{ marginBottom: '1rem', padding: '0.625rem 0.875rem', background: 'var(--error)', color: 'white', borderRadius: 'var(--radius-sm)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveGateway}
          disabled={!dirty || saving}
          style={{
            padding: '0.5rem 1.5rem',
            background: !dirty || saving ? 'var(--border)' : 'var(--primary)',
            color: !dirty || saving ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !dirty || saving ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetGateway}
          disabled={!dirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !dirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !dirty ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
