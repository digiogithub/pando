import { useEffect, useState } from 'react'
import {
  AUTO_MODEL_ID,
  useModelAutoModeStore,
  type DecisionProviderKind,
  type ModelAutoRoute,
} from '@pando/client/stores/modelAutoModeStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ModelCombobox from '@/components/shared/ModelCombobox'
import { useDialogs } from '@/components/shared/useDialogs'
import { Badge, Button, Input, SettingsRow, SettingsSection, Select, Switch, Textarea } from '@/components/ui'
import { ArrowDown, ArrowUp, CircleAlert, CircleCheck, Play, Plus, Trash2, X, TriangleAlert } from '@/components/ui/icons'

const MAX_ROUTES = 25
const MAX_DESCRIPTION = 500

const PROVIDER_OPTIONS: { value: DecisionProviderKind; label: string }[] = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'typesafe', label: 'TypeSafe Jev' },
  { value: 'custom', label: 'Custom Jev-compatible gateway' },
]

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

const STARTER_ROUTES: Omit<ModelAutoRoute, 'model' | 'fallbacks' | 'disabled'>[] = [
  { id: 'quick_question', description: 'A short factual or conceptual question that needs no code changes or tool use.' },
  { id: 'implementation', description: 'Writing, modifying or refactoring code: implementing a feature, fixing a bug, editing files.' },
  { id: 'planning', description: 'Designing an approach, architecture or multi-step plan before any code is written.' },
  { id: 'review', description: 'Reviewing, auditing or explaining existing code, diffs or pull requests.' },
]

const MAX_FALLBACKS = 2
// The synthetic "auto" entry must never be routed to.
const HIDDEN_MODELS = [AUTO_MODEL_ID]

function isLocalURL(url: string): boolean {
  try {
    const h = new URL(url).hostname
    return h === 'localhost' || h === '127.0.0.1' || h === '::1' || h === '[::1]'
  } catch {
    return true
  }
}

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

function RouteModelFields({
  index,
  route,
  errors,
  onChange,
}: {
  index: number
  route: ModelAutoRoute
  errors: { model: string[]; fallbacks: string[] }
  onChange: (patch: Partial<ModelAutoRoute>) => void
}) {
  // Number of empty fallback fields the user opened with "Add fallback" but has not filled yet.
  const [pending, setPending] = useState(0)
  const count = Math.min(MAX_FALLBACKS, route.fallbacks.length + pending)

  const setFallback = (slot: number, value: string) => {
    const fb = [...route.fallbacks]
    fb[slot] = value
    const next = Array.from(fb, (v) => v ?? '').filter((v) => !!v)
    if (next.length > route.fallbacks.length) setPending((p) => Math.max(0, p - 1))
    onChange({ fallbacks: next })
  }

  const removeFallback = (slot: number) => {
    if (slot < route.fallbacks.length) onChange({ fallbacks: route.fallbacks.filter((_, i) => i !== slot) })
    else setPending((p) => Math.max(0, p - 1))
  }

  return (
    <div className="flex flex-col gap-3" data-testid={`ama-route-${index}-models`}>
      <div className="settings-field">
        <span className="settings-field-label">Primary model</span>
        <ModelCombobox
          value={route.model}
          onChange={(v) => onChange({ model: v })}
          ariaLabel={`Route ${index + 1} primary model`}
          invalid={errors.model.length > 0}
          excludeIds={HIDDEN_MODELS}
          placeholder="Select a model"
        />
        <FieldErrors errors={errors.model} />
      </div>
      {Array.from({ length: count }, (_, slot) => (
        <div key={slot} className="settings-field" data-testid={`ama-route-${index}-fallback-${slot + 1}`}>
          <span className="settings-field-label">Fallback {slot + 1}</span>
          <div className="flex items-center gap-2">
            <ModelCombobox
              value={route.fallbacks[slot] ?? ''}
              onChange={(v) => setFallback(slot, v)}
              ariaLabel={`Route ${index + 1} fallback ${slot + 1}`}
              invalid={errors.fallbacks.length > 0}
              excludeIds={HIDDEN_MODELS}
              placeholder="Select a model"
            />
            <Button
              variant="secondary"
              aria-label={`Remove route ${index + 1} fallback ${slot + 1}`}
              onClick={() => removeFallback(slot)}
            >
              <X size={14} />
            </Button>
          </div>
        </div>
      ))}
      {count < MAX_FALLBACKS && (
        <div>
          <Button
            variant="secondary"
            aria-label={`Add route ${index + 1} fallback`}
            onClick={() => setPending(count + 1 - route.fallbacks.length)}
          >
            <Plus size={14} /> Add fallback
          </Button>
        </div>
      )}
      <FieldErrors errors={errors.fallbacks} />
    </div>
  )
}

