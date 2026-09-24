import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSandboxStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import TagListEditor from '@/components/shared/TagListEditor'
import { Badge, Button, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'

/** Read-only list used for resolved paths and for locked list fields. */
function PathList({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) {
    return <div className="text-sm text-muted">{empty}</div>
  }
  return (
    <ul className="m-0 p-2 list-none bg-raised border border-border rounded-sm font-mono text-xs text-muted break-words max-h-[180px] overflow-y-auto">
      {items.map((p) => (
        <li key={p}>{p}</li>
      ))}
    </ul>
  )
}

export default function SandboxSettings() {
  const { t } = useTranslation()
  const {
    config,
    info,
    dirty,
    loading,
    saving,
    error,
    fetchSandbox,
    updateField,
    saveSandbox,
    resetSandbox,
    isLocked,
  } = useSandboxStore()
  useUnsavedChangesGuard({
    id: 'sandbox',
    dirty,
    save: async () => {
      await saveSandbox()
      return !useSandboxStore.getState().error
    },
    discard: resetSandbox,
  })

  useEffect(() => {
    fetchSandbox()
  }, [fetchSandbox])

  if (loading && !info) {
    return <div className="settings-loading">{t('settings.sandbox.loading')}</div>
  }

  const enabled = !config.disabled && config.mode !== 'off'
  const mode = config.mode || 'workspace-write'
  const network = config.network || 'allowed'
  const useBwrap = config.useBwrap || 'auto'
  const extendTo = config.extendTo ?? []
  const status = info?.status
  const capability = info?.capability
  const enforced = capability?.enforced ?? false
  const isLinux = info?.platform === 'linux'
  const lockedHint = t('settings.sandbox.locked')

  const onOffLocked = isLocked('sandbox.disabled') || isLocked('sandbox.mode')

  const setExtend = (target: string, on: boolean) => {
    const next = extendTo.filter((v) => v !== target)
    if (on) next.push(target)
    updateField('extendTo', next)
  }

  const setEnabled = (on: boolean) => {
    updateField('disabled', !on)
    if (on && config.mode === 'off') updateField('mode', '')
  }

  const setMode = (value: string) => {
    updateField('mode', value === 'workspace-write' ? '' : value)
    if (value !== 'off' && config.disabled) updateField('disabled', false)
  }

  const modeOptions = [
    { value: 'workspace-write', label: t('settings.sandbox.modeWorkspaceWrite') },
    { value: 'read-only', label: t('settings.sandbox.modeReadOnly') },
    { value: 'strict', label: t('settings.sandbox.modeStrict') },
    { value: 'off', label: t('settings.sandbox.modeOff') },
  ]
  const networkOptions = [
    { value: 'allowed', label: t('settings.sandbox.networkAllowed') },
    { value: 'restricted', label: t('settings.sandbox.networkRestricted') },
  ]
  const bwrapOptions = [
    { value: 'auto', label: t('settings.sandbox.bwrapAuto') },
    { value: 'always', label: t('settings.sandbox.bwrapAlways') },
    { value: 'never', label: t('settings.sandbox.bwrapNever') },
  ]

  const statusTone: 'success' | 'warning' | 'danger' =
    status?.active && status?.full !== false ? 'success' : status?.enabled ? 'warning' : 'danger'

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">
          {t('settings.sandbox.title')}
          {status && <Badge tone={statusTone} dot>{status.label ?? '—'}</Badge>}
        </h2>
        <p className="settings-page-description">{t('settings.sandbox.description')}</p>
      </header>

      {status && !status.enabled && (
        <div className="settings-banner settings-banner--danger">
          <span>
            <strong className="text-fg">{t('settings.sandbox.disabledWarningTitle')}</strong>
            <br />
            {t('settings.sandbox.disabledWarning')}
          </span>
        </div>
      )}
      {status && status.enabled && !enforced && (
        <div className="settings-banner settings-banner--warning">
          <span>
            <strong className="text-fg">{t('settings.sandbox.notEnforcedTitle')}</strong>
            <br />
            {t('settings.sandbox.notEnforced', { reason: capability?.reason || t('settings.sandbox.unavailable') })}
          </span>
        </div>
      )}
      {info?.envOverride && (
        <div className="settings-banner settings-banner--warning">
          <span>{t('settings.sandbox.envOverride', { value: info.envOverride })}</span>
        </div>
      )}

      <SettingsSection>
        <SettingsRow
          label={t('settings.sandbox.enabled')}
          description={onOffLocked ? lockedHint : t('settings.sandbox.enabledDescription')}
          htmlFor="sandbox-enabled"
        >
          <Switch id="sandbox-enabled" checked={enabled} onCheckedChange={setEnabled} disabled={onOffLocked} />
        </SettingsRow>
        <SettingsRow label={t('settings.sandbox.mode')} description={onOffLocked ? lockedHint : t('settings.sandbox.modeHint')} htmlFor="sandbox-mode">
          <Select id="sandbox-mode" value={mode} options={modeOptions} disabled={onOffLocked} onChange={(e) => setMode(e.target.value)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.sandbox.network')}
          description={isLocked('sandbox.network') ? lockedHint : t('settings.sandbox.networkHint')}
          htmlFor="sandbox-network"
        >
          <Select
            id="sandbox-network"
            value={network}
            options={networkOptions}
            disabled={isLocked('sandbox.network')}
            onChange={(e) => updateField('network', e.target.value === 'allowed' ? '' : e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.sandbox.autoAllowBash')}
          description={isLocked('sandbox.autoAllowBashDisabled') ? lockedHint : t('settings.sandbox.autoAllowBashDescription')}
          htmlFor="sandbox-auto-allow-bash"
        >
          <Switch
            id="sandbox-auto-allow-bash"
            checked={!config.autoAllowBashDisabled}
            onCheckedChange={(v) => updateField('autoAllowBashDisabled', !v)}
            disabled={isLocked('sandbox.autoAllowBashDisabled')}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.sandbox.useBwrap')}
          description={
            isLocked('sandbox.useBwrap')
              ? lockedHint
              : isLinux
                ? t('settings.sandbox.useBwrapHint')
                : t('settings.sandbox.useBwrapNotLinux')
          }
          htmlFor="sandbox-use-bwrap"
        >
          <Select
            id="sandbox-use-bwrap"
            value={useBwrap}
            options={bwrapOptions}
            disabled={isLocked('sandbox.useBwrap')}
            onChange={(e) => updateField('useBwrap', e.target.value === 'auto' ? '' : e.target.value)}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title={t('settings.sandbox.writableRoots')} description={t('settings.sandbox.writableRootsHint')}>
        <div className="p-4">
          {isLocked('sandbox.writableRoots') ? (
            <>
              <PathList items={config.writableRoots ?? []} empty={t('settings.sandbox.none')} />
              <div className="mt-1 text-xs text-muted">{lockedHint}</div>
            </>
          ) : (
            <TagListEditor
              items={config.writableRoots ?? []}
              onChange={(items) => updateField('writableRoots', items)}
              placeholder={t('settings.sandbox.addPath')}
            />
          )}
        </div>
      </SettingsSection>

      <SettingsSection title={t('settings.sandbox.denyPaths')} description={t('settings.sandbox.denyPathsHint')}>
        <div className="p-4">
          {isLocked('sandbox.denyPaths') ? (
            <>
              <PathList items={config.denyPaths ?? []} empty={t('settings.sandbox.none')} />
              <div className="mt-1 text-xs text-muted">{lockedHint}</div>
            </>
          ) : (
            <TagListEditor
              items={config.denyPaths ?? []}
              onChange={(items) => updateField('denyPaths', items)}
              placeholder={t('settings.sandbox.addPath')}
            />
          )}
        </div>
      </SettingsSection>

      <SettingsSection title={t('settings.sandbox.extendTo')} description={t('settings.sandbox.extendToHint')}>
        <SettingsRow label={t('settings.sandbox.extendMcp')} description={isLocked('sandbox.extendTo') ? lockedHint : undefined} htmlFor="sandbox-extend-mcp">
          <Switch
            id="sandbox-extend-mcp"
            checked={extendTo.includes('mcp')}
            onCheckedChange={(v) => setExtend('mcp', v)}
            disabled={isLocked('sandbox.extendTo')}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.sandbox.extendSubagents')}
          description={isLocked('sandbox.extendTo') ? lockedHint : undefined}
          htmlFor="sandbox-extend-subagents"
        >
          <Switch
            id="sandbox-extend-subagents"
            checked={extendTo.includes('subagents')}
            onCheckedChange={(v) => setExtend('subagents', v)}
            disabled={isLocked('sandbox.extendTo')}
          />
        </SettingsRow>
      </SettingsSection>

      {info?.policy && status?.enabled && (
        <SettingsSection title={t('settings.sandbox.effectiveWritable')}>
          <div className="p-4 flex flex-col gap-4">
            <PathList items={info.policy.writableRoots} empty={t('settings.sandbox.none')} />
            <div>
              <p className="text-sm font-semibold text-fg mb-1">{t('settings.sandbox.protectedPaths')}</p>
              <p className="text-xs text-muted mb-2">{t('settings.sandbox.protectedPathsHint')}</p>
              <PathList items={info.policy.protectedPaths} empty={t('settings.sandbox.none')} />
            </div>
          </div>
        </SettingsSection>
      )}

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveSandbox} disabled={!dirty || saving} loading={saving}>
          {saving ? t('settings.sandbox.saving') : t('settings.sandbox.save')}
        </Button>
        <Button variant="secondary" onClick={resetSandbox} disabled={!dirty}>
          {t('settings.sandbox.reset')}
        </Button>
      </div>
    </div>
  )
}
