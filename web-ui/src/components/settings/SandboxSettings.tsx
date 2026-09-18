import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSandboxStore } from '@pando/client/stores/settingsStore'
import { SelectInput, Toggle } from '@/components/shared/FormInput'
import TagListEditor from '@/components/shared/TagListEditor'

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '0.5rem',
}

const subsectionTitle: React.CSSProperties = {
  fontSize: 14,
  fontWeight: 600,
  color: 'var(--fg)',
  marginBottom: '0.375rem',
}

const helpText: React.CSSProperties = {
  fontSize: 13,
  color: 'var(--fg-muted)',
  marginTop: 0,
  marginBottom: '1rem',
  lineHeight: 1.6,
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const fieldsWrap: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: '1rem',
}

const lockedNote: React.CSSProperties = {
  fontSize: 12,
  color: 'var(--fg-muted)',
  marginTop: '0.25rem',
}

const pathList: React.CSSProperties = {
  margin: 0,
  padding: '0.5rem 0.75rem',
  listStyle: 'none',
  background: 'var(--selected)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontFamily: 'var(--font-mono, monospace)',
  fontSize: 12,
  color: 'var(--fg-muted)',
  overflowWrap: 'anywhere',
  maxHeight: 180,
  overflowY: 'auto',
}

function callout(color: string): React.CSSProperties {
  return {
    padding: '0.75rem 1rem',
    border: `1px solid ${color}`,
    borderLeft: `4px solid ${color}`,
    borderRadius: 'var(--radius-sm)',
    fontSize: 13,
    color: 'var(--fg)',
    marginBottom: '1.25rem',
    lineHeight: 1.6,
    background: 'var(--selected)',
  }
}