export default function ModelAutoModeSettings() {
  const s = useModelAutoModeStore()
  const { draft, original, info, fieldErrors } = s
  const { confirm, dialogs } = useDialogs()
  const [prompt, setPrompt] = useState('')
  const [history, setHistory] = useState('')
  const [headerKey, setHeaderKey] = useState('')
  const [headerValue, setHeaderValue] = useState('')

  useUnsavedChangesGuard({
    id: 'model-auto-mode',
    dirty: s.dirty,
    save: async () => s.save(),
    discard: s.reset,
  })

  useEffect(() => {
    void s.fetchConfig()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const errs = (field: string) =>
    fieldErrors.filter((e) => e.field === `modelAutoMode.${field}` || e.field === field).map((e) => e.message)

  const router = draft.router
  // The server's effective URL only applies to the provider it was computed for.
  const defaultURL =
    router.provider === original.router.provider && info.effectiveBaseURL
      ? info.effectiveBaseURL
      : PROVIDER_DEFAULT_URL[router.provider]
  const effectiveURL = router.baseURL || defaultURL
  const remote = !!effectiveURL && !isLocalURL(effectiveURL)
  const coderLabel = 'the coder model'

  const updateRoute = (i: number, patch: Partial<ModelAutoRoute>) =>
    s.setRoutes(draft.routes.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))

  const moveRoute = (i: number, delta: number) => {
    const j = i + delta
    if (j < 0 || j >= draft.routes.length) return
    const next = [...draft.routes]
    ;[next[i], next[j]] = [next[j], next[i]]
    s.setRoutes(next)
  }

  const deleteRoute = async (i: number) => {
    const ok = await confirm({
      title: 'Delete route',
      message: `Delete route "${draft.routes[i].id || i + 1}"?`,
      confirmLabel: 'Delete',
      dangerous: true,
    })
    if (ok) s.setRoutes(draft.routes.filter((_, idx) => idx !== i))
  }

  const addRoute = () =>
    s.setRoutes([...draft.routes, { id: '', description: '', model: '', fallbacks: [], disabled: false }])

  const addStarters = () => {
    const have = new Set(draft.routes.map((r) => r.id))
    const fresh = STARTER_ROUTES.filter((r) => !have.has(r.id)).map((r) => ({
      ...r,
      model: '',
      fallbacks: [],
      disabled: false,
    }))
    s.setRoutes([...draft.routes, ...fresh].slice(0, MAX_ROUTES))
  }

  const applyPreset = (id: string) => {
    const p = CUSTOM_PRESETS.find((x) => x.id === id)
    if (!p) return
    s.updateRouter({ baseURL: p.baseURL, ...(p.model ? { model: p.model } : {}) })
  }

  const rm = s.routerModels
  const dm = rm?.models ?? []
  const pd = s.playground?.decision

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Auto mode</h2>
        <p className="settings-page-description">
          Let a small decision model pick the best configured model for each prompt. When routing is not possible,
          the turn falls back to {coderLabel}.
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

      <SettingsSection title="General">
        <SettingsRow label="Enable Auto mode" htmlFor="ama-enabled" description="Adds “Auto” as the first entry of every model selector.">
          <Switch id="ama-enabled" checked={draft.enabled} onCheckedChange={(v) => s.update({ enabled: v })} />
        </SettingsRow>
        <SettingsRow label="Use Auto by default" htmlFor="ama-default" description="New sessions start with Auto selected.">
          <Switch id="ama-default" checked={draft.defaultAuto} onCheckedChange={(v) => s.update({ defaultAuto: v })} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Decision provider" description="The provider that answers the routing question for each prompt.">
        <SettingsRow label="Provider" htmlFor="ama-provider">
          <Select
            id="ama-provider"
            value={router.provider}
            onChange={(e) => {
              s.updateRouter({ provider: e.target.value as DecisionProviderKind, model: '' })
            }}
            options={PROVIDER_OPTIONS}
          />
        </SettingsRow>

        {router.provider === 'custom' && (
          <SettingsRow label="Preset" htmlFor="ama-preset" description="Fills the base URL (and model when known).">
            <Select
              id="ama-preset"
              value=""
              onChange={(e) => applyPreset(e.target.value)}
              options={[{ value: '', label: 'Choose a preset…' }, ...CUSTOM_PRESETS.map((p) => ({ value: p.id, label: p.label }))]}
            />
          </SettingsRow>
        )}

        <SettingsRow label="Base URL" htmlFor="ama-baseurl" stacked description="Leave empty to use the provider default.">
          <Input
            id="ama-baseurl"
            value={router.baseURL}
            placeholder={defaultURL}
            invalid={errs('router.baseURL').length > 0}
            onChange={(e) => s.updateRouter({ baseURL: e.target.value })}
          />
          <FieldErrors errors={errs('router.baseURL')} />
        </SettingsRow>

        {router.provider !== 'ollama' && (
          <SettingsRow
            label="API key"
            htmlFor="ama-apikey"
            stacked
            description={
              info.apiKeySet && !s.clearApiKey
                ? <>A key is stored: <code data-testid="ama-key-masked">{info.apiKeyMasked}</code>. Type a new key to replace it.</>
                : 'No key stored.'
            }
          >
            <div className="flex gap-2">
              <Input
                id="ama-apikey"
                type="password"
                autoComplete="off"
                value={router.apiKey}
                placeholder={info.apiKeySet && !s.clearApiKey ? info.apiKeyMasked : 'API key'}
                onChange={(e) => s.updateRouter({ apiKey: e.target.value })}
              />
              {info.apiKeySet && (
                <Button
                  variant="secondary"
                  onClick={() => s.setClearApiKey(!s.clearApiKey)}
                >
                  {s.clearApiKey ? 'Keep key' : 'Clear key'}
                </Button>
              )}
            </div>
            <FieldErrors errors={errs('router.apiKey')} />
          </SettingsRow>
        )}

        {router.provider === 'custom' && (
          <SettingsRow label="Extra headers" stacked description="Optional headers sent with every request to the gateway.">
            <div className="flex flex-col gap-2">
              {Object.entries(router.headers).map(([k, v]) => (
                <div key={k} className="flex gap-2">
                  <Input aria-label={`Header ${k} name`} value={k} readOnly />
                  <Input
                    aria-label={`Header ${k} value`}
                    value={v}
                    onChange={(e) => s.updateRouter({ headers: { ...router.headers, [k]: e.target.value } })}
                  />
                  <Button
                    variant="secondary"
                    aria-label={`Remove header ${k}`}
                    onClick={() => {
                      const h = { ...router.headers }
                      delete h[k]
                      s.updateRouter({ headers: h })
                    }}
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              ))}
              <div className="flex gap-2">
                <Input aria-label="New header name" placeholder="Header name" value={headerKey} onChange={(e) => setHeaderKey(e.target.value)} />
                <Input aria-label="New header value" placeholder="Value" value={headerValue} onChange={(e) => setHeaderValue(e.target.value)} />
                <Button
                  variant="secondary"
                  disabled={!headerKey.trim()}
                  onClick={() => {
                    s.updateRouter({ headers: { ...router.headers, [headerKey.trim()]: headerValue } })
                    setHeaderKey('')
                    setHeaderValue('')
                  }}
                >
                  <Plus size={14} /> Add
                </Button>
              </div>
            </div>
          </SettingsRow>
        )}

        {router.provider === 'ollama' && (
          <SettingsRow label="Keep alive" htmlFor="ama-keepalive" description="How long Ollama keeps the decision model loaded (e.g. 30m).">
            <Input id="ama-keepalive" value={router.keepAlive} placeholder="30m" onChange={(e) => s.updateRouter({ keepAlive: e.target.value })} />
          </SettingsRow>
        )}

        <SettingsRow label="Decision model" htmlFor="ama-model" stacked>
          <div className="flex gap-2 items-center">
            {rm && rm.status !== 'unsupported' ? (
              <Select
                id="ama-model"
                value={router.model}
                invalid={errs('router.model').length > 0}
                onChange={(e) => s.updateRouter({ model: e.target.value })}
                options={[
                  { value: '', label: '— select a model —' },
                  ...(router.model && !dm.some((m) => m.id === router.model) ? [{ value: router.model, label: router.model }] : []),
                  ...dm.map((m) => ({ value: m.id, label: m.id })),
                ]}
              />
            ) : (
              <Input
                id="ama-model"
                value={router.model}
                placeholder="model id"
                invalid={errs('router.model').length > 0}
                onChange={(e) => s.updateRouter({ model: e.target.value })}
              />
            )}
            <Button variant="secondary" loading={s.routerModelsLoading} onClick={() => void s.loadRouterModels()}>
              Load models
            </Button>
          </div>
          <FieldErrors errors={errs('router.model')} />
          {rm?.status === 'unsupported' && (
            <p className="text-xs text-fg-muted">This provider cannot list models; type the model id manually.</p>
          )}
          {rm?.status === 'unfiltered' && (
            <p className="text-xs text-fg-muted">
              This provider cannot tell which models support decisions; all models are listed.
            </p>
          )}
          {rm?.status === 'filtered' && (
            <label className="flex items-center gap-2 text-xs">
              <Switch
                aria-label="Show all models"
                checked={s.showAllModels}
                onCheckedChange={(v) => {
                  s.setShowAllModels(v)
                  void s.loadRouterModels(v)
                }}
              />
              Show all models
            </label>
          )}
          {rm && dm.length === 0 && router.provider === 'ollama' && rm.status !== 'unsupported' && (
            <p className="text-xs text-fg-muted">
              No decision models found. Install one with <code>ollama pull tev1:0.8b</code>.
            </p>
          )}
          {router.provider === 'ollama' && (rm?.suggestions?.length ?? 0) > 0 && (
            <div className="flex flex-col gap-2 text-xs mt-2" data-testid="pull-suggestions">
              <span className="text-fg-muted">Suggested decision models:</span>
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
                        aria-label={`Pull ${name}`}
                        loading={running}
                        disabled={running}
                        onClick={() => void s.pullModel(name)}
                      >
                        Pull
                      </Button>
                      <span className="w-40 text-fg-muted truncate">
                        {running && (
                          <>
                            {job.status ?? 'starting'}
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
            <div>This provider is remote: your prompts leave your machine to be classified.</div>
          </div>
        )}

        <SettingsRow label="Connection" stacked>
          <div>
            <Button variant="secondary" loading={s.testing} onClick={() => void s.testConnection()}>
              Test connection
            </Button>
          </div>
          {s.testResult && (
            <div data-testid="ama-test-report" className="flex flex-col gap-2">
              {s.testResult.error && <FieldErrors errors={[s.testResult.error]} />}
              {s.testResult.report && (
                <ul className="flex flex-col gap-1">
                  <Check ok={s.testResult.report.reachable} label="Reachable" />
                  <Check ok={s.testResult.report.authorized} label="Authorized" />
                  {s.testResult.report.kind === 'ollama' && (
                    <Check ok={s.testResult.report.versionOK} label={`Version ≥ 0.35${s.testResult.report.version ? ` (found ${s.testResult.report.version})` : ''}`} />
                  )}
                  <Check ok={s.testResult.report.modelPresent} label="Model present" />
                  <Check ok={s.testResult.report.isDecisionModel} label="Decision-capable model" />
                  <li className="text-sm text-fg-muted">Latency: {s.testResult.report.latencyMs} ms</li>
                </ul>
              )}
              {(s.testResult.problems ?? []).map((p, i) => (
                <div key={i} className="text-sm" style={{ color: 'var(--danger)' }}>{p}</div>
              ))}
              {s.testResult.ok && <Badge tone="success">Healthy</Badge>}
            </div>
          )}
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Routing tuning">
        <SettingsRow label="Threshold" htmlFor="ama-threshold" description="Minimum probability of the chosen route (0.05–1, default 0.60).">
          <div className="flex items-center gap-2">
            <input
              aria-label="Threshold slider"
              type="range"
              min={0.05}
              max={1}
              step={0.05}
              value={draft.threshold}
              onChange={(e) => s.update({ threshold: Number(e.target.value) })}
            />
            <Input
              id="ama-threshold"
              type="number"
              min={0.05}
              max={1}
              step={0.05}
              value={draft.threshold}
              invalid={errs('threshold').length > 0}
              onChange={(e) => s.update({ threshold: Number(e.target.value) })}
            />
          </div>
          <FieldErrors errors={errs('threshold')} />
        </SettingsRow>
        <SettingsRow label="Minimum confidence" htmlFor="ama-minconf" description="0 disables the extra confidence check.">
          <Input id="ama-minconf" type="number" min={0} max={1} step={0.05} value={draft.minConfidence} onChange={(e) => s.update({ minConfidence: Number(e.target.value) })} />
          <FieldErrors errors={errs('minConfidence')} />
        </SettingsRow>
        <SettingsRow label="Timeout (ms)" htmlFor="ama-timeout" description="0 uses the default.">
          <Input id="ama-timeout" type="number" min={0} step={100} value={draft.timeoutMs} onChange={(e) => s.update({ timeoutMs: Number(e.target.value) })} />
          <FieldErrors errors={errs('timeoutMs')} />
        </SettingsRow>
        <SettingsRow label="History prompts" htmlFor="ama-history" description="Previous user prompts included as context for the decision.">
          <Input id="ama-history" type="number" min={0} step={1} value={draft.historyPrompts} onChange={(e) => s.update({ historyPrompts: Number(e.target.value) })} />
          <FieldErrors errors={errs('historyPrompts')} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection
        title={`Routes (${draft.routes.length}/${MAX_ROUTES})`}
        description="Each route describes a kind of prompt and the model that should handle it. Order matters only for ties."
      >
        <FieldErrors errors={errs('routes')} />
        {draft.routes.length === 0 && (
          <div className="p-4 text-sm text-fg-muted">No routes yet. Add your own or start from the suggested ones.</div>
        )}
        {draft.routes.map((r, i) => (
          <div key={i} className="p-4 flex flex-col gap-2" data-testid={`ama-route-${i}`}>
            <div className="flex gap-2 items-center">
              <Input
                aria-label={`Route ${i + 1} id`}
                value={r.id}
                placeholder="route_id"
                invalid={errs(`routes[${i}].id`).length > 0}
                onChange={(e) => updateRoute(i, { id: e.target.value })}
              />
              <label className="flex items-center gap-1 text-xs whitespace-nowrap">
                <Switch aria-label={`Route ${i + 1} disabled`} checked={r.disabled} onCheckedChange={(v) => updateRoute(i, { disabled: v })} />
                Disabled
              </label>
              <Button variant="secondary" aria-label={`Move route ${i + 1} up`} disabled={i === 0} onClick={() => moveRoute(i, -1)}><ArrowUp size={14} /></Button>
              <Button variant="secondary" aria-label={`Move route ${i + 1} down`} disabled={i === draft.routes.length - 1} onClick={() => moveRoute(i, 1)}><ArrowDown size={14} /></Button>
              <Button variant="secondary" aria-label={`Delete route ${i + 1}`} onClick={() => void deleteRoute(i)}><Trash2 size={14} /></Button>
            </div>
            <FieldErrors errors={errs(`routes[${i}].id`)} />
            <Textarea
              aria-label={`Route ${i + 1} description`}
              value={r.description}
              rows={2}
              maxLength={MAX_DESCRIPTION * 2}
              invalid={errs(`routes[${i}].description`).length > 0}
              placeholder="Describe when this route should be used"
              onChange={(e) => updateRoute(i, { description: e.target.value })}
            />
            <div className="flex justify-between text-xs text-fg-muted">
              <FieldErrors errors={errs(`routes[${i}].description`)} />
              <span style={r.description.length > MAX_DESCRIPTION ? { color: 'var(--danger)' } : undefined}>
                {r.description.length}/{MAX_DESCRIPTION}
              </span>
            </div>
            <RouteModelFields
              index={i}
              route={r}
              errors={{ model: errs(`routes[${i}].model`), fallbacks: errs(`routes[${i}].fallbacks`) }}
              onChange={(patch) => updateRoute(i, patch)}
            />
          </div>
        ))}
        <div className="p-4 flex gap-2">
          <Button variant="secondary" disabled={draft.routes.length >= MAX_ROUTES} onClick={addRoute}><Plus size={14} /> Add route</Button>
          <Button variant="secondary" disabled={draft.routes.length >= MAX_ROUTES} onClick={addStarters}>Add starter routes</Button>
        </div>
      </SettingsSection>

      <SettingsSection title="Playground" description="Try a prompt against the current (unsaved) configuration. Nothing is sent to a chat model.">
        <div className="p-4 flex flex-col gap-2">
          <Textarea aria-label="Playground prompt" rows={3} value={prompt} placeholder="Type a prompt to route…" onChange={(e) => setPrompt(e.target.value)} />
          <Textarea aria-label="Playground history" rows={2} value={history} placeholder="Optional previous prompts, one per line" onChange={(e) => setHistory(e.target.value)} />
          <div>
            <Button
              variant="secondary"
              loading={s.playgroundRunning}
              disabled={!prompt.trim()}
              onClick={() => void s.runPlayground(prompt, history.split('\n').map((l) => l.trim()).filter(Boolean))}
            >
              <Play size={14} /> Route
            </Button>
          </div>
          {s.playgroundError && <FieldErrors errors={[s.playgroundError]} />}
          {pd && (
            <div data-testid="ama-playground-result" className="flex flex-col gap-2 text-sm">
              <div>
                Route: <strong>{pd.matched ? pd.routeId : 'no match'}</strong>{' '}
                <Badge tone={pd.matched ? 'success' : 'warning'}>{pd.reason}</Badge>
              </div>
              {pd.error && <div style={{ color: 'var(--danger)' }}>{pd.error}</div>}
              {pd.probabilities && Object.entries(pd.probabilities).sort((a, b) => b[1] - a[1]).map(([k, p]) => (
                <div key={k} className="flex items-center gap-2">
                  <span style={{ width: 140 }} className="truncate">{k}</span>
                  <div style={{ flex: 1, height: 8, background: 'var(--bg-input)', borderRadius: 4 }}>
                    <div role="progressbar" aria-label={`${k} probability`} aria-valuenow={Math.round(p * 100)} style={{ width: `${Math.round(p * 100)}%`, height: 8, background: 'var(--accent)', borderRadius: 4 }} />
                  </div>
                  <span style={{ width: 44 }} className="text-xs">{(p * 100).toFixed(0)}%</span>
                </div>
              ))}
              <div className="text-xs text-fg-muted">
                Candidates: {(pd.candidates ?? []).join(', ') || '—'} · Usable: {(s.playground?.usableCandidates ?? []).join(', ') || '—'}
              </div>
              {s.playground?.skipped && Object.keys(s.playground.skipped).length > 0 && (
                <div className="text-xs text-fg-muted">
                  Skipped: {Object.entries(s.playground.skipped).map(([m, why]) => `${m} (${why})`).join(', ')}
                </div>
              )}
              <div className="text-xs text-fg-muted">
                {pd.latencyMs} ms{pd.costUsd != null ? ` · $${pd.costUsd.toFixed(6)}` : ''}
                {pd.routerModel ? ` · ${pd.routerProvider ?? ''} ${pd.routerModel}` : ''}
              </div>
            </div>
          )}
        </div>
      </SettingsSection>

      <div className="settings-actions">
        <Button variant="primary" onClick={() => void s.save()} disabled={!s.dirty || s.saving} loading={s.saving}>
          {s.saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={s.reset} disabled={!s.dirty}>Reset</Button>
      </div>
      {dialogs}
    </div>
  )
}
