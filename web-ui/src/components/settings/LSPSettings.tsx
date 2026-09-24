import { useEffect, useMemo, useState } from 'react'
import { useLSPStore } from '@pando/client/stores/lspStore'
import type { LSPConfig, LSPServerStatus } from '@pando/client/types'
import TagListEditor from '@/components/shared/TagListEditor'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import { useToast } from '@pando/client/stores/toastStore'
import { Badge, Button, Dialog, IconButton, Input, Select, SettingsRow, SettingsSection, Switch, type BadgeTone } from '@/components/ui'
import { ChevronDown, ChevronRight, Pencil, Plus, RefreshCw, Trash2 } from '@/components/ui/icons'

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

/** Tone of the availability badge: installed, installable by Pando, or manual. */
function availabilityTone(availability: string): BadgeTone {
  switch (availability) {
    case 'installed':
      return 'success'
    case 'installable':
      return 'accent'
    default:
      return 'neutral'
  }
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
    <div>
      <header className="settings-page-header settings-page-header--row">
        <div className="settings-page-header-text">
          <h2 className="settings-page-title">Language Servers (LSP)</h2>
          <p className="settings-page-description">Configure LSP servers Pando starts to read diagnostics and navigate code.</p>
        </div>
        <div className="settings-page-header-actions">
          <Button variant="primary" icon={<Plus size={14} />} onClick={openAdd}>
            Add LSP
          </Button>
        </div>
      </header>

      <SettingsSection title="On-demand activation">
        <SettingsRow
          label="On-demand activation"
          description="Start a language server when Pando touches a file it handles, instead of at boot"
          htmlFor="lsp-auto-activate"
        >
          <Switch
            id="lsp-auto-activate"
            checked={activation.autoActivate}
            onCheckedChange={(v) => saveActivation({ ...activation, autoActivate: v })}
          />
        </SettingsRow>
        <SettingsRow label="Activate on" htmlFor="lsp-activate-on">
          <Select
            id="lsp-activate-on"
            value={activation.activateOn}
            options={ACTIVATE_ON_OPTIONS}
            disabled={!activation.autoActivate || saving}
            onChange={(e) => saveActivation({ ...activation, activateOn: e.target.value })}
          />
        </SettingsRow>
        <SettingsRow
          label="Install servers automatically"
          description="Install npm-distributed servers with bun or npm when their binary is missing"
          htmlFor="lsp-auto-install"
        >
          <Switch
            id="lsp-auto-install"
            checked={activation.autoInstall}
            onCheckedChange={(v) => saveActivation({ ...activation, autoInstall: v })}
          />
        </SettingsRow>
        <SettingsRow
          label="Package manager"
          description={'With "off", npm-distributed servers are only used when their binary is already on PATH.'}
          htmlFor="lsp-runner"
        >
          <Select
            id="lsp-runner"
            value={activation.runner}
            options={RUNNER_OPTIONS}
            disabled={saving}
            onChange={(e) => saveActivation({ ...activation, runner: e.target.value })}
          />
        </SettingsRow>
        <SettingsRow label="Timeouts" stacked>
          <div className="settings-field-grid">
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="lsp-startup-timeout">Startup timeout</label>
              <Input
                id="lsp-startup-timeout"
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
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="lsp-install-timeout">Install timeout</label>
              <Input
                id="lsp-install-timeout"
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
        </SettingsRow>
      </SettingsSection>

      {loading && <div className="settings-loading">Loading…</div>}

      {!loading && configs.length === 0 && (
        <div className="settings-empty-row">
          No language server configured explicitly. Pando still activates the built-in catalogue below on demand.
        </div>
      )}

      {!loading && configs.length > 0 && (
        <SettingsSection title="Configured servers">
          {configs.map((c) => {
            const status = statusByName[c.language]
            return (
              <div className="ui-settings-row" key={c.language}>
                <div className="ui-settings-row-text">
                  <span className="ui-settings-row-label font-mono">{c.language}</span>
                  <p className="ui-settings-row-description font-mono truncate">
                    {c.command}
                    {(c.args ?? []).length > 0 && <span className="text-faint"> {c.args.join(' ')}</span>}
                  </p>
                </div>
                <div className="ui-settings-row-control flex-wrap justify-end">
                  <Badge tone={c.disabled ? 'neutral' : 'success'}>{c.disabled ? 'Disabled' : 'Enabled'}</Badge>
                  {status && (
                    <Badge tone={availabilityTone(status.availability)} title={status.reason || status.hint || ''}>
                      {status.availabilityLabel}
                    </Badge>
                  )}
                  {status?.runState === 'ready' && <Badge tone="info">running</Badge>}
                  <IconButton aria-label={`Edit ${c.language}`} tooltip icon={<Pencil size={14} />} size="sm" onClick={() => openEdit(c)} />
                  <IconButton
                    aria-label={`Test ${c.language}`}
                    tooltip
                    icon={<RefreshCw size={14} />}
                    size="sm"
                    loading={testing === c.language}
                    onClick={() => handleTestConnection(c)}
                  />
                  <IconButton
                    aria-label={`Delete ${c.language}`}
                    tooltip
                    variant="danger"
                    icon={<Trash2 size={14} />}
                    size="sm"
                    onClick={() => setConfirmDelete(c.language)}
                  />
                </div>
              </div>
            )
          })}
        </SettingsSection>
      )}

      {/* Built-in catalogue */}
      {!loading && available.length > 0 && (
        <SettingsSection>
          <div className="p-2">
            <Button variant="ghost" size="sm" icon={showCatalog ? <ChevronDown size={14} /> : <ChevronRight size={14} />} onClick={() => setShowCatalog((v) => !v)}>
              Built-in catalogue ({available.length} more servers)
            </Button>

            {showCatalog && (
              <div className="mt-2 flex flex-col gap-2">
                <p className="text-xs text-muted px-2">
                  These servers are activated on demand without any configuration. Servers marked
                  <strong className="text-fg"> installable</strong> are installed by Pando with bun or npm the first
                  time they are needed; <strong className="text-fg">opt-in</strong> servers stay off until you add
                  them.
                </p>
                {available.map((s) => (
                  <div key={s.name} className="ui-settings-row">
                    <div className="ui-settings-row-text">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="ui-settings-row-label font-mono">{s.name}</span>
                        <Badge tone={availabilityTone(s.availability)}>{s.availabilityLabel}</Badge>
                        {s.optIn && <span className="text-xs text-muted italic">opt-in</span>}
                      </div>
                      <p className="ui-settings-row-description">
                        {s.description}
                        {(s.languages ?? []).length > 0 && ` · ${(s.languages ?? []).join(' ')}`}
                        {(s.filenames ?? []).length > 0 && ` · ${(s.filenames ?? []).join(' ')}`}
                      </p>
                      {s.availability === 'manual' && (s.hint || s.reason) && (
                        <p className="ui-settings-row-description font-mono">{s.hint ? `run: ${s.hint}` : s.reason}</p>
                      )}
                    </div>
                    <div className="ui-settings-row-control">
                      <Button variant="secondary" size="sm" onClick={() => enablePreset(s)}>
                        {s.optIn ? 'Enable' : 'Customize'}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </SettingsSection>
      )}

      {/* Add/Edit Modal */}
      <Dialog
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editLang ? `Edit: ${editLang}` : 'Add Language Server'}
        size="md"
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
          {!editLang ? (
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="lsp-form-preset">Language</label>
              <Select
                id="lsp-form-preset"
                value={statusByName[form.language] ? form.language : ''}
                onChange={(e) => {
                  const preset = statusByName[e.target.value]
                  if (preset) {
                    setForm(statusToForm(preset))
                  } else {
                    setField('language', '')
                  }
                }}
                options={[
                  { value: '', label: 'Select a catalogue server…' },
                  ...available.map((s) => ({ value: s.name, label: `${s.name} — ${s.description} [${s.availabilityLabel}]` })),
                ]}
              />
              <Input
                className="font-mono mt-1"
                value={form.language}
                onChange={(e) => setField('language', e.target.value)}
                placeholder="or type a custom key (e.g. kotlin)"
              />
            </div>
          ) : (
            <div className="settings-field">
              <label className="settings-field-label" htmlFor="lsp-form-language">Language</label>
              <Input id="lsp-form-language" value={form.language} disabled />
            </div>
          )}

          <div className="settings-field">
            <label className="settings-field-label" htmlFor="lsp-form-command">Command</label>
            <Input id="lsp-form-command" placeholder="gopls" value={form.command} onChange={(e) => setField('command', e.target.value)} />
          </div>

          <TagListEditor items={form.args} onChange={(v) => setField('args', v)} placeholder="Add argument…" />

          <TagListEditor items={form.languages} onChange={(v) => setField('languages', v)} placeholder=".go" />

          <TagListEditor items={form.filenames} onChange={(v) => setField('filenames', v)} placeholder="Dockerfile" />

          <div className="flex items-center gap-3">
            <Switch id="lsp-form-autostart" checked={form.autostart} onCheckedChange={(v) => setField('autostart', v)} />
            <label htmlFor="lsp-form-autostart" className="cursor-pointer">
              <div className="text-sm font-medium text-fg">Autostart</div>
              <div className="text-xs text-muted">Start this server at boot instead of waiting for a matching file</div>
            </label>
          </div>

          <div className="flex items-center gap-3">
            <Switch id="lsp-form-disabled" checked={form.disabled} onCheckedChange={(v) => setField('disabled', v)} />
            <label htmlFor="lsp-form-disabled" className="cursor-pointer">
              <div className="text-sm font-medium text-fg">Disabled</div>
              <div className="text-xs text-muted">Disable this language server without removing it</div>
            </label>
          </div>
        </div>
      </Dialog>

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
