import { useEffect, useState } from 'react'
import { useExtensionsStore } from '@/stores/extensionsStore'
import { Toggle, TextInput, SelectInput } from '@/components/shared/FormInput'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import api from '@/services/api'
import type { InstalledSkill, SkillCatalogItem } from '@/types'

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
  defaultScope,
}: {
  onClose: () => void
  installedNames: string[]
  onInstall: (name: string) => void
  defaultScope: string
}) {
  const [items, setItems] = useState<SkillCatalogItem[]>([])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [installing, setInstalling] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Debounced search: fires 300 ms after the user stops typing (min 2 chars)
  useEffect(() => {
    const q = search.trim()
    if (q.length < 2) {
      setItems([])
      setError(null)
      return
    }

    setLoading(true)
    setError(null)

    const timer = setTimeout(() => {
      api
        .get<{ skills: SkillCatalogItem[] }>(`/api/v1/skills/catalog?q=${encodeURIComponent(q)}`)
        .then((data) => setItems(data.skills ?? []))
        .catch((e) => setError(e instanceof Error ? e.message : 'Search failed'))
        .finally(() => setLoading(false))
    }, 300)

    return () => clearTimeout(timer)
  }, [search])

  const handleInstall = async (item: SkillCatalogItem) => {
    setInstalling(item.name)
    setError(null)
    try {
      await api.post('/api/v1/skills/install', {
        name: item.name,
        source: item.source,
        skillId: item.skillId,
        scope: defaultScope === 'project' ? 'project' : 'global',
      })
      onInstall(item.name)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Install failed')
    } finally {
      setInstalling(null)
    }
  }

  const formatInstalls = (n: number) => {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M installs`
    if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K installs`
    return `${n} installs`
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
            autoFocus
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Type to search skills… (min 2 chars)"
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
            onFocus={(e) => { e.target.style.borderColor = 'var(--border-focus)' }}
            onBlur={(e) => { e.target.style.borderColor = 'var(--border)' }}
          />
        </div>

        {/* Content */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '0.75rem 1.5rem' }}>
          {search.trim().length < 2 && (
            <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>
              Start typing to search the skills catalog.
            </p>
          )}
          {loading && (
            <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Searching…</p>
          )}
          {error && (
            <p style={{ color: 'var(--error, #e55)', fontSize: 14 }}>{error}</p>
          )}
          {!loading && !error && search.trim().length >= 2 && items.length === 0 && (
            <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>No skills found.</p>
          )}
          {!loading &&
            items.map((item) => {
              const isInstalled = installedNames.includes(item.name)
              return (
                <div
                  key={item.skillId || item.name}
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
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginTop: '0.15rem' }}>
                      {item.source}
                      {item.installs > 0 && (
                        <span style={{ marginLeft: '0.75rem' }}>{formatInstalls(item.installs)}</span>
                      )}
                    </div>
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
                      onClick={() => handleInstall(item)}
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

// ---- Installed Skills List ----
function InstalledSkillsList({
  skills: installed,
  loading,
  error,
  onUninstall,
}: {
  skills: InstalledSkill[]
  loading: boolean
  error: string | null
  onUninstall: (name: string) => void
}) {
  if (loading) {
    return <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>Loading installed skills…</p>
  }
  if (error) {
    return <p style={{ color: 'var(--error, #e55)', fontSize: 14 }}>{error}</p>
  }
  if (installed.length === 0) {
    return (
      <p style={{ color: 'var(--fg-muted)', fontSize: 14 }}>
        No skills installed. Skills are loaded from ~/.pando/skills/ and .pando/skills/.
      </p>
    )
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0' }}>
      {installed.map((skill) => (
        <div
          key={skill.name}
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
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
              <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--fg)' }}>
                {skill.name}
              </span>
              {skill.version && (
                <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>v{skill.version}</span>
              )}
              <span
                style={{
                  fontSize: 11,
                  padding: '0.1rem 0.4rem',
                  borderRadius: 'var(--radius-sm)',
                  background: 'var(--selected)',
                  color: 'var(--fg-muted)',
                  border: '1px solid var(--border)',
                }}
              >
                {skill.scope}
              </span>
              {skill.active && (
                <span
                  style={{
                    fontSize: 11,
                    padding: '0.1rem 0.4rem',
                    borderRadius: 'var(--radius-sm)',
                    background: 'var(--primary)',
                    color: 'var(--primary-fg)',
                  }}
                >
                  active
                </span>
              )}
            </div>
            {skill.description && (
              <div style={{ fontSize: 13, color: 'var(--fg-muted)', marginTop: '0.2rem' }}>
                {skill.description}
              </div>
            )}
            {skill.source && skill.source !== '(local)' && (
              <div style={{ fontSize: 12, color: 'var(--fg-dim, var(--fg-muted))', marginTop: '0.15rem' }}>
                {skill.source}
              </div>
            )}
          </div>
          <button
            onClick={() => onUninstall(skill.name)}
            style={{
              padding: '0.25rem 0.75rem',
              background: 'transparent',
              color: 'var(--error, #e55)',
              border: '1px solid var(--error, #e55)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 13,
              cursor: 'pointer',
              flexShrink: 0,
              fontFamily: 'inherit',
            }}
          >
            Uninstall
          </button>
        </div>
      ))}
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
  const [installedSkills, setInstalledSkills] = useState<InstalledSkill[]>([])
  const [installedLoading, setInstalledLoading] = useState(true)
  const [installedError, setInstalledError] = useState<string | null>(null)

  useEffect(() => {
    fetchExtensions()
  }, [fetchExtensions])

  // Load installed skills from disk on mount.
  const loadInstalled = () => {
    setInstalledLoading(true)
    setInstalledError(null)
    api
      .get<{ skills: InstalledSkill[] }>('/api/v1/skills/installed')
      .then((data) => setInstalledSkills(data.skills ?? []))
      .catch((e) => setInstalledError(e instanceof Error ? e.message : 'Failed to load installed skills'))
      .finally(() => setInstalledLoading(false))
  }

  useEffect(() => {
    loadInstalled()
  }, [])

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
      setInstalledSkills((prev) => prev.filter((s) => s.name !== uninstallTarget))
    } catch (e) {
      setInstalledError(e instanceof Error ? e.message : 'Uninstall failed')
    } finally {
      setUninstallTarget(null)
    }
  }

  const handleInstalled = (name: string) => {
    // Refresh installed list so the newly installed skill appears.
    loadInstalled()
    // Also mark as installed in the catalog modal so the button flips to "Installed".
    setInstalledSkills((prev) =>
      prev.some((s) => s.name === name)
        ? prev
        : [...prev, { name, description: '', version: '', source: '', scope: 'global', active: false, skillId: '' }],
    )
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

      {/* ---- Installed Skills ---- */}
      <div style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
          <p style={{ ...subTitle, margin: 0 }}>Installed Skills</p>
          <button
            onClick={loadInstalled}
            style={{
              background: 'none',
              border: 'none',
              color: 'var(--fg-muted)',
              fontSize: 12,
              cursor: 'pointer',
              fontFamily: 'inherit',
              padding: '0.2rem 0.5rem',
            }}
          >
            Refresh
          </button>
        </div>
        <InstalledSkillsList
          skills={installedSkills}
          loading={installedLoading}
          error={installedError}
          onUninstall={(name) => setUninstallTarget(name)}
        />
      </div>

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
          installedNames={installedSkills.map((s) => s.name)}
          onInstall={handleInstalled}
          defaultScope={catalog.defaultScope || 'global'}
        />
      )}
    </div>
  )
}
