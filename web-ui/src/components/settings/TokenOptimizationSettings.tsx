import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTokenOptimizationStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import TagListEditor from '@/components/shared/TagListEditor'
import api from '@pando/client/services/api'
import type { SavingsReport } from '@pando/client/types'
import { Button, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'

export default function TokenOptimizationSettings() {
  const { t } = useTranslation()
  const {
    config,
    dirty,
    loading,
    saving,
    error,
    fetchTokenOptimization,
    updateField,
    saveTokenOptimization,
    resetTokenOptimization,
  } = useTokenOptimizationStore()
  useUnsavedChangesGuard({
    id: 'token-optimization',
    dirty,
    save: async () => {
      await saveTokenOptimization()
      return !useTokenOptimizationStore.getState().error
    },
    discard: resetTokenOptimization,
  })

  useEffect(() => {
    fetchTokenOptimization()
  }, [fetchTokenOptimization])

  if (loading) {
    return <div className="settings-loading">{t('settings.tokenOptimization.loading')}</div>
  }

  const readModeOptions = [
    { value: 'full', label: t('settings.tokenOptimization.readModeFull') },
    { value: 'auto', label: t('settings.tokenOptimization.readModeAuto') },
    { value: 'signatures', label: t('settings.tokenOptimization.readModeSignatures') },
    { value: 'map', label: t('settings.tokenOptimization.readModeMap') },
  ]

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">{t('settings.tokenOptimization.title')}</h2>
        <p className="settings-page-description">{t('settings.tokenOptimization.description')}</p>
      </header>

      <SettingsSection title={t('settings.tokenOptimization.fileReadsSection')}>
        <SettingsRow
          label={t('settings.tokenOptimization.readModeDefault')}
          description={t('settings.tokenOptimization.readModeDefaultDescription')}
          htmlFor="tok-opt-read-mode"
        >
          <Select
            id="tok-opt-read-mode"
            options={readModeOptions}
            value={config.readModeDefault}
            onChange={(e) => updateField('readModeDefault', e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label={t('settings.tokenOptimization.readDedup')}
          description={t('settings.tokenOptimization.readDedupDescription')}
          htmlFor="tok-opt-read-dedup"
        >
          <Switch id="tok-opt-read-dedup" checked={!config.readDedupDisabled} onCheckedChange={(v) => updateField('readDedupDisabled', !v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.tokenOptimization.readModeLearning')}
          description={t('settings.tokenOptimization.readModeLearningDescription')}
          htmlFor="tok-opt-read-learning"
        >
          <Switch id="tok-opt-read-learning" checked={config.readModeLearning} onCheckedChange={(v) => updateField('readModeLearning', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title={t('settings.tokenOptimization.shellOutputSection')}>
        <SettingsRow
          label={t('settings.tokenOptimization.outputFilter')}
          description={t('settings.tokenOptimization.outputFilterDescription')}
          htmlFor="tok-opt-output-filter"
        >
          <Switch id="tok-opt-output-filter" checked={config.outputFilterEnabled} onCheckedChange={(v) => updateField('outputFilterEnabled', v)} />
        </SettingsRow>
        <SettingsRow label={t('settings.tokenOptimization.outputFilterPaths')} description={t('settings.tokenOptimization.outputFilterPathsDescription')} stacked>
          <TagListEditor
            items={config.outputFilterPaths ?? []}
            onChange={(items) => updateField('outputFilterPaths', items)}
            placeholder={t('settings.tokenOptimization.outputFilterPathsPlaceholder')}
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title={t('settings.tokenOptimization.codeGraphSection')}>
        <SettingsRow
          label={t('settings.tokenOptimization.buildCodeGraph')}
          description={t('settings.tokenOptimization.buildCodeGraphDescription')}
          htmlFor="tok-opt-code-graph"
        >
          <Switch id="tok-opt-code-graph" checked={config.buildCodeGraph} onCheckedChange={(v) => updateField('buildCodeGraph', v)} />
        </SettingsRow>
        <SettingsRow
          label={t('settings.tokenOptimization.relatedFilesHint')}
          description={t('settings.tokenOptimization.relatedFilesHintDescription')}
          htmlFor="tok-opt-related-files"
        >
          <Switch id="tok-opt-related-files" checked={config.relatedFilesHint} onCheckedChange={(v) => updateField('relatedFilesHint', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title={t('settings.tokenOptimization.savingsSection')}>
        <SettingsRow
          label={t('settings.tokenOptimization.savingsLedger')}
          description={t('settings.tokenOptimization.savingsLedgerDescription')}
          htmlFor="tok-opt-savings-ledger"
        >
          <Switch id="tok-opt-savings-ledger" checked={!config.savingsLedgerDisabled} onCheckedChange={(v) => updateField('savingsLedgerDisabled', !v)} />
        </SettingsRow>
        <div className="border-t border-border p-4">
          <SavingsWidget />
        </div>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveTokenOptimization} disabled={!dirty || saving} loading={saving}>
          {saving ? t('common.saving') : t('common.save')}
        </Button>
        <Button variant="secondary" onClick={resetTokenOptimization} disabled={!dirty}>
          {t('common.reset')}
        </Button>
      </div>
    </div>
  )
}

// SavingsWidget shows a compact, read-only summary of the token-savings ledger
// (Phase 5) — total tokens saved, % reduction and a per-source breakdown.
function SavingsWidget() {
  const { t } = useTranslation()
  const [report, setReport] = useState<SavingsReport | null>(null)
  const [loading, setLoading] = useState(true)

  const load = () => {
    setLoading(true)
    api
      .get<SavingsReport>('/api/v1/savings')
      .then((r) => setReport(r))
      .catch(() => setReport(null))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [])

  const numberFmt = (n: number) => n.toLocaleString()

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-semibold text-fg">{t('settings.tokenOptimization.savingsWidgetTitle')}</span>
        <Button variant="secondary" size="sm" onClick={load} disabled={loading}>
          {t('common.refresh')}
        </Button>
      </div>

      {loading && <p className="text-sm text-muted m-0">{t('settings.tokenOptimization.loading')}</p>}

      {!loading && (!report || report.events === 0) && (
        <p className="text-sm text-muted m-0">{t('settings.tokenOptimization.savingsWidgetEmpty')}</p>
      )}

      {!loading && report && report.events > 0 && (
        <div>
          <p className="text-sm text-fg mb-2">
            <strong>{numberFmt(report.saved_tokens)}</strong>{' '}
            {t('settings.tokenOptimization.savingsWidgetSaved', {
              pct: report.reduction_pct.toFixed(1),
            })}
          </p>
          <table className="w-full text-xs text-muted border-collapse">
            <tbody>
              {(report.by_source ?? []).map((s) => (
                <tr key={s.source}>
                  <td className="py-0.5 capitalize">{s.source}</td>
                  <td className="py-0.5 text-right">
                    {numberFmt(s.saved_tokens)} ({s.events})
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
