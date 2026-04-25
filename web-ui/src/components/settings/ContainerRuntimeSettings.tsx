import { useEffect } from 'react'
import { SelectInput, TextInput, Textarea, Toggle } from '@/components/shared/FormInput'
import { useContainerStore } from '@/stores/containerStore'

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

const infoCardStyle: React.CSSProperties = {
  padding: '0.875rem 1rem',
  background: 'var(--panel)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontSize: 13,
  color: 'var(--fg-muted)',
}

function listValue(value: string[]) {
  return value.join(', ')
}

export default function ContainerRuntimeSettings() {
  const {
    config,
    capabilities,
    currentRuntime,
    sessions,
    events,
    dirty,
    loading,
    saving,
    error,
    fetchAll,
    updateField,
    saveConfig,
    resetConfig,
    stopSession,
    refreshObservability,
  } = useContainerStore()

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  if (loading) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
  }

  return (
    <div style={{ maxWidth: 880 }}>
      <h2 style={sectionTitle}>Container Runtime</h2>

      <div style={{ display: 'grid', gap: '1rem', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}>
        {capabilities.map((capability) => (
          <div key={capability.type} style={infoCardStyle}>
            <div style={{ fontWeight: 700, color: 'var(--fg)', marginBottom: '0.35rem' }}>{capability.type}</div>
            <div>Status: {capability.available ? 'available' : 'unavailable'}</div>
            <div>Exec: {capability.exec ? 'yes' : 'no'}</div>
            <div>Workspace FS: {capability.fs ? 'yes' : 'no'}</div>
            {capability.version && <div>Version: {capability.version}</div>}
            {capability.socket && <div>Socket: {capability.socket}</div>}
          </div>
        ))}
      </div>

      <div style={{ ...infoCardStyle, marginTop: '1rem' }}>
        Current selection: <strong style={{ color: 'var(--fg)' }}>{currentRuntime || config.runtime || 'host'}</strong>. Auto mode prefers rootless Podman, then Docker, then host.
      </div>

      <div style={dividerStyle} />

      <div style={{ display: 'grid', gap: '1rem', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))' }}>
        <SelectInput
          label="Runtime"
          options={[
            { value: 'host', label: 'host' },
            { value: 'docker', label: 'docker' },
            { value: 'podman', label: 'podman' },
            { value: 'embedded', label: 'embedded' },
            { value: 'auto', label: 'auto' },
          ]}
          value={config.runtime}
          onChange={(e) => updateField('runtime', e.target.value)}
        />
        <TextInput label="Image" value={config.image} onChange={(e) => updateField('image', e.target.value)} />
        <SelectInput
          label="Pull Policy"
          options={[
            { value: 'if-not-present', label: 'if-not-present' },
            { value: 'always', label: 'always' },
            { value: 'never', label: 'never' },
          ]}
          value={config.pull_policy}
          onChange={(e) => updateField('pull_policy', e.target.value)}
        />
        <TextInput label="Socket" value={config.socket} onChange={(e) => updateField('socket', e.target.value)} />
        <TextInput label="Work Dir" value={config.work_dir} onChange={(e) => updateField('work_dir', e.target.value)} />
        <SelectInput
          label="Network"
          options={[
            { value: 'none', label: 'none' },
            { value: 'bridge', label: 'bridge' },
            { value: 'host', label: 'host' },
            { value: 'slirp4netns', label: 'slirp4netns' },
          ]}
          value={config.network}
          onChange={(e) => updateField('network', e.target.value)}
        />
        <TextInput label="User" value={config.user} onChange={(e) => updateField('user', e.target.value)} />
        <TextInput label="CPU Limit" value={config.cpu_limit} onChange={(e) => updateField('cpu_limit', e.target.value)} />
        <TextInput label="Memory Limit" value={config.mem_limit} onChange={(e) => updateField('mem_limit', e.target.value)} />
        <TextInput
          label="PIDs Limit"
          type="number"
          value={String(config.pids_limit)}
          onChange={(e) => updateField('pids_limit', Number(e.target.value || 0))}
        />
      </div>

      <div style={dividerStyle} />

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Read-Only Root Filesystem"
          description="Recommended secure default for containerized sessions."
          checked={config.read_only}
          onChange={(value) => updateField('read_only', value)}
        />
        <Toggle
          label="No New Privileges"
          description="Prevent processes from gaining additional Linux privileges."
          checked={config.no_new_privileges}
          onChange={(value) => updateField('no_new_privileges', value)}
        />
      </div>

      <div style={dividerStyle} />

      <div style={{ display: 'grid', gap: '1rem', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))' }}>
        <Textarea
          label="Allowed Environment Variables"
          rows={3}
          value={listValue(config.allow_env)}
          onChange={(e) => updateField('allow_env', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
        />
        <Textarea
          label="Allowed Mount Paths"
          rows={3}
          value={listValue(config.allow_mounts)}
          onChange={(e) => updateField('allow_mounts', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
        />
        <Textarea
          label="Extra Environment"
          rows={3}
          value={listValue(config.extra_env)}
          onChange={(e) => updateField('extra_env', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
        />
        <Textarea
          label="Extra Mounts"
          rows={3}
          value={listValue(config.extra_mounts)}
          onChange={(e) => updateField('extra_mounts', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
        />
      </div>

      <div style={dividerStyle} />

      <div style={{ display: 'flex', gap: '0.75rem', marginBottom: '1.5rem' }}>
        <button
          onClick={saveConfig}
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
          onClick={resetConfig}
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
        <button
          onClick={refreshObservability}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: 'var(--fg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Refresh Activity
        </button>
      </div>

      {error && (
        <div style={{ ...infoCardStyle, marginBottom: '1.5rem', color: 'var(--error)', borderColor: 'var(--error)' }}>
          {error}
        </div>
      )}

      <div style={{ display: 'grid', gap: '1.5rem', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))' }}>
        <div>
          <h3 style={{ margin: '0 0 0.75rem', color: 'var(--fg)' }}>Active Sessions</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            {sessions.length === 0 && <div style={infoCardStyle}>No active container sessions.</div>}
            {sessions.map((session) => (
              <div key={session.sessionId} style={infoCardStyle}>
                <div style={{ fontWeight: 700, color: 'var(--fg)' }}>{session.sessionId}</div>
                <div>Runtime: {session.runtime}</div>
                {session.containerId && <div>Container: {session.containerId}</div>}
                <div>Workdir: {session.workDir}</div>
                <div>Created: {new Date(session.createdAt).toLocaleString()}</div>
                <button
                  onClick={() => stopSession(session.sessionId)}
                  style={{
                    marginTop: '0.75rem',
                    padding: '0.4rem 0.9rem',
                    background: 'transparent',
                    color: 'var(--error)',
                    border: '1px solid var(--error)',
                    borderRadius: 'var(--radius-sm)',
                    cursor: 'pointer',
                    fontFamily: 'inherit',
                  }}
                >
                  Stop Session
                </button>
              </div>
            ))}
          </div>
        </div>

        <div>
          <h3 style={{ margin: '0 0 0.75rem', color: 'var(--fg)' }}>Recent Events</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            {events.length === 0 && <div style={infoCardStyle}>No container activity recorded yet.</div>}
            {events.map((event, index) => (
              <div key={`${event.timestamp}-${event.sessionId}-${index}`} style={infoCardStyle}>
                <div style={{ fontWeight: 700, color: 'var(--fg)' }}>{event.event}</div>
                <div>Runtime: {event.runtimeType}</div>
                {event.sessionId && <div>Session: {event.sessionId}</div>}
                {event.containerId && <div>Container: {event.containerId}</div>}
                <div>Time: {new Date(event.timestamp).toLocaleString()}</div>
                {event.details && <div>Details: {event.details}</div>}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
