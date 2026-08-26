import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPalette, faDisplay, faRectangleList, faArrowUpRightFromSquare } from '@fortawesome/free-solid-svg-icons'
import { format } from 'date-fns'
import type { DesignArtifact } from '@pando/client/stores/designStore'
import { getBaseURL } from '@pando/client/services/api'
import api from '@pando/client/services/api'

interface ArtifactGalleryProps {
  artifacts: DesignArtifact[]
  loading: boolean
  onOpen: (id: string) => void
}

/**
 * ArtifactGallery is the landing view of the Design section: every artifact in
 * the project, newest first.
 *
 * Each card shows a live screenshot rather than a stored thumbnail. Versions are
 * snapshots of a directory, not image archives, so there is no thumbnail to
 * store — and rendering on demand means a card can never show a picture of a
 * design that no longer exists on disk.
 */
export default function ArtifactGallery({ artifacts, loading, onOpen }: ArtifactGalleryProps) {
  const { t } = useTranslation()

  if (loading) {
    return <div style={{ padding: '1.5rem', color: 'var(--fg-muted)', fontSize: 13 }}>{t('design.gallery.loading')}</div>
  }

  if (artifacts.length === 0) {
    return (
      <div style={{ padding: '2rem', maxWidth: 560, color: 'var(--fg-muted)', fontSize: 13, lineHeight: 1.7 }}>
        <FontAwesomeIcon icon={faPalette} style={{ fontSize: 22, marginBottom: '0.75rem', display: 'block' }} />
        <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)', marginBottom: '0.5rem' }}>
          {t('design.gallery.emptyTitle')}
        </div>
        {t('design.gallery.emptyBody')}
      </div>
    )
  }

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
        gap: '1rem',
        padding: '1rem',
        overflow: 'auto',
      }}
    >
      {artifacts.map((artifact) => (
        <ArtifactCard key={artifact.id} artifact={artifact} onOpen={() => onOpen(artifact.id)} />
      ))}
    </div>
  )
}

function ArtifactCard({ artifact, onOpen }: { artifact: DesignArtifact; onOpen: () => void }) {
  const { t } = useTranslation()
  const token = api.getToken()
  const thumbnail = `${getBaseURL()}/api/v1/design/artifacts/${artifact.id}/screenshot${
    token ? `?token=${encodeURIComponent(token)}` : ''
  }`

  return (
    <div
      style={{
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-sm)',
        overflow: 'hidden',
        background: 'var(--surface)',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <button
        onClick={onOpen}
        style={{
          border: 'none',
          padding: 0,
          background: 'white',
          cursor: 'pointer',
          height: 140,
          overflow: 'hidden',
          display: 'block',
        }}
      >
        <img
          src={thumbnail}
          alt={artifact.title}
          loading="lazy"
          style={{ width: '100%', objectFit: 'cover', objectPosition: 'top', display: 'block' }}
          // A machine with no headless browser answers 503 here; the card must
          // still be usable, so the broken image simply disappears.
          onError={(e) => {
            e.currentTarget.style.display = 'none'
          }}
        />
      </button>

      <div style={{ padding: '0.6rem 0.7rem', display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
        <button
          onClick={onOpen}
          style={{
            background: 'none',
            border: 'none',
            padding: 0,
            textAlign: 'left',
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--fg)',
            cursor: 'pointer',
          }}
        >
          {artifact.title}
        </button>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: 10.5, color: 'var(--fg-muted)' }}>
          <FontAwesomeIcon icon={artifact.kind === 'deck' ? faRectangleList : faDisplay} style={{ fontSize: 10 }} />
          <span>{t(`design.kind.${artifact.kind}`)}</span>
          <span>· v{artifact.current_version}</span>
          {artifact.slides ? <span>· {t('design.deck.count', { count: artifact.slides })}</span> : null}
        </div>
        <div style={{ fontSize: 10, color: 'var(--fg-muted)', opacity: 0.8 }}>{artifact.dir}</div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.2rem' }}>
          <span style={{ fontSize: 10, color: 'var(--fg-muted)', opacity: 0.75 }}>{safeDate(artifact.created_at)}</span>
          {artifact.url && (
            <a
              href={artifact.url}
              target="_blank"
              rel="noreferrer noopener"
              title={t('design.openExternal')}
              style={{ marginLeft: 'auto', color: 'var(--fg-muted)', fontSize: 10 }}
            >
              <FontAwesomeIcon icon={faArrowUpRightFromSquare} />
            </a>
          )}
        </div>
      </div>
    </div>
  )
}

function safeDate(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return format(parsed, 'yyyy-MM-dd')
}
