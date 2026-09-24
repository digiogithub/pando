import { useEffect, useMemo, useState } from 'react'
import { useDesignStore, type DesignExtractSource } from '@pando/client/stores/designStore'
import { useUnsavedChangesGuard } from './unsavedChanges'
import { Button, Input, SegmentedControl, SettingsRow, SettingsSection } from '@/components/ui'

/**
 * DesignSystemSettings is where a project chooses the design system its
 * artifacts are held to: extract one from something that already looks right,
 * or edit the tokens by hand.
 *
 * The token editor writes values through as-is. It deliberately offers no
 * colour picker for non-colour groups and no validation beyond "not empty":
 * tokens hold CSS, and second-guessing what is valid CSS here would reject
 * values a browser accepts.
 */

const sources: { id: DesignExtractSource; label: string; hint: string }[] = [
  { id: 'code', label: 'Code', hint: 'Directory to scan (blank = project root)' },
  { id: 'url', label: 'URL', hint: 'https://…' },
  { id: 'image', label: 'Image', hint: 'Path to a screenshot or logo' },
  { id: 'text', label: 'Style guide', hint: 'File path, or a bundled example name' },
]

export default function DesignSystemSettings() {
  const system = useDesignStore((s) => s.system)
  const examples = useDesignStore((s) => s.systemExamples)
  const loading = useDesignStore((s) => s.systemLoading)
  const busy = useDesignStore((s) => s.systemBusy)
  const lastExtraction = useDesignStore((s) => s.lastExtraction)
  const fetchSystem = useDesignStore((s) => s.fetchSystem)
  const fetchSystemExamples = useDesignStore((s) => s.fetchSystemExamples)
  const saveSystemTokens = useDesignStore((s) => s.saveSystemTokens)
  const extractSystem = useDesignStore((s) => s.extractSystem)

  const [draft, setDraft] = useState<Record<string, Record<string, string>>>({})
  const [name, setName] = useState('')
  const [source, setSource] = useState<DesignExtractSource>('code')
  const [target, setTarget] = useState('')

  useEffect(() => {
    void fetchSystem()
    void fetchSystemExamples()
  }, [fetchSystem, fetchSystemExamples])

  // The draft is seeded from the server whenever the stored system changes,
  // including after an extraction, so the editor always shows what is on disk
  // rather than a stale copy of what it used to be.
  useEffect(() => {
    if (!system) return
    setDraft(structuredClone(system.system.tokens ?? {}))
    setName(system.system.name ?? '')
  }, [system])

  const groups = useMemo(() => Object.keys(draft).sort(), [draft])
  const dirty = useMemo(() => {
    if (!system) return false
    return (
      JSON.stringify(draft) !== JSON.stringify(system.system.tokens ?? {}) ||
      name !== (system.system.name ?? '')
    )
  }, [draft, name, system])

  const resetDraft = () => {
    if (!system) return
    setDraft(structuredClone(system.system.tokens ?? {}))
    setName(system.system.name ?? '')
  }

  useUnsavedChangesGuard({
    id: 'design-system',
    dirty,
    // The store reports a failed save only through a toast; a successful one
    // replaces `system` with the server's payload, so a new object means saved.
    save: async () => {
      const before = useDesignStore.getState().system
      await saveSystemTokens(draft, { name })
      return useDesignStore.getState().system !== before
    },
    discard: resetDraft,
  })

  if (loading && !system) {
    return <div className="settings-loading">Loading…</div>
  }

  const activeSource = sources.find((s) => s.id === source)!

  return (
    <div>
      <header className="settings-page-header">
        <h2 className="settings-page-title">Design system</h2>
        {system && !system.exists && (
          <p className="settings-page-description">
            This project has not committed a design system yet. The values below are the neutral
            defaults; saving or extracting writes them.
          </p>
        )}
      </header>

      {system && (
        <div className="flex flex-col gap-0.5 mb-4 font-mono text-xs text-muted">
          <span>tokens: {system.tokens_path}</span>
          <span>stylesheet: {system.stylesheet_path}</span>
          <span>contract: {system.contract_path}</span>
        </div>
      )}

      <SettingsSection>
        <SettingsRow label="Name" htmlFor="design-system-name">
          <Input id="design-system-name" className="font-mono" value={name} onChange={(e) => setName(e.target.value)} />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection title="Extract from">
        <div className="p-4 flex flex-col gap-3">
          <SegmentedControl
            aria-label="Extract from"
            value={source}
            onChange={(v) => setSource(v as DesignExtractSource)}
            items={sources.map((s) => ({ value: s.id, label: s.label }))}
          />
          <div className="flex gap-2 items-center">
            <Input
              className="flex-1 font-mono"
              value={target}
              placeholder={activeSource.hint}
              onChange={(e) => setTarget(e.target.value)}
            />
            <Button variant="primary" disabled={busy} onClick={() => void extractSystem(source, target, { name })}>
              Extract
            </Button>
            <Button variant="secondary" disabled={busy} onClick={() => void extractSystem(source, target, { name, dryRun: true })}>
              Preview
            </Button>
          </div>
          {source === 'text' && examples.length > 0 && (
            <div className="flex gap-1.5 flex-wrap">
              {examples.map((e) => (
                <Button key={e.name} variant="secondary" size="sm" title={e.title} onClick={() => setTarget(e.name)}>
                  {e.name}
                </Button>
              ))}
            </div>
          )}
          {lastExtraction?.notes?.length ? (
            <ul className="m-0 pl-4 text-xs text-muted">
              {lastExtraction.notes.map((note) => (
                <li key={note}>{note}</li>
              ))}
            </ul>
          ) : null}
        </div>
      </SettingsSection>

      {groups.map((group) => (
        <SettingsSection key={group} title={group}>
          {Object.keys(draft[group]).sort().map((token) => (
            <SettingsRow key={token} label={<code className="font-mono text-xs text-muted">--{group}-{token}</code>}>
              {group === 'color' && /^#[0-9a-fA-F]{6}$/.test(draft[group][token]) && (
                <input
                  type="color"
                  value={draft[group][token]}
                  className="w-8 h-7 p-0 border border-border rounded-xs"
                  onChange={(e) => setDraft((d) => ({ ...d, [group]: { ...d[group], [token]: e.target.value } }))}
                />
              )}
              <Input
                className="font-mono"
                value={draft[group][token]}
                onChange={(e) => setDraft((d) => ({ ...d, [group]: { ...d[group], [token]: e.target.value } }))}
              />
            </SettingsRow>
          ))}
        </SettingsSection>
      ))}

      <div className="settings-actions">
        <Button variant="primary" disabled={!dirty || busy} onClick={() => void saveSystemTokens(draft, { name })}>
          Save
        </Button>
        <Button
          variant="secondary"
          disabled={!dirty || busy}
          onClick={resetDraft}
        >
          Reset
        </Button>
      </div>
    </div>
  )
}
