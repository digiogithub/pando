import { useCallback, useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@pando/client/stores/servicesSettingsStore'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import MaskedInput from '@/components/shared/MaskedInput'
import DirBrowserDialog from '@/components/shared/DirBrowserDialog'
import { useProjectStore } from '@pando/client/stores/projectStore'
import { useToastStore } from '@pando/client/stores/toastStore'
import api from '@pando/client/services/api'
import type { CodeProjectInfo } from '@pando/client/types'

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

const subSectionTitle: React.CSSProperties = {
  fontSize: 14,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '0.875rem',
  textTransform: 'uppercase' as const,
  letterSpacing: '0.05em',
}

const selectStyle: React.CSSProperties = {
  background: 'var(--input-bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 14,
  padding: '0.5rem 0.75rem',
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
  cursor: 'pointer',
}

const EMBEDDING_PROVIDERS = ['', 'openai', 'openai-compatible', 'anthropic', 'ollama']

const smallButtonStyle: React.CSSProperties = {
  padding: '0.5rem 0.875rem',
  background: 'transparent',
  color: 'var(--fg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
  fontFamily: 'inherit',
  whiteSpace: 'nowrap',
}

const fieldLabelStyle: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
}

const hintStyle: React.CSSProperties = { fontSize: 12, color: 'var(--fg-dim)' }

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
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <label style={fieldLabelStyle}>{label}</label>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'stretch' }}>
        <select
          value={models.some((m) => m.id === value) ? value : ''}
          onChange={(e) => e.target.value && onChange(e.target.value)}
          disabled={!provider || loading || models.length === 0}
          style={{ ...selectStyle, flex: 1 }}
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
        </select>
        <button type="button" onClick={() => void load()} disabled={!provider || loading} style={smallButtonStyle}>
          {loading ? '…' : 'Refresh'}
        </button>
      </div>
      <input
        value={value}
        list={listId}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
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
      <datalist id={listId}>
        {models.map((m) => (
          <option key={m.id} value={m.id} />
        ))}
      </datalist>
      <span style={hintStyle}>
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

