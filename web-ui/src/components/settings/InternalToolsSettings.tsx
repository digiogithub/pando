import { useEffect } from 'react'
import { useToolsStore } from '@/stores/settingsStore'
import { Toggle, TextInput, MaskedInput } from '@/components/shared/FormInput'
import type { ToolsConfig } from '@/types'

// ---- Config status indicator ----

type ConfigStatus = 'ok' | 'disabled' | 'incomplete'

function StatusDot({ status }: { status: ConfigStatus }) {
  const colors: Record<ConfigStatus, string> = {
    ok: '#22c55e',
    disabled: '#94a3b8',
    incomplete: '#f59e0b',
  }
  const labels: Record<ConfigStatus, string> = {
    ok: 'Configured',
    disabled: 'Disabled',
    incomplete: 'Missing config',
  }
  return (
    <span
      title={labels[status]}
      style={{
        display: 'inline-block',
        width: 8,
        height: 8,
        borderRadius: '50%',
        background: colors[status],
        flexShrink: 0,
      }}
    />
  )
}

// ---- ToolCard ----

interface ToolCardProps {
  title: string
  status: ConfigStatus
  enabled: boolean
  onToggle: (v: boolean) => void
  children?: React.ReactNode
}

function ToolCard({ title, status, enabled, onToggle, children }: ToolCardProps) {
  return (
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-sm)',
        background: 'var(--surface, var(--input-bg))',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0.75rem 1rem',
          borderBottom: enabled && children ? '1px solid var(--border)' : 'none',
          background: 'var(--sidebar-bg)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <StatusDot status={status} />
          <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--fg)' }}>{title}</span>
        </div>
        <Toggle label="" checked={enabled} onChange={onToggle} />
      </div>

      {/* Body — only visible when enabled */}
      {enabled && children && (
        <div
          style={{
            padding: '1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '1rem',
          }}
        >
          {children}
        </div>
      )}
    </div>
  )
}

// ---- Helpers ----

function isMasked(val: string): boolean {
  return val.startsWith('••••')
}

function configStatus(enabled: boolean, ...requiredKeys: string[]): ConfigStatus {
  if (!enabled) return 'disabled'
  const allSet = requiredKeys.every((k) => k && !isMasked(k) && k.trim() !== '' || isMasked(k))
  return allSet ? 'ok' : 'incomplete'
}

function simpleStatus(enabled: boolean): ConfigStatus {
  return enabled ? 'ok' : 'disabled'
}

// ---- Main component ----

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

