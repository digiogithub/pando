import { useCallback, useEffect, useMemo, useState } from 'react'
import { useExtensionsStore } from '@pando/client/stores/extensionsStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import ConfirmDialog from '@/components/shared/ConfirmDialog'
import TagListEditor from '@/components/shared/TagListEditor'
import api from '@pando/client/services/api'
import type { InstalledSkill, SkillCatalogItem } from '@pando/client/types'
import { Badge, Button, Dialog, Input, Select, SettingsRow, SettingsSection, Switch } from '@/components/ui'

const SCOPE_OPTIONS = [
  { value: 'session', label: 'Session' },
  { value: 'global', label: 'Global' },
  { value: 'project', label: 'Project' },
]

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

  const trimmedSearch = search.trim()
  const shouldSearch = trimmedSearch.length >= 2
  const searchRequestKey = shouldSearch ? trimmedSearch : null

  // Debounced search: fires 300 ms after the user stops typing (min 2 chars)
  useEffect(() => {
    if (!searchRequestKey) return

    setLoading(true)
    setError(null)

    const timer = setTimeout(() => {
      api
        .get<{ skills: SkillCatalogItem[] }>(`/api/v1/skills/catalog?q=${encodeURIComponent(searchRequestKey)}`)
        .then((data) => setItems(data.skills ?? []))
        .catch((e) => setError(e instanceof Error ? e.message : 'Search failed'))
        .finally(() => setLoading(false))
    }, 300)

    return () => clearTimeout(timer)
  }, [searchRequestKey])

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
    <Dialog open onClose={onClose} title="Skills Catalog" size="md">
      <div className="flex flex-col gap-3">
        <Input
          data-autofocus
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Type to search skills… (min 2 chars)"
        />

        <div className="flex flex-col max-h-[50vh] overflow-y-auto">
          {search.trim().length < 2 && <p className="text-sm text-muted">Start typing to search the skills catalog.</p>}
          {loading && <p className="text-sm text-muted">Searching…</p>}
          {error && <p className="text-sm text-danger">{error}</p>}
          {!loading && !error && search.trim().length >= 2 && items.length === 0 && (
            <p className="text-sm text-muted">No skills found.</p>
          )}
          {!loading &&
            items.map((item) => {
              const isInstalled = installedNames.includes(item.name)
              return (
                <div key={item.skillId || item.name} className="flex items-start justify-between gap-4 py-3 border-b border-border last:border-b-0">
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-semibold text-fg">{item.name}</div>
                    <div className="text-xs text-muted mt-0.5">
                      {item.source}
                      {item.installs > 0 && <span className="ml-3">{formatInstalls(item.installs)}</span>}
                    </div>
                  </div>
                  {isInstalled ? (
                    <Badge tone="accent" className="shrink-0">Installed</Badge>
                  ) : (
                    <Button size="sm" onClick={() => void handleInstall(item)} disabled={installing === item.name} loading={installing === item.name} className="shrink-0">
                      {installing === item.name ? 'Installing…' : 'Install'}
                    </Button>
                  )}
                </div>
              )
            })}
        </div>
      </div>
    </Dialog>
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
    return <p className="text-sm text-muted">Loading installed skills…</p>
  }
  if (error) {
    return <p className="text-sm text-danger">{error}</p>
  }
  if (installed.length === 0) {
    return <p className="text-sm text-muted">No skills installed. Skills are loaded from ~/.pando/skills/ and .pando/skills/.</p>
  }
  return (
    <div className="flex flex-col">
      {installed.map((skill) => (
        <div key={skill.name} className="flex items-start justify-between gap-4 py-3 border-b border-border last:border-b-0">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-sm font-semibold text-fg">{skill.name}</span>
              {skill.version && <span className="text-xs text-muted">v{skill.version}</span>}
              <Badge>{skill.scope}</Badge>
              {skill.active && <Badge tone="accent">active</Badge>}
            </div>
            {skill.description && <div className="text-sm text-muted mt-0.5">{skill.description}</div>}
            {skill.source && skill.source !== '(local)' && <div className="text-xs text-faint mt-0.5">{skill.source}</div>}
          </div>
          <Button variant="danger" size="sm" className="shrink-0" onClick={() => onUninstall(skill.name)}>
            Uninstall
          </Button>
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
  useUnsavedChangesGuard({
    id: 'skills',
    dirty: extensionsDirty,
    save: async () => {
      await saveExtensions()
      return !useExtensionsStore.getState().extensionsError
    },
    discard: resetExtensions,
  })

  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null)
  const [showCatalog, setShowCatalog] = useState(false)
  const [installedSkills, setInstalledSkills] = useState<InstalledSkill[]>([])
  const [installedLoading, setInstalledLoading] = useState(true)
  const [installedError, setInstalledError] = useState<string | null>(null)

  useEffect(() => {
    fetchExtensions()
  }, [fetchExtensions])

  // Load installed skills from disk on mount.
  const loadInstalled = useCallback(() => {
    setInstalledLoading(true)
    setInstalledError(null)
    api
      .get<{ skills: InstalledSkill[] }>('/api/v1/skills/installed')
      .then((data) => setInstalledSkills(data.skills ?? []))
      .catch((e) => setInstalledError(e instanceof Error ? e.message : 'Failed to load installed skills'))
      .finally(() => setInstalledLoading(false))
  }, [])

  const installedRequest = useMemo(() => api.get<{ skills: InstalledSkill[] }>('/api/v1/skills/installed'), [])

  useEffect(() => {
    installedRequest
      .then((data) => setInstalledSkills(data.skills ?? []))
      .catch((e) => setInstalledError(e instanceof Error ? e.message : 'Failed to load installed skills'))
      .finally(() => setInstalledLoading(false))
  }, [installedRequest])

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
    return <div className="settings-loading">Loading extensions settings…</div>
  }

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Skills</h2>
      </header>

      <SettingsSection>
        <SettingsRow label="Enable skills" description="Allow Pando to discover and inject skills into sessions" htmlFor="skills-enabled">
          <Switch id="skills-enabled" checked={skills.enabled} onCheckedChange={handleSkillsEnabledToggle} />
        </SettingsRow>
      </SettingsSection>

      <section className="ui-settings-section">
        <header className="ui-settings-section-header flex items-center justify-between">
          <h3 className="ui-settings-section-title">Installed skills</h3>
          <Button variant="ghost" size="sm" onClick={loadInstalled}>Refresh</Button>
        </header>
        <div className="ui-settings-group">
          <div className="px-4">
            <InstalledSkillsList
              skills={installedSkills}
              loading={installedLoading}
              error={installedError}
              onUninstall={(name) => setUninstallTarget(name)}
            />
          </div>
        </div>
      </section>

      <SettingsSection title="Skill paths" description="Local skill search directories">
        <div className="p-4">
          <TagListEditor items={skills.paths ?? []} onChange={handlePathsChange} placeholder="/path/to/my-skills" />
        </div>
      </SettingsSection>

      <SettingsSection title="Catalog">
        <SettingsRow label="Enable catalog" description="Browse and install skills from the online catalog" htmlFor="skills-catalog-enabled">
          <Switch id="skills-catalog-enabled" checked={catalog.enabled} onCheckedChange={(v) => handleCatalogUpdate({ enabled: v })} />
        </SettingsRow>
        <SettingsRow label="Base URL" htmlFor="skills-catalog-base-url">
          <Input id="skills-catalog-base-url" placeholder="https://skills.sh" value={catalog.baseUrl} onChange={(e) => handleCatalogUpdate({ baseUrl: e.target.value })} />
        </SettingsRow>
        <SettingsRow label="Auto update" description="Automatically update installed skills when new versions are available" htmlFor="skills-catalog-auto-update">
          <Switch id="skills-catalog-auto-update" checked={catalog.autoUpdate} onCheckedChange={(v) => handleCatalogUpdate({ autoUpdate: v })} />
        </SettingsRow>
        <SettingsRow label="Default scope" htmlFor="skills-catalog-default-scope">
          <Select id="skills-catalog-default-scope" options={SCOPE_OPTIONS} value={catalog.defaultScope} onChange={(e) => handleCatalogUpdate({ defaultScope: e.target.value })} />
        </SettingsRow>
        <div className="p-4">
          <Button variant="primary" disabled={!catalog.enabled} onClick={() => setShowCatalog(true)}>
            Browse Catalog
          </Button>
        </div>
      </SettingsSection>

      {extensionsError && <div className="settings-banner settings-banner--danger" role="alert">{extensionsError}</div>}

      <div className="settings-actions">
        <Button variant="primary" onClick={saveExtensions} disabled={!extensionsDirty || extensionsSaving} loading={extensionsSaving}>
          {extensionsSaving ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="secondary" onClick={resetExtensions} disabled={!extensionsDirty}>
          Reset
        </Button>
      </div>

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
