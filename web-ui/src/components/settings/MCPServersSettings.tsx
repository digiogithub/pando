import { useEffect, useState } from 'react'
import { useMCPServersStore } from '@pando/client/stores/mcpServersStore'
import type { MCPAuthType, MCPServerAuthConfig, MCPServerConfig, MCPToolInfo, MCPType } from '@pando/client/types'
import KeyValueEditor, { envToKV, kvToEnv, type KVPair } from '@/components/shared/KeyValueEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import MaskedInput from '@/components/shared/MaskedInput'
import { useToast } from '@pando/client/stores/toastStore'
import { Badge, Button, Dialog, Divider, EmptyState, IconButton, Input, Select, Textarea } from '@/components/ui'
import { LogIn, LogOut, Pencil, Plug, Plus, RefreshCw, Trash2 } from '@/components/ui/icons'

const MCP_TYPES: { value: MCPType; label: string }[] = [
  { value: 'stdio', label: 'stdio' },
  { value: 'sse', label: 'SSE' },
  { value: 'streamable-http', label: 'Streamable HTTP' },
]

const MCP_AUTH_TYPES: { value: MCPAuthType; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'bearer', label: 'Bearer token' },
  { value: 'basic', label: 'Basic (username/password)' },
  { value: 'header', label: 'Custom header' },
  { value: 'oauth', label: 'OAuth 2.1 (authorization code)' },
]

function emptyAuthForm(): MCPServerAuthConfig {
  return { type: 'none', hasToken: false, hasPassword: false }
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="settings-field">
      <label className="settings-field-label">{label}</label>
      {children}
    </div>
  )
}

/**
 * AuthStatusBadge renders the OAuth login state for one server (only
 * meaningful when authType === 'oauth'): ok / expired / needs login.
 */
function AuthStatusBadge({ authType, hasTokens, expired }: { authType: MCPAuthType; hasTokens: boolean; expired: boolean }) {
  if (authType !== 'oauth') return null
  if (hasTokens && expired) return <Badge tone="warning">OAuth expired</Badge>
  if (hasTokens) return <Badge tone="success">OAuth ok</Badge>
  return <Badge tone="neutral">needs login</Badge>
}

function ServerStatusBadge({ running }: { running: boolean }) {
  return <Badge tone={running ? 'success' : 'neutral'} dot>{running ? 'Running' : 'Stopped'}</Badge>
}

interface ModalFormState {
  name: string
  command: string
  args: string[]
  argsText: string
  envPairs: KVPair[]
  headerPairs: KVPair[]
  type: MCPType
  url: string
  enabled: boolean
  auth: MCPServerAuthConfig
}

function parseCommandLineArgs(input: string): string[] {
  const args: string[] = []
  let current = ''
  let quote: 'single' | 'double' | null = null

  for (let index = 0; index < input.length; index += 1) {
    const char = input[index]

    if (quote === 'single') {
      if (char === "'") {
        quote = null
      } else {
        current += char
      }
      continue
    }

    if (quote === 'double') {
      if (char === '"') {
        quote = null
        continue
      }
      if (char === '\\' && index + 1 < input.length) {
        const next = input[index + 1]
        if (next === '"' || next === '\\') {
          current += next
          index += 1
          continue
        }
      }
      current += char
      continue
    }

    if (/\s/.test(char)) {
      if (current) {
        args.push(current)
        current = ''
      }
      continue
    }

    if (char === "'") {
      quote = 'single'
      continue
    }

    if (char === '"') {
      quote = 'double'
      continue
    }

    if (char === '\\' && index + 1 < input.length) {
      current += input[index + 1]
      index += 1
      continue
    }

    current += char
  }

  if (current) {
    args.push(current)
  }

  return args
}