export default function InternalToolsSettings() {
  const { config, dirtyKeys, dirty, loading, saving, error, fetchTools, updateField, updateApiKey, saveTools, resetTools } =
    useToolsStore()

  useEffect(() => {
    fetchTools()
  }, [fetchTools])

  if (loading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading tools configuration…
      </div>
    )
  }

  // Effective API key values: prefer the user-typed draft
  function apiKeyValue(field: keyof ToolsConfig): string {
    return (dirtyKeys[field] as string | undefined) ?? (config[field] as string)
  }

  function handleApiKey(field: keyof ToolsConfig) {
    return (e: React.ChangeEvent<HTMLInputElement>) => updateApiKey(field, e.target.value)
  }

  return (
    <div style={{ maxWidth: 680 }}>
      <h2 style={sectionTitle}>Internal Tools</h2>
      <p style={{ fontSize: 13, color: 'var(--fg-muted)', marginBottom: '1.5rem' }}>
        Configure the built-in tools available to the AI assistant. Disabled tools are never invoked.
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>

        {/* Fetch */}
        <ToolCard
          title="Fetch"
          enabled={config.fetchEnabled}
          status={simpleStatus(config.fetchEnabled)}
          onToggle={(v) => updateField('fetchEnabled', v)}
        >
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: '0.125rem',
            }}
          >
            <label
              style={{
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--fg-muted)',
                textTransform: 'uppercase',
                letterSpacing: '0.04em',
              }}
            >
              Max Response Size (MB)
            </label>
            <input
              type="number"
              min={1}
              max={100}
              value={config.fetchMaxSizeMB}
              onChange={(e) => updateField('fetchMaxSizeMB', parseInt(e.target.value, 10) || 10)}
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
              }}
            />
          </div>
        </ToolCard>

        {/* Google Search */}
        <ToolCard
          title="Google Search"
          enabled={config.googleSearchEnabled}
          status={configStatus(config.googleSearchEnabled, config.googleApiKey, config.googleSearchEngineId)}
          onToggle={(v) => updateField('googleSearchEnabled', v)}
        >
          <MaskedInput
            label="API Key"
            placeholder="Enter Google API key…"
            value={apiKeyValue('googleApiKey')}
            onChange={handleApiKey('googleApiKey')}
          />
          <TextInput
            label="Custom Search Engine ID (CX)"
            placeholder="e.g. 017576662512468239146:omuauf_lfve"
            value={config.googleSearchEngineId}
            onChange={(e) => updateField('googleSearchEngineId', e.target.value)}
          />
        </ToolCard>

        {/* Brave Search */}
        <ToolCard
          title="Brave Search"
          enabled={config.braveSearchEnabled}
          status={configStatus(config.braveSearchEnabled, config.braveApiKey)}
          onToggle={(v) => updateField('braveSearchEnabled', v)}
        >
          <MaskedInput
            label="API Key"
            placeholder="Enter Brave Search API key…"
            value={apiKeyValue('braveApiKey')}
            onChange={handleApiKey('braveApiKey')}
          />
        </ToolCard>

        {/* Perplexity */}
        <ToolCard
          title="Perplexity"
          enabled={config.perplexitySearchEnabled}
          status={configStatus(config.perplexitySearchEnabled, config.perplexityApiKey)}
          onToggle={(v) => updateField('perplexitySearchEnabled', v)}
        >
          <MaskedInput
            label="API Key"
            placeholder="Enter Perplexity API key…"
            value={apiKeyValue('perplexityApiKey')}
            onChange={handleApiKey('perplexityApiKey')}
          />
        </ToolCard>

        {/* Exa */}
        <ToolCard
          title="Exa AI Search"
          enabled={config.exaSearchEnabled}
          status={configStatus(config.exaSearchEnabled, config.exaApiKey)}
          onToggle={(v) => updateField('exaSearchEnabled', v)}
        >
          <MaskedInput
            label="API Key"
            placeholder="Enter Exa API key…"
            value={apiKeyValue('exaApiKey')}
            onChange={handleApiKey('exaApiKey')}
          />
        </ToolCard>

        {/* Context7 */}
        <ToolCard
          title="Context7 (Library Docs)"
          enabled={config.context7Enabled}
          status={simpleStatus(config.context7Enabled)}
          onToggle={(v) => updateField('context7Enabled', v)}
        />

        {/* Browser */}
        <ToolCard
          title="Browser (Chrome DevTools)"
          enabled={config.browserEnabled}
          status={simpleStatus(config.browserEnabled)}
          onToggle={(v) => updateField('browserEnabled', v)}
        >
          <TextInput
            label="User Data Directory"
            placeholder="/tmp/pando-browser"
            value={config.browserUserDataDir}
            onChange={(e) => updateField('browserUserDataDir', e.target.value)}
          />
          <Toggle
            label="Headless mode"
            description="Run browser without a visible window"
            checked={config.browserHeadless}
            onChange={(v) => updateField('browserHeadless', v)}
          />
          <div style={{ display: 'flex', gap: '1rem' }}>
            <div style={{ flex: 1 }}>
              <label
                style={{
                  display: 'block',
                  fontSize: 12,
                  fontWeight: 600,
                  color: 'var(--fg-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.04em',
                  marginBottom: '0.375rem',
                }}
              >
                Timeout (seconds)
              </label>
              <input
                type="number"
                min={5}
                max={300}
                value={config.browserTimeout}
                onChange={(e) => updateField('browserTimeout', parseInt(e.target.value, 10) || 30)}
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
                }}
              />
            </div>
            <div style={{ flex: 1 }}>
              <label
                style={{
                  display: 'block',
                  fontSize: 12,
                  fontWeight: 600,
                  color: 'var(--fg-muted)',
                  textTransform: 'uppercase',
                  letterSpacing: '0.04em',
                  marginBottom: '0.375rem',
                }}
              >
                Max Sessions
              </label>
              <input
                type="number"
                min={1}
                max={20}
                value={config.browserMaxSessions}
                onChange={(e) => updateField('browserMaxSessions', parseInt(e.target.value, 10) || 3)}
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
                }}
              />
            </div>
          </div>
        </ToolCard>

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
          onClick={saveTools}
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
          onClick={resetTools}
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
