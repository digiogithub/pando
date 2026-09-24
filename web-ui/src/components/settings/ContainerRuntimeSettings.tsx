import { useEffect } from 'react'
import { useContainerStore } from '@pando/client/stores/containerStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { Button, Card, Input, Select, SettingsRow, SettingsSection, Switch, Textarea } from '@/components/ui'

function listValue(value: string[] | null | undefined) {
  return (value ?? []).join(', ')
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
  useUnsavedChangesGuard({
    id: 'container-runtime',
    dirty,
    save: async () => {
      await saveConfig()
      return !useContainerStore.getState().error
    },
    discard: resetConfig,
  })

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Container Runtime</h2>
      </header>

      <div className="grid gap-3 mb-3 grid-cols-[repeat(auto-fit,minmax(220px,1fr))]">
        {capabilities.map((capability) => (
          <Card key={capability.type} padding="sm">
            <div className="font-semibold text-fg mb-1">{capability.type}</div>
            <div className="text-sm text-muted">Status: {capability.available ? 'available' : 'unavailable'}</div>
            <div className="text-sm text-muted">Exec: {capability.exec ? 'yes' : 'no'}</div>
            <div className="text-sm text-muted">Workspace FS: {capability.fs ? 'yes' : 'no'}</div>
            {capability.version && <div className="text-sm text-muted">Version: {capability.version}</div>}
            {capability.socket && <div className="text-sm text-muted">Socket: {capability.socket}</div>}
          </Card>
        ))}
      </div>

      <div className="settings-banner">
        <span>
          Current selection: <strong className="text-fg">{currentRuntime || config.runtime || 'host'}</strong>. Auto mode
          prefers rootless Podman, then Docker, then host.
        </span>
      </div>

      <SettingsSection title="Runtime configuration">
        <SettingsRow label="Runtime" htmlFor="container-runtime">
          <Select
            id="container-runtime"
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
        </SettingsRow>
        <SettingsRow label="Image" htmlFor="container-image">
          <Input id="container-image" value={config.image} onChange={(e) => updateField('image', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Pull policy" htmlFor="container-pull-policy">
          <Select
            id="container-pull-policy"
            options={[
              { value: 'if-not-present', label: 'if-not-present' },
              { value: 'always', label: 'always' },
              { value: 'never', label: 'never' },
            ]}
            value={config.pull_policy}
            onChange={(e) => updateField('pull_policy', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Socket" htmlFor="container-socket">
          <Input id="container-socket" value={config.socket} onChange={(e) => updateField('socket', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Work dir" htmlFor="container-work-dir">
          <Input id="container-work-dir" value={config.work_dir} onChange={(e) => updateField('work_dir', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Network" htmlFor="container-network">
          <Select
            id="container-network"
            options={[
              { value: 'none', label: 'none' },
              { value: 'bridge', label: 'bridge' },
              { value: 'host', label: 'host' },
              { value: 'slirp4netns', label: 'slirp4netns' },
            ]}
            value={config.network}
            onChange={(e) => updateField('network', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="User" htmlFor="container-user">
          <Input id="container-user" value={config.user} onChange={(e) => updateField('user', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="CPU limit" htmlFor="container-cpu-limit">
          <Input id="container-cpu-limit" value={config.cpu_limit} onChange={(e) => updateField('cpu_limit', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Memory limit" htmlFor="container-mem-limit">
          <Input id="container-mem-limit" value={config.mem_limit} onChange={(e) => updateField('mem_limit', e.target.value)} />
        </SettingsRow>
        <SettingsRow label="PIDs limit" htmlFor="container-pids-limit">
          <Input
            id="container-pids-limit"
            type="number"
            value={String(config.pids_limit)}
            onChange={(e) => updateField('pids_limit', Number(e.target.value || 0))}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Security">
        <SettingsRow
          label="Read-only root filesystem"
          description="Recommended secure default for containerized sessions."
          htmlFor="container-read-only"
        >
          <Switch id="container-read-only" checked={config.read_only} onCheckedChange={(v) => updateField('read_only', v)} />
        </SettingsRow>
        <SettingsRow
          label="No new privileges"
          description="Prevent processes from gaining additional Linux privileges."
          htmlFor="container-no-new-priv"
        >
          <Switch id="container-no-new-priv" checked={config.no_new_privileges} onCheckedChange={(v) => updateField('no_new_privileges', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Advanced">
        <SettingsRow label="Allowed environment variables" stacked>
          <Textarea
            rows={3}
            value={listValue(config.allow_env)}
            onChange={(e) => updateField('allow_env', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
          />
        </SettingsRow>
        <SettingsRow label="Allowed mount paths" stacked>
          <Textarea
            rows={3}
            value={listValue(config.allow_mounts)}
            onChange={(e) => updateField('allow_mounts', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
          />
        </SettingsRow>
        <SettingsRow label="Extra environment" stacked>
          <Textarea
            rows={3}
            value={listValue(config.extra_env)}
            onChange={(e) => updateField('extra_env', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
          />
        </SettingsRow>
        <SettingsRow label="Extra mounts" stacked>
          <Textarea
            rows={3}
            value={listValue(config.extra_mounts)}
            onChange={(e) => updateField('extra_mounts', e.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
          />
        </SettingsRow>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveConfig} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetConfig} disabled={!dirty}>
          Reset
        </Button>
        <Button variant="secondary" onClick={refreshObservability}>
          Refresh Activity
        </Button>
      </div>

      <div className="grid gap-6 mt-6 grid-cols-[repeat(auto-fit,minmax(320px,1fr))]">
        <div>
          <h3 className="text-sm font-semibold text-fg mb-3">Active sessions</h3>
          <div className="flex flex-col gap-3">
            {sessions.length === 0 && <Card padding="sm" className="text-sm text-muted">No active container sessions.</Card>}
            {sessions.map((session) => (
              <Card key={session.sessionId} padding="sm">
                <div className="font-semibold text-fg">{session.sessionId}</div>
                <div className="text-sm text-muted">Runtime: {session.runtime}</div>
                {session.containerId && <div className="text-sm text-muted">Container: {session.containerId}</div>}
                <div className="text-sm text-muted">Workdir: {session.workDir}</div>
                <div className="text-sm text-muted">Created: {new Date(session.createdAt).toLocaleString()}</div>
                <Button variant="danger" size="sm" className="mt-2" onClick={() => stopSession(session.sessionId)}>
                  Stop session
                </Button>
              </Card>
            ))}
          </div>
        </div>

        <div>
          <h3 className="text-sm font-semibold text-fg mb-3">Recent events</h3>
          <div className="flex flex-col gap-3">
            {events.length === 0 && <Card padding="sm" className="text-sm text-muted">No container activity recorded yet.</Card>}
            {events.map((event, index) => (
              <Card key={`${event.timestamp}-${event.sessionId}-${index}`} padding="sm">
                <div className="font-semibold text-fg">{event.event}</div>
                <div className="text-sm text-muted">Runtime: {event.runtimeType}</div>
                {event.sessionId && <div className="text-sm text-muted">Session: {event.sessionId}</div>}
                {event.containerId && <div className="text-sm text-muted">Container: {event.containerId}</div>}
                <div className="text-sm text-muted">Time: {new Date(event.timestamp).toLocaleString()}</div>
                {event.details && <div className="text-sm text-muted">Details: {event.details}</div>}
              </Card>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
