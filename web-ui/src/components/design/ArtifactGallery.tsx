import { useTranslation } from 'react-i18next'
import { format } from 'date-fns'
import type { DesignArtifact } from '@pando/client/stores/designStore'
import { getBaseURL } from '@pando/client/services/api'
import api from '@pando/client/services/api'
import { ExternalLink, LayoutGrid, Monitor, Palette } from '@/components/ui/icons'

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
    return <div className="design-loading">{t('design.gallery.loading')}</div>
  }

  if (artifacts.length === 0) {
    return (
      <div className="design-empty">
        <Palette size={22} />
        <div className="design-empty-title">{t('design.gallery.emptyTitle')}</div>
        {t('design.gallery.emptyBody')}
      </div>
    )
  }

  return (
    <div className="design-gallery">
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
    <div className="design-card">
      <button type="button" onClick={onOpen} className="design-card-thumb">
        <img
          src={thumbnail}
          alt={artifact.title}
          loading="lazy"
          // A machine with no headless browser answers 503 here; the card must
          // still be usable, so the broken image simply disappears.
          onError={(e) => {
            e.currentTarget.style.display = 'none'
          }}
        />
      </button>

      <div className="design-card-body">
        <button type="button" onClick={onOpen} className="design-card-title">
          {artifact.title}
        </button>
        <div className="design-card-meta">
          {artifact.kind === 'deck' ? <LayoutGrid size={10} /> : <Monitor size={10} />}
          <span>{t(`design.kind.${artifact.kind}`)}</span>
          <span>· v{artifact.current_version}</span>
          {artifact.slides ? <span>· {t('design.deck.count', { count: artifact.slides })}</span> : null}
        </div>
        <div className="design-card-dir">{artifact.dir}</div>
        <div className="design-card-footer">
          <span className="design-card-date">{safeDate(artifact.created_at)}</span>
          {artifact.url && (
            <a href={artifact.url} target="_blank" rel="noreferrer noopener" title={t('design.openExternal')} className="design-card-external">
              <ExternalLink size={11} />
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