export default function RemembrancesSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateRemembrances, saveServices, resetServices } =
    useServicesSettingsStore()

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
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
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
    <div style={{ maxWidth: 640 }}>
      <h2 style={sectionTitle}>Remembrances</h2>

      <Toggle
        label="Enabled"
        description="Enable the Remembrances memory system"
        checked={rem.enabled}
        onChange={(v) => updateRemembrances('enabled', v)}
      />

      <div style={dividerStyle} />

      {/* KB Filesystem Sync */}
      <p style={subSectionTitle}>KB Filesystem Sync</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end' }}>
          <div style={{ flex: 1 }}>
            <TextInput
              label="KB Path"
              value={rem.kb_path}
              onChange={(e) => updateRemembrances('kb_path', e.target.value)}
              placeholder="./.kb"
            />
          </div>
          <button type="button" onClick={() => setBrowsingKBPath(true)} style={smallButtonStyle}>
            Browse…
          </button>
        </div>
        <Toggle
          label="Watch KB Path"
          description="Monitor markdown changes in real time and re-index automatically"
          checked={rem.kb_watch}
          onChange={(v) => updateRemembrances('kb_watch', v)}
        />
        <Toggle
          label="Auto Import on Startup"
          description="Import markdown files from KB path when Pando starts"
          checked={rem.kb_auto_import}
          onChange={(v) => updateRemembrances('kb_auto_import', v)}
        />
        <Toggle
          label="Convert Documents"
          description="Convert docx/pdf/xlsx and other rich documents to Markdown on the fly and index them, referencing the original file"
          checked={rem.kb_convert_documents}
          onChange={(v) => updateRemembrances('kb_convert_documents', v)}
        />
        <Toggle
          label="Wiki Links"
          description="Index [[wiki links]] written in KB documents as a navigable graph: backlinks, related documents and concepts still undocumented"
          checked={rem.kb_wiki_links}
          onChange={(v) => updateRemembrances('kb_wiki_links', v)}
        />
      </div>

      <div style={dividerStyle} />

      {/* Document Embeddings */}
      <p style={subSectionTitle}>Document Embeddings</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Embedding Provider
          </label>
          <select
            value={rem.document_embedding_provider}
            onChange={(e) => updateRemembrances('document_embedding_provider', e.target.value)}
            style={selectStyle}
            onFocus={(e) => (e.currentTarget.style.borderColor = 'var(--border-focus)')}
            onBlur={(e) => (e.currentTarget.style.borderColor = 'var(--border)')}
          >
            {EMBEDDING_PROVIDERS.map((p) => (
              <option key={p} value={p}>{p || '— select provider —'}</option>
            ))}
          </select>
        </div>

        <EmbeddingModelPicker
          label="Embedding Model"
          listId="doc-embedding-models"
          provider={rem.document_embedding_provider}
          baseUrl={rem.document_embedding_base_url}
          apiKey={rem.document_embedding_api_key}
          value={rem.document_embedding_model}
          placeholder="text-embedding-3-small"
          onChange={(v) => updateRemembrances('document_embedding_model', v)}
        />

        {(rem.document_embedding_provider === 'openai-compatible' || rem.document_embedding_provider === 'ollama') && (
          <TextInput
            label="Base URL"
            value={rem.document_embedding_base_url}
            onChange={(e) => updateRemembrances('document_embedding_base_url', e.target.value)}
            placeholder={rem.document_embedding_provider === 'ollama' ? 'http://localhost:11434' : 'https://api.example.com/v1'}
          />
        )}

        <MaskedInput
          label="Embedding API Key"
          value={rem.document_embedding_api_key}
          onChange={(v) => updateRemembrances('document_embedding_api_key', v)}
          placeholder="sk-…"
        />

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginTop: '0.25rem' }}>
          <button
            onClick={handleTestDocEmbedding}
            disabled={testingDoc || !rem.document_embedding_provider || !rem.document_embedding_model}
            style={{
              padding: '0.4rem 0.875rem',
              background: 'transparent',
              color: testingDoc ? 'var(--fg-dim)' : 'var(--primary)',
              border: `1px solid ${testingDoc ? 'var(--border)' : 'var(--primary)'}`,
              borderRadius: 'var(--radius-sm)',
              fontSize: 12,
              fontWeight: 600,
              cursor: testingDoc ? 'not-allowed' : 'pointer',
              fontFamily: 'inherit',
              whiteSpace: 'nowrap',
            }}
          >
            {testingDoc ? 'Testing…' : 'Test Connection'}
          </button>
          {docTestResult && (
            <span style={{ fontSize: 12, color: docTestResult.ok ? 'var(--success, #4ade80)' : 'var(--error, #f87171)' }}>
              {docTestResult.ok
                ? `✓ OK — ${docTestResult.dimension}d, ${docTestResult.latency_ms}ms`
                : `✗ ${docTestResult.error}`}
            </span>
          )}
        </div>
      </div>

      <div style={dividerStyle} />

      {/* Code Embeddings */}
      <p style={subSectionTitle}>Code Embeddings</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Use Same Model as Document"
          checked={rem.use_same_model}
          onChange={(v) => updateRemembrances('use_same_model', v)}
        />

        {!rem.use_same_model && (
          <>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
              <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                Code Embedding Provider
              </label>
              <select
                value={rem.code_embedding_provider}
                onChange={(e) => updateRemembrances('code_embedding_provider', e.target.value)}
                style={selectStyle}
                onFocus={(e) => (e.currentTarget.style.borderColor = 'var(--border-focus)')}
                onBlur={(e) => (e.currentTarget.style.borderColor = 'var(--border)')}
              >
                {EMBEDDING_PROVIDERS.map((p) => (
                  <option key={p} value={p}>{p || '— select provider —'}</option>
                ))}
              </select>
            </div>

            <EmbeddingModelPicker
              label="Code Embedding Model"
              listId="code-embedding-models"
              provider={rem.code_embedding_provider}
              baseUrl={rem.code_embedding_base_url}
              apiKey={rem.code_embedding_api_key}
              value={rem.code_embedding_model}
              placeholder="nomic-embed-code"
              onChange={(v) => updateRemembrances('code_embedding_model', v)}
            />

            {(rem.code_embedding_provider === 'openai-compatible' || rem.code_embedding_provider === 'ollama') && (
              <TextInput
                label="Base URL"
                value={rem.code_embedding_base_url}
                onChange={(e) => updateRemembrances('code_embedding_base_url', e.target.value)}
                placeholder={rem.code_embedding_provider === 'ollama' ? 'http://localhost:11434' : 'https://api.example.com/v1'}
              />
            )}

            <MaskedInput
              label="Code Embedding API Key"
              value={rem.code_embedding_api_key}
              onChange={(v) => updateRemembrances('code_embedding_api_key', v)}
              placeholder="sk-…"
            />
          </>
        )}

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginTop: '0.25rem' }}>
          <button
            onClick={handleTestCodeEmbedding}
            disabled={testingCode}
            style={{
              padding: '0.4rem 0.875rem',
              background: 'transparent',
              color: testingCode ? 'var(--fg-dim)' : 'var(--primary)',
              border: `1px solid ${testingCode ? 'var(--border)' : 'var(--primary)'}`,
              borderRadius: 'var(--radius-sm)',
              fontSize: 12,
              fontWeight: 600,
              cursor: testingCode ? 'not-allowed' : 'pointer',
              fontFamily: 'inherit',
              whiteSpace: 'nowrap',
            }}
          >
            {testingCode ? 'Testing…' : 'Test Connection'}
          </button>
          {codeTestResult && (
            <span style={{ fontSize: 12, color: codeTestResult.ok ? 'var(--success, #4ade80)' : 'var(--error, #f87171)' }}>
              {codeTestResult.ok
                ? `✓ OK — ${codeTestResult.dimension}d, ${codeTestResult.latency_ms}ms`
                : `✗ ${codeTestResult.error}`}
            </span>
          )}
        </div>
      </div>

      <div style={dividerStyle} />

      {/* Chunking */}
      <p style={subSectionTitle}>Chunking</p>
      <div style={{ display: 'flex', gap: '1rem' }}>
        <div style={{ flex: 1 }}>
          <TextInput
            label="Chunk Size"
            type="number"
            value={String(rem.chunk_size)}
            onChange={(e) => updateRemembrances('chunk_size', Number(e.target.value))}
            placeholder="512"
          />
        </div>
        <div style={{ flex: 1 }}>
          <TextInput
            label="Chunk Overlap"
            type="number"
            value={String(rem.chunk_overlap)}
            onChange={(e) => updateRemembrances('chunk_overlap', Number(e.target.value))}
            placeholder="64"
          />
        </div>
        <div style={{ flex: 1 }}>
          <TextInput
            label="Index Workers"
            type="number"
            value={String(rem.index_workers)}
            onChange={(e) => updateRemembrances('index_workers', Number(e.target.value))}
            placeholder="2"
          />
        </div>
      </div>

      <div style={dividerStyle} />

      {/* Code Indexing actions */}
      <p style={subSectionTitle}>Code Indexing</p>
      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
        <p style={{ margin: 0, fontSize: 13, color: 'var(--fg-muted)', flex: 1 }}>
          Trigger a full re-index of all registered code projects.
        </p>
        <button
          onClick={handleReindexAll}
          style={{
            padding: '0.5rem 1rem',
            background: 'transparent',
            color: 'var(--primary)',
            border: '1px solid var(--primary)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
            fontFamily: 'inherit',
            whiteSpace: 'nowrap',
          }}
        >
          Re-index All
        </button>
      </div>

      <div style={dividerStyle} />

      {/* Context Enrichment */}
      <p style={subSectionTitle}>Context Enrichment</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Enable Context Enrichment"
          description="Before each prompt, search KB and code index and prepend relevant context automatically"
          checked={rem.context_enrichment_enabled}
          onChange={(v) => updateRemembrances('context_enrichment_enabled', v)}
        />

        {rem.context_enrichment_enabled && (
          <>
            {/* Code Project selector */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
              <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                Code Project
              </label>
              <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                <select
                  value={rem.context_enrichment_code_project}
                  onChange={(e) => updateRemembrances('context_enrichment_code_project', e.target.value)}
                  style={{ ...selectStyle, flex: 1 }}
                  onFocus={(e) => (e.currentTarget.style.borderColor = 'var(--border-focus)')}
                  onBlur={(e) => (e.currentTarget.style.borderColor = 'var(--border)')}
                >
                  <option value="">— none (KB only) —</option>
                  {projects.map((p) => (
                    <option key={p.project_id} value={p.project_id}>
                      {p.name || p.project_id} ({p.root_path})
                    </option>
                  ))}
                </select>
                <button
                  onClick={handleIndexWorkdir}
                  disabled={indexing}
                  title="Index the current working directory as a new code project"
                  style={{
                    padding: '0.5rem 0.875rem',
                    background: 'transparent',
                    color: indexing ? 'var(--fg-dim)' : 'var(--primary)',
                    border: `1px solid ${indexing ? 'var(--border)' : 'var(--primary)'}`,
                    borderRadius: 'var(--radius-sm)',
                    fontSize: 12,
                    fontWeight: 600,
                    cursor: indexing ? 'not-allowed' : 'pointer',
                    fontFamily: 'inherit',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {indexing ? 'Indexing…' : '+ Index workdir'}
                </button>
              </div>
              <p style={{ margin: 0, fontSize: 12, color: 'var(--fg-muted)' }}>
                Select a previously indexed project to include code search results, or index the working directory.
              </p>
            </div>

            {/* Results count — KB and Code */}
            <div style={{ display: 'flex', gap: '1rem' }}>
              <div style={{ flex: 1 }}>
                <TextInput
                  label="KB Results"
                  type="number"
                  value={String(rem.context_enrichment_kb_results)}
                  onChange={(e) => updateRemembrances('context_enrichment_kb_results', Number(e.target.value))}
                  placeholder="3"
                />
              </div>
              <div style={{ flex: 1 }}>
                <TextInput
                  label="Code Results"
                  type="number"
                  value={String(rem.context_enrichment_code_results)}
                  onChange={(e) => updateRemembrances('context_enrichment_code_results', Number(e.target.value))}
                  placeholder="5"
                />
              </div>
            </div>

            {/* Events enrichment */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
              <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                Past Session Events
              </label>
              <p style={{ margin: 0, fontSize: 12, color: 'var(--fg-muted)' }}>
                Search saved events from previous sessions and prepend relevant ones as context.
              </p>
            </div>
            <div style={{ display: 'flex', gap: '1rem' }}>
              <div style={{ flex: 1 }}>
                <TextInput
                  label="Events Results"
                  type="number"
                  value={String(rem.context_enrichment_events_results)}
                  onChange={(e) => updateRemembrances('context_enrichment_events_results', Number(e.target.value))}
                  placeholder="3"
                />
              </div>
              <div style={{ flex: 1 }}>
                <TextInput
                  label="Last Days"
                  type="number"
                  value={String(rem.context_enrichment_events_last_days)}
                  onChange={(e) => updateRemembrances('context_enrichment_events_last_days', Number(e.target.value))}
                  placeholder="30"
                />
              </div>
            </div>
            <TextInput
              label="Subject Filter"
              value={rem.context_enrichment_events_subject}
              onChange={(e) => updateRemembrances('context_enrichment_events_subject', e.target.value)}
              placeholder="e.g. pando  (leave empty for all subjects)"
            />

            {/* Agent loop enrichment */}
            <Toggle
              label="Agent Loop Enrichment"
              description="Run enrichment as a separate agent loop on the context-enricher model. It searches memory, KB and the code index iteratively; the main agent only receives the resulting context block."
              checked={rem.context_enrichment_agent_loop_enabled ?? false}
              onChange={(v) => updateRemembrances('context_enrichment_agent_loop_enabled', v)}
            />

            {rem.context_enrichment_agent_loop_enabled && (
              <>
                <div style={{ display: 'flex', gap: '1rem' }}>
                  <div style={{ flex: 1 }}>
                    <TextInput
                      label="Loop Timeout (s)"
                      type="number"
                      value={String(rem.context_enrichment_agent_loop_timeout_seconds ?? 60)}
                      onChange={(e) => updateRemembrances('context_enrichment_agent_loop_timeout_seconds', Number(e.target.value))}
                      placeholder="60"
                    />
                  </div>
                  <div style={{ flex: 1 }}>
                    <TextInput
                      label="Loop Max Chars"
                      type="number"
                      value={String(rem.context_enrichment_agent_loop_max_chars ?? 6000)}
                      onChange={(e) => updateRemembrances('context_enrichment_agent_loop_max_chars', Number(e.target.value))}
                      placeholder="6000"
                    />
                  </div>
                </div>
                <Toggle
                  label="Run on Every Message"
                  description="Off (default): the loop runs once per session, on the first message. On: it runs on every user turn."
                  checked={rem.context_enrichment_agent_loop_every_message ?? false}
                  onChange={(v) => updateRemembrances('context_enrichment_agent_loop_every_message', v)}
                />
                <Toggle
                  label="Announce in Chat"
                  description="Show start and end notices in the chat while the enrichment agent runs, like context compaction does"
                  checked={!(rem.context_enrichment_agent_loop_silent ?? false)}
                  onChange={(v) => updateRemembrances('context_enrichment_agent_loop_silent', !v)}
                />
                <Toggle
                  label="Fallback to Search"
                  description="Use the classic search pipeline when the loop fails, times out or finds nothing"
                  checked={!(rem.context_enrichment_agent_loop_fallback_disabled ?? false)}
                  onChange={(v) => updateRemembrances('context_enrichment_agent_loop_fallback_disabled', !v)}
                />
                <Toggle
                  label="Show Loop in Chat"
                  description="Record the loop as a child session of the chat session so its tool calls can be inspected"
                  checked={!(rem.context_enrichment_agent_loop_hidden_in_chat ?? false)}
                  onChange={(v) => updateRemembrances('context_enrichment_agent_loop_hidden_in_chat', !v)}
                />
              </>
            )}
          </>
        )}
      </div>

      <div style={dividerStyle} />

      {/* Memory System */}
      <p style={subSectionTitle}>Memory System</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Memory Enabled"
          description="Enable the key-value memory subsystem (stored memories survive across sessions)"
          checked={rem.memory_enabled ?? false}
          onChange={(v) => updateRemembrances('memory_enabled', v)}
        />
        <Toggle
          label="Auto-inject in context"
          description="Prepend relevant stored memories into the system prompt before each turn"
          checked={rem.memory_context_enrichment_enabled ?? false}
          onChange={(v) => updateRemembrances('memory_context_enrichment_enabled', v)}
        />
        <div style={{ display: 'flex', gap: '1rem' }}>
          <div style={{ flex: 1 }}>
            <TextInput
              label="Context max items"
              type="number"
              value={String(rem.memory_context_max_items ?? 10)}
              onChange={(e) => updateRemembrances('memory_context_max_items', Number(e.target.value))}
              placeholder="10"
            />
          </div>
          <div style={{ flex: 1 }}>
            <TextInput
              label="Context max chars"
              type="number"
              value={String(rem.memory_context_max_chars ?? 2000)}
              onChange={(e) => updateRemembrances('memory_context_max_chars', Number(e.target.value))}
              placeholder="2000"
            />
          </div>
        </div>
        <div style={{ display: 'flex', gap: '1rem' }}>
          <div style={{ flex: 1 }}>
            <TextInput
              label="Default TTL (days)"
              type="number"
              value={String(rem.memory_default_ttl_days ?? 0)}
              onChange={(e) => updateRemembrances('memory_default_ttl_days', Number(e.target.value))}
              placeholder="0 = no expiry"
            />
          </div>
          <div style={{ flex: 1 }}>
            <TextInput
              label="GC interval"
              value={rem.memory_gc_interval ?? '1h'}
              onChange={(e) => updateRemembrances('memory_gc_interval', e.target.value)}
              placeholder="1h"
            />
          </div>
        </div>
        <Toggle
          label="Auto-capture memories"
          description="Automatically extract and store key facts from conversations"
          checked={rem.memory_auto_capture ?? false}
          onChange={(v) => updateRemembrances('memory_auto_capture', v)}
        />
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
          onClick={saveServices}
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
            fontFamily: 'inherit',
          }}
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetServices}
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
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
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
