import { useEffect, useState } from 'react'
import { useProvidersStore } from '@/stores/settingsStore'
import type { ProviderConfigItem } from '@/types'
import { Toggle } from '@/components/shared/FormInput'

// Provider metadata: display name, icon (emoji), supports base URL customization
interface ProviderMeta {
  name: string
  label: string
  icon: string
  hasBaseUrl: boolean
  hasOAuth: boolean
}

const PROVIDER_META: ProviderMeta[] = [
  { name: 'anthropic', label: 'Anthropic', icon: '🤖', hasBaseUrl: false, hasOAuth: false },
  { name: 'openai', label: 'OpenAI', icon: '🧠', hasBaseUrl: true, hasOAuth: false },
  { name: 'ollama', label: 'Ollama', icon: '🦙', hasBaseUrl: true, hasOAuth: false },
  { name: 'google', label: 'Gemini', icon: '✨', hasBaseUrl: false, hasOAuth: false },
  { name: 'groq', label: 'GROQ', icon: '⚡', hasBaseUrl: false, hasOAuth: false },
  { name: 'openrouter', label: 'OpenRouter', icon: '🔀', hasBaseUrl: true, hasOAuth: false },
  { name: 'xai', label: 'xAI (Grok)', icon: '🌌', hasBaseUrl: false, hasOAuth: false },
  { name: 'copilot', label: 'GitHub Copilot', icon: '🐙', hasBaseUrl: false, hasOAuth: true },
]

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

// Masked API key input — only sends a new value if the user typed something
function MaskedInput({
  maskedValue,
  onChange,
  placeholder,
}: {
  maskedValue: string
  onChange: (val: string) => void
  placeholder?: string
}) {
  const [localVal, setLocalVal] = useState('')
  const [showKey, setShowKey] = useState(false)
  const [touched, setTouched] = useState(false)

  // Display: if user hasn't typed yet, show masked placeholder; otherwise show what they typed
  const displayValue = touched ? localVal : ''
  const inputPlaceholder = maskedValue
    ? maskedValue // shows "••••last4" from server
    : placeholder ?? 'Enter API key…'

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    setLocalVal(e.target.value)
    setTouched(true)
    onChange(e.target.value)
  }

  return (
    <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
      <input
        type={showKey ? 'text' : 'password'}
        autoComplete="new-password"
        value={displayValue}
        placeholder={inputPlaceholder}
        onChange={handleChange}
        style={{
          background: 'var(--input-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--fg)',
          fontSize: 14,
          padding: '0.5rem 2.5rem 0.5rem 0.75rem',
          outline: 'none',
          width: '100%',
          fontFamily: 'monospace',
          boxSizing: 'border-box',
          transition: 'border-color 0.15s',
        }}
        onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
        onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
      />
      <button
        type="button"
        tabIndex={-1}
        onClick={() => setShowKey((v) => !v)}
        title={showKey ? 'Hide key' : 'Show key'}
        style={{
          position: 'absolute',
          right: '0.5rem',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          color: 'var(--fg-muted)',
          fontSize: 14,
          padding: 0,
          lineHeight: 1,
        }}
      >
        {showKey ? '🙈' : '👁'}
      </button>
    </div>
  )
}

function StatusBadge({ configured }: { configured: boolean }) {
  return (
    <span
      style={{
        fontSize: 11,
        fontWeight: 600,
        padding: '0.2rem 0.5rem',
        borderRadius: 'var(--radius-sm)',
        background: configured ? 'rgba(34,197,94,0.12)' : 'var(--border)',
        color: configured ? '#16a34a' : 'var(--fg-muted)',
        letterSpacing: '0.03em',
      }}
    >
      {configured ? 'Configured' : 'Not configured'}
    </span>
  )
}

