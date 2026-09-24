import { useEffect, useRef, useState } from 'react'
import api from '@pando/client/services/api'
import type { ProviderAccount, ProviderAccountTestResult } from '@pando/client/types'
import KeyValueEditor, { type KVPair } from '@/components/shared/KeyValueEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToast } from '@pando/client/stores/toastStore'
import { Badge, Button, Card, Dialog, EmptyState, IconButton, Input, Menu, MenuItem, MenuLabel, Select, Switch } from '@/components/ui'
import {
  Bot,
  Brain,
  CircleCheck,
  CircleX,
  Cloud,
  Code,
  Eye,
  EyeOff,
  Globe,
  Network,
  Package,
  Plug,
  Plus,
  Rocket,
  Server,
  Sparkles,
  Zap,
  type LucideIcon,
} from '@/components/ui/icons'

const PROVIDER_TYPES: { value: string; label: string; icon: LucideIcon }[] = [
  { value: 'anthropic', label: 'Anthropic', icon: Bot },
  { value: 'openai', label: 'OpenAI', icon: Brain },
  { value: 'openai-compatible', label: 'OpenAI Compatible (custom)', icon: Plug },
  { value: 'ollama', label: 'Ollama', icon: Package },
  { value: 'gemini', label: 'Google Gemini', icon: Sparkles },
  { value: 'groq', label: 'Groq', icon: Zap },
  { value: 'openrouter', label: 'OpenRouter', icon: Network },
  { value: 'xai', label: 'xAI (Grok)', icon: Rocket },
  { value: 'azure', label: 'Azure OpenAI', icon: Cloud },
  { value: 'bedrock', label: 'AWS Bedrock', icon: Server },
  { value: 'vertexai', label: 'Google Vertex AI', icon: Globe },
  { value: 'copilot', label: 'GitHub Copilot', icon: Code },
  { value: 'antigravity', label: 'Antigravity', icon: Sparkles },
]

const TYPES_WITH_BASE_URL = ['openai-compatible', 'azure', 'ollama', 'openai']
const TYPES_WITH_EXTRA_HEADERS = ['openai-compatible', 'azure', 'openai', 'anthropic', 'openrouter']
const TYPES_WITH_OAUTH = ['copilot', 'vertexai', 'antigravity']

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
    <div className="relative flex items-center">
      <Input
        type={showKey ? 'text' : 'password'}
        autoComplete="new-password"
        value={displayValue}
        placeholder={inputPlaceholder}
        onChange={handleChange}
        className="font-mono pr-10"
      />
      <IconButton
        className="absolute right-1"
        tabIndex={-1}
        aria-label={showKey ? 'Hide key' : 'Show key'}
        tooltip
        size="sm"
        icon={showKey ? <EyeOff size={14} /> : <Eye size={14} />}
        onClick={() => setShowKey((v) => !v)}
      />
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

// Sanitize the Account ID as the user types: lowercase and replace any character
// outside [a-z0-9-] with a hyphen. Trailing hyphens are preserved so a hyphen can
// still be typed mid-word; this guarantees the ID never contains spaces.
function sanitizeAccountId(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9-]+/g, '-')
}

type TestStatus = 'idle' | 'testing' | 'ok' | 'fail'

// ── AccountCard ──────────────────────────────────────────────────────────────

