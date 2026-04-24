import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSettingsStore } from '@/stores/settingsStore'
import { TextInput, SelectInput, Textarea, Toggle } from '@/components/shared/FormInput'
import ModelCombobox from '@/components/shared/ModelCombobox'
import { SUPPORTED_LANGUAGES } from '@/i18n'
import { useTheme } from '@/hooks/useTheme'

const THEME_OPTIONS = [
  { value: 'light', labelKey: 'settings.general.themeLight' },
  { value: 'dark', labelKey: 'settings.general.themeDark' },
]

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
  const themeOptions = THEME_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) }))

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>{t('settings.general.title')}</h2>

      {/* Text fields */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <TextInput
          label={t('settings.general.workingDirectory')}
          placeholder={t('settings.general.workingDirectoryPlaceholder')}
          value={config.home_directory}
          onChange={(e) => updateField('home_directory', e.target.value)}
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

        <SelectInput
          label={t('settings.general.theme')}
          options={themeOptions}
          value={config.theme}
          onChange={(e) => {
            const t = e.target.value as 'light' | 'dark'
            updateField('theme', t)
            setTheme(t)
          }}
        />
      </div>

      <div style={dividerStyle} />

      {/* Toggles */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label={t('settings.general.autoSave')}
          description={t('settings.general.autoSaveDescription')}
          checked={config.auto_save}
          onChange={(v) => updateField('auto_save', v)}
        />
        <Toggle
          label={t('settings.general.markdownPreview')}
          description={t('settings.general.markdownPreviewDescription')}
          checked={config.markdown_preview}
          onChange={(v) => updateField('markdown_preview', v)}
        />
        <Toggle
          label={t('settings.general.llmCache')}
          description={t('settings.general.llmCacheDescription')}
          checked={config.llm_cache_enabled}
          onChange={(v) => updateField('llm_cache_enabled', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Custom instructions */}
      <Textarea
        label={t('settings.general.customInstructions')}
        placeholder={t('settings.general.customInstructionsPlaceholder')}
        value={config.custom_instructions}
        rows={5}
        onChange={(e) => updateField('custom_instructions', e.target.value)}
      />

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
