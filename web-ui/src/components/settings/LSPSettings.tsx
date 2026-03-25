import { useEffect, useState } from 'react'
import { useLSPStore } from '@/stores/lspStore'
import type { LSPConfig } from '@/types'
import TagListEditor from '@/components/shared/TagListEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import { useToast } from '@/stores/toastStore'

const LANGUAGE_PRESETS = [
  { value: 'go', label: 'Go (gopls)' },
  { value: 'typescript', label: 'TypeScript (typescript-language-server)' },
  { value: 'python', label: 'Python (pylsp / pyright)' },
  { value: 'rust', label: 'Rust (rust-analyzer)' },
  { value: 'lua', label: 'Lua (lua-language-server)' },
  { value: 'c', label: 'C/C++ (clangd)' },
  { value: 'java', label: 'Java (jdtls)' },
  { value: 'custom', label: 'Custom…' },
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

interface ModalFormState {
  language: string
  command: string
  args: string[]
  languages: string[]
  disabled: boolean
}

function emptyForm(): ModalFormState {
  return { language: '', command: '', args: [], languages: [], disabled: false }
}

function configToForm(c: LSPConfig): ModalFormState {
  return {
    language: c.language,
    command: c.command,
    args: c.args ?? [],
    languages: c.languages ?? [],
    disabled: c.disabled,
  }
}

export default function LSPSettings() {
  const { configs, loading, saving, fetchLSP, saveLSP, deleteLSP } = useLSPStore()
  const toast = useToast()

  const [modalOpen, setModalOpen] = useState(false)
  const [editLang, setEditLang] = useState<string | null>(null)
  const [form, setForm] = useState<ModalFormState>(emptyForm())
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [testing, setTesting] = useState<string | null>(null)

  useEffect(() => {
    fetchLSP()
  }, [fetchLSP])

  function openAdd() {
    setEditLang(null)
    setForm(emptyForm())
    setModalOpen(true)
  }

  function openEdit(c: LSPConfig) {
    setEditLang(c.language)
    setForm(configToForm(c))
    setModalOpen(true)
  }

  async function handleSave() {
    if (!form.language.trim()) {
      toast.error('Language key is required')
      return
    }
    if (!form.command.trim()) {
      toast.error('Command is required')
      return
    }
    const config: LSPConfig = {
      language: form.language.trim(),
      command: form.command.trim(),
      args: form.args,
      languages: form.languages,
      disabled: form.disabled,
    }
    await saveLSP(config)
    setModalOpen(false)
  }

  async function handleDelete(language: string) {
    await deleteLSP(language)
    setConfirmDelete(null)
  }

  function handleTestConnection(c: LSPConfig) {
    setTesting(c.language)
    // Simple client-side check: verify command is non-empty
    setTimeout(() => {
      if (!c.command.trim()) {
        toast.error(`${c.language}: command is empty`)
      } else {
        toast.info(`${c.language}: command is "${c.command}" (binary check requires server-side validation)`)
      }
      setTesting(null)
    }, 300)
  }

  function setField<K extends keyof ModalFormState>(key: K, value: ModalFormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  return (
    <div style={{ maxWidth: 800 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1.25rem' }}>
        <h2 style={{ ...sectionTitle, marginBottom: 0 }}>Language Servers (LSP)</h2>
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
          + Add LSP
        </button>
      </div>

      {loading && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
      )}

      {!loading && configs.length === 0 && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14, padding: '1rem 0' }}>
          No language servers configured. Add one to enable LSP features.
        </div>
      )}

      {!loading && configs.length > 0 && (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Language', 'Command', 'Args', 'Status', 'Actions'].map((h) => (
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
            {configs.map((c) => (
              <tr
                key={c.language}
                style={{ borderBottom: '1px solid var(--border)', verticalAlign: 'middle' }}
              >
                <td style={{ padding: '0.625rem 0.75rem', fontFamily: 'monospace', color: 'var(--fg)', fontWeight: 600 }}>
                  {c.language}
                </td>
                <td style={{ padding: '0.625rem 0.75rem', fontFamily: 'monospace', color: 'var(--fg-muted)', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {c.command}
                </td>
                <td style={{ padding: '0.625rem 0.75rem', color: 'var(--fg-muted)' }}>
                  {(c.args ?? []).length > 0 ? (
                    <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{c.args.join(' ')}</span>
                  ) : (
                    <span style={{ color: 'var(--fg-dim)', fontSize: 12 }}>—</span>
                  )}
                </td>
                <td style={{ padding: '0.625rem 0.75rem' }}>
                  <span
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      padding: '0.125rem 0.5rem',
                      borderRadius: 9999,
                      fontSize: 11,
                      fontWeight: 600,
                      background: c.disabled ? 'var(--secondary)' : 'var(--success)',
                      color: c.disabled ? 'var(--fg)' : 'white',
                    }}
                  >
                    {c.disabled ? 'Disabled' : 'Enabled'}
                  </span>
                </td>
                <td style={{ padding: '0.625rem 0.75rem' }}>
                  <div style={{ display: 'flex', gap: '0.375rem' }}>
                    <button onClick={() => openEdit(c)} style={actionBtn}>Edit</button>
                    <button
                      onClick={() => handleTestConnection(c)}
                      disabled={testing === c.language}
                      style={{ ...actionBtn, opacity: testing === c.language ? 0.6 : 1 }}
                    >
                      {testing === c.language ? '…' : 'Test'}
                    </button>
                    <button
                      onClick={() => setConfirmDelete(c.language)}
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
            style={{ background: 'var(--card-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)', padding: '1.5rem', width: 520, maxWidth: '95vw', maxHeight: '90vh', overflowY: 'auto', boxShadow: '0 8px 32px rgba(0,0,0,0.4)' }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
              {editLang ? `Edit: ${editLang}` : 'Add Language Server'}
            </h3>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {/* Language key: preset selector when adding */}
              {!editLang ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
                  <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
                    Language
                  </label>
                  <select
                    value={form.language}
                    onChange={(e) => {
                      const val = e.target.value
                      if (val === 'custom') {
                        setField('language', '')
                      } else {
                        setField('language', val)
                      }
                    }}
                    style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 14, padding: '0.5rem 0.75rem', fontFamily: 'inherit', cursor: 'pointer', marginBottom: '0.25rem' }}
                  >
                    <option value="">Select language…</option>
                    {LANGUAGE_PRESETS.map((p) => (
                      <option key={p.value} value={p.value}>{p.label}</option>
                    ))}
                  </select>
                  <input
                    value={form.language}
                    onChange={(e) => setField('language', e.target.value)}
                    placeholder="or type a custom key (e.g. kotlin)"
                    style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 13, padding: '0.5rem 0.75rem', fontFamily: 'monospace', outline: 'none' }}
                    onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
                    onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
                  />
                </div>
              ) : (
                <TextInput
                  label="Language"
                  value={form.language}
                  onChange={(e) => setField('language', e.target.value)}
                  disabled
                />
              )}

              <TextInput
                label="Command"
                placeholder="gopls"
                value={form.command}
                onChange={(e) => setField('command', e.target.value)}
              />

              <TagListEditor
                label="Args"
                items={form.args}
                onChange={(v) => setField('args', v)}
                placeholder="Add argument…"
              />

              <TagListEditor
                label="Language file extensions (optional)"
                items={form.languages}
                onChange={(v) => setField('languages', v)}
                placeholder=".go"
              />

              <Toggle
                label="Disabled"
                description="Disable this language server without removing it"
                checked={form.disabled}
                onChange={(v) => setField('disabled', v)}
              />
            </div>

            <div style={dividerStyle} />

            <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
              <button
                onClick={() => setModalOpen(false)}
                style={{ padding: '0.5rem 1.25rem', background: 'transparent', color: 'var(--fg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', fontSize: 14, cursor: 'pointer', fontFamily: 'inherit' }}
              >
                Cancel
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                style={{ padding: '0.5rem 1.25rem', background: 'var(--primary)', color: 'var(--primary-fg)', border: 'none', borderRadius: 'var(--radius-sm)', fontSize: 14, fontWeight: 600, cursor: saving ? 'not-allowed' : 'pointer', fontFamily: 'inherit', opacity: saving ? 0.7 : 1 }}
              >
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirm */}
      {confirmDelete && (
        <ConfirmDialog
          title="Delete Language Server"
          message={`Remove LSP configuration for "${confirmDelete}"?`}
          confirmLabel="Delete"
          dangerous
          onConfirm={() => handleDelete(confirmDelete)}
          onCancel={() => setConfirmDelete(null)}
        />
      )}
    </div>
  )
}