function AccountCard({
  account,
  onEdit,
  onDelete,
  onTest,
  onLogin,
  testStatus,
}: {
  account: ProviderAccount
  onEdit: () => void
  onDelete: () => void
  onTest: () => void
  onLogin?: () => void
  testStatus: TestStatus
}) {
  const meta = PROVIDER_TYPES.find((t) => t.value === account.type)
  const typeLabel = meta?.label ?? account.type

  return (
    <Card padding="sm" className="flex items-center gap-3.5">
      {meta ? (
        <meta.icon size={22} className="shrink-0 text-muted" aria-hidden />
      ) : (
        <Plug size={22} className="shrink-0 text-muted" aria-hidden />
      )}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-md font-semibold text-fg">{account.displayName}</span>
          <span className="text-xs text-muted font-mono">{account.id}</span>
          <Badge tone={account.disabled ? 'neutral' : 'success'} dot>
            {account.disabled ? 'Disabled' : 'Enabled'}
          </Badge>
        </div>
        <div className="text-xs text-muted mt-0.5">
          {typeLabel}
          {account.apiKey && <span className="ml-2 font-mono">· {account.apiKey}</span>}
        </div>
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {onLogin && (
          <Button variant="secondary" size="sm" onClick={onLogin}>
            Login
          </Button>
        )}
        <Button variant="secondary" size="sm" onClick={onEdit}>
          Edit
        </Button>
        <Button variant="secondary" size="sm" onClick={onTest} loading={testStatus === 'testing'}>
          Test
        </Button>
        {testStatus === 'ok' && <CircleCheck size={16} className="text-success" aria-label="Connection OK" />}
        {testStatus === 'fail' && <CircleX size={16} className="text-danger" aria-label="Connection failed" />}
        <Button variant="danger" size="sm" onClick={onDelete}>
          Delete
        </Button>
      </div>
    </Card>
  )
}

// ── AddProviderDropdown ──────────────────────────────────────────────────────

