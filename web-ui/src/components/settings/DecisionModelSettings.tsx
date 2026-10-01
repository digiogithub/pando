import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  isHostedDecisionProvider,
  useDecisionModelStore,
  type DecisionProviderKind,
} from '@pando/client/stores/decisionModelStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { interpolate } from '@/utils/interpolate'
import { Badge, Button, Input, SettingsRow, SettingsSection, Select, Switch } from '@/components/ui'
import { CircleAlert, CircleCheck, Plus, Trash2, TriangleAlert } from '@/components/ui/icons'

const PROVIDER_DEFAULT_URL: Record<DecisionProviderKind, string> = {
  ollama: 'http://localhost:11434',
  typesafe: 'https://api.typesafe.ai',
  custom: '',
}

const CUSTOM_PRESETS: { id: string; label: string; baseURL: string; model: string }[] = [
  { id: 'openrouter', label: 'OpenRouter', baseURL: 'https://openrouter.ai/api', model: 'typesafe/jev-1.13' },
  { id: 'litellm', label: 'LiteLLM', baseURL: 'http://localhost:4000/typesafe', model: '' },
  { id: 'kev', label: 'Kev', baseURL: 'http://localhost:8009', model: '' },
]

function FieldErrors({ errors }: { errors: string[] }) {
  if (errors.length === 0) return null
  return (
    <div role="alert" className="text-xs" style={{ color: 'var(--danger)' }}>
      {errors.map((m, i) => (
        <div key={i}>{m}</div>
      ))}
    </div>
  )
}

function Check({ ok, label }: { ok: boolean; label: string }) {
  return (
    <li className="flex items-center gap-2 text-sm">
      {ok ? (
        <CircleCheck size={14} style={{ color: 'var(--success)' }} aria-label="ok" />
      ) : (
        <CircleAlert size={14} style={{ color: 'var(--danger)' }} aria-label="failed" />
      )}
      <span>{label}</span>
    </li>
  )
}

