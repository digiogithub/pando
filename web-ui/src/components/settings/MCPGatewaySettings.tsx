import { useEffect } from 'react'
import { useMCPGatewayStore } from '@pando/client/stores/mcpGatewayStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { Button, Input, SettingsRow, SettingsSection, Switch } from '@/components/ui'

export default function MCPGatewaySettings() {
  const { config, dirty, loading, saving, error, fetchGateway, updateField, saveGateway, resetGateway } =
    useMCPGatewayStore()
  useUnsavedChangesGuard({
    id: 'mcp-gateway',
    dirty,
    save: async () => {
      await saveGateway()
      return !useMCPGatewayStore.getState().error
    },
    discard: resetGateway,
  })

  useEffect(() => {
    fetchGateway()
  }, [fetchGateway])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">MCP Gateway</h2>
        <p className="settings-page-description">
          The gateway tracks tool usage to promote frequently-used MCP servers as favorites.
        </p>
      </header>

      <SettingsSection>
        <SettingsRow
          label="Enable MCP Gateway"
          description="Automatically track and surface frequently used MCP tools"
          htmlFor="mcp-gateway-enabled"
        >
          <Switch id="mcp-gateway-enabled" checked={config.enabled} onCheckedChange={(v) => updateField('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Favorite threshold" description="Minimum uses to become a favorite" htmlFor="mcp-gateway-threshold">
          <Input
            id="mcp-gateway-threshold"
            type="number"
            min={1}
            value={config.favorite_threshold}
            onChange={(e) => updateField('favorite_threshold', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label="Max favorites" description="Maximum number of favorites" htmlFor="mcp-gateway-max">
          <Input
            id="mcp-gateway-max"
            type="number"
            min={1}
            value={config.max_favorites}
            onChange={(e) => updateField('max_favorites', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label="Favorite window (days)" description="Rolling window for usage counting" htmlFor="mcp-gateway-window">
          <Input
            id="mcp-gateway-window"
            type="number"
            min={1}
            value={config.favorite_window_days}
            onChange={(e) => updateField('favorite_window_days', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label="Decay days" description="Days until unused favorites are demoted" htmlFor="mcp-gateway-decay">
          <Input
            id="mcp-gateway-decay"
            type="number"
            min={1}
            value={config.decay_days}
            onChange={(e) => updateField('decay_days', Number(e.target.value))}
          />
        </SettingsRow>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveGateway} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetGateway} disabled={!dirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