function AddProviderDropdown({ onSelect }: { onSelect: (type: string) => void }) {
  const [open, setOpen] = useState(false)
  const anchorRef = useRef<HTMLButtonElement>(null)

  return (
    <>
      <Button ref={anchorRef} variant="primary" icon={<Plus size={16} />} onClick={() => setOpen((v) => !v)}>
        Add provider
      </Button>
      <Menu open={open} onClose={() => setOpen(false)} anchorRef={anchorRef} aria-label="Select provider type">
        <MenuLabel>Select provider type</MenuLabel>
        {PROVIDER_TYPES.map((pt) => (
          <MenuItem
            key={pt.value}
            icon={<pt.icon size={16} />}
            onSelect={() => {
              setOpen(false)
              onSelect(pt.value)
            }}
          >
            {pt.label}
          </MenuItem>
        ))}
      </Menu>
    </>
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
    <Dialog
      open
      onClose={onClose}
      title={editId ? `Edit: ${editId}` : 'Add Provider Account'}
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" onClick={onSave} disabled={saving} loading={saving}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {!editId && (
          <div className="settings-field">
            <label className="settings-field-label">Account ID</label>
            <Input
              placeholder="my-account (lowercase, letters, numbers, hyphens)"
              value={form.id}
              onChange={(e) => setField('id', sanitizeAccountId(e.target.value))}
            />
          </div>
        )}

        <div className="settings-field">
          <label className="settings-field-label">Display Name</label>
          <Input placeholder="My Anthropic Account" value={form.displayName} onChange={handleDisplayNameChange} />
        </div>

        <div className="settings-field">
          <label className="settings-field-label">Provider Type</label>
          <Select value={form.type} onChange={(e) => setField('type', e.target.value)} options={PROVIDER_TYPES.map((t) => ({ value: t.value, label: t.label }))} />
        </div>

        {needsAPIKey && (
          <div className="settings-field">
            <label className="settings-field-label">API Key</label>
            <MaskedApiKeyInput
              maskedValue={editId ? (accounts.find((a) => a.id === editId)?.apiKey ?? '') : ''}
              onChange={(val) => {
                setField('apiKey', val)
                setField('apiKeyTouched', true)
              }}
            />
          </div>
        )}

        {showBaseUrl && (
          <div className="settings-field">
            <label className="settings-field-label">Base URL</label>
            <Input placeholder="https://api.example.com/v1" value={form.baseUrl} onChange={(e) => setField('baseUrl', e.target.value)} />
          </div>
        )}

        {showExtraHeaders && (
          <KeyValueEditor
            label="Extra Headers"
            pairs={form.extraHeaderPairs}
            onChange={(v) => setField('extraHeaderPairs', v)}
            keyPlaceholder="Header-Name"
            valuePlaceholder="value"
          />
        )}

        {showOAuth && (
          <div className="flex items-center gap-3">
            <Switch id="account-use-oauth" checked={form.useOAuth} onCheckedChange={(v) => setField('useOAuth', v)} />
            <label htmlFor="account-use-oauth" className="cursor-pointer">
              <div className="text-sm font-medium text-fg">Use OAuth</div>
              <div className="text-xs text-muted">Authenticate via OAuth instead of an API key</div>
            </label>
          </div>
        )}

        <div className="flex items-center gap-3">
          <Switch id="account-enabled" checked={!form.disabled} onCheckedChange={(v) => setField('disabled', !v)} />
          <label htmlFor="account-enabled" className="cursor-pointer">
            <div className="text-sm font-medium text-fg">Enabled</div>
            <div className="text-xs text-muted">Allow this account to be used for AI requests</div>
          </label>
        </div>
      </div>
    </Dialog>
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

    // Handle OAuth redirect results from query params.
    const params = new URLSearchParams(window.location.search)
    const authSuccess = params.get('authSuccess')
    const authError = params.get('authError')
    const authAccount = params.get('account')
    if (authSuccess) {
      toast.success(`Login successful${authAccount ? ` for "${authAccount}"` : ''}`)
      // Clean up URL params.
      const url = new URL(window.location.href)
      url.searchParams.delete('authSuccess')
      url.searchParams.delete('account')
      window.history.replaceState({}, '', url.toString())
    } else if (authError) {
      toast.error(`Login failed: ${authError}`)
      const url = new URL(window.location.href)
      url.searchParams.delete('authError')
      window.history.replaceState({}, '', url.toString())
    }
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
        // GitHub Copilot authenticates via the OAuth device-code flow. Launch it
        // automatically right after the account is created so the user gets the
        // verification code without having to run the login command manually.
        if (form.type === 'copilot') {
          await startCopilotLogin()
        }
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

  async function startCopilotLogin() {
    try {
      const result = await api.post<{ verificationUri: string; userCode: string }>(
        '/api/v1/auth/providers/copilot/login',
        {}
      )
      if (result.verificationUri) {
        window.open(result.verificationUri, '_blank', 'noopener,noreferrer')
      }
      // Persistent toast (ttl=0): the device code must stay visible until the
      // user finishes authorizing at github.com/login/device.
      toast.info(
        `GitHub Copilot — Enter code: ${result.userCode} at ${result.verificationUri}`,
        0
      )
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to start Copilot login')
    }
  }

  async function handleLogin(account: ProviderAccount) {
    try {
      const result = await api.post<{ authUrl: string; accountId: string }>(
        '/api/v1/config/provider-accounts/antigravity/start',
        { accountId: account.id, displayName: account.displayName }
      )
      if (result.authUrl) {
        window.open(result.authUrl, '_blank', 'noopener,noreferrer')
        toast.info(`Complete Google login for "${account.displayName}" in the opened browser tab.`)
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Failed to start login')
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
    <div>
      <header className="settings-page-header settings-page-header--row">
        <div className="settings-page-header-text">
          <h2 className="settings-page-title">Providers</h2>
          <p className="settings-page-description">
            Manage AI provider accounts. Each account has its own credentials and can be assigned to agents
            independently.
          </p>
        </div>
        <div className="settings-page-header-actions">
          <AddProviderDropdown onSelect={openAdd} />
        </div>
      </header>

      {loading && <div className="settings-loading">Loading…</div>}

      {!loading && accounts.length === 0 && (
        <EmptyState
          icon={<Plug size={20} />}
          title="No providers configured"
          description={'Use the "Add provider" button above to configure your first provider.'}
        />
      )}

      {!loading && accounts.length > 0 && (
        <div className="flex flex-col gap-2.5">
          {accounts.map((a) => (
            <AccountCard
              key={a.id}
              account={a}
              testStatus={testStatuses[a.id] ?? 'idle'}
              onEdit={() => openEdit(a)}
              onDelete={() => setConfirmDelete(a.id)}
              onTest={() => handleTest(a.id)}
              onLogin={TYPES_WITH_OAUTH.includes(a.type) && a.type === 'antigravity' ? () => handleLogin(a) : undefined}
            />
          ))}
        </div>
      )}

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
