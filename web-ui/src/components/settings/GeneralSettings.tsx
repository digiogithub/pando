import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ModelCombobox from '@/components/shared/ModelCombobox'
import CopyButton from '@/components/shared/CopyButton'
import { Button, Input, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'
import { SUPPORTED_LANGUAGES } from '@/i18n'
import { hasStoredTheme, isWebThemeId, useTheme } from '@/hooks/useTheme'

export default function GeneralSettings() {
  const { t } = useTranslation()
  const {
    config,
    dirty,
    loading,
    saving,
    error,
    fetchSettings,
    updateField,
    saveSettings,
    resetSettings,
    regenerateTelemetryId,
  } = useSettingsStore()
  useUnsavedChangesGuard({
    id: 'general',
    dirty,
    save: async () => {
      await saveSettings()
      return !useSettingsStore.getState().error
    },
    discard: resetSettings,
  })
  const { setTheme } = useTheme()

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  // Adopt the backend theme only on a browser with no local choice yet. The
  // theme picker itself now lives in Appearance; this silent copy of the
  // guard is kept here too so a fresh browser adopts it as soon as Settings
  // opens (General is the default landing category) rather than only after
  // the user happens to visit Appearance. Idempotent: hasStoredTheme() is
  // true after the first run, so Appearance's own copy is a no-op later.
  useEffect(() => {
    if (!loading && isWebThemeId(config.theme) && !hasStoredTheme()) {
      setTheme(config.theme)
    }
  }, [loading, config.theme, setTheme])

  if (loading) {
    return <div className="settings-loading">{t('settings.general.loadingSettings')}</div>
  }

  const languageOptions = SUPPORTED_LANGUAGES.map((l) => ({ value: l.value, label: l.label }))

  // The level names double as the /caveman arguments, so they stay untranslated.
  const cavemanOptions = [
    { value: 'off', label: t('settings.general.cavemanOff') },
    { value: 'lite', label: 'Lite' },
    { value: 'full', label: 'Full' },
    { value: 'ultra', label: 'Ultra' },
  ]

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">{t('settings.general.title')}</h2>
      </header>

      <SettingsSection>
        <SettingsRow label={t('settings.general.homeDirectory')} htmlFor="general-home-dir">
          <Input id="general-home-dir" value={config.home_directory} readOnly />
        </SettingsRow>
        <SettingsRow label={t('settings.general.workingDirectory')} htmlFor="general-working-dir">
          <Input
            id="general-working-dir"
            placeholder={t('settings.general.workingDirectoryPlaceholder')}
            value={config.working_directory}
            onChange={(e) => updateField('working_directory', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.defaultModel')} stacked>
          <ModelCombobox value={config.default_model} onChange={(v) => updateField('default_model', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.language')} htmlFor="general-language">
          <Select
            id="general-language"
            options={languageOptions}
            value={config.language}
            onChange={(e) => updateField('language', e.target.value)}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection>
        <SettingsRow
          label={t('settings.general.llmCache')}
          description={t('settings.general.llmCacheDescription')}
          htmlFor="general-llm-cache"
        >
          <Switch id="general-llm-cache" checked={config.llm_cache_enabled} onCheckedChange={(v) => updateField('llm_cache_enabled', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.modelsDev')}
          description={t('settings.general.modelsDevDescription')}
          htmlFor="general-models-dev"
        >
          <Switch id="general-models-dev" checked={config.models_dev_enabled} onCheckedChange={(v) => updateField('models_dev_enabled', v)} />
        </SettingsRow>
        <SettingsRow
          label="Optimize images"
          description="Resize and recompress images to the model's vision resolution before sending. Saves bandwidth and latency."
          htmlFor="general-image-resize"
        >
          <Switch id="general-image-resize" checked={config.image_auto_resize} onCheckedChange={(v) => updateField('image_auto_resize', v)} />
        </SettingsRow>
        <SettingsRow
          label="Anthropic Files API (beta)"
          description="Opt in to the Anthropic beta Messages API and upload images to the Files API once, referencing them by file_id across turns. Off = classic base64 path."
          htmlFor="general-files-api"
        >
          <Switch id="general-files-api" checked={config.image_use_files_api} onCheckedChange={(v) => updateField('image_use_files_api', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.outputFilter')}
          description={t('settings.general.outputFilterDescription')}
          htmlFor="general-output-filter"
        >
          <Switch id="general-output-filter" checked={config.output_filter_enabled} onCheckedChange={(v) => updateField('output_filter_enabled', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.autoCompact')}
          description={t('settings.general.autoCompactDescription')}
          htmlFor="general-auto-compact"
        >
          <Switch id="general-auto-compact" checked={config.auto_compact} onCheckedChange={(v) => updateField('auto_compact', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.showHiddenFiles')}
          description={t('settings.general.showHiddenFilesDescription')}
          htmlFor="general-hidden-files"
        >
          <Switch id="general-hidden-files" checked={config.show_hidden_files} onCheckedChange={(v) => updateField('show_hidden_files', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.nerdFonts')}
          description={t('settings.general.nerdFontsDescription')}
          htmlFor="general-nerd-fonts"
        >
          <Switch id="general-nerd-fonts" checked={config.nerd_fonts} onCheckedChange={(v) => updateField('nerd_fonts', v)} />
        </SettingsRow>
      </SettingsSection>

      {/* Diagnostics: local debug logging plus opt-in remote telemetry
          (Better Stack). Telemetry is off by default; the debug id is
          generated server-side on first enable and only shown once the
          server confirms it (after a save/regenerate), never invented
          client-side. */}
      <SettingsSection title={t('settings.general.diagnosticsTitle')}>
        <SettingsRow
          label={t('settings.general.debug')}
          description={t('settings.general.debugDescription')}
          htmlFor="general-debug"
        >
          <Switch id="general-debug" checked={config.debug} onCheckedChange={(v) => updateField('debug', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.telemetryEnabled')}
          description={
            !config.telemetry_available
              ? t('settings.general.telemetryUnavailableHint')
              : t('settings.general.telemetryEnabledDescription')
          }
          htmlFor="general-telemetry"
        >
          <Switch
            id="general-telemetry"
            checked={config.telemetry_enabled}
            disabled={!config.telemetry_available}
            onCheckedChange={(v) => updateField('telemetry_enabled', v)}
          />
        </SettingsRow>

        {config.telemetry_debug_id && (
          <SettingsRow label={t('settings.general.telemetryDebugId')}>
            <span className="settings-code-value">{config.telemetry_debug_id}</span>
            <CopyButton text={config.telemetry_debug_id} size="md" />
            <Button variant="secondary" size="sm" disabled={saving} onClick={() => regenerateTelemetryId()}>
              {t('settings.general.telemetryRegenerateId')}
            </Button>
          </SettingsRow>
        )}

        {config.telemetry_enabled && (
          <SettingsRow label={t('settings.general.telemetryMinLevel')} htmlFor="general-telemetry-level">
            <Select
              id="general-telemetry-level"
              options={[
                { value: 'debug', label: t('settings.general.telemetryMinLevelDebug') },
                { value: 'info', label: t('settings.general.telemetryMinLevelInfo') },
                { value: 'warn', label: t('settings.general.telemetryMinLevelWarn') },
                { value: 'error', label: t('settings.general.telemetryMinLevelError') },
              ]}
              value={config.telemetry_min_level || 'info'}
              onChange={(e) => updateField('telemetry_min_level', e.target.value)}
            />
          </SettingsRow>
        )}
      </SettingsSection>

      {/* Feedback optimization — caveman output brevity: global default
          only. Sessions that ran /caveman or /caveman-finish keep their own
          level. */}
      <SettingsSection title={t('settings.general.feedbackOptimizationTitle')}>
        <SettingsRow label={t('settings.general.caveman')} description={t('settings.general.cavemanDescription')} htmlFor="general-caveman">
          <Select
            id="general-caveman"
            options={cavemanOptions}
            value={config.caveman_default_mode || 'off'}
            onChange={(e) => updateField('caveman_default_mode', e.target.value === 'off' ? '' : e.target.value)}
          />
        </SettingsRow>
      </SettingsSection>

      {/* Tool Discovery */}
      <SettingsSection>
        <SettingsRow
          label={t('settings.general.toolDiscovery')}
          description={t('settings.general.toolDiscoveryDescription')}
          htmlFor="general-tool-discovery"
        >
          <Switch id="general-tool-discovery" checked={config.tool_discovery_enabled} onCheckedChange={(v) => updateField('tool_discovery_enabled', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.toolDiscoveryMode')} htmlFor="general-tool-discovery-mode">
          <Select
            id="general-tool-discovery-mode"
            options={[
              { value: 'auto', label: t('settings.general.toolDiscoveryModeAuto') },
              { value: 'always', label: t('settings.general.toolDiscoveryModeAlways') },
              { value: 'off', label: t('settings.general.toolDiscoveryModeOff') },
            ]}
            value={config.tool_discovery_mode || 'auto'}
            disabled={!config.tool_discovery_enabled}
            onChange={(e) => updateField('tool_discovery_mode', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.toolDiscoveryMaxDirectTools')} htmlFor="general-tool-discovery-max">
          <Input
            id="general-tool-discovery-max"
            type="number"
            min={0}
            value={config.tool_discovery_max_direct_tools}
            disabled={!config.tool_discovery_enabled}
            onChange={(e) => updateField('tool_discovery_max_direct_tools', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.toolDiscoverySearchLimit')} htmlFor="general-tool-discovery-limit">
          <Input
            id="general-tool-discovery-limit"
            type="number"
            min={0}
            value={config.tool_discovery_search_limit}
            disabled={!config.tool_discovery_enabled}
            onChange={(e) => updateField('tool_discovery_search_limit', Number(e.target.value))}
          />
        </SettingsRow>
      </SettingsSection>

      {/* Delegation (subagent conclusions + agent-loop resurrection) */}
      <SettingsSection title={t('settings.general.delegation')}>
        <SettingsRow
          label={t('settings.general.delegationEnabled')}
          description={t('settings.general.delegationEnabledDescription')}
          htmlFor="general-delegation-enabled"
        >
          <Switch id="general-delegation-enabled" checked={config.delegation_enabled} onCheckedChange={(v) => updateField('delegation_enabled', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationInjectIntoLiveLoop')}
          description={t('settings.general.delegationInjectIntoLiveLoopDescription')}
          htmlFor="general-delegation-inject"
        >
          <Switch id="general-delegation-inject" checked={config.delegation_inject_into_live_loop} onCheckedChange={(v) => updateField('delegation_inject_into_live_loop', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationResurrectIdleLoop')}
          description={t('settings.general.delegationResurrectIdleLoopDescription')}
          htmlFor="general-delegation-resurrect"
        >
          <Switch id="general-delegation-resurrect" checked={config.delegation_resurrect_idle_loop} onCheckedChange={(v) => updateField('delegation_resurrect_idle_loop', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationSynthesizeFallback')}
          description={t('settings.general.delegationSynthesizeFallbackDescription')}
          htmlFor="general-delegation-synth"
        >
          <Switch id="general-delegation-synth" checked={config.delegation_synthesize_fallback} onCheckedChange={(v) => updateField('delegation_synthesize_fallback', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationMaxResurrections')} htmlFor="general-delegation-max-res">
          <Input
            id="general-delegation-max-res"
            type="number"
            min={0}
            value={config.delegation_max_resurrections}
            disabled={!config.delegation_enabled}
            onChange={(e) => updateField('delegation_max_resurrections', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationMaxDepth')} htmlFor="general-delegation-max-depth">
          <Input
            id="general-delegation-max-depth"
            type="number"
            min={0}
            value={config.delegation_max_depth}
            disabled={!config.delegation_enabled}
            onChange={(e) => updateField('delegation_max_depth', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationMaxConcurrent')} htmlFor="general-delegation-max-conc">
          <Input
            id="general-delegation-max-conc"
            type="number"
            min={0}
            value={config.delegation_max_concurrent}
            disabled={!config.delegation_enabled}
            onChange={(e) => updateField('delegation_max_concurrent', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationResurrectionTimeout')} htmlFor="general-delegation-timeout">
          <Input
            id="general-delegation-timeout"
            value={config.delegation_resurrection_timeout}
            disabled={!config.delegation_enabled}
            onChange={(e) => updateField('delegation_resurrection_timeout', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationReuseWarmInstances')}
          description={t('settings.general.delegationReuseWarmInstancesDescription')}
          htmlFor="general-delegation-warm-reuse"
        >
          <Switch id="general-delegation-warm-reuse" checked={config.delegation_reuse_warm_instances} onCheckedChange={(v) => updateField('delegation_reuse_warm_instances', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationAutoStartWarm')}
          description={t('settings.general.delegationAutoStartWarmDescription')}
          htmlFor="general-delegation-warm-auto"
        >
          <Switch id="general-delegation-warm-auto" checked={config.delegation_auto_start_warm} onCheckedChange={(v) => updateField('delegation_auto_start_warm', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationWarmIdleTimeout')} htmlFor="general-delegation-warm-idle">
          <Input
            id="general-delegation-warm-idle"
            value={config.delegation_warm_idle_timeout}
            disabled={!config.delegation_enabled || !config.delegation_reuse_warm_instances}
            onChange={(e) => updateField('delegation_warm_idle_timeout', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationWarmQueueDepth')} htmlFor="general-delegation-warm-queue">
          <Input
            id="general-delegation-warm-queue"
            type="number"
            min={0}
            value={config.delegation_warm_queue_depth}
            disabled={!config.delegation_enabled || !config.delegation_reuse_warm_instances}
            onChange={(e) => updateField('delegation_warm_queue_depth', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationAllowExternalWarmTargets')}
          description={t('settings.general.delegationAllowExternalWarmTargetsDescription')}
          htmlFor="general-delegation-warm-external"
        >
          <Switch id="general-delegation-warm-external" checked={config.delegation_allow_external_warm_targets} onCheckedChange={(v) => updateField('delegation_allow_external_warm_targets', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationAcceptDelegations')}
          description={t('settings.general.delegationAcceptDelegationsDescription')}
          htmlFor="general-delegation-accept"
        >
          <Switch id="general-delegation-accept" checked={config.delegation_accept_delegations} onCheckedChange={(v) => updateField('delegation_accept_delegations', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationConclusionGate')}
          description={t('settings.general.delegationConclusionGateDescription')}
          htmlFor="general-delegation-gate"
        >
          <Switch id="general-delegation-gate" checked={config.delegation_conclusion_gate} onCheckedChange={(v) => updateField('delegation_conclusion_gate', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationBreaker')}
          description={t('settings.general.delegationBreakerDescription')}
          htmlFor="general-delegation-breaker"
        >
          <Switch id="general-delegation-breaker" checked={config.delegation_breaker} onCheckedChange={(v) => updateField('delegation_breaker', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationMaxTaskRetries')} htmlFor="general-delegation-retries">
          <Input
            id="general-delegation-retries"
            type="number"
            min={0}
            value={config.delegation_max_task_retries}
            disabled={!config.delegation_breaker}
            onChange={(e) => updateField('delegation_max_task_retries', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationRateLimitCooldown')} htmlFor="general-delegation-cooldown">
          <Input
            id="general-delegation-cooldown"
            value={config.delegation_rate_limit_cooldown}
            disabled={!config.delegation_breaker}
            onChange={(e) => updateField('delegation_rate_limit_cooldown', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationRecentSuccessWindow')} htmlFor="general-delegation-window">
          <Input
            id="general-delegation-window"
            value={config.delegation_recent_success_window}
            disabled={!config.delegation_breaker}
            onChange={(e) => updateField('delegation_recent_success_window', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.general.delegationEventLog')}
          description={t('settings.general.delegationEventLogDescription')}
          htmlFor="general-delegation-log"
        >
          <Switch id="general-delegation-log" checked={config.delegation_event_log} onCheckedChange={(v) => updateField('delegation_event_log', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.general.delegationEventLogMaxEntries')} htmlFor="general-delegation-log-max">
          <Input
            id="general-delegation-log-max"
            type="number"
            min={0}
            value={config.delegation_event_log_max_entries}
            disabled={!config.delegation_event_log}
            onChange={(e) => updateField('delegation_event_log_max_entries', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.orchestratorMaxParallel')} htmlFor="general-orch-max-parallel">
          <Input
            id="general-orch-max-parallel"
            type="number"
            min={1}
            value={config.orchestrator_max_parallel}
            onChange={(e) => updateField('orchestrator_max_parallel', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.orchestratorMaxPerEngine')} htmlFor="general-orch-max-engine">
          <Input
            id="general-orch-max-engine"
            type="number"
            min={0}
            value={config.orchestrator_max_per_engine}
            onChange={(e) => updateField('orchestrator_max_per_engine', Number(e.target.value))}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.orchestratorClaimTtl')} htmlFor="general-orch-claim-ttl">
          <Input
            id="general-orch-claim-ttl"
            value={config.orchestrator_claim_ttl}
            onChange={(e) => updateField('orchestrator_claim_ttl', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label={t('settings.general.orchestratorDispatchInterval')} htmlFor="general-orch-dispatch">
          <Input
            id="general-orch-dispatch"
            value={config.orchestrator_dispatch_interval}
            onChange={(e) => updateField('orchestrator_dispatch_interval', e.target.value)}
          />
        </SettingsRow>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveSettings} disabled={!dirty || saving} loading={saving}>
          {saving ? t('common.saving') : t('common.save')}
        </Button>
        <Button variant="secondary" onClick={resetSettings} disabled={!dirty}>
          {t('common.reset')}
        </Button>
      </div>
    </div>
  )
}
