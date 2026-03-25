import { useEffect } from 'react'
import { useBashStore } from '@/stores/settingsStore'
import TagListEditor from '@/components/shared/TagListEditor'

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const subsectionTitle: React.CSSProperties = {
  fontSize: 14,
  fontWeight: 600,
  color: 'var(--fg)',
  marginBottom: '0.375rem',
}

const helpText: React.CSSProperties = {
  fontSize: 12,
  color: 'var(--fg-muted)',
  marginTop: 0,
  marginBottom: '0.75rem',
}

export default function BashSettings() {
  const { config, dirty, loading, saving, error, fetchBash, updateField, saveBash, resetBash } =
    useBashStore()

  useEffect(() => {
    fetchBash()
  }, [fetchBash])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading bash configuration…
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Bash Settings</h2>

      {/* Info box */}
      <div
        style={{
          padding: '0.75rem 1rem',
          background: 'var(--selected)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          fontSize: 13,
          color: 'var(--fg-muted)',
          marginBottom: '1.5rem',
          lineHeight: 1.6,
        }}
      >
        <strong style={{ color: 'var(--fg)' }}>ℹ Banned vs Allowed Commands</strong>
        <br />
        <strong>Banned commands</strong> are always blocked and cannot be executed, even with user
        confirmation. <strong>Allowed commands</strong> run without requiring confirmation — useful
        for safe, frequently-used commands you trust unconditionally.
      </div>

      {/* Banned Commands */}
      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subsectionTitle}>Banned Commands</p>
        <p style={helpText}>
          Commands listed here are always blocked, regardless of user confirmation. Pando's
          built-in defaults apply when this list is empty.
        </p>
        <TagListEditor
          label="Banned commands"
          items={config.bannedCommands ?? []}
          onChange={(items) => updateField('bannedCommands', items)}
          placeholder="Add a command to ban…"
        />
      </div>

      <div style={dividerStyle} />

      {/* Allowed Commands */}
      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subsectionTitle}>Allowed Commands</p>
        <p style={helpText}>
          Commands listed here skip the confirmation prompt and run immediately. Use this for
          read-only or trusted commands like <code>ls</code>, <code>cat</code>, <code>git status</code>.
        </p>
        <TagListEditor
          label="Allowed commands"
          items={config.allowedCommands ?? []}
          onChange={(items) => updateField('allowedCommands', items)}
          placeholder="Add a command to allow…"
        />
      </div>

      <div style={dividerStyle} />

      {error && (
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
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveBash}
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
            transition: 'background 0.15s',
            fontFamily: 'inherit',
          }}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetBash}
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
            transition: 'color 0.15s',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
