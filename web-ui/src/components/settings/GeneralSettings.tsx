import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSettingsStore } from '@/stores/settingsStore'
import { TextInput, SelectInput, Toggle } from '@/components/shared/FormInput'
import ModelCombobox from '@/components/shared/ModelCombobox'
import ThemePicker from '@/components/shared/ThemePicker'
import { SUPPORTED_LANGUAGES } from '@/i18n'
import { useTheme } from '@/hooks/useTheme'

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

const fieldLabel: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  marginBottom: '0.5rem',
  display: 'block',
}

export default function GeneralSettings() {
  const { t } = useTranslation()
  const { config, dirty, loading, saving, error, fetchSettings, updateField, saveSettings, resetSettings } =
    useSettingsStore()
  const { setTheme } = useTheme()

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  // Apply theme from backend config once settings are loaded
  useEffect(() => {
    if (!loading && config.theme) {
      setTheme(config.theme)
    }
  }, [loading, config.theme, setTheme])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        {t('settings.general.loadingSettings')}
      </div>
    )
  }

  const languageOptions = SUPPORTED_LANGUAGES.map((l) => ({ value: l.value, label: l.label }))

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>{t('settings.general.title')}</h2>

      {/* Text fields */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <TextInput
          label={t('settings.general.homeDirectory')}
          value={config.home_directory}
          readOnly
        />

        <TextInput
          label={t('settings.general.workingDirectory')}
          placeholder={t('settings.general.workingDirectoryPlaceholder')}
          value={config.working_directory}
          onChange={(e) => updateField('working_directory', e.target.value)}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            {t('settings.general.defaultModel')}
          </label>
          <ModelCombobox
            value={config.default_model}
            onChange={(v) => updateField('default_model', v)}
          />
        </div>

        <SelectInput
          label={t('settings.general.language')}
          options={languageOptions}
          value={config.language}
          onChange={(e) => updateField('language', e.target.value)}
        />

        {/* Theme picker */}
        <div>
          <label style={fieldLabel}>{t('settings.general.theme')}</label>
          <ThemePicker
            value={config.theme || 'pando-light'}
            onChange={(themeId) => {
              updateField('theme', themeId)
              setTheme(themeId)
            }}
          />
        </div>
      </div>

      <div style={dividerStyle} />

      {/* Toggles */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label={t('settings.general.llmCache')}
          description={t('settings.general.llmCacheDescription')}
          checked={config.llm_cache_enabled}
          onChange={(v) => updateField('llm_cache_enabled', v)}
        />
        <Toggle
          label={t('settings.general.outputFilter')}
          description={t('settings.general.outputFilterDescription')}
          checked={config.output_filter_enabled}
          onChange={(v) => updateField('output_filter_enabled', v)}
        />
        <Toggle
          label={t('settings.general.autoCompact')}
          description={t('settings.general.autoCompactDescription')}
          checked={config.auto_compact}
          onChange={(v) => updateField('auto_compact', v)}
        />
        <Toggle
          label={t('settings.general.showHiddenFiles')}
          description={t('settings.general.showHiddenFilesDescription')}
          checked={config.show_hidden_files}
          onChange={(v) => updateField('show_hidden_files', v)}
        />
        <Toggle
          label={t('settings.general.nerdFonts')}
          description={t('settings.general.nerdFontsDescription')}
          checked={config.nerd_fonts}
          onChange={(v) => updateField('nerd_fonts', v)}
        />
        <Toggle
          label={t('settings.general.debug')}
          description={t('settings.general.debugDescription')}
          checked={config.debug}
          onChange={(v) => updateField('debug', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Tool Discovery */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <Toggle
          label={t('settings.general.toolDiscovery')}
          description={t('settings.general.toolDiscoveryDescription')}
          checked={config.tool_discovery_enabled}
          onChange={(v) => updateField('tool_discovery_enabled', v)}
        />
        <SelectInput
          label={t('settings.general.toolDiscoveryMode')}
          options={[
            { value: 'auto', label: t('settings.general.toolDiscoveryModeAuto') },
            { value: 'always', label: t('settings.general.toolDiscoveryModeAlways') },
            { value: 'off', label: t('settings.general.toolDiscoveryModeOff') },
          ]}
          value={config.tool_discovery_mode || 'auto'}
          disabled={!config.tool_discovery_enabled}
          onChange={(e) => updateField('tool_discovery_mode', e.target.value)}
        />
        <TextInput
          label={t('settings.general.toolDiscoveryMaxDirectTools')}
          type="number"
          min={0}
          value={config.tool_discovery_max_direct_tools}
          disabled={!config.tool_discovery_enabled}
          onChange={(e) => updateField('tool_discovery_max_direct_tools', Number(e.target.value))}
        />
        <TextInput
          label={t('settings.general.toolDiscoverySearchLimit')}
          type="number"
          min={0}
          value={config.tool_discovery_search_limit}
          disabled={!config.tool_discovery_enabled}
          onChange={(e) => updateField('tool_discovery_search_limit', Number(e.target.value))}
        />
      </div>

      <div style={dividerStyle} />

      {/* Delegation (subagent conclusions + agent-loop resurrection) */}
      <h3 style={{ ...sectionTitle, fontSize: 15, marginBottom: '1rem' }}>
        {t('settings.general.delegation')}
      </h3>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <Toggle
          label={t('settings.general.delegationEnabled')}
          description={t('settings.general.delegationEnabledDescription')}
          checked={config.delegation_enabled}
          onChange={(v) => updateField('delegation_enabled', v)}
        />
        <Toggle
          label={t('settings.general.delegationInjectIntoLiveLoop')}
          description={t('settings.general.delegationInjectIntoLiveLoopDescription')}
          checked={config.delegation_inject_into_live_loop}
          onChange={(v) => updateField('delegation_inject_into_live_loop', v)}
        />
        <Toggle
          label={t('settings.general.delegationResurrectIdleLoop')}
          description={t('settings.general.delegationResurrectIdleLoopDescription')}
          checked={config.delegation_resurrect_idle_loop}
          onChange={(v) => updateField('delegation_resurrect_idle_loop', v)}
        />
        <Toggle
          label={t('settings.general.delegationSynthesizeFallback')}
          description={t('settings.general.delegationSynthesizeFallbackDescription')}
          checked={config.delegation_synthesize_fallback}
          onChange={(v) => updateField('delegation_synthesize_fallback', v)}
        />
        <TextInput
          label={t('settings.general.delegationMaxResurrections')}
          type="number"
          min={0}
          value={config.delegation_max_resurrections}
          disabled={!config.delegation_enabled}
          onChange={(e) => updateField('delegation_max_resurrections', Number(e.target.value))}
        />
        <TextInput
          label={t('settings.general.delegationMaxDepth')}
          type="number"
          min={0}
          value={config.delegation_max_depth}
          disabled={!config.delegation_enabled}
          onChange={(e) => updateField('delegation_max_depth', Number(e.target.value))}
        />
        <TextInput
          label={t('settings.general.delegationMaxConcurrent')}
          type="number"
          min={0}
          value={config.delegation_max_concurrent}
          disabled={!config.delegation_enabled}
          onChange={(e) => updateField('delegation_max_concurrent', Number(e.target.value))}
        />
        <TextInput
          label={t('settings.general.delegationResurrectionTimeout')}
          value={config.delegation_resurrection_timeout}
          disabled={!config.delegation_enabled}
          onChange={(e) => updateField('delegation_resurrection_timeout', e.target.value)}
        />
        <Toggle
          label={t('settings.general.delegationReuseWarmInstances')}
          description={t('settings.general.delegationReuseWarmInstancesDescription')}
          checked={config.delegation_reuse_warm_instances}
          onChange={(v) => updateField('delegation_reuse_warm_instances', v)}
        />
        <Toggle
          label={t('settings.general.delegationAutoStartWarm')}
          description={t('settings.general.delegationAutoStartWarmDescription')}
          checked={config.delegation_auto_start_warm}
          onChange={(v) => updateField('delegation_auto_start_warm', v)}
        />
        <TextInput
          label={t('settings.general.delegationWarmIdleTimeout')}
          value={config.delegation_warm_idle_timeout}
          disabled={!config.delegation_enabled || !config.delegation_reuse_warm_instances}
          onChange={(e) => updateField('delegation_warm_idle_timeout', e.target.value)}
        />
        <TextInput
          label={t('settings.general.delegationWarmQueueDepth')}
          type="number"
          min={0}
          value={config.delegation_warm_queue_depth}
          disabled={!config.delegation_enabled || !config.delegation_reuse_warm_instances}
          onChange={(e) => updateField('delegation_warm_queue_depth', Number(e.target.value))}
        />
        <Toggle
          label={t('settings.general.delegationAllowExternalWarmTargets')}
          description={t('settings.general.delegationAllowExternalWarmTargetsDescription')}
          checked={config.delegation_allow_external_warm_targets}
          onChange={(v) => updateField('delegation_allow_external_warm_targets', v)}
        />
        <Toggle
          label={t('settings.general.delegationAcceptDelegations')}
          description={t('settings.general.delegationAcceptDelegationsDescription')}
          checked={config.delegation_accept_delegations}
          onChange={(v) => updateField('delegation_accept_delegations', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Error message */}
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

      {/* Action buttons */}
      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveSettings}
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
          {saving ? t('common.saving') : t('common.save')}
        </button>

        <button
          onClick={resetSettings}
          disabled={!dirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !dirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: `1px solid ${!dirty ? 'var(--border)' : 'var(--border)'}`,
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !dirty ? 'not-allowed' : 'pointer',
            transition: 'color 0.15s',
            fontFamily: 'inherit',
          }}
        >
          {t('common.reset')}
        </button>
      </div>
    </div>
  )
}
