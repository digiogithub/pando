import { useEffect, useMemo, useState } from 'react'
import { useLSPStore } from '@pando/client/stores/lspStore'
import type { LSPConfig, LSPServerStatus } from '@pando/client/types'
import TagListEditor from '@/components/shared/TagListEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { TextInput, Toggle } from '@/components/shared/FormInput'
import { useToast } from '@pando/client/stores/toastStore'

const ACTIVATE_ON_OPTIONS = [
  { value: 'edits', label: 'edits — files Pando edits (default)' },
  { value: 'reads', label: 'reads — also files it opens or views' },
  { value: 'workspace', label: 'workspace — also files changed outside Pando' },
  { value: 'off', label: 'off — never start a server on demand' },
]

const RUNNER_OPTIONS = [
  { value: 'auto', label: 'auto — bun when available, npm otherwise (default)' },
  { value: 'bun', label: 'bun — only bun, never npm' },
  { value: 'npm', label: 'npm — only npm/npx, even if bun is installed' },
  { value: 'off', label: 'off — never use bun or npm' },
]

/** Colour of the availability badge: installed, installable by Pando, or manual. */
function availabilityColor(availability: string): string {
  switch (availability) {
    case 'installed':
      return 'var(--success)'
    case 'installable':
      return 'var(--primary)'
    default:
      return 'var(--secondary)'
  }
}

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
  filenames: string[]
  disabled: boolean
  autostart: boolean
}

function emptyForm(): ModalFormState {
  return { language: '', command: '', args: [], languages: [], filenames: [], disabled: false, autostart: false }
}

function configToForm(c: LSPConfig): ModalFormState {
  return {
    language: c.language,
    command: c.command,
    args: c.args ?? [],
    languages: c.languages ?? [],
    filenames: c.filenames ?? [],
    disabled: c.disabled,
    autostart: c.autostart ?? false,
  }
}

/** Prefill the form from a catalogue preset the user is enabling. */
function statusToForm(s: LSPServerStatus): ModalFormState {
  return {
    language: s.name,
    command: s.command,
    args: s.args ?? [],
    languages: s.languages ?? [],
    filenames: s.filenames ?? [],
    disabled: false,
    autostart: s.autostart,
  }
}