/** The shared decision model: provider, credentials, model and call timeout. */
export default function DecisionModelSettings() {
  const { t } = useTranslation()
  const s = useDecisionModelStore()
  const { draft, original, info, fieldErrors } = s
  const [headerKey, setHeaderKey] = useState('')
  const [headerValue, setHeaderValue] = useState('')
  // Text with variables: interpolate() makes it independent of i18n being ready.
  const tt = (key: string, def: string, vars: Record<string, string | number> = {}) =>
    interpolate(t(key, { defaultValue: def, ...vars }), vars)
  const k = (name: string) => `settings.decisionModel.${name}`

  useUnsavedChangesGuard({
    id: 'decision-model',
    dirty: s.dirty,
    save: async () => s.save(),
    discard: s.reset,
  })

  useEffect(() => {
    void s.fetchConfig()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const errs = (field: string) =>
    fieldErrors.filter((e) => e.field === `decisionModel.${field}` || e.field === field).map((e) => e.message)

  const router = draft.router
  // The server's effective URL only applies to the provider it was computed for.
  const defaultURL =
    router.provider === original.router.provider && info.effectiveBaseURL
      ? info.effectiveBaseURL
      : PROVIDER_DEFAULT_URL[router.provider]
  const effectiveURL = router.baseURL || defaultURL
  const remote = isHostedDecisionProvider(router.provider, effectiveURL)

  const providerOptions: { value: DecisionProviderKind; label: string }[] = [
    { value: 'ollama', label: t(k('providers.ollama'), 'Ollama (local)') },
    { value: 'typesafe', label: t(k('providers.typesafe'), 'TypeSafe Jev') },
    { value: 'custom', label: t(k('providers.custom'), 'Custom Jev-compatible gateway') },
  ]

  const applyPreset = (id: string) => {
    const p = CUSTOM_PRESETS.find((x) => x.id === id)
    if (!p) return
    s.updateRouter({ baseURL: p.baseURL, ...(p.model ? { model: p.model } : {}) })
  }

  const rm = s.routerModels
  const dm = rm?.models ?? []
  const report = s.testResult?.report

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">{t(k('title'), 'Decision model')}</h2>
        <p className="settings-page-description">
          {t(
            k('description'),
            'A small model that answers quick decision questions for Pando: Auto mode routing, persona auto-select and the context filter. Configure it once here; every feature reuses it.',
          )}
        </p>
      </header>

      {s.error && fieldErrors.length === 0 && (
        <div className="settings-banner settings-banner--danger" role="alert">{s.error}</div>
      )}
      {s.warnings.length > 0 && (
        <div className="settings-banner settings-banner--warning" role="status">
          <TriangleAlert size={14} />
          <div>{s.warnings.map((w, i) => <div key={i}>{w}</div>)}</div>
        </div>
      )}

      <SettingsSection
        title={t(k('sections.provider'), 'Decision provider')}
        description={t(k('sections.providerDescription'), 'The provider that answers the decision questions.')}
      >
        <SettingsRow label={t(k('provider'), 'Provider')} htmlFor="dm-provider">
          <Select
            id="dm-provider"
            value={router.provider}
            onChange={(e) => s.updateRouter({ provider: e.target.value as DecisionProviderKind, model: '' })}
            options={providerOptions}
          />
        </SettingsRow>

        {router.provider === 'custom' && (
          <SettingsRow
            label={t(k('preset'), 'Preset')}
            htmlFor="dm-preset"
            description={t(k('presetHint'), 'Fills the base URL (and model when known).')}
          >
            <Select
              id="dm-preset"
              value=""
              onChange={(e) => applyPreset(e.target.value)}
              options={[
                { value: '', label: t(k('presetChoose'), 'Choose a preset…') },
                ...CUSTOM_PRESETS.map((p) => ({ value: p.id, label: p.label })),
              ]}
            />
          </SettingsRow>
        )}

        <SettingsRow
          label={t(k('baseUrl'), 'Base URL')}
          htmlFor="dm-baseurl"
          stacked
          description={t(k('baseUrlHint'), 'Leave empty to use the provider default.')}
        >
          <Input
            id="dm-baseurl"
            value={router.baseURL}
            placeholder={defaultURL}
            invalid={errs('router.baseURL').length > 0}
            onChange={(e) => s.updateRouter({ baseURL: e.target.value })}
          />
          <FieldErrors errors={errs('router.baseURL')} />
        </SettingsRow>

        {router.provider !== 'ollama' && (
          <SettingsRow
            label={t(k('apiKey'), 'API key')}
            htmlFor="dm-apikey"
            stacked
            description={
              info.apiKeySet && !s.clearApiKey ? (
                <>
                  {t(k('apiKeyStored'), 'A key is stored:')}{' '}
                  <code data-testid="dm-key-masked">{info.apiKeyMasked}</code>{' '}
                  {t(k('apiKeyReplace'), 'Type a new key to replace it.')}
                </>
              ) : (
                t(k('apiKeyNone'), 'No key stored.')
              )
            }
          >
            <div className="flex gap-2">
              <Input
                id="dm-apikey"
                type="password"
                autoComplete="off"
                value={router.apiKey}
                placeholder={info.apiKeySet && !s.clearApiKey ? info.apiKeyMasked : t(k('apiKeyPlaceholder'), 'API key')}
                onChange={(e) => s.updateRouter({ apiKey: e.target.value })}
              />
              {info.apiKeySet && (
                <Button variant="secondary" onClick={() => s.setClearApiKey(!s.clearApiKey)}>
                  {s.clearApiKey ? t(k('keepKey'), 'Keep key') : t(k('clearKey'), 'Clear key')}
                </Button>
              )}
            </div>
            <FieldErrors errors={errs('router.apiKey')} />
          </SettingsRow>
        )}

        {router.provider === 'custom' && (
          <SettingsRow
            label={t(k('headers'), 'Extra headers')}
            stacked
            description={t(k('headersHint'), 'Optional headers sent with every request to the gateway.')}
          >
            <div className="flex flex-col gap-2">
              {Object.entries(router.headers).map(([name, value]) => (
                <div key={name} className="flex gap-2">
                  <Input aria-label={tt(k('headerNameOf'), 'Header {{name}} name', { name })} value={name} readOnly />
                  <Input
                    aria-label={tt(k('headerValueOf'), 'Header {{name}} value', { name })}
                    value={value}
                    onChange={(e) => s.updateRouter({ headers: { ...router.headers, [name]: e.target.value } })}
                  />
                  <Button
                    variant="secondary"
                    aria-label={tt(k('removeHeader'), 'Remove header {{name}}', { name })}
                    onClick={() => {
                      const h = { ...router.headers }
                      delete h[name]
                      s.updateRouter({ headers: h })
                    }}
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              ))}
              <div className="flex gap-2">
                <Input
                  aria-label={t(k('newHeaderName'), 'New header name')}
                  placeholder={t(k('headerName'), 'Header name')}
                  value={headerKey}
                  onChange={(e) => setHeaderKey(e.target.value)}
                />
                <Input
                  aria-label={t(k('newHeaderValue'), 'New header value')}
                  placeholder={t(k('headerValue'), 'Value')}
                  value={headerValue}
                  onChange={(e) => setHeaderValue(e.target.value)}
                />
                <Button
                  variant="secondary"
                  disabled={!headerKey.trim()}
                  onClick={() => {
                    s.updateRouter({ headers: { ...router.headers, [headerKey.trim()]: headerValue } })
                    setHeaderKey('')
                    setHeaderValue('')
                  }}
                >
                  <Plus size={14} /> {t(k('addHeader'), 'Add')}
                </Button>
              </div>
            </div>
          </SettingsRow>
        )}

        {router.provider === 'ollama' && (
          <SettingsRow
            label={t(k('keepAlive'), 'Keep alive')}
            htmlFor="dm-keepalive"
            description={t(k('keepAliveHint'), 'How long Ollama keeps the decision model loaded (e.g. 30m).')}
          >
            <Input id="dm-keepalive" value={router.keepAlive} placeholder="30m" onChange={(e) => s.updateRouter({ keepAlive: e.target.value })} />
          </SettingsRow>
        )}

        <SettingsRow label={t(k('model'), 'Decision model')} htmlFor="dm-model" stacked>
          <div className="flex gap-2 items-center">
            {rm && rm.status !== 'unsupported' ? (
              <Select
                id="dm-model"
                value={router.model}
                invalid={errs('router.model').length > 0}
                onChange={(e) => s.updateRouter({ model: e.target.value })}
                options={[
                  { value: '', label: t(k('modelSelect'), '— select a model —') },
                  ...(router.model && !dm.some((m) => m.id === router.model) ? [{ value: router.model, label: router.model }] : []),
                  ...dm.map((m) => ({ value: m.id, label: m.id })),
                ]}
              />
            ) : (
              <Input
                id="dm-model"
                value={router.model}
                placeholder={t(k('modelPlaceholder'), 'model id')}
                invalid={errs('router.model').length > 0}
                onChange={(e) => s.updateRouter({ model: e.target.value })}
              />
            )}
            <Button variant="secondary" loading={s.routerModelsLoading} onClick={() => void s.loadRouterModels()}>
              {t(k('loadModels'), 'Load models')}
            </Button>
          </div>
          <FieldErrors errors={errs('router.model')} />
          {rm?.status === 'unsupported' && (
            <p className="text-xs text-fg-muted">{t(k('unsupportedHint'), 'This provider cannot list models; type the model id manually.')}</p>
          )}
          {rm?.status === 'unfiltered' && (
            <p className="text-xs text-fg-muted">
              {t(k('unfilteredHint'), 'This provider cannot tell which models support decisions; all models are listed.')}
            </p>
          )}
          {rm?.status === 'filtered' && (
            <label className="flex items-center gap-2 text-xs">
              <Switch
                aria-label={t(k('showAll'), 'Show all models')}
                checked={s.showAllModels}
                onCheckedChange={(v) => {
                  s.setShowAllModels(v)
                  void s.loadRouterModels(v)
                }}
              />
              {t(k('showAll'), 'Show all models')}
            </label>
          )}
          {rm && dm.length === 0 && router.provider === 'ollama' && rm.status !== 'unsupported' && (
            <p className="text-xs text-fg-muted">
              {t(k('noModels'), 'No decision models found. Install one with')} <code>ollama pull tev1:0.8b</code>.
            </p>
          )}
          {router.provider === 'ollama' && (rm?.suggestions?.length ?? 0) > 0 && (
            <div className="flex flex-col gap-2 text-xs mt-2" data-testid="pull-suggestions">
              <span className="text-fg-muted">{t(k('suggested'), 'Suggested decision models:')}</span>
              <ul className="flex flex-col gap-2">
                {rm?.suggestions?.map((name) => {
                  const job = s.pulls[name]
                  const running = job?.state === 'running'
                  const pct = job && job.total > 0 ? Math.round((job.completed / job.total) * 100) : null
                  return (
                    <li key={name} data-testid={`pull-row-${name}`} className="flex items-center gap-3">
                      <code className="flex-1 min-w-0 truncate">{name}</code>
                      <Button
                        variant="secondary"
                        aria-label={tt(k('pullAria'), 'Pull {{name}}', { name })}
                        loading={running}
                        disabled={running}
                        onClick={() => void s.pullModel(name)}
                      >
                        {t(k('pull'), 'Pull')}
                      </Button>
                      <span className="w-40 text-fg-muted truncate">
                        {running && (
                          <>
                            {job.status ?? t(k('pullStarting'), 'starting')}
                            {pct !== null ? ` ${pct}%` : ''}
                          </>
                        )}
                        {job?.state === 'error' && <span style={{ color: 'var(--danger)' }}>{job.error}</span>}
                      </span>
                    </li>
                  )
                })}
              </ul>
            </div>
          )}
          {rm?.hint && <p className="text-xs text-fg-muted">{rm.hint}</p>}
          {rm?.error && <p className="text-xs" style={{ color: 'var(--danger)' }}>{rm.error}</p>}
        </SettingsRow>

        {remote && (
          <div className="settings-banner settings-banner--warning" role="note" style={{ margin: 12 }}>
            <TriangleAlert size={14} />
            <div>
              {t(k('inUse.hosted'), 'This provider is remote: your prompts leave your machine to be processed by the decision model.')}
            </div>
          </div>
        )}

        <SettingsRow
          label={t(k('timeout'), 'Timeout (ms)')}
          htmlFor="dm-timeout"
          description={t(k('timeoutHint'), 'Upper bound of one decision call. 0 uses the default (1500 ms local, 3000 ms remote).')}
        >
          <Input
            id="dm-timeout"
            type="number"
            min={0}
            step={100}
            value={draft.timeoutMs}
            invalid={errs('timeoutMs').length > 0}
            onChange={(e) => s.update({ timeoutMs: Number(e.target.value) })}
          />
          <FieldErrors errors={errs('timeoutMs')} />
        </SettingsRow>

        <SettingsRow label={t(k('connection'), 'Connection')} stacked>
          <div>
            <Button variant="secondary" loading={s.testing} onClick={() => void s.testConnection()}>
              {t(k('testConnection'), 'Test connection')}
            </Button>
          </div>
          {s.testResult && (
            <div data-testid="dm-test-report" className="flex flex-col gap-2">
              {s.testResult.error && <FieldErrors errors={[s.testResult.error]} />}
              {report && (
                <ul className="flex flex-col gap-1">
                  <Check ok={report.reachable} label={t(k('report.reachable'), 'Reachable')} />
                  <Check ok={report.authorized} label={t(k('report.authorized'), 'Authorized')} />
                  {report.kind === 'ollama' && (
                    <Check
                      ok={report.versionOK}
                      label={`${t(k('report.version'), 'Version ≥ 0.35')}${
                        report.version ? ` (${tt(k('report.found'), 'found {{version}}', { version: report.version })})` : ''
                      }`}
                    />
                  )}
                  <Check ok={report.modelPresent} label={t(k('report.modelPresent'), 'Model present')} />
                  <Check ok={report.isDecisionModel} label={t(k('report.decisionCapable'), 'Decision-capable model')} />
                  <li className="text-sm text-fg-muted">{t(k('report.latency'), 'Latency')}: {report.latencyMs} ms</li>
                </ul>
              )}
              {(s.testResult.problems ?? []).map((p, i) => (
                <div key={i} className="text-sm" style={{ color: 'var(--danger)' }}>{p}</div>
              ))}
              {s.testResult.ok && <Badge tone="success">{t(k('health.healthy'), 'Healthy')}</Badge>}
            </div>
          )}
        </SettingsRow>
      </SettingsSection>

      <div className="settings-actions">
        <Button variant="primary" onClick={() => void s.save()} disabled={!s.dirty || s.saving} loading={s.saving}>
          {s.saving ? t('common.saving', 'Saving…') : t('common.save', 'Save')}
        </Button>
        <Button variant="secondary" onClick={s.reset} disabled={!s.dirty}>{t('common.reset', 'Reset')}</Button>
      </div>
    </div>
  )
}