function formatArgsAsCommandLine(args: string[]): string {
  return args
    .map((arg) => {
      if (arg === '') {
        return '""'
      }
      if (/^[^\s"'\\]+$/.test(arg)) {
        return arg
      }
      return `"${arg.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
    })
    .join(' ')
}

function emptyForm(): ModalFormState {
  return {
    name: '',
    command: '',
    args: [],
    argsText: '',
    envPairs: [],
    headerPairs: [],
    type: 'stdio',
    url: '',
    enabled: true,
    auth: emptyAuthForm(),
  }
}

function serverToForm(s: MCPServerConfig): ModalFormState {
  return {
    name: s.name,
    command: s.command,
    args: s.args ?? [],
    argsText: formatArgsAsCommandLine(s.args ?? []),
    envPairs: envToKV(s.env ?? []),
    headerPairs: Object.entries(s.headers ?? {}).map(([key, value]) => ({ key, value })),
    type: s.type ?? 'stdio',
    url: s.url ?? '',
    enabled: true, // env has no disabled field; treat all as enabled for display
    auth: s.auth ? { ...s.auth, oauth: s.auth.oauth ? { ...s.auth.oauth } : undefined } : emptyAuthForm(),
  }
}

function formToServer(f: ModalFormState): MCPServerConfig {
  return {
    name: f.name,
    command: f.command,
    args: parseCommandLineArgs(f.argsText),
    env: kvToEnv(f.envPairs),
    type: f.type,
    url: f.url,
    headers: Object.fromEntries(f.headerPairs.filter((pair) => pair.key.trim()).map((pair) => [pair.key.trim(), pair.value])),
    auth: f.auth,
  }
}

function ToolsDialog({ tools, serverName, onClose }: { tools: MCPToolInfo[]; serverName: string; onClose: () => void }) {
  return (
    <Dialog
      open
      onClose={onClose}
      title={
        <>
          Tools — <span className="font-mono">{serverName}</span>{' '}
          <span className="text-xs font-normal text-muted">({tools.length})</span>
        </>
      }
      size="md"
    >
      {tools.length === 0 ? (
        <div className="text-sm text-muted py-2">No tools discovered yet. Try reloading the server.</div>
      ) : (
        <div className="flex flex-col gap-2.5">
          {tools.map((t) => (
            <div key={t.name} className="p-2.5 bg-input rounded-sm border border-border">
              <div className={`font-mono text-sm font-semibold text-fg ${t.description ? 'mb-1' : ''}`}>{t.name}</div>
              {t.description && <div className="text-xs text-muted leading-relaxed">{t.description}</div>}
            </div>
          ))}
        </div>
      )}
    </Dialog>
  )
}

export default function MCPServersSettings() {
  const { servers, loading, saving, authBusy, fetchServers, saveServer, deleteServer, reloadServer, loginServer, logoutServer } =
    useMCPServersStore()
  const toast = useToast()

  const [modalOpen, setModalOpen] = useState(false)
  const [editName, setEditName] = useState<string | null>(null) // null = adding new
  const [form, setForm] = useState<ModalFormState>(emptyForm())
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [reloading, setReloading] = useState<string | null>(null)
  const [toolsOverlay, setToolsOverlay] = useState<{ name: string; tools: MCPToolInfo[] } | null>(null)

  useEffect(() => {
    fetchServers()
  }, [fetchServers])

  function openAdd() {
    setEditName(null)
    setForm(emptyForm())
    setModalOpen(true)
  }

  function openEdit(s: MCPServerConfig) {
    setEditName(s.name)
    setForm(serverToForm(s))
    setModalOpen(true)
  }

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error('Name is required')
      return
    }
    const server = formToServer(form)
    await saveServer(server)
    setModalOpen(false)
  }

  async function handleDelete(name: string) {
    await deleteServer(name)
    setConfirmDelete(null)
  }

  async function handleReload(name: string) {
    setReloading(name)
    try {
      await reloadServer(name)
      toast.success(`Reload scheduled for ${name}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : 'Reload failed')
    } finally {
      setReloading(null)
    }
  }

  function setField<K extends keyof ModalFormState>(key: K, value: ModalFormState[K]) {
    setForm((f) => {
      if (key === 'argsText') {
        return { ...f, argsText: value as string, args: parseCommandLineArgs(value as string) }
      }
      return { ...f, [key]: value }
    })
  }

  function setAuthField<K extends keyof MCPServerAuthConfig>(key: K, value: MCPServerAuthConfig[K]) {
    setForm((f) => ({ ...f, auth: { ...f.auth, [key]: value } }))
  }

  function setOAuthField<K extends keyof NonNullable<MCPServerAuthConfig['oauth']>>(
    key: K,
    value: NonNullable<MCPServerAuthConfig['oauth']>[K],
  ) {
    setForm((f) => ({
      ...f,
      auth: {
        ...f.auth,
        oauth: { hasClientSecret: f.auth.oauth?.hasClientSecret ?? false, ...f.auth.oauth, [key]: value },
      },
    }))
  }

  const isRemoteServer = form.type === 'sse' || form.type === 'streamable-http'

  return (
    <div>
      <header className="settings-page-header settings-page-header--row">
        <div className="settings-page-header-text">
          <h2 className="settings-page-title">MCP Servers</h2>
          <p className="settings-page-description">Connect external tool servers over stdio, SSE or streamable HTTP.</p>
        </div>
        <div className="settings-page-header-actions">
          <Button variant="primary" icon={<Plus size={14} />} onClick={openAdd}>
            Add Server
          </Button>
        </div>
      </header>

      {loading && <div className="settings-loading">Loading…</div>}

      {!loading && servers.length === 0 && (
        <EmptyState
          icon={<Plug size={20} />}
          title="No MCP servers configured"
          description="Add one to get started."
          action={
            <Button variant="primary" icon={<Plus size={14} />} onClick={openAdd}>
              Add Server
            </Button>
          }
        />
      )}

      {!loading && servers.length > 0 && (
        <div className="settings-list">
          {servers.map((s) => (
            <div key={s.name} className="settings-list-row">
              <div className="settings-list-main">
                <div className="settings-list-title">
                  <span className="settings-list-name">{s.name}</span>
                  <Badge outline>{s.type || 'stdio'}</Badge>
                  <ServerStatusBadge running={Boolean(s.running)} />
                  <AuthStatusBadge
                    authType={s.auth?.type ?? 'none'}
                    hasTokens={Boolean(s.authStatus?.hasTokens)}
                    expired={Boolean(s.authStatus?.expired)}
                  />
                </div>
                <div className="settings-list-meta" title={s.type === 'stdio' ? s.command : s.url}>
                  {s.type === 'stdio' ? s.command : s.url}
                </div>
              </div>
              <button
                type="button"
                onClick={() => setToolsOverlay({ name: s.name, tools: s.tools ?? [] })}
                title="Click to view tools"
                className="settings-list-count"
              >
                {s.tools?.length ?? 0} tools
              </button>
              <div className="settings-list-actions">
                <IconButton aria-label={`Edit ${s.name}`} tooltip="Edit" size="sm" icon={<Pencil size={14} />} onClick={() => openEdit(s)} />
                <IconButton
                  aria-label={`Reload ${s.name}`}
                  tooltip="Reload"
                  size="sm"
                  icon={<RefreshCw size={14} />}
                  loading={reloading === s.name}
                  onClick={() => handleReload(s.name)}
                />
                {s.auth?.type === 'oauth' && (
                  <>
                    <IconButton
                      aria-label={s.authStatus?.hasTokens ? `Re-authorize ${s.name}` : `Login ${s.name}`}
                      tooltip="Opens the authorization URL in a new tab"
                      size="sm"
                      icon={<LogIn size={14} />}
                      loading={authBusy === s.name}
                      onClick={() => loginServer(s.name)}
                    />
                    {s.authStatus?.hasTokens && (
                      <IconButton
                        aria-label={`Logout ${s.name}`}
                        tooltip="Logout"
                        size="sm"
                        icon={<LogOut size={14} />}
                        loading={authBusy === s.name}
                        onClick={() => logoutServer(s.name)}
                      />
                    )}
                  </>
                )}
                <IconButton aria-label={`Delete ${s.name}`} tooltip="Delete" size="sm" icon={<Trash2 size={14} />} onClick={() => setConfirmDelete(s.name)} />
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add/Edit dialog */}
      <Dialog
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editName ? `Edit: ${editName}` : 'Add MCP Server'}
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={() => setModalOpen(false)}>
              Cancel
            </Button>
            <Button variant="primary" onClick={handleSave} disabled={saving} loading={saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Name">
            <Input placeholder="my-server" value={form.name} onChange={(e) => setField('name', e.target.value)} disabled={!!editName} />
          </Field>

          <Field label="Type">
            <Select options={MCP_TYPES} value={form.type} onChange={(e) => setField('type', e.target.value as MCPType)} />
          </Field>

          {form.type === 'stdio' ? (
            <Field label="Command">
              <Input
                placeholder="npx @modelcontextprotocol/server-filesystem"
                value={form.command}
                onChange={(e) => setField('command', e.target.value)}
              />
            </Field>
          ) : (
            <Field label="URL">
              <Input placeholder="http://localhost:3000/mcp" value={form.url} onChange={(e) => setField('url', e.target.value)} />
            </Field>
          )}

          {!isRemoteServer && (
            <Field label="Arguments">
              <Textarea
                placeholder={'--port 3000 --workspace "/path with spaces"'}
                value={form.argsText}
                onChange={(e) => setField('argsText', e.target.value)}
                rows={3}
              />
              <p className="text-xs text-muted leading-relaxed mt-1">
                Enter arguments as you would in a command line. They will be saved as an array of strings.
              </p>
            </Field>
          )}

          <KeyValueEditor
            label="Environment Variables"
            pairs={form.envPairs}
            onChange={(v) => setField('envPairs', v)}
            keyPlaceholder="ENV_VAR"
            valuePlaceholder="value"
          />

          {isRemoteServer && (
            <KeyValueEditor
              label="Headers"
              pairs={form.headerPairs}
              onChange={(v) => setField('headerPairs', v)}
              keyPlaceholder="Header-Name"
              valuePlaceholder="Header value"
            />
          )}

          {isRemoteServer && (
            <>
              <Divider className="my-1" />
              <div className="text-sm font-semibold text-fg">Authentication</div>

              <Field label="Auth Type">
                <Select options={MCP_AUTH_TYPES} value={form.auth.type} onChange={(e) => setAuthField('type', e.target.value as MCPAuthType)} />
              </Field>

              {(form.auth.type === 'bearer' || form.auth.type === 'header') && (
                <MaskedInput
                  label={form.auth.type === 'header' ? 'Header Value' : 'Bearer Token'}
                  placeholder={form.auth.hasToken ? 'Leave blank to keep the stored token' : 'Enter a token'}
                  value={form.auth.token ?? ''}
                  onChange={(value) => setAuthField('token', value)}
                />
              )}

              {form.auth.type === 'header' && (
                <Field label="Header Name">
                  <Input placeholder="Authorization" value={form.auth.headerName ?? ''} onChange={(e) => setAuthField('headerName', e.target.value)} />
                </Field>
              )}

              {form.auth.type === 'basic' && (
                <>
                  <Field label="Username">
                    <Input value={form.auth.username ?? ''} onChange={(e) => setAuthField('username', e.target.value)} />
                  </Field>
                  <MaskedInput
                    label="Password"
                    placeholder={form.auth.hasPassword ? 'Leave blank to keep the stored password' : 'Enter a password'}
                    value={form.auth.password ?? ''}
                    onChange={(value) => setAuthField('password', value)}
                  />
                </>
              )}

              {form.auth.type === 'oauth' && (
                <>
                  <p className="text-xs text-muted leading-relaxed">
                    OAuth 2.1 authorization-code flow with PKCE. Leave Client ID empty to rely on dynamic
                    client registration (RFC 7591) if the server supports it. Save this server first, then
                    use the Login button in the table to authorize it.
                  </p>
                  <Field label="Client ID">
                    <Input
                      placeholder="(optional — dynamic registration if empty)"
                      value={form.auth.oauth?.clientID ?? ''}
                      onChange={(e) => setOAuthField('clientID', e.target.value)}
                    />
                  </Field>
                  <MaskedInput
                    label="Client Secret"
                    placeholder={form.auth.oauth?.hasClientSecret ? 'Leave blank to keep the stored secret' : 'Only for confidential clients'}
                    value={form.auth.oauth?.clientSecret ?? ''}
                    onChange={(value) => setOAuthField('clientSecret', value)}
                  />
                  <Field label="Scopes">
                    <Input
                      placeholder="scope1 scope2"
                      value={(form.auth.oauth?.scopes ?? []).join(' ')}
                      onChange={(e) => setOAuthField('scopes', e.target.value.split(/\s+/).filter(Boolean))}
                    />
                  </Field>
                  <Field label="Callback Port">
                    <Input
                      type="number"
                      placeholder="19876"
                      value={form.auth.oauth?.callbackPort ? String(form.auth.oauth.callbackPort) : ''}
                      onChange={(e) => setOAuthField('callbackPort', e.target.value ? Number(e.target.value) : undefined)}
                    />
                  </Field>
                </>
              )}
            </>
          )}
        </div>
      </Dialog>

      {/* Delete confirm */}
      {confirmDelete && (
        <ConfirmDialog
          title="Delete MCP Server"
          message={`Are you sure you want to delete "${confirmDelete}"? This action cannot be undone.`}
          confirmLabel="Delete"
          dangerous
          onConfirm={() => handleDelete(confirmDelete)}
          onCancel={() => setConfirmDelete(null)}
        />
      )}

      {/* Tools dialog */}
      {toolsOverlay && (
        <ToolsDialog tools={toolsOverlay.tools} serverName={toolsOverlay.name} onClose={() => setToolsOverlay(null)} />
      )}
    </div>
  )
}
