import { useEffect } from 'react'
import { useSettingsStore } from '@/stores/settingsStore'
import { TextInput, SelectInput, Textarea, Toggle } from '@/components/shared/FormInput'
import ModelCombobox from '@/components/shared/ModelCombobox'

const LANGUAGE_OPTIONS = [
  { value: 'en', label: 'English' },
  { value: 'es', label: 'Spanish' },
  { value: 'fr', label: 'French' },
  { value: 'de', label: 'German' },
  { value: 'pt', label: 'Portuguese' },
  { value: 'ja', label: 'Japanese' },
  { value: 'zh', label: 'Chinese' },
]

const THEME_OPTIONS = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
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
  const { config, dirty, loading, saving, error, fetchSettings, updateField, saveSettings, resetSettings } =
    useSettingsStore()

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading settings…
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>General Settings</h2>

      {/* Text fields */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
        <TextInput
          label="Working Directory"
          placeholder="/home/user/project"
          value={config.home_directory}
          onChange={(e) => updateField('home_directory', e.target.value)}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Default Model
          </label>
          <ModelCombobox
            value={config.default_model}
            onChange={(v) => updateField('default_model', v)}
          />
        </div>

        <SelectInput
          label="Default Provider"
          options={PROVIDER_OPTIONS}
          value={config.default_provider}
          onChange={(e) => updateField('default_provider', e.target.value)}
        />

        <SelectInput
          label="Language"
          options={LANGUAGE_OPTIONS}
          value={config.language}
          onChange={(e) => updateField('language', e.target.value)}
        />

        <SelectInput
          label="Theme"
          options={THEME_OPTIONS}
          value={config.theme}
          onChange={(e) => updateField('theme', e.target.value as 'light' | 'dark')}
        />
      </div>

      <div style={dividerStyle} />

      {/* Toggles */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Auto-save"
          description="Automatically save session progress"
          checked={config.auto_save}
          onChange={(v) => updateField('auto_save', v)}
        />
        <Toggle
          label="Markdown preview"
          description="Render markdown in chat messages"
          checked={config.markdown_preview}
          onChange={(v) => updateField('markdown_preview', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Custom instructions */}
      <Textarea
        label="Custom instructions"
        placeholder="Add any global instructions for the AI assistant…"
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
          {saving ? 'Saving…' : 'Save'}
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
          Reset
        </button>
      </div>
    </div>
  )
}
