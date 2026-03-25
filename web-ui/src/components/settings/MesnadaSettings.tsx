import { useEffect } from 'react'
import { useServicesSettingsStore } from '@/stores/servicesSettingsStore'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import type { MesnadaACPConfig, MesnadaACPServerConfig, MesnadaOrchestratorConfig, MesnadaTUIConfig, MesnadaServerConfig } from '@/types'

const ENGINE_OPTIONS = ['claude', 'copilot', 'openai', 'google', 'ollama']

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

const subSectionTitle: React.CSSProperties = {
  fontSize: 14,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '0.875rem',
  textTransform: 'uppercase' as const,
  letterSpacing: '0.05em',
}

export default function MesnadaSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateMesnada, saveServices, resetServices } =
    useServicesSettingsStore()

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

  if (loading) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
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
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Mesnada</h2>

      <Toggle
        label="Enabled"
        description="Enable Mesnada integration"
        checked={mesnada.enabled}
        onChange={(v) => updateMesnada('enabled', v)}
      />

      <div style={dividerStyle} />

      {/* Server */}
      <p style={subSectionTitle}>Server</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <TextInput
          label="Host"
          value={mesnada.server.host}
          onChange={(e) => setServer('host', e.target.value)}
          placeholder="localhost"
        />
        <TextInput
          label="Port"
          type="number"
          value={String(mesnada.server.port)}
          onChange={(e) => setServer('port', Number(e.target.value))}
          placeholder="9090"
        />
      </div>

      <div style={dividerStyle} />

      {/* Orchestrator */}
      <p style={subSectionTitle}>Orchestrator</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <TextInput
          label="Store Path"
          value={mesnada.orchestrator.storePath}
          onChange={(e) => setOrchestrator('storePath', e.target.value)}
          placeholder="/var/lib/mesnada/store"
        />
        <TextInput
          label="Log Directory"
          value={mesnada.orchestrator.logDir}
          onChange={(e) => setOrchestrator('logDir', e.target.value)}
          placeholder="/var/log/mesnada"
        />

        {/* MaxParallel slider */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Max Parallel — <span style={{ color: 'var(--fg)', fontWeight: 700 }}>{mesnada.orchestrator.maxParallel}</span>
          </label>
          <input
            type="range"
            min={1}
            max={20}
            value={mesnada.orchestrator.maxParallel}
            onChange={(e) => setOrchestrator('maxParallel', Number(e.target.value))}
            style={{ width: '100%', accentColor: 'var(--primary)' }}
          />
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)' }}>
            <span>1</span><span>20</span>
          </div>
        </div>

        {/* DefaultEngine select */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Default Engine
          </label>
          <select
            value={mesnada.orchestrator.defaultEngine}
            onChange={(e) => setOrchestrator('defaultEngine', e.target.value)}
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
              cursor: 'pointer',
            }}
            onFocus={(e) => (e.currentTarget.style.borderColor = 'var(--border-focus)')}
            onBlur={(e) => (e.currentTarget.style.borderColor = 'var(--border)')}
          >
            {ENGINE_OPTIONS.map((opt) => (
              <option key={opt} value={opt}>{opt}</option>
            ))}
          </select>
        </div>

        <TextInput
          label="Default Model"
          value={mesnada.orchestrator.defaultModel}
          onChange={(e) => setOrchestrator('defaultModel', e.target.value)}
          placeholder="claude-sonnet-4-6"
        />
        <TextInput
          label="Persona Path"
          value={mesnada.orchestrator.personaPath}
          onChange={(e) => setOrchestrator('personaPath', e.target.value)}
          placeholder="/path/to/personas"
        />
      </div>

      <div style={dividerStyle} />

      {/* ACP */}
      <p style={subSectionTitle}>ACP (Agent Communication Protocol)</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="ACP Enabled"
          checked={mesnada.acp.enabled}
          onChange={(v) => setACP('enabled', v)}
        />
        <TextInput
          label="Default Agent"
          value={mesnada.acp.defaultAgent}
          onChange={(e) => setACP('defaultAgent', e.target.value)}
          placeholder="default"
        />
        <Toggle
          label="Auto Permission"
          description="Automatically grant permissions to agents"
          checked={mesnada.acp.autoPermission}
          onChange={(v) => setACP('autoPermission', v)}
        />

        <div style={{ paddingLeft: '1rem', borderLeft: '2px solid var(--border)', display: 'flex', flexDirection: 'column', gap: '0.875rem' }}>
          <p style={{ margin: 0, fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>ACP Server</p>
          <Toggle
            label="ACP Server Enabled"
            checked={mesnada.acp.server.enabled}
            onChange={(v) => setACPServer('enabled', v)}
          />
          <TextInput
            label="Host"
            value={mesnada.acp.server.host}
            onChange={(e) => setACPServer('host', e.target.value)}
            placeholder="localhost"
          />
          <TextInput
            label="Port"
            type="number"
            value={String(mesnada.acp.server.port)}
            onChange={(e) => setACPServer('port', Number(e.target.value))}
            placeholder="9091"
          />
          <Toggle
            label="Require Auth"
            checked={mesnada.acp.server.requireAuth}
            onChange={(v) => setACPServer('requireAuth', v)}
          />
        </div>
      </div>

      <div style={dividerStyle} />

      {/* TUI */}
      <p style={subSectionTitle}>TUI</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="TUI Enabled"
          description="Enable the Terminal User Interface"
          checked={mesnada.tui.enabled}
          onChange={(v) => setTUI('enabled', v)}
        />
        <Toggle
          label="Web UI Enabled"
          description="Enable the Web User Interface"
          checked={mesnada.tui.webui}
          onChange={(v) => setTUI('webui', v)}
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
