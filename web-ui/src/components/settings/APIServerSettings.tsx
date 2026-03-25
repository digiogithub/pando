import { useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@/stores/servicesSettingsStore'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import RestartRequiredBanner from '@/components/shared/RestartRequiredBanner'
import MaskedInput from '@/components/shared/MaskedInput'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToastStore } from '@/stores/toastStore'
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

export default function APIServerSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateServer, saveServices, resetServices } =
    useServicesSettingsStore()

  const [authToken, setAuthToken] = useState('')
  const [showRegenConfirm, setShowRegenConfirm] = useState(false)

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

  if (loading) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
  }

  const server = config.server

  async function handleRegenerateToken() {
    try {
      const data = await api.post<{ token: string }>('/api/v1/config/api-server/regenerate-token', {})
      setAuthToken(data.token ?? '')
      useToastStore.getState().addToast('Token regenerated', 'success')
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Failed to regenerate token',
        'error',
      )
    } finally {
      setShowRegenConfirm(false)
    }
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>API Server</h2>

      <RestartRequiredBanner />

      <Toggle
        label="Enabled"
        description="Enable the HTTP API server"
        checked={server.enabled}
        onChange={(v) => updateServer('enabled', v)}
      />

      <div style={dividerStyle} />

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <TextInput
          label="Host"
          value={server.host}
          onChange={(e) => updateServer('host', e.target.value)}
          placeholder="localhost"
        />
        <TextInput
          label="Port"
          type="number"
          value={String(server.port)}
          onChange={(e) => updateServer('port', Number(e.target.value))}
          placeholder="9999"
        />
      </div>

      <div
        style={{
          marginTop: '0.875rem',
          padding: '0.5rem 0.75rem',
          background: 'rgba(217, 119, 6, 0.08)',
          border: '1px solid rgba(217, 119, 6, 0.2)',
          borderRadius: 'var(--radius-sm)',
          fontSize: 12,
          color: 'var(--fg-muted)',
        }}
      >
        Changing host or port requires restarting the API server.
      </div>

      <div style={dividerStyle} />

      {/* Auth */}
      <Toggle
        label="Require Authentication"
        description="Protect API endpoints with a bearer token"
        checked={server.requireAuth}
        onChange={(v) => updateServer('requireAuth', v)}
      />

      {server.requireAuth && (
        <div style={{ marginTop: '1rem' }}>
          <MaskedInput
            label="Auth Token"
            value={authToken}
            onChange={setAuthToken}
            placeholder="Token will appear after regeneration"
            actionLabel="Regenerate"
            onAction={() => setShowRegenConfirm(true)}
          />
          <p style={{ marginTop: '0.375rem', fontSize: 12, color: 'var(--fg-muted)' }}>
            Click "Regenerate" to create a new secure token. The token is shown once.
          </p>
        </div>
      )}

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

      {showRegenConfirm && (
        <ConfirmDialog
          title="Regenerate Auth Token"
          message="This will invalidate the current token. Any clients using the old token will lose access. Continue?"
          confirmLabel="Regenerate"
          dangerous
          onConfirm={handleRegenerateToken}
          onCancel={() => setShowRegenConfirm(false)}
        />
      )}
    </div>
  )
}
