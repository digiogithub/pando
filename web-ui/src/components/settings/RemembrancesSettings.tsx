import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import MaskedInput from '@/components/shared/MaskedInput'
import DirBrowserDialog from '@/components/shared/DirBrowserDialog'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'
import type { CodeProjectInfo } from '@pando/client/types'
import { Badge, Button, Input, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'
import { SectionHero } from '@/components/brand'

const EMBEDDING_PROVIDERS = ['', 'openai', 'openai-compatible', 'anthropic', 'ollama']

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="settings-field">
      <label className="settings-field-label">{label}</label>
      {children}
    </div>
  )
}

/** A Switch + label/description pair for use inside a padded block that is
 * not a `SettingsRow` list (e.g. a conditionally-revealed sub-section). */
function ToggleField({
  id,
  label,
  description,
  checked,
  onCheckedChange,
}: {
  id: string
  label: string
  description?: string
  checked: boolean
  onCheckedChange: (v: boolean) => void
}) {
  return (
    <div className="flex items-center gap-3">
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} />
      <label htmlFor={id} className="cursor-pointer">
        <div className="text-sm font-medium text-fg">{label}</div>
        {description && <div className="text-xs text-muted">{description}</div>}
      </label>
    </div>
  )
}

interface EmbeddingModelInfo {
  id: string
  name?: string
  size?: string
}

interface EmbeddingModelsResponse {
  provider: string
  models: EmbeddingModelInfo[]
  source: 'api' | 'heuristic' | 'static'
  error?: string
}

const SOURCE_HINTS: Record<string, string> = {
  api: 'reported as embedding models by the provider',
  heuristic: 'provider does not flag embedders — filtered by name',
  static: 'known catalog (provider publishes no model list)',
}

/**
 * EmbeddingModelPicker keeps the free-text field (some deployments serve models
 * that no listing endpoint knows about) and adds the list the provider actually
 * offers, so the model no longer has to be typed from memory.
 */
function EmbeddingModelPicker({
  label,
  listId,
  provider,
  baseUrl,
  apiKey,
  value,
  placeholder,
  onChange,
}: {
  label: string
  listId: string
  provider: string
  baseUrl: string
  apiKey: string
  value: string
  placeholder?: string
  onChange: (value: string) => void
}) {
  const [models, setModels] = useState<EmbeddingModelInfo[]>([])
  const [source, setSource] = useState<string>('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!provider) {
      setModels([])
      setSource('')
      setError(null)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams({ provider })
      if (baseUrl) params.set('base_url', baseUrl)
      if (apiKey) params.set('api_key', apiKey)
      const data = await api.get<EmbeddingModelsResponse>(
        `/api/v1/remembrances/embedding-models?${params.toString()}`,
      )
      setModels(data.models ?? [])
      setSource(data.source ?? '')
      setError(data.error ?? null)
    } catch (e) {
      setModels([])
      setError(e instanceof Error ? e.message : 'Failed to list models')
    } finally {
      setLoading(false)
    }
  }, [provider, baseUrl, apiKey])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <div className="flex flex-col gap-1.5">
      <label className="settings-field-label">{label}</label>
      <div className="flex gap-2 items-stretch">
        <Select
          className="flex-1"
          value={models.some((m) => m.id === value) ? value : ''}
          onChange={(e) => e.target.value && onChange(e.target.value)}
          disabled={!provider || loading || models.length === 0}
        >
          <option value="">
            {loading
              ? 'Loading models…'
              : models.length === 0
                ? '— no models available —'
                : '— select a model —'}
          </option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.size ? `${m.name || m.id} (${m.size})` : m.name || m.id}
            </option>
          ))}
        </Select>
        <Button type="button" variant="secondary" onClick={() => void load()} disabled={!provider || loading}>
          {loading ? '…' : 'Refresh'}
        </Button>
      </div>
      <Input value={value} list={listId} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} />
      <datalist id={listId}>
        {models.map((m) => (
          <option key={m.id} value={m.id} />
        ))}
      </datalist>
      <span className="text-xs text-faint">
        {error
          ? `Could not list models: ${error}. Type the model name manually.`
          : source
            ? `${models.length} model(s) — ${SOURCE_HINTS[source] ?? source}. You can also type any name.`
            : 'Select an embedding provider to list its models.'}
      </span>
    </div>
  )
}

