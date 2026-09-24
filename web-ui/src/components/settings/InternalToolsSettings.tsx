import { useEffect, useMemo, useState } from 'react'
import { useToolsStore } from '@pando/client/stores/settingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import api from '@pando/client/services/api'
import MaskedInput from '@/components/shared/MaskedInput'
import type { BrowserInstallInfo, ToolsConfig } from '@pando/client/types'
import { Button, Card, Input, Select, Switch } from '@/components/ui'

// ---- Config status indicator ----

type ConfigStatus = 'ok' | 'disabled' | 'incomplete'

const STATUS_LABEL: Record<ConfigStatus, string> = {
  ok: 'Configured',
  disabled: 'Disabled',
  incomplete: 'Missing config',
}
const STATUS_CLASS: Record<ConfigStatus, string> = {
  ok: 'bg-success',
  disabled: 'bg-faint',
  incomplete: 'bg-warning',
}

function StatusDot({ status }: { status: ConfigStatus }) {
  return <span title={STATUS_LABEL[status]} className={`inline-block w-2 h-2 rounded-full shrink-0 ${STATUS_CLASS[status]}`} />
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="settings-field">
      <label className="settings-field-label">{label}</label>
      {children}
    </div>
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
  const id = `tool-${title.replace(/\W+/g, '-').toLowerCase()}`
  return (
    <Card padding="none">
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2">
          <StatusDot status={status} />
          <span className="text-sm font-semibold text-fg">{title}</span>
        </div>
        <Switch id={id} aria-label={title} checked={enabled} onCheckedChange={onToggle} />
      </div>

      {enabled && children && <div className="flex flex-col gap-4 p-4 border-t border-border">{children}</div>}
    </Card>
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

// Remote CDP-server browsers (Lightpanda, Obscura) are launched by Pando as a
// CDP WebSocket server rather than a Chromium executable, so profile/user-data-dir
// and headless flags do not apply to them.
function isRemoteBrowserType(type: string): boolean {
  return type === 'lightpanda' || type === 'obscura'
}

// ---- Main component ----

export default function InternalToolsSettings() {
  const { config, dirtyKeys, dirty, loading, saving, error, fetchTools, updateField, updateApiKey, saveTools, resetTools } =
    useToolsStore()
  useUnsavedChangesGuard({
    id: 'tools',
    dirty,
    save: async () => {
      await saveTools()
      return !useToolsStore.getState().error
    },
    discard: resetTools,
  })
  const [browsers, setBrowsers] = useState<BrowserInstallInfo[]>([])

  useEffect(() => {
    fetchTools()
  }, [fetchTools])

  useEffect(() => {
    api.get<{ browsers: BrowserInstallInfo[] }>('/api/v1/config/browsers')
      .then((data) => setBrowsers(data.browsers ?? []))
      .catch(() => setBrowsers([]))
  }, [])

  const browserOptions = useMemo(() => {
    const base = [
      { value: 'chrome', label: 'Google Chrome' },
      { value: 'msedge', label: 'Microsoft Edge' },
      { value: 'chromium', label: 'Chromium' },
      { value: 'opera', label: 'Opera' },
    ]
    const seen = new Set(base.map((option) => option.value))
    for (const browser of browsers) {
      if (!seen.has(browser.type)) {
        base.push({ value: browser.type, label: browser.label })
      }
    }
    return base
  }, [browsers])

  const detectedBrowser = useMemo(
    () => browsers.find((browser) => browser.type === config.browserType),
    [browsers, config.browserType],
  )

  if (loading) {
    return <div className="settings-loading">Loading tools configuration…</div>
  }

  // Effective API key values: prefer the user-typed draft
  function apiKeyValue(field: keyof ToolsConfig): string {
    return (dirtyKeys[field] as string | undefined) ?? (config[field] as string)
  }

  function handleApiKey(field: keyof ToolsConfig) {
    return (value: string) => updateApiKey(field, value)
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Internal Tools</h2>
        <p className="settings-page-description">
          Configure the built-in tools available to the AI assistant. Disabled tools are never invoked.
        </p>
      </header>

      <div className="flex flex-col gap-3">
        <ToolCard title="Fetch" enabled={config.fetchEnabled} status={simpleStatus(config.fetchEnabled)} onToggle={(v) => updateField('fetchEnabled', v)}>
          <Field label="Max response size (MB)">
            <Input
              type="number"
              min={1}
              max={100}
              value={config.fetchMaxSizeMB}
              onChange={(e) => updateField('fetchMaxSizeMB', parseInt(e.target.value, 10) || 10)}
            />
          </Field>
        </ToolCard>

        <ToolCard
          title="Google Search"
          enabled={config.googleSearchEnabled}
          status={configStatus(config.googleSearchEnabled, config.googleApiKey, config.googleSearchEngineId)}
          onToggle={(v) => updateField('googleSearchEnabled', v)}
        >
          <MaskedInput label="API key" placeholder="Enter Google API key…" value={apiKeyValue('googleApiKey')} onChange={handleApiKey('googleApiKey')} />
          <Field label="Custom search engine ID (CX)">
            <Input
              placeholder="e.g. 017576662512468239146:omuauf_lfve"
              value={config.googleSearchEngineId}
              onChange={(e) => updateField('googleSearchEngineId', e.target.value)}
            />
          </Field>
        </ToolCard>

        <ToolCard
          title="Brave Search"
          enabled={config.braveSearchEnabled}
          status={configStatus(config.braveSearchEnabled, config.braveApiKey)}
          onToggle={(v) => updateField('braveSearchEnabled', v)}
        >
          <MaskedInput label="API key" placeholder="Enter Brave Search API key…" value={apiKeyValue('braveApiKey')} onChange={handleApiKey('braveApiKey')} />
        </ToolCard>

        <ToolCard
          title="Perplexity"
          enabled={config.perplexitySearchEnabled}
          status={configStatus(config.perplexitySearchEnabled, config.perplexityApiKey)}
          onToggle={(v) => updateField('perplexitySearchEnabled', v)}
        >
          <MaskedInput label="API key" placeholder="Enter Perplexity API key…" value={apiKeyValue('perplexityApiKey')} onChange={handleApiKey('perplexityApiKey')} />
        </ToolCard>

        <ToolCard
          title="Exa AI Search"
          enabled={config.exaSearchEnabled}
          status={configStatus(config.exaSearchEnabled, config.exaApiKey)}
          onToggle={(v) => updateField('exaSearchEnabled', v)}
        >
          <MaskedInput label="API key" placeholder="Enter Exa API key…" value={apiKeyValue('exaApiKey')} onChange={handleApiKey('exaApiKey')} />
        </ToolCard>

        <ToolCard
          title="Sourcegraph Code Search"
          enabled={config.sourcegraphEnabled}
          status={simpleStatus(config.sourcegraphEnabled)}
          onToggle={(v) => updateField('sourcegraphEnabled', v)}
        >
          <MaskedInput
            label="Access token (optional — uses public API if empty)"
            placeholder="sgp_…"
            value={apiKeyValue('sourcegraphToken')}
            onChange={handleApiKey('sourcegraphToken')}
          />
        </ToolCard>

        <ToolCard title="Context7 (Library Docs)" enabled={config.context7Enabled} status={simpleStatus(config.context7Enabled)} onToggle={(v) => updateField('context7Enabled', v)} />

        <ToolCard title="Browser (Chrome DevTools)" enabled={config.browserEnabled} status={simpleStatus(config.browserEnabled)} onToggle={(v) => updateField('browserEnabled', v)}>
          <Field label="Browser">
            <Select
              value={config.browserType}
              options={browserOptions}
              onChange={(e) => {
                const browserType = e.target.value
                const selected = browsers.find((browser) => browser.type === browserType)
                updateField('browserType', browserType)
                updateField('browserExecutable', selected?.executable ?? '')
                if (!config.browserUserDataDir && selected?.userDataDir) {
                  updateField('browserUserDataDir', selected.userDataDir)
                }
              }}
            />
          </Field>
          {isRemoteBrowserType(config.browserType) && (
            <p className="text-xs text-muted m-0">
              Launched by Pando as a local CDP server; profile, user-data-dir and headless options do not apply.
            </p>
          )}
          <Field label="Browser executable">
            <Input placeholder="Auto-detected from selected browser" value={config.browserExecutable} onChange={(e) => updateField('browserExecutable', e.target.value)} />
          </Field>
          <Field label="User data directory">
            <Input placeholder="/tmp/pando-browser" value={config.browserUserDataDir} onChange={(e) => updateField('browserUserDataDir', e.target.value)} />
          </Field>
          {detectedBrowser && (
            <p className="text-xs text-muted m-0">
              Detected: {detectedBrowser.label} · {detectedBrowser.executable}
            </p>
          )}
          <div className="flex items-center gap-3">
            <Switch id="browser-headless" checked={config.browserHeadless} onCheckedChange={(v) => updateField('browserHeadless', v)} />
            <label htmlFor="browser-headless" className="cursor-pointer">
              <div className="text-sm font-medium text-fg">Headless mode</div>
              <div className="text-xs text-muted">Run browser without a visible window</div>
            </label>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Timeout (seconds)">
              <Input type="number" min={5} max={300} value={config.browserTimeout} onChange={(e) => updateField('browserTimeout', parseInt(e.target.value, 10) || 30)} />
            </Field>
            <Field label="Max sessions">
              <Input type="number" min={1} max={20} value={config.browserMaxSessions} onChange={(e) => updateField('browserMaxSessions', parseInt(e.target.value, 10) || 3)} />
            </Field>
          </div>
        </ToolCard>

        <ToolCard
          title="Desktop Controller (Accessibility Automation)"
          enabled={config.desktopEnabled}
          status={simpleStatus(config.desktopEnabled)}
          onToggle={(v) => updateField('desktopEnabled', v)}
        >
          <p className="text-xs text-muted m-0">
            Lets the agent read and act on the user&apos;s desktop UI via the OS accessibility tree. Can read and act
            on the whole desktop session — review carefully before enabling.
          </p>
          <Field label="Backend">
            <Select
              value={config.desktopBackend}
              options={[
                { value: 'auto', label: 'Auto (platform default)' },
                { value: 'atspi', label: 'AT-SPI2 (Linux)' },
                { value: 'uia', label: 'UI Automation (Windows)' },
                { value: 'ax', label: 'Accessibility API (macOS)' },
                { value: 'cdp', label: 'Chrome DevTools Protocol' },
                { value: 'null', label: 'Disabled (Null)' },
              ]}
              onChange={(e) => updateField('desktopBackend', e.target.value)}
            />
          </Field>
          <div className="flex items-center gap-3">
            <Switch id="desktop-physical-input" checked={config.desktopAllowPhysicalInput} onCheckedChange={(v) => updateField('desktopAllowPhysicalInput', v)} />
            <label htmlFor="desktop-physical-input" className="cursor-pointer">
              <div className="text-sm font-medium text-fg">Allow physical input fallback</div>
              <div className="text-xs text-muted">Fall back to synthetic mouse/keyboard when a native accessibility action is unsupported</div>
            </label>
          </div>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
            <Field label="Max nodes">
              <Input value={String(config.desktopMaxNodes)} onChange={(e) => updateField('desktopMaxNodes', parseInt(e.target.value, 10) || 500)} />
            </Field>
            <Field label="Default depth">
              <Input value={String(config.desktopDefaultDepth)} onChange={(e) => updateField('desktopDefaultDepth', parseInt(e.target.value, 10) || 3)} />
            </Field>
            <Field label="Action timeout (s)">
              <Input value={String(config.desktopActionTimeout)} onChange={(e) => updateField('desktopActionTimeout', parseInt(e.target.value, 10) || 10)} />
            </Field>
            <Field label="Snapshot TTL (s)">
              <Input value={String(config.desktopSnapshotTTL)} onChange={(e) => updateField('desktopSnapshotTTL', parseInt(e.target.value, 10) || 60)} />
            </Field>
            <Field label="Screenshot scale">
              <Input value={String(config.desktopScreenshotScale)} onChange={(e) => updateField('desktopScreenshotScale', parseFloat(e.target.value) || 1.0)} />
            </Field>
          </div>
          <Field label="Allowed apps (comma-separated, empty = all)">
            <Input
              placeholder="e.g. Firefox, VSCode"
              value={(config.desktopAllowedApps ?? []).join(', ')}
              onChange={(e) => updateField('desktopAllowedApps', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
            />
          </Field>
          <Field label="Denied apps (comma-separated)">
            <Input
              placeholder="e.g. 1Password, Keychain Access"
              value={(config.desktopDeniedApps ?? []).join(', ')}
              onChange={(e) => updateField('desktopDeniedApps', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
            />
          </Field>
        </ToolCard>
      </div>

      {error && <div className="settings-banner settings-banner--danger mt-4" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveTools} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetTools} disabled={!dirty}>
          Reset
        </Button>
      </div>
    </div>
  )
}
