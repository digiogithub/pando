import { useEffect, useState } from 'react'
import { useServicesSettingsStore } from '@/stores/servicesSettingsStore'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import MaskedInput from '@/components/shared/MaskedInput'
import { useToastStore } from '@/stores/toastStore'
import api from '@/services/api'
import type { CodeProjectInfo } from '@/types'

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

const EMBEDDING_PROVIDERS = ['', 'openai', 'openai-compatible', 'anthropic', 'ollama', 'nomic']

export default function RemembrancesSettings() {
  const { config, dirty, loading, saving, error, fetchServices, updateRemembrances, saveServices, resetServices } =
    useServicesSettingsStore()

  const [projects, setProjects] = useState<CodeProjectInfo[]>([])
  const [indexing, setIndexing] = useState(false)

  useEffect(() => {
    fetchServices()
  }, [fetchServices])

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

  async function handleReindexAll() {
    try {
      await api.post('/api/v1/config/remembrances/reindex', {})
      useToastStore.getState().addToast('Re-index started', 'success')
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
        <TextInput
          label="KB Path"
          value={rem.kb_path}
          onChange={(e) => updateRemembrances('kb_path', e.target.value)}
          placeholder="./.kb"
        />
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

        <TextInput
          label="Embedding Model"
          value={rem.document_embedding_model}
          onChange={(e) => updateRemembrances('document_embedding_model', e.target.value)}
          placeholder="text-embedding-3-small"
        />

        {rem.document_embedding_provider === 'openai-compatible' && (
          <TextInput
            label="Base URL"
            value={rem.document_embedding_base_url}
            onChange={(e) => updateRemembrances('document_embedding_base_url', e.target.value)}
            placeholder="https://api.example.com/v1"
          />
        )}

        <MaskedInput
          label="Embedding API Key"
          value={rem.document_embedding_api_key}
          onChange={(v) => updateRemembrances('document_embedding_api_key', v)}
          placeholder="sk-…"
        />
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

            <TextInput
              label="Code Embedding Model"
              value={rem.code_embedding_model}
              onChange={(e) => updateRemembrances('code_embedding_model', e.target.value)}
              placeholder="nomic-embed-code"
            />

            {rem.code_embedding_provider === 'openai-compatible' && (
              <TextInput
                label="Base URL"
                value={rem.code_embedding_base_url}
                onChange={(e) => updateRemembrances('code_embedding_base_url', e.target.value)}
                placeholder="https://api.example.com/v1"
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

            {/* Results count */}
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
          </>
        )}
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
    </div>
  )
}