interface EmbeddingTestResult {
  ok: boolean
  error?: string
  latency_ms: number
  dimension?: number
  provider: string
  model: string
  base_url?: string
}

interface TestConnectionResponse {
  document?: EmbeddingTestResult
  code?: EmbeddingTestResult
}

function TestConnectionRow({
  testing,
  disabled,
  result,
  onTest,
}: {
  testing: boolean
  disabled?: boolean
  result: EmbeddingTestResult | null
  onTest: () => void
}) {
  return (
    <div className="flex items-center gap-3">
      <Button variant="secondary" size="sm" onClick={onTest} disabled={testing || disabled} loading={testing}>
        {testing ? 'Testing…' : 'Test connection'}
      </Button>
      {result && (
        <Badge tone={result.ok ? 'success' : 'danger'} dot>
          {result.ok ? `OK — ${result.dimension}d, ${result.latency_ms}ms` : result.error}
        </Badge>
      )}
    </div>
  )
}

export default function RemembrancesSettings() {
  const { t } = useTranslation()
  const { config, dirty, loading, saving, error, fetchServices, updateRemembrances, saveServices, resetServices } =
    useServicesSettingsStore()
  useUnsavedChangesGuard({
    id: 'remembrances',
    dirty,
    save: async () => {
      await saveServices()
      return !useServicesSettingsStore.getState().error
    },
    discard: resetServices,
  })

  const [projects, setProjects] = useState<CodeProjectInfo[]>([])
  const [indexing, setIndexing] = useState(false)
  const [testingDoc, setTestingDoc] = useState(false)
  const [testingCode, setTestingCode] = useState(false)
  const [docTestResult, setDocTestResult] = useState<EmbeddingTestResult | null>(null)
  const [codeTestResult, setCodeTestResult] = useState<EmbeddingTestResult | null>(null)
  const [browsingKBPath, setBrowsingKBPath] = useState(false)
  // The KB path is normally relative to the project, so the directory picker
  // starts at the instance working directory instead of $HOME.
  const workspace = useProjectStore((s) => s.workspace)
  const fetchWorkspace = useProjectStore((s) => s.fetchWorkspace)

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

  useEffect(() => {
    if (!workspace) void fetchWorkspace()
  }, [workspace, fetchWorkspace])

  useEffect(() => {
    if (config.remembrances.enabled) {
      api
        .get<CodeProjectInfo[]>('/api/v1/remembrances/projects')
        .then(setProjects)
        .catch(() => setProjects([]))
    }
  }, [config.remembrances.enabled])

  if (loading) {
    return <div className="settings-loading">Loading…</div>
  }

  const rem = config.remembrances

  async function handleTestDocEmbedding() {
    setTestingDoc(true)
    setDocTestResult(null)
    try {
      const result = await api.post<TestConnectionResponse>('/api/v1/remembrances/test-connection', { type: 'document' })
      setDocTestResult(result.document ?? null)
    } catch (e) {
      setDocTestResult({ ok: false, error: e instanceof Error ? e.message : 'Request failed', latency_ms: 0, provider: '', model: '' })
    } finally {
      setTestingDoc(false)
    }
  }

  async function handleTestCodeEmbedding() {
    setTestingCode(true)
    setCodeTestResult(null)
    try {
      const result = await api.post<TestConnectionResponse>('/api/v1/remembrances/test-connection', { type: 'code' })
      setCodeTestResult(result.code ?? null)
    } catch (e) {
      setCodeTestResult({ ok: false, error: e instanceof Error ? e.message : 'Request failed', latency_ms: 0, provider: '', model: '' })
    } finally {
      setTestingCode(false)
    }
  }

  async function handleReindexAll() {
    try {
      const result = await api.post<{ started?: { project_id: string }[]; failed?: { project_id: string }[] }>(
        '/api/v1/remembrances/reindex',
        {},
      )
      const started = result.started?.length ?? 0
      const failed = result.failed?.length ?? 0
      useToastStore.getState().addToast(
        started === 0
          ? 'No code projects registered — nothing to re-index'
          : `Re-index started for ${started} project${started === 1 ? '' : 's'}` +
              (failed > 0 ? ` (${failed} failed)` : ''),
        started === 0 ? 'info' : 'success',
      )
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Re-index failed',
        'error',
      )
    }
  }

  async function handleIndexWorkdir() {
    setIndexing(true)
    try {
      const result = await api.post<{ project_id: string; job_id: string }>('/api/v1/remembrances/projects/index', {})
      useToastStore.getState().addToast(
        `Indexing started — project: ${result.project_id}`,
        'success',
      )
      // Set the newly created project as the selected one
      updateRemembrances('context_enrichment_code_project', result.project_id)
      // Reload project list
      const updated = await api.get<CodeProjectInfo[]>('/api/v1/remembrances/projects')
      setProjects(updated)
    } catch (e) {
      useToastStore.getState().addToast(
        e instanceof Error ? e.message : 'Indexing failed',
        'error',
      )
    } finally {
      setIndexing(false)
    }
  }

  return (
    <div>
      <SectionHero
        variant="remembrances"
        title={t('settings.remembrances.hero.title')}
        tagline={t('settings.remembrances.hero.tagline')}
      />

      <SettingsSection>
        <SettingsRow label="Enabled" description="Enable the Remembrances memory system" htmlFor="rem-enabled">
          <Switch id="rem-enabled" checked={rem.enabled} onCheckedChange={(v) => updateRemembrances('enabled', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="KB filesystem sync">
        <SettingsRow label="KB path" htmlFor="rem-kb-path">
          <div className="flex gap-2 w-full">
            <Input id="rem-kb-path" className="flex-1" value={rem.kb_path} onChange={(e) => updateRemembrances('kb_path', e.target.value)} placeholder="./.kb" />
            <Button type="button" variant="secondary" onClick={() => setBrowsingKBPath(true)}>
              Browse…
            </Button>
          </div>
        </SettingsRow>
        <SettingsRow label="Watch KB path" description="Monitor markdown changes in real time and re-index automatically" htmlFor="rem-kb-watch">
          <Switch id="rem-kb-watch" checked={rem.kb_watch} onCheckedChange={(v) => updateRemembrances('kb_watch', v)} />
        </SettingsRow>
        <SettingsRow label="Auto import on startup" description="Import markdown files from KB path when Pando starts" htmlFor="rem-kb-auto-import">
          <Switch id="rem-kb-auto-import" checked={rem.kb_auto_import} onCheckedChange={(v) => updateRemembrances('kb_auto_import', v)} />
        </SettingsRow>
        <SettingsRow
          label="Convert documents"
          description="Convert docx/pdf/xlsx and other rich documents to Markdown on the fly and index them, referencing the original file"
          htmlFor="rem-kb-convert"
        >
          <Switch id="rem-kb-convert" checked={rem.kb_convert_documents} onCheckedChange={(v) => updateRemembrances('kb_convert_documents', v)} />
        </SettingsRow>
        <SettingsRow
          label="Wiki links"
          description="Index [[wiki links]] written in KB documents as a navigable graph: backlinks, related documents and concepts still undocumented"
          htmlFor="rem-kb-wiki-links"
        >
          <Switch id="rem-kb-wiki-links" checked={rem.kb_wiki_links} onCheckedChange={(v) => updateRemembrances('kb_wiki_links', v)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Document embeddings">
        <div className="p-4 flex flex-col gap-4">
          <Field label="Embedding provider">
            <Select
              value={rem.document_embedding_provider}
              onChange={(e) => updateRemembrances('document_embedding_provider', e.target.value)}
              options={EMBEDDING_PROVIDERS.map((p) => ({ value: p, label: p || '— select provider —' }))}
            />
          </Field>

          <EmbeddingModelPicker
            label="Embedding model"
            listId="doc-embedding-models"
            provider={rem.document_embedding_provider}
            baseUrl={rem.document_embedding_base_url}
            apiKey={rem.document_embedding_api_key}
            value={rem.document_embedding_model}
            placeholder="text-embedding-3-small"
            onChange={(v) => updateRemembrances('document_embedding_model', v)}
          />

          {(rem.document_embedding_provider === 'openai-compatible' || rem.document_embedding_provider === 'ollama') && (
            <Field label="Base URL">
              <Input
                value={rem.document_embedding_base_url}
                onChange={(e) => updateRemembrances('document_embedding_base_url', e.target.value)}
                placeholder={rem.document_embedding_provider === 'ollama' ? 'http://localhost:11434' : 'https://api.example.com/v1'}
              />
            </Field>
          )}

          <MaskedInput
            label="Embedding API key"
            value={rem.document_embedding_api_key}
            onChange={(v) => updateRemembrances('document_embedding_api_key', v)}
            placeholder="sk-…"
          />

          <TestConnectionRow
            testing={testingDoc}
            disabled={!rem.document_embedding_provider || !rem.document_embedding_model}
            result={docTestResult}
            onTest={handleTestDocEmbedding}
          />
        </div>
      </SettingsSection>

      <SettingsSection title="Code embeddings">
        <SettingsRow label="Use same model as document" htmlFor="rem-use-same-model">
          <Switch id="rem-use-same-model" checked={rem.use_same_model} onCheckedChange={(v) => updateRemembrances('use_same_model', v)} />
        </SettingsRow>
        {!rem.use_same_model && (
          <div className="p-4 flex flex-col gap-4 border-t border-border">
            <Field label="Code embedding provider">
              <Select
                value={rem.code_embedding_provider}
                onChange={(e) => updateRemembrances('code_embedding_provider', e.target.value)}
                options={EMBEDDING_PROVIDERS.map((p) => ({ value: p, label: p || '— select provider —' }))}
              />
            </Field>

            <EmbeddingModelPicker
              label="Code embedding model"
              listId="code-embedding-models"
              provider={rem.code_embedding_provider}
              baseUrl={rem.code_embedding_base_url}
              apiKey={rem.code_embedding_api_key}
              value={rem.code_embedding_model}
              placeholder="nomic-embed-code"
              onChange={(v) => updateRemembrances('code_embedding_model', v)}
            />

            {(rem.code_embedding_provider === 'openai-compatible' || rem.code_embedding_provider === 'ollama') && (
              <Field label="Base URL">
                <Input
                  value={rem.code_embedding_base_url}
                  onChange={(e) => updateRemembrances('code_embedding_base_url', e.target.value)}
                  placeholder={rem.code_embedding_provider === 'ollama' ? 'http://localhost:11434' : 'https://api.example.com/v1'}
                />
              </Field>
            )}

            <MaskedInput
              label="Code embedding API key"
              value={rem.code_embedding_api_key}
              onChange={(v) => updateRemembrances('code_embedding_api_key', v)}
              placeholder="sk-…"
            />
          </div>
        )}
        <div className="p-4 border-t border-border">
          <TestConnectionRow testing={testingCode} result={codeTestResult} onTest={handleTestCodeEmbedding} />
        </div>
      </SettingsSection>

      <SettingsSection title="Chunking">
        <div className="p-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
          <Field label="Chunk size">
            <Input type="number" value={String(rem.chunk_size)} onChange={(e) => updateRemembrances('chunk_size', Number(e.target.value))} placeholder="512" />
          </Field>
          <Field label="Chunk overlap">
            <Input type="number" value={String(rem.chunk_overlap)} onChange={(e) => updateRemembrances('chunk_overlap', Number(e.target.value))} placeholder="64" />
          </Field>
          <Field label="Index workers">
            <Input type="number" value={String(rem.index_workers)} onChange={(e) => updateRemembrances('index_workers', Number(e.target.value))} placeholder="2" />
          </Field>
        </div>
      </SettingsSection>

      <SettingsSection title="Code indexing">
        <SettingsRow label="Re-index all" description="Trigger a full re-index of all registered code projects.">
          <Button variant="secondary" onClick={handleReindexAll}>
            Re-index all
          </Button>
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Context enrichment">
        <SettingsRow
          label="Enable context enrichment"
          description="Before each prompt, search KB and code index and prepend relevant context automatically"
          htmlFor="rem-ctx-enabled"
        >
          <Switch id="rem-ctx-enabled" checked={rem.context_enrichment_enabled} onCheckedChange={(v) => updateRemembrances('context_enrichment_enabled', v)} />
        </SettingsRow>

        {rem.context_enrichment_enabled && (
          <div className="p-4 flex flex-col gap-4 border-t border-border">
            <Field label="Code project">
              <div className="flex gap-2 items-center">
                <Select
                  className="flex-1"
                  value={rem.context_enrichment_code_project}
                  onChange={(e) => updateRemembrances('context_enrichment_code_project', e.target.value)}
                >
                  <option value="">— none (KB only) —</option>
                  {projects.map((p) => (
                    <option key={p.project_id} value={p.project_id}>
                      {p.name || p.project_id} ({p.root_path})
                    </option>
                  ))}
                </Select>
                <Button
                  variant="secondary"
                  onClick={handleIndexWorkdir}
                  disabled={indexing}
                  loading={indexing}
                  title="Index the current working directory as a new code project"
                >
                  {indexing ? 'Indexing…' : '+ Index workdir'}
                </Button>
              </div>
              <p className="text-xs text-muted mt-1">
                Select a previously indexed project to include code search results, or index the working directory.
              </p>
            </Field>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Field label="KB results">
                <Input type="number" value={String(rem.context_enrichment_kb_results)} onChange={(e) => updateRemembrances('context_enrichment_kb_results', Number(e.target.value))} placeholder="3" />
              </Field>
              <Field label="Code results">
                <Input type="number" value={String(rem.context_enrichment_code_results)} onChange={(e) => updateRemembrances('context_enrichment_code_results', Number(e.target.value))} placeholder="5" />
              </Field>
            </div>

            <div>
              <div className="settings-field-label mb-1">Past session events</div>
              <p className="text-xs text-muted m-0">Search saved events from previous sessions and prepend relevant ones as context.</p>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Field label="Events results">
                <Input
                  type="number"
                  value={String(rem.context_enrichment_events_results)}
                  onChange={(e) => updateRemembrances('context_enrichment_events_results', Number(e.target.value))}
                  placeholder="3"
                />
              </Field>
              <Field label="Last days">
                <Input
                  type="number"
                  value={String(rem.context_enrichment_events_last_days)}
                  onChange={(e) => updateRemembrances('context_enrichment_events_last_days', Number(e.target.value))}
                  placeholder="30"
                />
              </Field>
            </div>
            <Field label="Subject filter">
              <Input
                value={rem.context_enrichment_events_subject}
                onChange={(e) => updateRemembrances('context_enrichment_events_subject', e.target.value)}
                placeholder="e.g. pando  (leave empty for all subjects)"
              />
            </Field>

            <ToggleField
              id="rem-ctx-agent-loop"
              label="Agent loop enrichment"
              description="Run enrichment as a separate agent loop on the context-enricher model. It searches memory, KB and the code index iteratively; the main agent only receives the resulting context block."
              checked={rem.context_enrichment_agent_loop_enabled ?? false}
              onCheckedChange={(v) => updateRemembrances('context_enrichment_agent_loop_enabled', v)}
            />

            {rem.context_enrichment_agent_loop_enabled && (
              <>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <Field label="Loop timeout (s)">
                    <Input
                      type="number"
                      value={String(rem.context_enrichment_agent_loop_timeout_seconds ?? 60)}
                      onChange={(e) => updateRemembrances('context_enrichment_agent_loop_timeout_seconds', Number(e.target.value))}
                      placeholder="60"
                    />
                  </Field>
                  <Field label="Loop max chars">
                    <Input
                      type="number"
                      value={String(rem.context_enrichment_agent_loop_max_chars ?? 6000)}
                      onChange={(e) => updateRemembrances('context_enrichment_agent_loop_max_chars', Number(e.target.value))}
                      placeholder="6000"
                    />
                  </Field>
                </div>
                <ToggleField
                  id="rem-ctx-every-msg"
                  label="Run on every message"
                  description="Off (default): the loop runs once per session, on the first message. On: it runs on every user turn."
                  checked={rem.context_enrichment_agent_loop_every_message ?? false}
                  onCheckedChange={(v) => updateRemembrances('context_enrichment_agent_loop_every_message', v)}
                />
                <ToggleField
                  id="rem-ctx-announce"
                  label="Announce in chat"
                  description="Show start and end notices in the chat while the enrichment agent runs, like context compaction does"
                  checked={!(rem.context_enrichment_agent_loop_silent ?? false)}
                  onCheckedChange={(v) => updateRemembrances('context_enrichment_agent_loop_silent', !v)}
                />
                <ToggleField
                  id="rem-ctx-fallback"
                  label="Fallback to search"
                  description="Use the classic search pipeline when the loop fails, times out or finds nothing"
                  checked={!(rem.context_enrichment_agent_loop_fallback_disabled ?? false)}
                  onCheckedChange={(v) => updateRemembrances('context_enrichment_agent_loop_fallback_disabled', !v)}
                />
                <ToggleField
                  id="rem-ctx-show-loop"
                  label="Show loop in chat"
                  description="Record the loop as a child session of the chat session so its tool calls can be inspected"
                  checked={!(rem.context_enrichment_agent_loop_hidden_in_chat ?? false)}
                  onCheckedChange={(v) => updateRemembrances('context_enrichment_agent_loop_hidden_in_chat', !v)}
                />
              </>
            )}
          </div>
        )}
      </SettingsSection>

      <SettingsSection title="Memory system">
        <SettingsRow
          label="Memory enabled"
          description="Enable the key-value memory subsystem (stored memories survive across sessions)"
          htmlFor="rem-mem-enabled"
        >
          <Switch id="rem-mem-enabled" checked={rem.memory_enabled ?? false} onCheckedChange={(v) => updateRemembrances('memory_enabled', v)} />
        </SettingsRow>
        <SettingsRow
          label="Auto-inject in context"
          description="Prepend relevant stored memories into the system prompt before each turn"
          htmlFor="rem-mem-auto-inject"
        >
          <Switch
            id="rem-mem-auto-inject"
            checked={rem.memory_context_enrichment_enabled ?? false}
            onCheckedChange={(v) => updateRemembrances('memory_context_enrichment_enabled', v)}
          />
        </SettingsRow>
        <div className="p-4 grid grid-cols-1 sm:grid-cols-2 gap-4 border-t border-border">
          <Field label="Context max items">
            <Input type="number" value={String(rem.memory_context_max_items ?? 10)} onChange={(e) => updateRemembrances('memory_context_max_items', Number(e.target.value))} placeholder="10" />
          </Field>
          <Field label="Context max chars">
            <Input type="number" value={String(rem.memory_context_max_chars ?? 2000)} onChange={(e) => updateRemembrances('memory_context_max_chars', Number(e.target.value))} placeholder="2000" />
          </Field>
          <Field label="Default TTL (days)">
            <Input type="number" value={String(rem.memory_default_ttl_days ?? 0)} onChange={(e) => updateRemembrances('memory_default_ttl_days', Number(e.target.value))} placeholder="0 = no expiry" />
          </Field>
          <Field label="GC interval">
            <Input value={rem.memory_gc_interval ?? '1h'} onChange={(e) => updateRemembrances('memory_gc_interval', e.target.value)} placeholder="1h" />
          </Field>
        </div>
        <SettingsRow
          label="Auto-capture memories"
          description="Automatically extract and store key facts from conversations"
          htmlFor="rem-mem-auto-capture"
        >
          <Switch id="rem-mem-auto-capture" checked={rem.memory_auto_capture ?? false} onCheckedChange={(v) => updateRemembrances('memory_auto_capture', v)} />
        </SettingsRow>
      </SettingsSection>

      {error && <div className="settings-banner settings-banner--danger" role="alert">{error}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveServices} disabled={!dirty || saving} loading={saving}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetServices} disabled={!dirty}>
          Reset
        </Button>
      </div>

      {browsingKBPath && (
        <DirBrowserDialog
          // The browser only understands absolute paths. A relative KB path (the
          // default './.kb') is project-relative, so the picker opens at the
          // working directory rather than at $HOME — and at the working
          // directory itself, not at the KB folder, which may not exist yet.
          initialPath={
            rem.kb_path.startsWith('/') || rem.kb_path.startsWith('~')
              ? rem.kb_path
              : workspace?.cwd || undefined
          }
          onSelect={(path) => {
            updateRemembrances('kb_path', path)
            setBrowsingKBPath(false)
          }}
          onClose={() => setBrowsingKBPath(false)}
        />
      )}
    </div>
  )
}
