import { useEffect, useState } from 'react'
import { useExtensionsStore } from '@/stores/extensionsStore'
import { Toggle, TextInput, SelectInput } from '@/components/shared/FormInput'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import api from '@/services/api'
import type { SkillCatalogItem } from '@/types'

const SCOPE_OPTIONS = [
  { value: 'session', label: 'Session' },
  { value: 'global', label: 'Global' },
  { value: 'project', label: 'Project' },
]

const dividerStyle: React.CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const sectionTitle: React.CSSProperties = {
  fontSize: 16,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1rem',
}

const subTitle: React.CSSProperties = {
  fontSize: 13,
  fontWeight: 600,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase' as const,
  letterSpacing: '0.04em',
  marginBottom: '0.75rem',
}

// ---- TagListEditor ----
// Inline tag-list editor for string arrays (paths, patterns, etc.)
function TagListEditor({
  label,
  values,
  onChange,
  placeholder = 'Add item…',
}: {
  label: string
  values: string[]
  onChange: (v: string[]) => void
  placeholder?: string
}) {
  const [input, setInput] = useState('')

  const add = () => {
    const trimmed = input.trim()
    if (trimmed && !values.includes(trimmed)) {
      onChange([...values, trimmed])
    }
    setInput('')
  }

  const remove = (idx: number) => {
    onChange(values.filter((_, i) => i !== idx))
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      <label
        style={{
          fontSize: 12,
          fontWeight: 600,
          color: 'var(--fg-muted)',
          textTransform: 'uppercase',
          letterSpacing: '0.04em',
        }}
      >
        {label}
      </label>

      {/* Tag list */}
      {values.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem' }}>
          {values.map((v, i) => (
            <span
              key={i}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.3rem',
                background: 'var(--selected)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-sm)',
                padding: '0.2rem 0.5rem',
                fontSize: 13,
                color: 'var(--fg)',
              }}
            >
              {v}
              <button
                onClick={() => remove(i)}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  color: 'var(--fg-muted)',
                  padding: 0,
                  lineHeight: 1,
                  fontSize: 14,
                  fontFamily: 'inherit',
                }}
                aria-label={`Remove ${v}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Add input */}
      <div style={{ display: 'flex', gap: '0.5rem' }}>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
          placeholder={placeholder}
          style={{
            flex: 1,
            background: 'var(--input-bg)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--fg)',
            fontSize: 14,
            padding: '0.4rem 0.75rem',
            outline: 'none',
            fontFamily: 'inherit',
          }}
          onFocus={(e) => {
            e.target.style.borderColor = 'var(--border-focus)'
          }}
          onBlur={(e) => {
            e.target.style.borderColor = 'var(--border)'
          }}
        />
        <button
          onClick={add}
          style={{
            padding: '0.4rem 1rem',
            background: 'var(--primary)',
            color: 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            cursor: 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Add
        </button>
      </div>
    </div>
  )
}

// ---- Catalog Modal ----
function CatalogModal({
  onClose,
  installedNames,
  onInstall,
}: {
  onClose: () => void
  installedNames: string[]
  onInstall: (name: string) => void
}) {
  const [items, setItems] = useState<SkillCatalogItem[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [installing, setInstalling] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .get<{ skills: SkillCatalogItem[] }>('/api/v1/skills/catalog')
      .then((data) => setItems(data.skills ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load catalog'))
      .finally(() => setLoading(false))
  }, [])

  const filtered = items.filter(
    (item) =>
      item.name.toLowerCase().includes(search.toLowerCase()) ||
      item.description?.toLowerCase().includes(search.toLowerCase()),
  )

  const handleInstall = async (name: string) => {
    setInstalling(name)
    try {
      await api.post('/api/v1/skills/install', { name })
      onInstall(name)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Install failed')
    } finally {
      setInstalling(null)
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
          width: 560,
          maxWidth: '90vw',
          maxHeight: '80vh',
          display: 'flex',
          flexDirection: 'column',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          style={{
            padding: '1.25rem 1.5rem',
            borderBottom: '1px solid var(--border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', margin: 0 }}>
            Skills Catalog
          </h3>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: 'var(--fg-muted)',
              cursor: 'pointer',
              fontSize: 20,
              lineHeight: 1,
              fontFamily: 'inherit',
            }}
          >
            ×
          </button>
        </div>

        {/* Search */}
        <div style={{ padding: '0.75rem 1.5rem', borderBottom: '1px solid var(--border)' }}>
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search skills…"
            style={{
              width: '100%',
              background: 'var(--input-bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--fg)',
              fontSize: 14,
              padding: '0.4rem 0.75rem',
              outline: 'none',
              fontFamily: 'inherit',
              boxSizing: 'border-box',
            }}
          />
        </div>

        {/* Content */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '0.75rem 1.5rem' }}>
          {loading && (
            <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading catalog…</p>
          )}
          {error && (
            <p style={{ color: 'var(--error, #e55)', fontSize: 14 }}>{error}</p>
          )}
          {!loading && !error && filtered.length === 0 && (
            <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>
              {search ? 'No skills match your search.' : 'Catalog is empty.'}
            </p>
          )}
          {!loading &&
            filtered.map((item) => {
              const isInstalled = installedNames.includes(item.name)
              return (
                <div
                  key={item.name}
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    justifyContent: 'space-between',
                    gap: '1rem',
                    padding: '0.75rem 0',
                    borderBottom: '1px solid var(--border)',
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--fg)' }}>
                      {item.name}
                      {item.version && (
                        <span style={{ marginLeft: '0.5rem', fontSize: 12, color: 'var(--fg-muted)' }}>
                          v{item.version}
                        </span>
                      )}
                    </div>
                    {item.description && (
                      <div style={{ fontSize: 13, color: 'var(--fg-muted)', marginTop: '0.2rem' }}>
                        {item.description}
                      </div>
                    )}
                  </div>
                  {isInstalled ? (
                    <span
                      style={{
                        fontSize: 12,
                        padding: '0.25rem 0.6rem',
                        borderRadius: 'var(--radius-sm)',
                        background: 'var(--selected)',
                        color: 'var(--primary)',
                        border: '1px solid var(--primary)',
                        flexShrink: 0,
                      }}
                    >
                      Installed
                    </span>
                  ) : (
                    <button
                      onClick={() => handleInstall(item.name)}
                      disabled={installing === item.name}
                      style={{
                        padding: '0.25rem 0.75rem',
                        background: 'var(--primary)',
                        color: 'var(--primary-fg)',
                        border: 'none',
                        borderRadius: 'var(--radius-sm)',
                        fontSize: 13,
                        cursor: installing === item.name ? 'not-allowed' : 'pointer',
                        flexShrink: 0,
                        fontFamily: 'inherit',
                      }}
                    >
                      {installing === item.name ? 'Installing…' : 'Install'}
                    </button>
                  )}
                </div>
              )
            })}
        </div>
      </div>
    </div>
  )
}

// ---- Main component ----
export default function SkillsSettings() {
  const {
    extensions,
    extensionsDirty,
    extensionsLoading,
    extensionsSaving,
    extensionsError,
    fetchExtensions,
    updateExtensions,
    saveExtensions,
    resetExtensions,
  } = useExtensionsStore()

  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null)
  const [showCatalog, setShowCatalog] = useState(false)
  const [installedSkills, setInstalledSkills] = useState<string[]>([])

  useEffect(() => {
    fetchExtensions()
  }, [fetchExtensions])

  const skills = extensions.skills
  const catalog = extensions.skillsCatalog

  const handleSkillsEnabledToggle = (v: boolean) => {
    updateExtensions({ skills: { ...skills, enabled: v } })
  }

  const handlePathsChange = (paths: string[]) => {
    updateExtensions({ skills: { ...skills, paths } })
  }

  const handleCatalogUpdate = (patch: Partial<typeof catalog>) => {
    updateExtensions({ skillsCatalog: { ...catalog, ...patch } })
  }

  const handleUninstall = async () => {
    if (!uninstallTarget) return
    try {
      await api.delete(`/api/v1/skills/${encodeURIComponent(uninstallTarget)}`)
      setInstalledSkills((prev) => prev.filter((n) => n !== uninstallTarget))
    } catch {
      // silently ignore — uninstall endpoint may not be available
    } finally {
      setUninstallTarget(null)
    }
  }

  const handleInstalled = (name: string) => {
    setInstalledSkills((prev) => (prev.includes(name) ? prev : [...prev, name]))
  }

  if (extensionsLoading) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>
        Loading extensions settings…
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 640 }}>
      <h2 style={{ fontSize: 18, fontWeight: 700, color: 'var(--fg)', marginBottom: '1.25rem' }}>
        Skills
      </h2>

      {/* ---- Skills enabled ---- */}
      <Toggle
        label="Enable Skills"
        description="Allow Pando to discover and inject skills into sessions"
        checked={skills.enabled}
        onChange={handleSkillsEnabledToggle}
      />

      <div style={dividerStyle} />

      {/* ---- Skill Paths ---- */}
      <div style={{ marginBottom: '1.5rem' }}>
        <p style={subTitle}>Skill Paths</p>
        <TagListEditor
          label="Local skill search directories"
          values={skills.paths ?? []}
          onChange={handlePathsChange}
          placeholder="/path/to/my-skills"
        />
      </div>

      <div style={dividerStyle} />

      {/* ---- Catalog ---- */}
      <p style={sectionTitle}>Catalog</p>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <Toggle
          label="Enable Catalog"
          description="Browse and install skills from the online catalog"
          checked={catalog.enabled}
          onChange={(v) => handleCatalogUpdate({ enabled: v })}
        />

        <TextInput
          label="Base URL"
          placeholder="https://skills.sh"
          value={catalog.baseUrl}
          onChange={(e) => handleCatalogUpdate({ baseUrl: e.target.value })}
        />

        <Toggle
          label="Auto Update"
          description="Automatically update installed skills when new versions are available"
          checked={catalog.autoUpdate}
          onChange={(v) => handleCatalogUpdate({ autoUpdate: v })}
        />

        <SelectInput
          label="Default Scope"
          options={SCOPE_OPTIONS}
          value={catalog.defaultScope}
          onChange={(e) => handleCatalogUpdate({ defaultScope: e.target.value })}
        />

        <button
          onClick={() => setShowCatalog(true)}
          disabled={!catalog.enabled}
          style={{
            alignSelf: 'flex-start',
            padding: '0.5rem 1.25rem',
            background: catalog.enabled ? 'var(--primary)' : 'var(--border)',
            color: catalog.enabled ? 'var(--primary-fg)' : 'var(--fg-muted)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: catalog.enabled ? 'pointer' : 'not-allowed',
            fontFamily: 'inherit',
          }}
        >
          Browse Catalog
        </button>
      </div>

      <div style={dividerStyle} />

      {/* Error */}
      {extensionsError && (
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
          {extensionsError}
        </div>
      )}

      {/* Actions */}
      <div style={{ display: 'flex', gap: '0.75rem' }}>
        <button
          onClick={saveExtensions}
          disabled={!extensionsDirty || extensionsSaving}
          style={{
            padding: '0.5rem 1.5rem',
            background: !extensionsDirty || extensionsSaving ? 'var(--border)' : 'var(--primary)',
            color: !extensionsDirty || extensionsSaving ? 'var(--fg-muted)' : 'var(--primary-fg)',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !extensionsDirty || extensionsSaving ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          {extensionsSaving ? 'Saving…' : 'Save'}
        </button>
        <button
          onClick={resetExtensions}
          disabled={!extensionsDirty}
          style={{
            padding: '0.5rem 1.5rem',
            background: 'transparent',
            color: !extensionsDirty ? 'var(--fg-dim)' : 'var(--fg-muted)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            fontSize: 14,
            fontWeight: 600,
            cursor: !extensionsDirty ? 'not-allowed' : 'pointer',
            fontFamily: 'inherit',
          }}
        >
          Reset
        </button>
      </div>

      {/* Uninstall confirm dialog */}
      {uninstallTarget && (
        <ConfirmDialog
          title="Uninstall Skill"
          message={`Are you sure you want to uninstall "${uninstallTarget}"? This action cannot be undone.`}
          confirmLabel="Uninstall"
          dangerous
          onConfirm={handleUninstall}
          onCancel={() => setUninstallTarget(null)}
        />
      )}

      {/* Catalog modal */}
      {showCatalog && (
        <CatalogModal
          onClose={() => setShowCatalog(false)}
          installedNames={installedSkills}
          onInstall={handleInstalled}
        />
      )}
    </div>
  )
}
