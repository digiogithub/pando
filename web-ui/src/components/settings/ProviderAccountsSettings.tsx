import { useEffect, useState } from 'react'
import api from '@/services/api'
import type { ProviderAccount, ProviderAccountTestResult } from '@/types'
import KeyValueEditor, { type KVPair } from '@/components/shared/KeyValueEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import { useToast } from '@/stores/toastStore'

const PROVIDER_TYPES = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'openai-compatible', label: 'OpenAI-Compatible' },
  { value: 'ollama', label: 'Ollama' },
  { value: 'copilot', label: 'GitHub Copilot' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'groq', label: 'Groq' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'xai', label: 'xAI (Grok)' },
  { value: 'azure', label: 'Azure OpenAI' },
  { value: 'bedrock', label: 'AWS Bedrock' },
  { value: 'vertexai', label: 'Vertex AI' },
]

const TYPES_WITH_BASE_URL = ['openai-compatible', 'azure', 'ollama']
const TYPES_WITH_EXTRA_HEADERS = ['openai-compatible']
const TYPES_WITH_OAUTH = ['anthropic', 'copilot']

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: 0,
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const labelStyle: React.CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
}

const selectStyle: React.CSSProperties = {
  background: 'var(--input-bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 14,
  padding: '0.5rem 0.75rem',
  fontFamily: 'inherit',
  cursor: 'pointer',
  width: '100%',
}

const actionBtn: React.CSSProperties = {
  padding: '0.25rem 0.625rem',
  background: 'transparent',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontSize: 12,
  cursor: 'pointer',
  color: 'var(--fg)',
  fontFamily: 'inherit',
}

const primaryBtn: React.CSSProperties = {
  padding: '0.5rem 1.25rem',
  background: 'var(--primary)',
  color: 'var(--primary-fg)',
  border: 'none',
  borderRadius: 'var(--radius-sm)',
  fontSize: 14,
  fontWeight: 600,
  cursor: 'pointer',
  fontFamily: 'inherit',
}

const cancelBtn: React.CSSProperties = {
  padding: '0.5rem 1.25rem',
  background: 'transparent',
  color: 'var(--fg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  fontSize: 14,
  cursor: 'pointer',
  fontFamily: 'inherit',
}

function StatusBadge({ disabled }: { disabled?: boolean }) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        padding: '0.125rem 0.5rem',
        borderRadius: 9999,
        fontSize: 11,
        fontWeight: 600,
        background: !disabled ? 'var(--success)' : 'var(--secondary)',
        color: !disabled ? 'white' : 'var(--fg)',
      }}
    >
      {!disabled ? 'Enabled' : 'Disabled'}
    </span>
  )
}

// Masked API key input - only sends a new value if user actually typed something
function MaskedApiKeyInput({
  maskedValue,
  onChange,
}: {
  maskedValue: string
  onChange: (val: string) => void
}) {
  const [localVal, setLocalVal] = useState('')
  const [showKey, setShowKey] = useState(false)
  const [touched, setTouched] = useState(false)

  const displayValue = touched ? localVal : ''
  const inputPlaceholder = maskedValue
    ? maskedValue
    : 'Enter API key…'

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

interface FormState {
  id: string
  displayName: string
  type: string
  apiKey: string          // new key typed by user (empty = not changed)
  apiKeyTouched: boolean  // true if user modified the key
  baseUrl: string
  extraHeaderPairs: KVPair[]
  disabled: boolean
  useOAuth: boolean
}

function emptyForm(): FormState {
  return {
    id: '',
    displayName: '',
    type: 'anthropic',
    apiKey: '',
    apiKeyTouched: false,
    baseUrl: '',
    extraHeaderPairs: [],
    disabled: false,
    useOAuth: false,
  }
}

function accountToForm(a: ProviderAccount): FormState {
  const headers = a.extraHeaders ?? {}
  return {
    id: a.id,
    displayName: a.displayName,
    type: a.type,
    apiKey: '',
    apiKeyTouched: false,
    baseUrl: a.baseUrl ?? '',
    extraHeaderPairs: Object.entries(headers).map(([key, value]) => ({ key, value })),
    disabled: a.disabled ?? false,
    useOAuth: a.useOAuth ?? false,
  }
}

function formToPayload(f: FormState, isEdit: boolean): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    displayName: f.displayName,
    type: f.type,
    baseUrl: f.baseUrl,
    disabled: f.disabled,
    useOAuth: f.useOAuth,
    extraHeaders: Object.fromEntries(f.extraHeaderPairs.map(({ key, value }) => [key, value])),
  }

  if (!isEdit) {
    payload.id = f.id
  }

  // Only include apiKey if user actually typed a new one
  if (f.apiKeyTouched && f.apiKey) {
    payload.apiKey = f.apiKey
  }

  return payload
}

