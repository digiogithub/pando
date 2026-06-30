import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useTokenOptimizationStore } from '@/stores/settingsStore'
import { SelectInput, Toggle } from '@/components/shared/FormInput'
import TagListEditor from '@/components/shared/TagListEditor'

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '0.5rem',
}

const subSectionTitle: React.CSSProperties = {
  fontSize: 14,
  fontWeight: 700,
  color: 'var(--fg)',
  textTransform: 'uppercase' as const,
  letterSpacing: '0.04em',
  marginBottom: '0.875rem',
}

const helpText: React.CSSProperties = {
  fontSize: 13,
  color: 'var(--fg-muted)',
  marginTop: 0,
  marginBottom: '1.25rem',
  lineHeight: 1.6,
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const togglesWrap: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: '1rem',
  marginBottom: '0.5rem',
}

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

  useEffect(() => {
    fetchTokenOptimization()
  }, [fetchTokenOptimization])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        {t('settings.tokenOptimization.loading')}
      </div>
    )
  }

  const readModeOptions = [
    { value: 'full', label: t('settings.tokenOptimization.readModeFull') },
    { value: 'auto', label: t('settings.tokenOptimization.readModeAuto') },
    { value: 'signatures', label: t('settings.tokenOptimization.readModeSignatures') },
    { value: 'map', label: t('settings.tokenOptimization.readModeMap') },
  ]

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>{t('settings.tokenOptimization.title')}</h2>
      <p style={helpText}>{t('settings.tokenOptimization.description')}</p>

      {/* File reads */}
      <p style={subSectionTitle}>{t('settings.tokenOptimization.fileReadsSection')}</p>
      <div style={{ marginBottom: '1rem' }}>
        <SelectInput
          label={t('settings.tokenOptimization.readModeDefault')}
          options={readModeOptions}
          value={config.readModeDefault}
          onChange={(e) => updateField('readModeDefault', e.target.value)}
        />
        <p style={{ ...helpText, marginTop: '0.375rem' }}>
          {t('settings.tokenOptimization.readModeDefaultDescription')}
        </p>
      </div>
      <div style={togglesWrap}>
        <Toggle
          label={t('settings.tokenOptimization.readDedup')}
          description={t('settings.tokenOptimization.readDedupDescription')}
          checked={!config.readDedupDisabled}
          onChange={(v) => updateField('readDedupDisabled', !v)}
        />
        <Toggle
          label={t('settings.tokenOptimization.readModeLearning')}
          description={t('settings.tokenOptimization.readModeLearningDescription')}
          checked={config.readModeLearning}
          onChange={(v) => updateField('readModeLearning', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Shell output (RTK) */}
      <p style={subSectionTitle}>{t('settings.tokenOptimization.shellOutputSection')}</p>
      <div style={togglesWrap}>
        <Toggle
          label={t('settings.tokenOptimization.outputFilter')}
          description={t('settings.tokenOptimization.outputFilterDescription')}
          checked={config.outputFilterEnabled}
          onChange={(v) => updateField('outputFilterEnabled', v)}
        />
      </div>
      <div style={{ marginBottom: '0.5rem' }}>
        <p style={{ fontSize: 13, fontWeight: 600, color: 'var(--fg)', marginBottom: '0.375rem' }}>
          {t('settings.tokenOptimization.outputFilterPaths')}
        </p>
        <p style={{ ...helpText, marginBottom: '0.5rem' }}>
          {t('settings.tokenOptimization.outputFilterPathsDescription')}
        </p>
        <TagListEditor
          label={t('settings.tokenOptimization.outputFilterPaths')}
          items={config.outputFilterPaths ?? []}
          onChange={(items) => updateField('outputFilterPaths', items)}
          placeholder={t('settings.tokenOptimization.outputFilterPathsPlaceholder')}
        />
      </div>

      <div style={dividerStyle} />

      {/* Code graph */}
      <p style={subSectionTitle}>{t('settings.tokenOptimization.codeGraphSection')}</p>
      <div style={togglesWrap}>
        <Toggle
          label={t('settings.tokenOptimization.buildCodeGraph')}
          description={t('settings.tokenOptimization.buildCodeGraphDescription')}
          checked={config.buildCodeGraph}
          onChange={(v) => updateField('buildCodeGraph', v)}
        />
        <Toggle
          label={t('settings.tokenOptimization.relatedFilesHint')}
          description={t('settings.tokenOptimization.relatedFilesHintDescription')}
          checked={config.relatedFilesHint}
          onChange={(v) => updateField('relatedFilesHint', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Savings */}
      <p style={subSectionTitle}>{t('settings.tokenOptimization.savingsSection')}</p>
      <div style={togglesWrap}>
        <Toggle
          label={t('settings.tokenOptimization.savingsLedger')}
          description={t('settings.tokenOptimization.savingsLedgerDescription')}
          checked={!config.savingsLedgerDisabled}
          onChange={(v) => updateField('savingsLedgerDisabled', !v)}
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
          onClick={saveTokenOptimization}
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
          onClick={resetTokenOptimization}
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
          {t('common.reset')}
        </button>
      </div>
    </div>
  )
}
