import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { useDesignStore, type DesignExtractSource } from '@pando/client/stores/designStore'

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

const sectionTitle: CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
  color: 'var(--fg)',
  marginBottom: '1.25rem',
}

const groupTitle: CSSProperties = {
  fontSize: 12,
  fontWeight: 700,
  color: 'var(--fg-muted)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  margin: '1rem 0 0.5rem',
}

const dividerStyle: CSSProperties = {
  borderTop: '1px solid var(--border)',
  margin: '1.5rem 0',
}

const inputStyle: CSSProperties = {
  flex: 1,
  padding: '0.4rem 0.6rem',
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg)',
  fontSize: 13,
  fontFamily: 'var(--font-mono, monospace)',
}

const buttonStyle: CSSProperties = {
  padding: '0.45rem 0.9rem',
  background: 'var(--primary)',
  color: 'var(--bg)',
  border: 'none',
  borderRadius: 'var(--radius-sm)',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
}

const secondaryButtonStyle: CSSProperties = {
  ...buttonStyle,
  background: 'var(--surface)',
  color: 'var(--fg)',
  border: '1px solid var(--border)',
}

const pathStyle: CSSProperties = {
  fontSize: 12,
  color: 'var(--fg-muted)',
  fontFamily: 'var(--font-mono, monospace)',
}

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

  if (loading && !system) {
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 14 }}>Loading…</div>
  }

  const activeSource = sources.find((s) => s.id === source)!

  return (
    <div style={{ maxWidth: 720 }}>
      <h2 style={sectionTitle}>Design system</h2>

      {system && !system.exists && (
        <p style={{ fontSize: 13, color: 'var(--fg-muted)', marginBottom: '1rem' }}>
          This project has not committed a design system yet. The values below are the neutral
          defaults; saving or extracting writes them.
        </p>
      )}

      {system && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.15rem', marginBottom: '1rem' }}>
          <span style={pathStyle}>tokens: {system.tokens_path}</span>
          <span style={pathStyle}>stylesheet: {system.stylesheet_path}</span>
          <span style={pathStyle}>contract: {system.contract_path}</span>
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        <label style={{ ...groupTitle, margin: 0 }}>Name</label>
        <input style={inputStyle} value={name} onChange={(e) => setName(e.target.value)} />
      </div>

      <div style={dividerStyle} />

      <h3 style={{ ...groupTitle, marginTop: 0 }}>Extract from</h3>
      <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap', marginBottom: '0.6rem' }}>
        {sources.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => setSource(s.id)}
            style={{
              ...secondaryButtonStyle,
              background: s.id === source ? 'var(--selected)' : 'var(--surface)',
              borderColor: s.id === source ? 'var(--primary)' : 'var(--border)',
            }}
          >
            {s.label}
          </button>
        ))}
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
        <input
          style={inputStyle}
          value={target}
          placeholder={activeSource.hint}
          onChange={(e) => setTarget(e.target.value)}
        />
        <button
          type="button"
          style={buttonStyle}
          disabled={busy}
          onClick={() => void extractSystem(source, target, { name })}
        >
          Extract
        </button>
        <button
          type="button"
          style={secondaryButtonStyle}
          disabled={busy}
          onClick={() => void extractSystem(source, target, { name, dryRun: true })}
        >
          Preview
        </button>
      </div>
      {source === 'text' && examples.length > 0 && (
        <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap', marginTop: '0.5rem' }}>
          {examples.map((e) => (
            <button
              key={e.name}
              type="button"
              title={e.title}
              style={{ ...secondaryButtonStyle, fontWeight: 400 }}
              onClick={() => setTarget(e.name)}
            >
              {e.name}
            </button>
          ))}
        </div>
      )}
      {lastExtraction?.notes?.length ? (
        <ul style={{ margin: '0.6rem 0 0', paddingLeft: '1.1rem', fontSize: 12, color: 'var(--fg-muted)' }}>
          {lastExtraction.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      ) : null}

      <div style={dividerStyle} />

      <h3 style={{ ...groupTitle, marginTop: 0 }}>Tokens</h3>
      {groups.map((group) => (
        <div key={group}>
          <div style={groupTitle}>{group}</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
            {Object.keys(draft[group]).sort().map((token) => (
              <div key={token} style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                <code style={{ ...pathStyle, minWidth: 150 }}>
                  --{group}-{token}
                </code>
                {group === 'color' && /^#[0-9a-fA-F]{6}$/.test(draft[group][token]) && (
                  <input
                    type="color"
                    value={draft[group][token]}
                    style={{ width: 32, height: 28, padding: 0, border: '1px solid var(--border)' }}
                    onChange={(e) =>
                      setDraft((d) => ({ ...d, [group]: { ...d[group], [token]: e.target.value } }))
                    }
                  />
                )}
                <input
                  style={inputStyle}
                  value={draft[group][token]}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, [group]: { ...d[group], [token]: e.target.value } }))
                  }
                />
              </div>
            ))}
          </div>
        </div>
      ))}

      <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.25rem' }}>
        <button
          type="button"
          style={{ ...buttonStyle, opacity: dirty && !busy ? 1 : 0.5 }}
          disabled={!dirty || busy}
          onClick={() => void saveSystemTokens(draft, { name })}
        >
          Save
        </button>
        <button
          type="button"
          style={secondaryButtonStyle}
          disabled={!dirty || busy}
          onClick={() => {
            if (!system) return
            setDraft(structuredClone(system.system.tokens ?? {}))
            setName(system.system.name ?? '')
          }}
        >
          Reset
        </button>
      </div>
    </div>
  )
}
