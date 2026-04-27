import { useEffect, useRef, useState } from 'react'
import api from '@/services/api'
import type { ProviderAccount, ProviderAccountTestResult } from '@/types'
import KeyValueEditor, { type KVPair } from '@/components/shared/KeyValueEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import { useToast } from '@/stores/toastStore'

const PROVIDER_TYPES = [
  { value: 'anthropic', label: 'Anthropic', icon: '🤖' },
  { value: 'openai', label: 'OpenAI', icon: '🧠' },
  { value: 'openai-compatible', label: 'OpenAI Compatible (custom)', icon: '🔌' },
  { value: 'ollama', label: 'Ollama', icon: '🦙' },
  { value: 'gemini', label: 'Google Gemini', icon: '✨' },
  { value: 'groq', label: 'Groq', icon: '⚡' },
  { value: 'openrouter', label: 'OpenRouter', icon: '🔀' },
  { value: 'xai', label: 'xAI (Grok)', icon: '🌌' },
  { value: 'azure', label: 'Azure OpenAI', icon: '☁️' },
  { value: 'bedrock', label: 'AWS Bedrock', icon: '🏔️' },
  { value: 'vertexai', label: 'Google Vertex AI', icon: '🔷' },
  { value: 'copilot', label: 'GitHub Copilot', icon: '🐙' },
]

const TYPES_WITH_BASE_URL = ['openai-compatible', 'azure', 'ollama', 'openai']
const TYPES_WITH_EXTRA_HEADERS = ['openai-compatible', 'azure', 'openai', 'anthropic', 'openrouter']
const TYPES_WITH_OAUTH = ['copilot', 'vertexai']

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

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
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
        background: !disabled ? 'rgba(34,197,94,0.15)' : 'var(--border)',
        color: !disabled ? '#16a34a' : 'var(--fg-muted)',
      }}
    >
      {!disabled ? 'Enabled' : 'Disabled'}
    </span>
  )
}

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
  const inputPlaceholder = maskedValue ? maskedValue : 'Enter API key…'

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
  apiKey: string
  apiKeyTouched: boolean
  baseUrl: string
  extraHeaderPairs: KVPair[]
  disabled: boolean
  useOAuth: boolean
}

function emptyForm(preselectedType = 'anthropic'): FormState {
  return {
    id: '',
    displayName: '',
    type: preselectedType,
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
  if (f.apiKeyTouched && f.apiKey) {
    payload.apiKey = f.apiKey
  }
  return payload
}

// Slug-ify a display name into a valid account ID
function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

type TestStatus = 'idle' | 'testing' | 'ok' | 'fail'

// ── AccountCard ──────────────────────────────────────────────────────────────

function AccountCard({
  account,
  onEdit,
  onDelete,
  onTest,
  testStatus,
}: {
  account: ProviderAccount
  onEdit: () => void
  onDelete: () => void
  onTest: () => void
  testStatus: TestStatus
}) {
  const meta = PROVIDER_TYPES.find((t) => t.value === account.type)
  const icon = meta?.icon ?? '🔌'
  const typeLabel = meta?.label ?? account.type

  return (
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
        padding: '0.875rem 1rem',
        background: 'var(--card-bg, var(--input-bg))',
        display: 'flex',
        alignItems: 'center',
        gap: '0.875rem',
      }}
    >
      <span style={{ fontSize: 22, flexShrink: 0 }}>{icon}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)' }}>
            {account.displayName}
          </span>
          <span style={{ fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'monospace' }}>
            {account.id}
          </span>
          <StatusBadge disabled={account.disabled} />
        </div>
        <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginTop: '0.2rem' }}>
          {typeLabel}
          {account.apiKey && (
            <span style={{ marginLeft: '0.5rem', fontFamily: 'monospace' }}>
              · {account.apiKey}
            </span>
          )}
        </div>
      </div>
      <div style={{ display: 'flex', gap: '0.375rem', alignItems: 'center', flexShrink: 0 }}>
        <button
          onClick={onEdit}
          style={{
            padding: '0.25rem 0.625rem',
            background: 'transparent',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 12,
            cursor: 'pointer',
            color: 'var(--fg)',
            fontFamily: 'inherit',
          }}
        >
          Edit
        </button>
        <button
          onClick={onTest}
          disabled={testStatus === 'testing'}
          style={{
            padding: '0.25rem 0.625rem',
            background: 'transparent',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 12,
            cursor: testStatus === 'testing' ? 'not-allowed' : 'pointer',
            color: 'var(--fg)',
            fontFamily: 'inherit',
            opacity: testStatus === 'testing' ? 0.6 : 1,
          }}
        >
          {testStatus === 'testing' ? '…' : 'Test'}
        </button>
        {testStatus === 'ok' && (
          <span style={{ color: 'var(--success, #16a34a)', fontSize: 14 }} title="Connection OK">✓</span>
        )}
        {testStatus === 'fail' && (
          <span style={{ color: 'var(--error, #dc2626)', fontSize: 14 }} title="Connection failed">✗</span>
        )}
        <button
          onClick={onDelete}
          style={{
            padding: '0.25rem 0.625rem',
            background: 'transparent',
            border: '1px solid var(--error, #dc2626)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 12,
            cursor: 'pointer',
            color: 'var(--error, #dc2626)',
            fontFamily: 'inherit',
          }}
        >
          Delete
        </button>
      </div>
    </div>
  )
}