// Test result indicator
type TestStatus = 'idle' | 'testing' | 'ok' | 'fail'

export default function ProviderAccountsSettings() {
  const toast = useToast()

  const [accounts, setAccounts] = useState<ProviderAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const [modalOpen, setModalOpen] = useState(false)
  const [editId, setEditId] = useState<string | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm())

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [testStatuses, setTestStatuses] = useState<Record<string, TestStatus>>({})

  async function loadAccounts() {
    setLoading(true)
    try {
      const data = await api.get<ProviderAccount[]>('/api/v1/config/provider-accounts')
      setAccounts(data ?? [])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to load provider accounts')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadAccounts()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function openAdd() {
    setEditId(null)
    setForm(emptyForm())
    setModalOpen(true)
  }

  function openEdit(a: ProviderAccount) {
    setEditId(a.id)
    setForm(accountToForm(a))
    setModalOpen(true)
  }

  async function handleSave() {
    if (!form.displayName.trim()) {
      toast.error('Display Name is required')
      return
    }
    if (!editId && !form.id.trim()) {
      toast.error('ID is required')
      return
    }
    if (!editId && !/^[a-z0-9-]+$/.test(form.id)) {
      toast.error('ID must match /^[a-z0-9-]+$/')
      return
    }

    setSaving(true)
    try {
      const payload = formToPayload(form, !!editId)
      if (editId) {
        await api.put(`/api/v1/config/provider-accounts/${editId}`, payload)
        toast.success('Account updated')
      } else {
        await api.post('/api/v1/config/provider-accounts', payload)
        toast.success('Account created')
      }
      setModalOpen(false)
      await loadAccounts()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await api.delete(`/api/v1/config/provider-accounts/${id}`)
      toast.success('Account deleted')
      setConfirmDelete(null)
      await loadAccounts()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Delete failed')
    }
  }

  async function handleTest(id: string) {
    setTestStatuses((s) => ({ ...s, [id]: 'testing' }))
    try {
      const result = await api.post<ProviderAccountTestResult>(
        `/api/v1/config/provider-accounts/${id}/test`,
        {}
      )
      if (result.ok) {
        setTestStatuses((s) => ({ ...s, [id]: 'ok' }))
        toast.success(`Connection OK${result.modelCount ? ` (${result.modelCount} models)` : ''}`)
      } else {
        setTestStatuses((s) => ({ ...s, [id]: 'fail' }))
        toast.error(result.error ?? 'Test failed')
      }
    } catch (e) {
      setTestStatuses((s) => ({ ...s, [id]: 'fail' }))
      toast.error(e instanceof Error ? e.message : 'Test failed')
    }
  }

  function setField<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  const showBaseUrl = TYPES_WITH_BASE_URL.includes(form.type)
  const showExtraHeaders = TYPES_WITH_EXTRA_HEADERS.includes(form.type)
  const showOAuth = TYPES_WITH_OAUTH.includes(form.type)

  return (
    <div style={{ maxWidth: 860 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.25rem' }}>
        <h2 style={sectionTitle}>Provider Accounts</h2>
        <button
          onClick={openAdd}
          style={{
            padding: '0.5rem 1.25rem',
            background: 'var(--primary)',
            color: 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
            fontFamily: 'inherit',
          }}
        >
          + Add Account
        </button>
      </div>

      <p style={{ fontSize: 14, color: 'var(--fg-muted)', marginBottom: '1.5rem' }}>
        Manage multiple provider accounts. Each account has its own credentials and can be assigned to agents independently.
      </p>

      {loading && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
      )}

      {!loading && accounts.length === 0 && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14, padding: '1rem 0' }}>
          No provider accounts configured. Add one to get started.
        </div>
      )}

      {!loading && accounts.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['ID', 'Name', 'Type', 'API Key', 'Status', 'Actions'].map((h) => (
                <th
                  key={h}
                  style={{
                    textAlign: 'left',
                    padding: '0.5rem 0.75rem',
                    color: 'var(--fg-muted)',
                    fontWeight: 600,
                    fontSize: 11,
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                  }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => {
              const testStatus = testStatuses[a.id] ?? 'idle'
              return (
                <tr
                  key={a.id}
                  style={{ borderBottom: '1px solid var(--border)', verticalAlign: 'middle' }}
                >
                  <td style={{ padding: '0.625rem 0.75rem', fontFamily: 'monospace', color: 'var(--fg)', fontWeight: 600 }}>
                    {a.id}
                  </td>
                  <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg)' }}>
                    {a.displayName}
                  </td>
                  <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg-muted)' }}>
                    {PROVIDER_TYPES.find((t) => t.value === a.type)?.label ?? a.type}
                  </td>
                  <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg-muted)', fontFamily: 'monospace', maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {a.apiKey ? a.apiKey : <span style={{ opacity: 0.4 }}>—</span>}
                  </td>
                  <td style={{ padding: '0.625rem 0.75rem' }}>
                    <StatusBadge disabled={a.disabled} />
                  </td>
                  <td style={{ padding: '0.625rem 0.75rem' }}>
                    <div style={{ display: 'flex', gap: '0.375rem', alignItems: 'center' }}>
                      <button onClick={() => openEdit(a)} style={actionBtn}>
                        ✏ Edit
                      </button>
                      <button
                        onClick={() => handleTest(a.id)}
                        disabled={testStatus === 'testing'}
                        style={{ ...actionBtn, opacity: testStatus === 'testing' ? 0.6 : 1 }}
                      >
                        {testStatus === 'testing' ? '…' : 'Test'}
                      </button>
                      {testStatus === 'ok' && (
                        <span style={{ color: 'var(--success)', fontSize: 14 }} title="Connection OK">✓</span>
                      )}
                      {testStatus === 'fail' && (
                        <span style={{ color: 'var(--error)', fontSize: 14 }} title="Connection failed">✗</span>
                      )}
                      <button
                        onClick={() => setConfirmDelete(a.id)}
                        style={{ ...actionBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                      >
                        🗑 Delete
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}

      {/* Add/Edit Modal */}
      {modalOpen && (
        <div
          style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={() => setModalOpen(false)}
        >
          <div
            style={{ background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)', padding: '1.5rem', width: 560, maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 8px 32px rgba(0,0,0,0.4)' }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
              {editId ? `Edit: ${editId}` : 'Add Provider Account'}
            </h3>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {/* ID — only for new accounts */}
              {!editId && (
                <TextInput
                  label="ID"
                  placeholder="my-account (lowercase, letters, numbers, hyphens)"
                  value={form.id}
                  onChange={(e) => setField('id', e.target.value)}
                />
              )}

              <TextInput
                label="Display Name"
                placeholder="My Anthropic Account"
                value={form.displayName}
                onChange={(e) => setField('displayName', e.target.value)}
              />

              {/* Type selector */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
                <label style={labelStyle}>Type</label>
                <select
                  value={form.type}
                  onChange={(e) => setField('type', e.target.value)}
                  style={selectStyle}
                >
                  {PROVIDER_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>{t.label}</option>
                  ))}
                </select>
              </div>

              {/* API Key */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
                <label style={labelStyle}>API Key</label>
                <MaskedApiKeyInput
                  maskedValue={editId ? (accounts.find((a) => a.id === editId)?.apiKey ?? '') : ''}
                  onChange={(val) => {
                    setField('apiKey', val)
                    setField('apiKeyTouched', true)
                  }}
                />
              </div>

              {/* Base URL — conditional */}
              {showBaseUrl && (
                <TextInput
                  label="Base URL"
                  placeholder="https://api.example.com/v1"
                  value={form.baseUrl}
                  onChange={(e) => setField('baseUrl', e.target.value)}
                />
              )}

              {/* Extra Headers — conditional */}
              {showExtraHeaders && (
                <KeyValueEditor
                  label="Extra Headers"
                  pairs={form.extraHeaderPairs}
                  onChange={(v) => setField('extraHeaderPairs', v)}
                  keyPlaceholder="Header-Name"
                  valuePlaceholder="value"
                />
              )}

              {/* UseOAuth toggle — conditional */}
              {showOAuth && (
                <Toggle
                  label="Use OAuth"
                  description="Authenticate via OAuth instead of an API key"
                  checked={form.useOAuth}
                  onChange={(v) => setField('useOAuth', v)}
                />
              )}

              {/* Enabled toggle */}
              <Toggle
                label="Enabled"
                description="Allow this account to be used for AI requests"
                checked={!form.disabled}
                onChange={(v) => setField('disabled', !v)}
              />
            </div>

            <div style={dividerStyle} />

            <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
              <button onClick={() => setModalOpen(false)} style={cancelBtn}>
                Cancel
              </button>
              <button onClick={handleSave} disabled={saving} style={primaryBtn}>
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirm dialog */}
      {confirmDelete && (
        <ConfirmDialog
          title="Delete Provider Account"
          message={`Are you sure you want to delete account "${confirmDelete}"? This action cannot be undone.`}
          confirmLabel="Delete"
          dangerous
          onConfirm={() => handleDelete(confirmDelete)}
          onCancel={() => setConfirmDelete(null)}
        />
      )}
    </div>
  )
}
