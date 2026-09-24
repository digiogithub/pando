import { useEffect } from 'react'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import type { MesnadaACPConfig, MesnadaACPServerConfig, MesnadaOrchestratorConfig, MesnadaTUIConfig, MesnadaServerConfig } from '@pando/client/types'
import { Button, Input, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'

const ENGINE_OPTIONS = ['pando', 'claude', 'copilot', 'openai', 'google', 'ollama'].map((v) => ({ value: v, label: v }))

export default function MesnadaSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateMesnada, saveServices, resetServices } =
    useServicesSettingsStore()
  useUnsavedChangesGuard({
    id: 'mesnada',
    dirty,
    save: async () => {
      await saveServices()
      return !useServicesSettingsStore.getState().error
    },
    discard: resetServices,
  })

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
  }

  const mesnada = config.mesnada

  function setServer<K extends keyof MesnadaServerConfig>(key: K, value: MesnadaServerConfig[K]) {
    updateMesnada('server', { ...mesnada.server, [key]: value })
  }

  function setOrchestrator<K extends keyof MesnadaOrchestratorConfig>(key: K, value: MesnadaOrchestratorConfig[K]) {
    updateMesnada('orchestrator', { ...mesnada.orchestrator, [key]: value })
  }

  function setACP<K extends keyof MesnadaACPConfig>(key: K, value: MesnadaACPConfig[K]) {
    updateMesnada('acp', { ...mesnada.acp, [key]: value })
  }

  function setACPServer<K extends keyof MesnadaACPServerConfig>(key: K, value: MesnadaACPServerConfig[K]) {
    setACP('server', { ...mesnada.acp.server, [key]: value })
  }

  function setTUI<K extends keyof MesnadaTUIConfig>(key: K, value: MesnadaTUIConfig[K]) {
    updateMesnada('tui', { ...mesnada.tui, [key]: value })
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Mesnada</h2>
      </header>

      <SettingsSection>
        <SettingsRow label="Enabled" description="Enable Mesnada integration" htmlFor="mesnada-enabled">
          <Switch id="mesnada-enabled" checked={mesnada.enabled} onCheckedChange={(v) => updateMesnada('enabled', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Server">
        <SettingsRow label="Host" htmlFor="mesnada-server-host">
          <Input id="mesnada-server-host" value={mesnada.server.host} onChange={(e) => setServer('host', e.target.value)} placeholder="localhost" />
        </SettingsRow>
        <SettingsRow label="Port" htmlFor="mesnada-server-port">
          <Input
            id="mesnada-server-port"
            type="number"
            value={String(mesnada.server.port)}
            onChange={(e) => setServer('port', Number(e.target.value))}
            placeholder="9090"
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Orchestrator">
        <SettingsRow label="Store path" htmlFor="mesnada-orch-store">
          <Input
            id="mesnada-orch-store"
            value={mesnada.orchestrator.storePath}
            onChange={(e) => setOrchestrator('storePath', e.target.value)}
            placeholder="/var/lib/mesnada/store"
          />
        </SettingsRow>
        <SettingsRow label="Log directory" htmlFor="mesnada-orch-log">
          <Input
            id="mesnada-orch-log"
            value={mesnada.orchestrator.logDir}
            onChange={(e) => setOrchestrator('logDir', e.target.value)}
            placeholder="/var/log/mesnada"
          />
        </SettingsRow>
        <SettingsRow label="Max parallel" description={`Currently ${mesnada.orchestrator.maxParallel} (1–20)`} htmlFor="mesnada-orch-max-parallel">
          <input
            id="mesnada-orch-max-parallel"
            type="range"
            min={1}
            max={20}
            value={mesnada.orchestrator.maxParallel}
            onChange={(e) => setOrchestrator('maxParallel', Number(e.target.value))}
            className="w-full accent-[var(--accent)]"
          />
        </SettingsRow>
        <SettingsRow label="Default engine" htmlFor="mesnada-orch-engine">
          <Select
            id="mesnada-orch-engine"
            options={ENGINE_OPTIONS}
            value={mesnada.orchestrator.defaultEngine}
            onChange={(e) => setOrchestrator('defaultEngine', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Default model" htmlFor="mesnada-orch-model">
          <Input
            id="mesnada-orch-model"
            value={mesnada.orchestrator.defaultModel}
            onChange={(e) => setOrchestrator('defaultModel', e.target.value)}
            placeholder="(empty = engine default)"
          />
        </SettingsRow>
        <SettingsRow label="Persona path" htmlFor="mesnada-orch-persona">
          <Input
            id="mesnada-orch-persona"
            value={mesnada.orchestrator.personaPath}
            onChange={(e) => setOrchestrator('personaPath', e.target.value)}
            placeholder="/path/to/personas"
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="ACP" description="Agent Communication Protocol">
        <SettingsRow label="ACP enabled" htmlFor="mesnada-acp-enabled">
          <Switch id="mesnada-acp-enabled" checked={mesnada.acp.enabled} onCheckedChange={(v) => setACP('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Default agent" htmlFor="mesnada-acp-agent">
          <Input id="mesnada-acp-agent" value={mesnada.acp.defaultAgent} onChange={(e) => setACP('defaultAgent', e.target.value)} placeholder="default" />
        </SettingsRow>
        <SettingsRow label="Auto permission" description="Automatically grant permissions to agents" htmlFor="mesnada-acp-auto-perm">
          <Switch id="mesnada-acp-auto-perm" checked={mesnada.acp.autoPermission} onCheckedChange={(v) => setACP('autoPermission', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="ACP server">
        <SettingsRow label="Enabled" htmlFor="mesnada-acp-server-enabled">
          <Switch id="mesnada-acp-server-enabled" checked={mesnada.acp.server.enabled} onCheckedChange={(v) => setACPServer('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Host" htmlFor="mesnada-acp-server-host">
          <Input id="mesnada-acp-server-host" value={mesnada.acp.server.host} onChange={(e) => setACPServer('host', e.target.value)} placeholder="localhost" />
        </SettingsRow>
        <SettingsRow label="Port" htmlFor="mesnada-acp-server-port">
          <Input
            id="mesnada-acp-server-port"
            type="number"
            value={String(mesnada.acp.server.port)}
            onChange={(e) => setACPServer('port', Number(e.target.value))}
            placeholder="9091"
          />
        </SettingsRow>
        <SettingsRow label="Require auth" htmlFor="mesnada-acp-server-auth">
          <Switch id="mesnada-acp-server-auth" checked={mesnada.acp.server.requireAuth} onCheckedChange={(v) => setACPServer('requireAuth', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="TUI">
        <SettingsRow label="TUI enabled" description="Enable the Terminal User Interface" htmlFor="mesnada-tui-enabled">
          <Switch id="mesnada-tui-enabled" checked={mesnada.tui.enabled} onCheckedChange={(v) => setTUI('enabled', v)} />
        </SettingsRow>
        <SettingsRow label="Web UI enabled" description="Enable the Web User Interface" htmlFor="mesnada-tui-webui">
          <Switch id="mesnada-tui-webui" checked={mesnada.tui.webui} onCheckedChange={(v) => setTUI('webui', v)} />
        </SettingsRow>
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