// ── AddProviderDropdown ──────────────────────────────────────────────────────

function AddProviderDropdown({ onSelect }: { onSelect: (type: string) => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // Close when clicking outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen((v) => !v)}
        style={{
          ...primaryBtn,
          display: 'flex',
          alignItems: 'center',
          gap: '0.375rem',
        }}
      >
        + Add Provider
        <span
          style={{
            fontSize: 10,
            display: 'inline-block',
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.15s',
          }}
        >
          ▼
        </span>
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 4px)',
            right: 0,
            background: 'var(--card-bg, var(--input-bg))',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius)',
            boxShadow: '0 8px 24px rgba(0,0,0,0.25)',
            minWidth: 240,
            zIndex: 200,
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              padding: '0.375rem 0.75rem',
              fontSize: 11,
              fontWeight: 700,
              color: 'var(--fg-muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.06em',
              borderBottom: '1px solid var(--border)',
            }}
          >
            Select Provider Type
          </div>
          {PROVIDER_TYPES.map((pt) => (
            <button
              key={pt.value}
              onClick={() => {
                setOpen(false)
                onSelect(pt.value)
              }}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.625rem',
                width: '100%',
                padding: '0.5rem 0.875rem',
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                textAlign: 'left',
                fontSize: 14,
                color: 'var(--fg)',
                fontFamily: 'inherit',
                transition: 'background 0.1s',
              }}
              onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--hover, rgba(255,255,255,0.05))' }}
              onMouseLeave={(e) => { e.currentTarget.style.background = 'none' }}
            >
              <span style={{ fontSize: 18 }}>{pt.icon}</span>
              <span>{pt.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ── AccountModal ─────────────────────────────────────────────────────────────

function AccountModal({
  editId,
  accounts,
  form,
  saving,
  setField,
  onSave,
  onClose,
}: {
  editId: string | null
  accounts: ProviderAccount[]
  form: FormState
  saving: boolean
  setField: <K extends keyof FormState>(key: K, value: FormState[K]) => void
  onSave: () => void
  onClose: () => void
}) {
  const showBaseUrl = TYPES_WITH_BASE_URL.includes(form.type)
  const showExtraHeaders = TYPES_WITH_EXTRA_HEADERS.includes(form.type)
  const showOAuth = TYPES_WITH_OAUTH.includes(form.type)
  const needsAPIKey = !showOAuth

  // Auto-generate ID from displayName when adding
  function handleDisplayNameChange(e: React.ChangeEvent<HTMLInputElement>) {
    const v = e.target.value
    setField('displayName', v)
    if (!editId) {
      setField('id', slugify(v))
    }
  }

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: 560,
          maxWidth: '95vw',
          maxHeight: '90vh',
          overflowY: 'auto',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
          {editId ? `Edit: ${editId}` : 'Add Provider Account'}
        </h3>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          {/* ID — only for new accounts */}
          {!editId && (
            <TextInput
              label="Account ID"
              placeholder="my-account (auto-generated, editable)"
              value={form.id}
              onChange={(e) => setField('id', e.target.value)}
            />
          )}

          <TextInput
            label="Display Name"
            placeholder="My Anthropic Account"
            value={form.displayName}
            onChange={handleDisplayNameChange}
          />

          {/* Type selector */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
            <label style={labelStyle}>Provider Type</label>
            <select
              value={form.type}
              onChange={(e) => setField('type', e.target.value)}
              style={selectStyle}
            >
              {PROVIDER_TYPES.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
          </div>

          {/* API Key */}
          {needsAPIKey && (
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
          )}

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
          <button onClick={onClose} style={cancelBtn}>
            Cancel
          </button>
          <button onClick={onSave} disabled={saving} style={primaryBtn}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export default function ProviderAccountsSettings() {
  const toast = useToast()

  const [accounts, setAccounts] = useState<ProviderAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const [modalOpen, setModalOpen] = useState(false)
  const [editId, setEditId] = useState<string | null>(null)
  const [form, setFormState] = useState<FormState>(emptyForm())

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [testStatuses, setTestStatuses] = useState<Record<string, TestStatus>>({})

  async function loadAccounts() {
    setLoading(true)
    try {
      const data = await api.get<{ providerAccounts: ProviderAccount[] }>(
        '/api/v1/config/provider-accounts'
      )
      setAccounts(data?.providerAccounts ?? [])
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

  function openAdd(preselectedType = 'anthropic') {
    setEditId(null)
    setFormState(emptyForm(preselectedType))
    setModalOpen(true)
  }

  function openEdit(a: ProviderAccount) {
    setEditId(a.id)
    setFormState(accountToForm(a))
    setModalOpen(true)
  }

  function setField<K extends keyof FormState>(key: K, value: FormState[K]) {
    setFormState((f) => ({ ...f, [key]: value }))
  }

  async function handleSave() {
    if (!form.displayName.trim()) {
      toast.error('Display Name is required')
      return
    }
    if (!editId && !form.id.trim()) {
      toast.error('Account ID is required')
      return
    }
    if (!editId && !/^[a-z0-9-]+$/.test(form.id)) {
      toast.error('Account ID must match /^[a-z0-9-]+$/')
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

  return (
    <div style={{ maxWidth: 760 }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: '0.75rem',
        }}
      >
        <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
          Providers
        </h2>
        <AddProviderDropdown onSelect={openAdd} />
      </div>

      <p style={{ fontSize: 14, color: 'var(--fg-muted)', marginBottom: '1.5rem' }}>
        Manage AI provider accounts. Each account has its own credentials and can be assigned to
        agents independently.
      </p>

      {loading && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
      )}

      {!loading && accounts.length === 0 && (
        <div
          style={{
            border: '2px dashed var(--border)',
            borderRadius: 'var(--radius)',
            padding: '2.5rem',
            textAlign: 'center',
            color: 'var(--fg-muted)',
            fontSize: 14,
          }}
        >
          <div style={{ fontSize: 32, marginBottom: '0.75rem' }}>🔌</div>
          <div style={{ fontWeight: 600, marginBottom: '0.25rem' }}>No providers configured</div>
          <div>Use the "Add Provider" button above to configure your first provider.</div>
        </div>
      )}

      {!loading && accounts.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.625rem' }}>
          {accounts.map((a) => (
            <AccountCard
              key={a.id}
              account={a}
              testStatus={testStatuses[a.id] ?? 'idle'}
              onEdit={() => openEdit(a)}
              onDelete={() => setConfirmDelete(a.id)}
              onTest={() => handleTest(a.id)}
            />
          ))}
        </div>
      )}

      {/* Add/Edit Modal */}
      {modalOpen && (
        <AccountModal
          editId={editId}
          accounts={accounts}
          form={form}
          saving={saving}
          setField={setField}
          onSave={handleSave}
          onClose={() => setModalOpen(false)}
        />
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