export default function LSPSettings() {
  const { configs, catalog, activation, loading, saving, fetchLSP, saveLSP, deleteLSP, saveActivation } =
    useLSPStore()
  const toast = useToast()

  const [modalOpen, setModalOpen] = useState(false)
  const [editLang, setEditLang] = useState<string | null>(null)
  const [form, setForm] = useState<ModalFormState>(emptyForm())
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [testing, setTesting] = useState<string | null>(null)
  const [showCatalog, setShowCatalog] = useState(false)
  const [startupTimeout, setStartupTimeout] = useState(activation.startupTimeout)
  const [installTimeout, setInstallTimeout] = useState(activation.installTimeout)

  useEffect(() => {
    fetchLSP()
  }, [fetchLSP])

  useEffect(() => {
    setStartupTimeout(activation.startupTimeout)
    setInstallTimeout(activation.installTimeout)
  }, [activation.startupTimeout, activation.installTimeout])

  // Servers the user has not configured: the rest of the built-in catalogue.
  const available = useMemo(() => catalog.filter((s) => !s.configured), [catalog])
  const statusByName = useMemo(() => {
    const map: Record<string, LSPServerStatus> = {}
    for (const s of catalog) map[s.name] = s
    return map
  }, [catalog])

  function openAdd() {
    setEditLang(null)
    setForm(emptyForm())
    setModalOpen(true)
  }

  function enablePreset(s: LSPServerStatus) {
    setEditLang(null)
    setForm(statusToForm(s))
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
      filenames: form.filenames,
      disabled: form.disabled,
      autostart: form.autostart,
    }
    await saveLSP(config)
    setModalOpen(false)
  }

  async function handleDelete(language: string) {
    await deleteLSP(language)
    setConfirmDelete(null)
  }

  // "Test" now reports what the server actually resolved to: the binary Pando
  // would spawn, or why it cannot obtain one.
  function handleTestConnection(c: LSPConfig) {
    setTesting(c.language)
    const status = statusByName[c.language]
    if (!status) {
      toast.info(`${c.language}: no status available yet`)
    } else if (status.availability === 'installed') {
      toast.info(`${c.language}: ${status.resolvedCommand || status.command} (${status.runState})`)
    } else if (status.availability === 'installable') {
      toast.info(`${c.language}: ${status.availabilityLabel} — Pando will install it on first use`)
    } else {
      toast.error(`${c.language}: ${status.reason || 'not installed'}`)
    }
    setTesting(null)
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

      {/* Global on-demand activation */}
      <div
        style={{
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-md)',
          padding: '1rem',
          marginBottom: '1.5rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.875rem',
        }}
      >
        <Toggle
          label="On-demand activation"
          description="Start a language server when Pando touches a file it handles, instead of at boot"
          checked={activation.autoActivate}
          onChange={(v) => saveActivation({ ...activation, autoActivate: v })}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Activate on
          </label>
          <select
            value={activation.activateOn}
            disabled={!activation.autoActivate || saving}
            onChange={(e) => saveActivation({ ...activation, activateOn: e.target.value })}
            style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 13, padding: '0.5rem 0.75rem', fontFamily: 'inherit', cursor: 'pointer' }}
          >
            {ACTIVATE_ON_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
        </div>

        <Toggle
          label="Install servers automatically"
          description="Install npm-distributed servers with bun or npm when their binary is missing"
          checked={activation.autoInstall}
          onChange={(v) => saveActivation({ ...activation, autoInstall: v })}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
          <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
            Package manager
          </label>
          <select
            value={activation.runner}
            disabled={saving}
            onChange={(e) => saveActivation({ ...activation, runner: e.target.value })}
            style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 13, padding: '0.5rem 0.75rem', fontFamily: 'inherit', cursor: 'pointer' }}
          >
            {RUNNER_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
            With “off”, npm-distributed servers are only used when their binary is already on PATH.
          </span>
        </div>

        <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
          <div style={{ flex: '1 1 160px' }}>
            <TextInput
              label="Startup timeout"
              value={startupTimeout}
              placeholder="20s"
              onChange={(e) => setStartupTimeout(e.target.value)}
              onBlur={() => {
                if (startupTimeout !== activation.startupTimeout) {
                  saveActivation({ ...activation, startupTimeout })
                }
              }}
            />
          </div>
          <div style={{ flex: '1 1 160px' }}>
            <TextInput
              label="Install timeout"
              value={installTimeout}
              placeholder="2m"
              onChange={(e) => setInstallTimeout(e.target.value)}
              onBlur={() => {
                if (installTimeout !== activation.installTimeout) {
                  saveActivation({ ...activation, installTimeout })
                }
              }}
            />
          </div>
        </div>
      </div>

      {loading && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
      )}

      {!loading && configs.length === 0 && (
        <div style={{ color: 'var(--fg-muted)', fontSize: 14, padding: '1rem 0' }}>
          No language server configured explicitly. Pando still activates the built-in catalogue below on demand.
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
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', alignItems: 'flex-start' }}>
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
                    {statusByName[c.language] && (
                      <span
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          padding: '0.125rem 0.5rem',
                          borderRadius: 9999,
                          fontSize: 11,
                          fontWeight: 600,
                          background: availabilityColor(statusByName[c.language].availability),
                          color: statusByName[c.language].availability === 'manual' ? 'var(--fg)' : 'white',
                        }}
                        title={statusByName[c.language].reason || statusByName[c.language].hint || ''}
                      >
                        {statusByName[c.language].availabilityLabel}
                      </span>
                    )}
                    {statusByName[c.language]?.runState === 'ready' && (
                      <span style={{ fontSize: 11, color: 'var(--fg-muted)' }}>running</span>
                    )}
                  </div>
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

      {/* Built-in catalogue */}
      {!loading && available.length > 0 && (
        <div style={{ marginTop: '1.5rem' }}>
          <button
            onClick={() => setShowCatalog((v) => !v)}
            style={{ ...actionBtn, fontSize: 13 }}
          >
            {showCatalog ? '▾' : '▸'} Built-in catalogue ({available.length} more servers)
          </button>

          {showCatalog && (
            <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>
                These servers are activated on demand without any configuration. Servers marked
                <strong> installable</strong> are installed by Pando with bun or npm the first time they are needed;
                <strong> opt-in</strong> servers stay off until you add them.
              </div>
              {available.map((s) => (
                <div
                  key={s.name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: '0.75rem',
                    border: '1px solid var(--border)',
                    borderRadius: 'var(--radius-sm)',
                    padding: '0.5rem 0.75rem',
                  }}
                >
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                      <span style={{ fontFamily: 'monospace', fontWeight: 600, color: 'var(--fg)', fontSize: 13 }}>
                        {s.name}
                      </span>
                      <span
                        style={{
                          padding: '0.125rem 0.5rem',
                          borderRadius: 9999,
                          fontSize: 11,
                          fontWeight: 600,
                          background: availabilityColor(s.availability),
                          color: s.availability === 'manual' ? 'var(--fg)' : 'white',
                        }}
                      >
                        {s.availabilityLabel}
                      </span>
                      {s.optIn && (
                        <span style={{ fontSize: 11, color: 'var(--fg-muted)', fontStyle: 'italic' }}>opt-in</span>
                      )}
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginTop: '0.125rem' }}>
                      {s.description}
                      {(s.languages ?? []).length > 0 && ` · ${(s.languages ?? []).join(' ')}`}
                      {(s.filenames ?? []).length > 0 && ` · ${(s.filenames ?? []).join(' ')}`}
                    </div>
                    {s.availability === 'manual' && (s.hint || s.reason) && (
                      <div style={{ fontSize: 12, color: 'var(--fg-dim)', marginTop: '0.125rem', fontFamily: 'monospace' }}>
                        {s.hint ? `run: ${s.hint}` : s.reason}
                      </div>
                    )}
                  </div>
                  <button onClick={() => enablePreset(s)} style={actionBtn}>
                    {s.optIn ? 'Enable' : 'Customize'}
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
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
                    value={statusByName[form.language] ? form.language : ''}
                    onChange={(e) => {
                      const preset = statusByName[e.target.value]
                      if (preset) {
                        setForm(statusToForm(preset))
                      } else {
                        setField('language', '')
                      }
                    }}
                    style={{ background: 'var(--input-bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', color: 'var(--fg)', fontSize: 14, padding: '0.5rem 0.75rem', fontFamily: 'inherit', cursor: 'pointer', marginBottom: '0.25rem' }}
                  >
                    <option value="">Select a catalogue server…</option>
                    {available.map((s) => (
                      <option key={s.name} value={s.name}>
                        {s.name} — {s.description} [{s.availabilityLabel}]
                      </option>
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

              <TagListEditor
                label="File names (optional)"
                items={form.filenames}
                onChange={(v) => setField('filenames', v)}
                placeholder="Dockerfile"
              />

              <Toggle
                label="Autostart"
                description="Start this server at boot instead of waiting for a matching file"
                checked={form.autostart}
                onChange={(v) => setField('autostart', v)}
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
