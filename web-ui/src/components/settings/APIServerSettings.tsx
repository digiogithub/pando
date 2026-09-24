import { useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import RestartRequiredBanner from '@/components/shared/RestartRequiredBanner'
import MaskedInput from '@/components/shared/MaskedInput'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'
import { Button, Input, SettingsRow, SettingsSection, Switch } from '@/components/ui'

export default function APIServerSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateServer, saveServices, resetServices } =
    useServicesSettingsStore()
  useUnsavedChangesGuard({
    id: 'api-server',
    dirty,
    save: async () => {
      await saveServices()
      return !useServicesSettingsStore.getState().error
    },
    discard: resetServices,
  })

  const [authToken, setAuthToken] = useState('')
  const [showRegenConfirm, setShowRegenConfirm] = useState(false)

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
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
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">API Server</h2>
      </header>

      <RestartRequiredBanner />

      <SettingsSection>
        <SettingsRow label="Enabled" description="Enable the HTTP API server" htmlFor="api-server-enabled">
          <Switch id="api-server-enabled" checked={server.enabled} onCheckedChange={(v) => updateServer('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Host" htmlFor="api-server-host">
          <Input
            id="api-server-host"
            value={server.host}
            onChange={(e) => updateServer('host', e.target.value)}
            placeholder="localhost"
          />
        </SettingsRow>
        <SettingsRow label="Port" description="Changing host or port requires restarting the API server." htmlFor="api-server-port">
          <Input
            id="api-server-port"
            type="number"
            value={String(server.port)}
            onChange={(e) => updateServer('port', Number(e.target.value))}
            placeholder="9999"
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection>
        <SettingsRow
          label="Require authentication"
          description="Protect API endpoints with a bearer token"
          htmlFor="api-server-auth"
        >
          <Switch id="api-server-auth" checked={server.requireAuth} onCheckedChange={(v) => updateServer('requireAuth', v)} />
        </SettingsRow>
        {server.requireAuth && (
          <div className="border-t border-border p-4">
            <MaskedInput
              label="Auth token"
              value={authToken}
              onChange={setAuthToken}
              placeholder="Token will appear after regeneration"
              actionLabel="Regenerate"
              onAction={() => setShowRegenConfirm(true)}
            />
            <p className="mt-2 text-xs text-muted">
              Click &quot;Regenerate&quot; to create a new secure token. The token is shown once.
            </p>
          </div>
        )}
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
