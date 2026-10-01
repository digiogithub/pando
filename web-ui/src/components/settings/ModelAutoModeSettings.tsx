import { useEffect, useState } from 'react'
import { AUTO_MODEL_ID, useModelAutoModeStore, type ModelAutoRoute } from '@pando/client/stores/modelAutoModeStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import DecisionModelInUse from './DecisionModelInUse'
import ModelCombobox from '@/components/shared/ModelCombobox'
import { useDialogs } from '@/components/shared/useDialogs'
import { Badge, Button, Input, SettingsRow, SettingsSection, Switch, Textarea } from '@/components/ui'
import { ArrowDown, ArrowUp, Play, Plus, Trash2, X, TriangleAlert } from '@/components/ui/icons'

const MAX_ROUTES = 25
const MAX_DESCRIPTION = 500

const STARTER_ROUTES: Omit<ModelAutoRoute, 'model' | 'fallbacks' | 'disabled'>[] = [
  { id: 'quick_question', description: 'A short factual or conceptual question that needs no code changes or tool use.' },
  { id: 'implementation', description: 'Writing, modifying or refactoring code: implementing a feature, fixing a bug, editing files.' },
  { id: 'planning', description: 'Designing an approach, architecture or multi-step plan before any code is written.' },
  { id: 'review', description: 'Reviewing, auditing or explaining existing code, diffs or pull requests.' },
]

const MAX_FALLBACKS = 2
// The synthetic "auto" entry must never be routed to.
const HIDDEN_MODELS = [AUTO_MODEL_ID]

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
  const { draft, fieldErrors } = s
  const { confirm, dialogs } = useDialogs()
  const [prompt, setPrompt] = useState('')
  const [history, setHistory] = useState('')

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

      <SettingsSection
        title="Decision model"
        description="Auto mode asks the shared decision model which route fits each prompt."
      >
        <div className="p-4 flex flex-col gap-2">
          <DecisionModelInUse
            consumerEnabled={draft.enabled}
            missingNote="Until one is configured, prompts use the coder model."
            testId="ama-decision-in-use"
          />
          <FieldErrors errors={errs('router.model')} />
        </div>
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

      <SettingsSection title="Playground" description="Try a prompt against the current (unsaved) routes and the saved decision model. Nothing is sent to a chat model.">
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
