import type { Project } from '@/types'

interface ProjectInitWizardProps {
  project: Project
  onConfirm: () => Promise<void>
  onCancel: () => void
}

function shortenPath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~')
}

export default function ProjectInitWizard({ project, onConfirm, onCancel }: ProjectInitWizardProps) {
  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div
        style={{
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          borderRadius: 8,
          padding: '1.5rem 2rem',
          maxWidth: 480,
          width: '90%',
        }}
      >
        <h3 style={{ margin: '0 0 1rem', fontSize: 16, fontWeight: 700, color: 'var(--fg)' }}>
          Initialize Project
        </h3>

        <div
          style={{
            padding: '0.625rem 0.75rem',
            background: 'var(--sidebar-bg)',
            borderRadius: 'var(--radius-sm)',
            marginBottom: '1rem',
            fontFamily: 'monospace',
            fontSize: 13,
            color: 'var(--fg)',
          }}
        >
          {shortenPath(project.path)}
        </div>

        <p style={{ margin: '0 0 1rem', fontSize: 13, color: 'var(--fg-muted)' }}>
          No Pando config found at this path. The following will be created:
        </p>

        <ul style={{ margin: '0 0 1.5rem', padding: '0 0 0 1.25rem', fontSize: 13, color: 'var(--fg-muted)', lineHeight: 2 }}>
          <li><code style={{ color: 'var(--fg)' }}>.pando.toml</code> — configuration</li>
          <li><code style={{ color: 'var(--fg)' }}>.pando/data/</code> — database</li>
          <li><code style={{ color: 'var(--fg)' }}>.pando/mesnada/</code> — agents &amp; personas</li>
          <li><code style={{ color: 'var(--fg)' }}>agents/skills/</code> — custom skills</li>
        </ul>

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
          <button
            onClick={onCancel}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg-muted)',
              fontSize: 13,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: 'none',
              background: 'var(--primary)',
              color: 'white',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Initialize &amp; Open
          </button>
        </div>
      </div>
    </div>
  )
}
