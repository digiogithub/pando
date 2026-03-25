import { useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@/stores/servicesSettingsStore'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import TagListEditor from '@/components/shared/TagListEditor'
import api from '@/services/api'

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

export default function SnapshotsSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateSnapshots, saveServices, resetServices } =
    useServicesSettingsStore()

  const [snapshotCount, setSnapshotCount] = useState<number | null>(null)

  useEffect(() => {
    fetchServices()
    // Fetch current snapshot count
    api.get<{ count: number }>('/api/v1/snapshots/count')
      .then((data) => setSnapshotCount(data.count))
      .catch(() => setSnapshotCount(null))
  }, [fetchServices])

  if (loading) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
  }

  const snaps = config.snapshots

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Snapshots</h2>

      {/* Info bar */}
      {snapshotCount !== null && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
            padding: '0.625rem 0.875rem',
            background: 'var(--selected)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
            color: 'var(--fg)',
            marginBottom: '1.25rem',
          }}
        >
          <span style={{ fontWeight: 600 }}>Current snapshots:</span>
          <span style={{ color: 'var(--primary)', fontWeight: 700 }}>{snapshotCount}</span>
        </div>
      )}

      <Toggle
        label="Enabled"
        description="Enable session snapshot system"
        checked={snaps.enabled}
        onChange={(v) => updateSnapshots('enabled', v)}
      />

      <div style={dividerStyle} />

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <TextInput
          label="Max Snapshots"
          type="number"
          value={String(snaps.maxSnapshots)}
          onChange={(e) => updateSnapshots('maxSnapshots', Number(e.target.value))}
          placeholder="50"
        />

        {/* Max File Size with MB label */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Max File Size
          </label>
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
            <input
              value={snaps.maxFileSize}
              onChange={(e) => updateSnapshots('maxFileSize', e.target.value)}
              placeholder="10MB"
              style={{
                flex: 1,
                background: 'var(--input-bg)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--fg)',
                fontSize: 14,
                padding: '0.5rem 0.75rem',
                outline: 'none',
                fontFamily: 'inherit',
                boxSizing: 'border-box',
              }}
              onFocus={(e) => (e.target.style.borderColor = 'var(--border-focus)')}
              onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
            />
            <span style={{ fontSize: 13, color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
              e.g. 10MB, 500KB
            </span>
          </div>
        </div>

        {/* Auto Cleanup Days */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Auto Cleanup (days)
          </label>
          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
            <Toggle
              label="Auto Cleanup"
              description="Automatically delete old snapshots"
              checked={snaps.autoCleanupDays > 0}
              onChange={(v) => updateSnapshots('autoCleanupDays', v ? 30 : 0)}
            />
            {snaps.autoCleanupDays > 0 && (
              <input
                type="number"
                value={snaps.autoCleanupDays}
                min={1}
                onChange={(e) => updateSnapshots('autoCleanupDays', Number(e.target.value))}
                style={{
                  width: 80,
                  background: 'var(--input-bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--radius-sm)',
                  color: 'var(--fg)',
                  fontSize: 14,
                  padding: '0.375rem 0.5rem',
                  outline: 'none',
                  fontFamily: 'inherit',
                  textAlign: 'center',
                }}
                onFocus={(e) => (e.target.style.borderColor = 'var(--border-focus)')}
                onBlur={(e) => (e.target.style.borderColor = 'var(--border)')}
              />
            )}
          </div>
        </div>

        <TagListEditor
          label="Exclude Patterns"
          items={snaps.excludePatterns ?? []}
          onChange={(items) => updateSnapshots('excludePatterns', items)}
          placeholder="e.g. *.log, node_modules/"
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
          onClick={saveServices}
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
          onClick={resetServices}
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