function ProviderCard({
  provider,
  meta,
  onUpdate,
  onKeyChange,
}: {
  provider: ProviderConfigItem
  meta: ProviderMeta
  onUpdate: (patch: Partial<ProviderConfigItem>) => void
  onKeyChange: (key: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const isConfigured = !!provider.apiKey || provider.useOAuth

  return (
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
        overflow: 'hidden',
        background: 'var(--card-bg, var(--input-bg))',
      }}
    >
      {/* Header row */}
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0.875rem 1rem',
          background: 'none',
          border: 'none',
          cursor: 'pointer',
          textAlign: 'left',
          fontFamily: 'inherit',
        }}
      >
        <span style={{ fontSize: 22 }}>{meta.icon}</span>
        <span style={{ flex: 1, fontSize: 15, fontWeight: 600, color: 'var(--fg)' }}>
          {meta.label}
        </span>
        <StatusBadge configured={isConfigured} />
        <span
          style={{
            color: 'var(--fg-muted)',
            fontSize: 12,
            marginLeft: '0.5rem',
            transform: expanded ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.2s',
            display: 'inline-block',
          }}
        >
          ▼
        </span>
      </button>

      {/* Collapsible form */}
      {expanded && (
        <div
          style={{
            padding: '0 1rem 1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '1rem',
            borderTop: '1px solid var(--border)',
          }}
        >
          <div style={{ height: '0.75rem' }} />

          {/* API Key — only for non-OAuth providers */}
          {!meta.hasOAuth && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
              <label
                style={{
                  fontSize: 12,
                  fontWeight: 600,
                  color: 'var(--fg-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.04em',
                }}
              >
                API Key
              </label>
              <MaskedInput
                maskedValue={provider.apiKey}
                onChange={onKeyChange}
                placeholder={`Enter ${meta.label} API key…`}
              />
            </div>
          )}

          {/* OAuth toggle */}
          {meta.hasOAuth && (
            <Toggle
              label="Use OAuth"
              description="Authenticate via GitHub OAuth instead of an API key"
              checked={provider.useOAuth}
              onChange={(v) => onUpdate({ useOAuth: v })}
            />
          )}

          {/* Base URL */}
          {meta.hasBaseUrl && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
              <label
                style={{
                  fontSize: 12,
                  fontWeight: 600,
                  color: 'var(--fg-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.04em',
                }}
              >
                Base URL
              </label>
              <input
                type="text"
                value={provider.baseUrl}
                placeholder="https://api.example.com/v1"
                onChange={(e) => onUpdate({ baseUrl: e.target.value })}
                style={{
                  background: 'var(--input-bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--radius-sm)',
                  color: 'var(--fg)',
                  fontSize: 14,
                  padding: '0.5rem 0.75rem',
                  outline: 'none',
                  width: '100%',
                  fontFamily: 'inherit',
                  boxSizing: 'border-box',
                  transition: 'border-color 0.15s',
                }}
                onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
                onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
              />
            </div>
          )}

          {/* Disabled toggle */}
          <Toggle
            label="Enabled"
            description="Allow this provider to be used for AI requests"
            checked={!provider.disabled}
            onChange={(v) => onUpdate({ disabled: !v })}
          />
        </div>
      )}
    </div>
  )
}

export default function ProvidersSettings() {
  const { providers, dirty, loading, saving, error, fetchProviders, updateProvider, updateProviderKey, saveProviders, resetProviders } =
    useProvidersStore()

  useEffect(() => {
    fetchProviders()
  }, [fetchProviders])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading providers…
      </div>
    )
  }

  // Build a map for quick lookup; fill in missing providers with empty defaults
  const providerMap = new Map(providers.map((p) => [p.name, p]))

  const displayProviders: ProviderConfigItem[] = PROVIDER_META.map((meta) =>
    providerMap.get(meta.name) ?? {
      name: meta.name,
      apiKey: '',
      baseUrl: '',
      disabled: true,
      useOAuth: false,
    }
  )

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Providers</h2>
      <p style={{ fontSize: 14, color: 'var(--fg-muted)', marginBottom: '1.5rem' }}>
        Configure API keys and settings for AI providers. Keys are stored securely and never shown in full.
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
        {PROVIDER_META.map((meta) => {
          const provider = displayProviders.find((p) => p.name === meta.name)!
          return (
            <ProviderCard
              key={meta.name}
              provider={provider}
              meta={meta}
              onUpdate={(patch) => updateProvider(meta.name, patch)}
              onKeyChange={(key) => updateProviderKey(meta.name, key)}
            />
          )
        })}
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
          onClick={saveProviders}
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
          onClick={resetProviders}
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
          Reset
        </button>
      </div>
    </div>
  )
}
