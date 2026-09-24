import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSettingsStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { Button, SegmentedControl } from '@/components/ui'
import { Monitor, Moon, Sun } from '@/components/ui/icons'
import {
  ACCENT_PRESETS,
  hasStoredTheme,
  isWebThemeId,
  THEME_FAMILIES,
  useTheme,
  type AccentPreset,
  type ThemeFamily,
  type ThemeModePref,
} from '@/hooks/useTheme'
import { ACCENT_PALETTES, FAMILY_PALETTES } from '@/styles/themes'
import { getUIScale, setUIScale, type UIScale } from './uiScale'

/**
 * Appearance settings: mode, theme family, accent and interface font size.
 *
 * `mode` and `family` are mirrored into the backend `theme` config field
 * ("family-mode", shared with the TUI) exactly like the old ThemePicker did
 * — `useTheme()` applies the choice locally/instantly, `updateField` stages
 * it for the page's Save button. `accent` and the font size are local-only
 * (never sent to the backend).
 */
export default function AppearanceSettings() {
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
  } = useSettingsStore()
  useUnsavedChangesGuard({
    id: 'appearance',
    dirty,
    save: async () => {
      await saveSettings()
      return !useSettingsStore.getState().error
    },
    discard: resetSettings,
  })
  const { family, mode, resolvedMode, accent, setFamily, setMode, setAccent, setTheme } = useTheme()
  const [uiSize, setUiSize] = useState<UIScale>(() => getUIScale())

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  // Adopt the backend theme only on a browser with no local choice yet. The
  // backend field is shared with the TUI, so non-WebUI ids are ignored.
  // (GeneralSettings, which used to own the theme picker, keeps a silent
  // copy of this same guarded effect so a fresh browser still adopts it the
  // first time Settings is opened, even if the user never visits this tab.)
  useEffect(() => {
    if (!loading && isWebThemeId(config.theme) && !hasStoredTheme()) {
      setTheme(config.theme)
    }
  }, [loading, config.theme, setTheme])

  const onModeChange = (m: ThemeModePref) => {
    setMode(m)
    updateField('theme', `${family}-${m}`)
  }

  const onFamilyChange = (f: ThemeFamily) => {
    setFamily(f)
    updateField('theme', `${f}-${mode}`)
  }

  const onSizeChange = (s: UIScale) => {
    setUiSize(s)
    setUIScale(s)
  }

  if (loading) {
    return <div className="settings-loading">{t('settings.general.loadingSettings')}</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">{t('settings.categories.appearance', 'Appearance')}</h2>
        <p className="settings-page-description">
          {t('settings.appearance.description', 'Choose how Pando looks on this device. Mode and theme are also used by the TUI; accent and font size are local to this browser.')}
        </p>
      </header>

      <section className="ui-settings-section">
        <div className="ui-settings-group">
          <div className="ui-settings-row">
            <div className="ui-settings-row-text">
              <span className="ui-settings-row-label">{t('settings.appearance.mode', 'Appearance mode')}</span>
              <p className="ui-settings-row-description">
                {t('settings.appearance.modeDescription', 'System follows your OS setting and switches live.')}
              </p>
            </div>
            <div className="ui-settings-row-control">
              <SegmentedControl<ThemeModePref>
                aria-label={t('settings.appearance.mode', 'Appearance mode')}
                value={mode}
                onChange={onModeChange}
                items={[
                  { value: 'light', label: t('settings.general.themeLight'), icon: <Sun size={14} /> },
                  { value: 'dark', label: t('settings.general.themeDark'), icon: <Moon size={14} /> },
                  { value: 'system', label: t('settings.general.themeSystem', 'System'), icon: <Monitor size={14} /> },
                ]}
              />
            </div>
          </div>
          <div className="ui-settings-row">
            <div className="ui-settings-row-text">
              <span className="ui-settings-row-label">{t('settings.appearance.fontSize', 'Interface font size')}</span>
              <p className="ui-settings-row-description">
                {t('settings.appearance.fontSizeDescription', 'Adjusts the base text size across the app.')}
              </p>
            </div>
            <div className="ui-settings-row-control">
              <SegmentedControl<UIScale>
                aria-label={t('settings.appearance.fontSize', 'Interface font size')}
                value={uiSize}
                onChange={onSizeChange}
                items={[
                  { value: 'small', label: <span className="settings-fontsize-sample text-[12px]">{t('settings.appearance.fontSizeSmall', 'Small')}</span> },
                  { value: 'default', label: <span className="settings-fontsize-sample text-[13px]">{t('settings.appearance.fontSizeDefault', 'Default')}</span> },
                  { value: 'large', label: <span className="settings-fontsize-sample text-[15px]">{t('settings.appearance.fontSizeLarge', 'Large')}</span> },
                ]}
              />
            </div>
          </div>
        </div>
      </section>

      <section className="ui-settings-section">
        <header className="ui-settings-section-header">
          <h3 className="ui-settings-section-title">{t('settings.appearance.themeFamily', 'Theme')}</h3>
          <p className="ui-settings-section-description">
            {t('settings.appearance.themeFamilyDescription', 'A colour family for surfaces and text. Mode above decides light or dark.')}
          </p>
        </header>
        <div className="ui-theme-grid" role="radiogroup" aria-label={t('settings.appearance.themeFamily', 'Theme')}>
          {THEME_FAMILIES.map((f) => {
            const p = FAMILY_PALETTES[f][resolvedMode]
            return (
              <button
                key={f}
                type="button"
                role="radio"
                aria-checked={family === f}
                className="ui-theme-card"
                onClick={() => onFamilyChange(f)}
              >
                <span className="ui-theme-preview" style={{ background: p.bg }} aria-hidden>
                  <span className="ui-theme-preview-shell" style={{ background: p.shell }} />
                  <span className="ui-theme-preview-main">
                    <span className="ui-theme-preview-line" style={{ background: p.fg, opacity: 0.85, width: '80%' }} />
                    <span className="ui-theme-preview-line" style={{ background: p.fg, opacity: 0.45, width: '60%' }} />
                    <span className="ui-theme-preview-line" style={{ background: p.raised, width: '70%' }} />
                    <span className="ui-theme-preview-dot" style={{ background: p.accent }} />
                  </span>
                </span>
                <span className="ui-theme-card-label">{FAMILY_PALETTES[f].label}</span>
              </button>
            )
          })}
        </div>
      </section>

      <section className="ui-settings-section">
        <header className="ui-settings-section-header">
          <h3 className="ui-settings-section-title">{t('settings.appearance.accent', 'Accent colour')}</h3>
          <p className="ui-settings-section-description">
            {t('settings.appearance.accentDescription', 'Used for the primary action, focus and selection. Everything else stays neutral.')}
          </p>
        </header>
        <div className="ui-swatches" role="radiogroup" aria-label={t('settings.appearance.accent', 'Accent colour')}>
          <button
            type="button"
            role="radio"
            aria-checked={accent === null}
            aria-label={t('settings.appearance.accentDefault', 'Theme default')}
            title={t('settings.appearance.accentDefault', 'Theme default')}
            className="ui-swatch settings-swatch-default"
            onClick={() => setAccent(null)}
          />
          {ACCENT_PRESETS.map((a: AccentPreset) => (
            <button
              key={a}
              type="button"
              role="radio"
              aria-checked={accent === a}
              aria-label={ACCENT_PALETTES[a].label}
              title={ACCENT_PALETTES[a].label}
              className="ui-swatch"
              style={{ background: ACCENT_PALETTES[a][resolvedMode] }}
              onClick={() => setAccent(a)}
            />
          ))}
        </div>
      </section>

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
