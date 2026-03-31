import { useEffect, useState } from 'react'
import { useMCPServersStore } from '@/stores/mcpServersStore'
import type { MCPServerConfig, MCPToolInfo, MCPType } from '@/types'
import TagListEditor from '@/components/shared/TagListEditor'
import KeyValueEditor, { envToKV, kvToEnv, type KVPair } from '@/components/shared/KeyValueEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { TextInput } from '@/components/shared/FormInput'
import { useToast } from '@/stores/toastStore'

const MCP_TYPES: { value: MCPType; label: string }[] = [
  { value: 'stdio', label: 'stdio' },
  { value: 'sse', label: 'SSE' },
  { value: 'streamable-http', label: 'Streamable HTTP' },
]

const sectionTitle: React.CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

function ServerStatusBadge({ enabled }: { enabled: boolean }) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        padding: '0.125rem 0.5rem',
        borderRadius: 9999,
        fontSize: 11,
        fontWeight: 600,
        background: enabled ? 'var(--success)' : 'var(--secondary)',
        color: enabled ? 'white' : 'var(--fg)',
      }}
    >
      {enabled ? 'Enabled' : 'Disabled'}
    </span>
  )
}

interface ModalFormState {
  name: string
  command: string
  args: string[]
  envPairs: KVPair[]
  type: MCPType
  url: string
  enabled: boolean
}

function emptyForm(): ModalFormState {
  return {
    name: '',
    command: '',
    args: [],
    envPairs: [],
    type: 'stdio',
    url: '',
    enabled: true,
  }
}

function serverToForm(s: MCPServerConfig): ModalFormState {
  return {
    name: s.name,
    command: s.command,
    args: s.args ?? [],
    envPairs: envToKV(s.env ?? []),
    type: s.type ?? 'stdio',
    url: s.url ?? '',
    enabled: true, // env has no disabled field; treat all as enabled for display
  }
}

function formToServer(f: ModalFormState): MCPServerConfig {
  return {
    name: f.name,
    command: f.command,
    args: f.args,
    env: kvToEnv(f.envPairs),
    type: f.type,
    url: f.url,
    headers: {},
  }
}

function ToolsOverlay({ tools, serverName, onClose }: { tools: MCPToolInfo[]; serverName: string; onClose: () => void }) {
  return (
    <div
      style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1100 }}
      onClick={onClose}
    >
      <div
        style={{ background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)', padding: '1.5rem', width: 520, maxWidth: '95vw', maxHeight: '80vh', display: 'flex', flexDirection: 'column', boxShadow: '0 8px 32px rgba(0,0,0,0.4)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h3 style={{ fontSize: 15, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
            Tools — <span style={{ fontFamily: 'monospace' }}>{serverName}</span>
            <span style={{ marginLeft: 8, fontSize: 12, fontWeight: 400, color: 'var(--fg-muted)' }}>({tools.length})</span>
          </h3>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-muted)', fontSize: 18, lineHeight: 1 }}>×</button>
        </div>
        <div style={{ overflowY: 'auto', flex: 1 }}>
          {tools.length === 0 ? (
            <div style={{ color: 'var(--fg-muted)', fontSize: 13, padding: '0.5rem 0' }}>No tools discovered yet. Try reloading the server.</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.625rem' }}>
              {tools.map((t) => (
                <div key={t.name} style={{ padding: '0.625rem 0.75rem', background: 'var(--input-bg)', borderRadius: 'var(--radius-sm)', border: '1px solid var(--border)' }}>
                  <div style={{ fontFamily: 'monospace', fontSize: 13, fontWeight: 600, color: 'var(--fg)', marginBottom: t.description ? 4 : 0 }}>{t.name}</div>
                  {t.description && <div style={{ fontSize: 12, color: 'var(--fg-muted)', lineHeight: 1.5 }}>{t.description}</div>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default function MCPServersSettings() {
  const { servers, loading, saving, fetchServers, saveServer, deleteServer, reloadServer } =
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
    setForm((f) => ({ ...f, [key]: value }))
  }

  return (
    <div style={{ maxWidth: 800 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.25rem' }}>
        <h2 style={{ ...sectionTitle, marginBottom: 0 }}>MCP Servers</h2>
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
          + Add Server
        </button>
      </div>

      {loading && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
      )}

      {!loading && servers.length === 0 && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14, padding: '1rem 0' }}>
          No MCP servers configured. Add one to get started.
        </div>
      )}

      {!loading && servers.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Name', 'Type', 'Command / URL', 'Status', 'Actions'].map((h) => (
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
            {servers.map((s) => (
              <tr
                key={s.name}
                style={{ borderBottom: '1px solid var(--border)', verticalAlign: 'middle' }}
              >
                <td style={{ padding: '0.625rem 0.75rem', fontFamily: 'monospace', color: 'var(--fg)', fontWeight: 600 }}>
                  {s.name}
                </td>
                <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg-muted)' }}>
                  {s.type || 'stdio'}
                </td>
                <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg-muted)', fontFamily: 'monospace', maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {s.type === 'stdio' ? s.command : s.url}
                </td>
                <td style={{ padding: '0.625rem 0.75rem' }}>
                  <ServerStatusBadge enabled />
                </td>
                <td style={{ padding: '0.625rem 0.75rem' }}>
                  <div style={{ display: 'flex', gap: '0.375rem' }}>
                    <button
                      onClick={() => openEdit(s)}
                      style={actionBtn}
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => handleReload(s.name)}
                      disabled={reloading === s.name}
                      style={{ ...actionBtn, opacity: reloading === s.name ? 0.6 : 1 }}
                    >
                      {reloading === s.name ? '…' : 'Reload'}
                    </button>
                    <button
                      onClick={() => setConfirmDelete(s.name)}
                      style={{ ...actionBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
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
            style={{ background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)', padding: '1.5rem', width: 540, maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 8px 32px rgba(0,0,0,0.4)' }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
              {editName ? `Edit: ${editName}` : 'Add MCP Server'}
            </h3>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <TextInput
                label="Name"
                placeholder="my-server"
                value={form.name}
                onChange={(e) => setField('name', e.target.value)}
                disabled={!!editName}
              />

              {/* Type selector */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
                <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                  Type
                </label>
                <select
                  value={form.type}
                  onChange={(e) => setField('type', e.target.value as MCPType)}
                  style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 14, padding: '0.5rem 0.75rem', fontFamily: 'inherit', cursor: 'pointer' }}
                >
                  {MCP_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>{t.label}</option>
                  ))}
                </select>
              </div>

              {form.type === 'stdio' ? (
                <TextInput
                  label="Command"
                  placeholder="npx @modelcontextprotocol/server-filesystem"
                  value={form.command}
                  onChange={(e) => setField('command', e.target.value)}
                />
              ) : (
                <TextInput
                  label="URL"
                  placeholder="http://localhost:3000/mcp"
                  value={form.url}
                  onChange={(e) => setField('url', e.target.value)}
                />
              )}

              <TagListEditor
                label="Args"
                items={form.args}
                onChange={(v) => setField('args', v)}
                placeholder="Add argument…"
              />

              <KeyValueEditor
                label="Environment Variables"
                pairs={form.envPairs}
                onChange={(v) => setField('envPairs', v)}
                keyPlaceholder="ENV_VAR"
                valuePlaceholder="value"
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
    </div>
  )
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