/** Read-only list used for resolved paths and for locked list fields. */
function PathList({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) {
    return <div style={{ ...helpText, marginBottom: 0 }}>{empty}</div>
  }
  return (
    <ul style={pathList}>
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

  useEffect(() => {
    fetchSandbox()
  }, [fetchSandbox])

  if (loading && !info) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        {t('settings.sandbox.loading')}
      </div>
    )
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

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>{t('settings.sandbox.title')}</h2>
      <p style={helpText}>{t('settings.sandbox.description')}</p>

      {/* Live status */}
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: '0.5rem',
          marginBottom: '1rem',
          fontSize: 13,
        }}
      >
        <span style={{ fontWeight: 600, color: 'var(--fg)' }}>{t('settings.sandbox.status')}:</span>
        <span
          style={{
            padding: '0.125rem 0.5rem',
            borderRadius: 'var(--radius-sm)',
            border: `1px solid ${status?.active && status?.full !== false ? 'var(--success)' : status?.enabled ? 'var(--warning)' : 'var(--error)'}`,
            color: status?.active && status?.full !== false ? 'var(--success)' : status?.enabled ? 'var(--warning)' : 'var(--error)',
            fontFamily: 'var(--font-mono, monospace)',
            overflowWrap: 'anywhere',
          }}
        >
          {status?.label ?? '—'}
        </span>
      </div>

      {status && !status.enabled && (
        <div style={callout('var(--error)')}>
          <strong>{t('settings.sandbox.disabledWarningTitle')}</strong>
          <br />
          {t('settings.sandbox.disabledWarning')}
        </div>
      )}
      {status && status.enabled && !enforced && (
        <div style={callout('var(--warning)')}>
          <strong>{t('settings.sandbox.notEnforcedTitle')}</strong>
          <br />
          {t('settings.sandbox.notEnforced', { reason: capability?.reason || t('settings.sandbox.unavailable') })}
        </div>
      )}
      {info?.envOverride && (
        <div style={callout('var(--warning)')}>
          {t('settings.sandbox.envOverride', { value: info.envOverride })}
        </div>
      )}

      <div style={fieldsWrap}>
        <Toggle
          label={t('settings.sandbox.enabled')}
          description={t('settings.sandbox.enabledDescription')}
          checked={enabled}
          onChange={setEnabled}
          disabled={onOffLocked}
          hint={onOffLocked ? lockedHint : undefined}
        />

        <div>
          <SelectInput
            label={t('settings.sandbox.mode')}
            value={mode}
            options={modeOptions}
            disabled={onOffLocked}
            onChange={(e) => setMode(e.target.value)}
          />
          <div style={lockedNote}>{onOffLocked ? lockedHint : t('settings.sandbox.modeHint')}</div>
        </div>

        <div>
          <SelectInput
            label={t('settings.sandbox.network')}
            value={network}
            options={networkOptions}
            disabled={isLocked('sandbox.network')}
            onChange={(e) => updateField('network', e.target.value === 'allowed' ? '' : e.target.value)}
          />
          <div style={lockedNote}>
            {isLocked('sandbox.network') ? lockedHint : t('settings.sandbox.networkHint')}
          </div>
        </div>

        <Toggle
          label={t('settings.sandbox.autoAllowBash')}
          description={t('settings.sandbox.autoAllowBashDescription')}
          checked={!config.autoAllowBashDisabled}
          onChange={(v) => updateField('autoAllowBashDisabled', !v)}
          disabled={isLocked('sandbox.autoAllowBashDisabled')}
          hint={isLocked('sandbox.autoAllowBashDisabled') ? lockedHint : undefined}
        />

        <div>
          <SelectInput
            label={t('settings.sandbox.useBwrap')}
            value={useBwrap}
            options={bwrapOptions}
            disabled={isLocked('sandbox.useBwrap')}
            onChange={(e) => updateField('useBwrap', e.target.value === 'auto' ? '' : e.target.value)}
          />
          <div style={lockedNote}>
            {isLocked('sandbox.useBwrap')
              ? lockedHint
              : isLinux
                ? t('settings.sandbox.useBwrapHint')
                : t('settings.sandbox.useBwrapNotLinux')}
          </div>
        </div>
      </div>

      <div style={dividerStyle} />

      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subsectionTitle}>{t('settings.sandbox.writableRoots')}</p>
        <p style={helpText}>{t('settings.sandbox.writableRootsHint')}</p>
        {isLocked('sandbox.writableRoots') ? (
          <>
            <PathList items={config.writableRoots ?? []} empty={t('settings.sandbox.none')} />
            <div style={lockedNote}>{lockedHint}</div>
          </>
        ) : (
          <TagListEditor
            label={t('settings.sandbox.writableRoots')}
            items={config.writableRoots ?? []}
            onChange={(items) => updateField('writableRoots', items)}
            placeholder={t('settings.sandbox.addPath')}
          />
        )}
      </div>

      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subsectionTitle}>{t('settings.sandbox.denyPaths')}</p>
        <p style={helpText}>{t('settings.sandbox.denyPathsHint')}</p>
        {isLocked('sandbox.denyPaths') ? (
          <>
            <PathList items={config.denyPaths ?? []} empty={t('settings.sandbox.none')} />
            <div style={lockedNote}>{lockedHint}</div>
          </>
        ) : (
          <TagListEditor
            label={t('settings.sandbox.denyPaths')}
            items={config.denyPaths ?? []}
            onChange={(items) => updateField('denyPaths', items)}
            placeholder={t('settings.sandbox.addPath')}
          />
        )}
      </div>

      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subsectionTitle}>{t('settings.sandbox.extendTo')}</p>
        <p style={helpText}>{t('settings.sandbox.extendToHint')}</p>
        <div style={fieldsWrap}>
          <Toggle
            label={t('settings.sandbox.extendMcp')}
            checked={extendTo.includes('mcp')}
            onChange={(v) => setExtend('mcp', v)}
            disabled={isLocked('sandbox.extendTo')}
            hint={isLocked('sandbox.extendTo') ? lockedHint : undefined}
          />
          <Toggle
            label={t('settings.sandbox.extendSubagents')}
            checked={extendTo.includes('subagents')}
            onChange={(v) => setExtend('subagents', v)}
            disabled={isLocked('sandbox.extendTo')}
            hint={isLocked('sandbox.extendTo') ? lockedHint : undefined}
          />
        </div>
      </div>

      <div style={dividerStyle} />

      {/* Resolved policy, read-only */}
      {info?.policy && status?.enabled && (
        <div style={{ marginBottom: '1.5rem' }}>
          <p style={subsectionTitle}>{t('settings.sandbox.effectiveWritable')}</p>
          <PathList items={info.policy.writableRoots} empty={t('settings.sandbox.none')} />
          <p style={{ ...subsectionTitle, marginTop: '1rem' }}>{t('settings.sandbox.protectedPaths')}</p>
          <p style={helpText}>{t('settings.sandbox.protectedPathsHint')}</p>
          <PathList items={info.policy.protectedPaths} empty={t('settings.sandbox.none')} />
        </div>
      )}

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

      <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
        <button
          onClick={saveSandbox}
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
          {saving ? t('settings.sandbox.saving') : t('settings.sandbox.save')}
        </button>
        <button
          onClick={resetSandbox}
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
          {t('settings.sandbox.reset')}
        </button>
      </div>
    </div>
  )
}
