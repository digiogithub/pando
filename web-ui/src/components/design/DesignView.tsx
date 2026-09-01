import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faPalette, faTriangleExclamation, faBorderAll } from '@fortawesome/free-solid-svg-icons'
import { useDesignStore } from '@pando/client/stores/designStore'
import { openExternal } from '../../services/desktopRuntime'
import ArtifactGallery from './ArtifactGallery'
import TemplateGallery from './TemplateGallery'
import DesignStudio from './DesignStudio'

/**
 * DesignView is the Design section: the gallery at /design, one artifact's
 * Studio at /design/:id.
 *
 * The route is the source of truth for which artifact is open, so a Studio URL
 * can be shared, bookmarked and reloaded — the same property the preview URLs
 * themselves have.
 */
export default function DesignView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { id } = useParams<{ id: string }>()
  // Which of the two galleries is showing. It is local state on purpose: it is
  // a view preference, not something a shared URL should carry.
  const [tab, setTab] = useState<'artifacts' | 'templates'>('artifacts')

  const status = useDesignStore((s) => s.status)
  const artifacts = useDesignStore((s) => s.artifacts)
  const loading = useDesignStore((s) => s.loading)
  const activeId = useDesignStore((s) => s.activeId)
  const fetchStatus = useDesignStore((s) => s.fetchStatus)
  const fetchArtifacts = useDesignStore((s) => s.fetchArtifacts)
  const openArtifact = useDesignStore((s) => s.openArtifact)
  const closeArtifact = useDesignStore((s) => s.closeArtifact)
  const canvasURL = useDesignStore((s) => s.canvasURL)

  // The canvas opens in its own window: it is the overview of every artifact,
  // and it belongs beside Pando rather than inside one of its panes.
  const openCanvas = async () => {
    const url = await canvasURL()
    if (url) void openExternal(url)
  }

  useEffect(() => {
    void fetchStatus()
  }, [fetchStatus])

  useEffect(() => {
    if (!status?.enabled) return
    void fetchArtifacts()
  }, [status?.enabled, fetchArtifacts])

  // The route drives the store, never the other way round.
  useEffect(() => {
    if (!status?.enabled) return
    if (id) {
      if (id !== activeId) void openArtifact(id)
    } else if (activeId) {
      closeArtifact()
    }
  }, [id, activeId, status?.enabled, openArtifact, closeArtifact])

  if (status && !status.enabled) {
    return <DesignDisabled />
  }

  const artifact = artifacts.find((a) => a.id === id)

  if (id && artifact) {
    return <DesignStudio artifact={artifact} onBack={() => navigate('/design')} />
  }

  if (id && !artifact && !loading && activeId !== id) {
    return (
      <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 13 }}>
        {t('design.notFound')}
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.6rem 1rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
        }}
      >
        <FontAwesomeIcon icon={faPalette} style={{ fontSize: 12, color: 'var(--primary)' }} />
        <span style={{ fontSize: 14, fontWeight: 600 }}>{t('design.title')}</span>
        {status && !status.renderer && (
          <span style={{ fontSize: 10.5, color: 'var(--warning, var(--fg-muted))', marginLeft: '0.5rem' }}>
            <FontAwesomeIcon icon={faTriangleExclamation} style={{ fontSize: 10, marginRight: 4 }} />
            {t('design.noRenderer')}
          </span>
        )}
        <div style={{ marginLeft: 'auto', display: 'flex', gap: '0.25rem' }}>
          <button
            type="button"
            onClick={() => void openCanvas()}
            title={t('design.openCanvas')}
            style={{
              padding: '0.25rem 0.6rem',
              fontSize: 12,
              cursor: 'pointer',
              borderRadius: 4,
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg-muted)',
              marginRight: '0.35rem',
            }}
          >
            <FontAwesomeIcon icon={faBorderAll} style={{ fontSize: 10, marginRight: 5 }} />
            {t('design.canvasWindow')}
          </button>
          {(['artifacts', 'templates'] as const).map((name) => (
            <button
              key={name}
              type="button"
              onClick={() => setTab(name)}
              style={{
                padding: '0.25rem 0.6rem',
                fontSize: 12,
                cursor: 'pointer',
                borderRadius: 4,
                border: '1px solid var(--border)',
                background: tab === name ? 'var(--surface)' : 'transparent',
                color: tab === name ? 'var(--fg)' : 'var(--fg-muted)',
              }}
            >
              {t(`design.tab.${name}`)}
            </button>
          ))}
        </div>
      </div>
      {tab === 'templates' ? (
        <TemplateGallery />
      ) : (
        <ArtifactGallery artifacts={artifacts} loading={loading} onOpen={(artifactId) => navigate(`/design/${artifactId}`)} />
      )}
    </div>
  )
}

function DesignDisabled() {
  const { t } = useTranslation()
  return (
    <div style={{ padding: '2rem', maxWidth: 620, color: 'var(--fg-muted)', fontSize: 13, lineHeight: 1.7 }}>
      <FontAwesomeIcon icon={faPalette} style={{ fontSize: 22, marginBottom: '0.75rem', display: 'block' }} />
      <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--fg)', marginBottom: '0.5rem' }}>
        {t('design.disabledTitle')}
      </div>
      <p>{t('design.disabledBody')}</p>
      <pre
        style={{
          marginTop: '0.75rem',
          padding: '0.6rem 0.8rem',
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          fontSize: 12,
          overflowX: 'auto',
        }}
      >{`[design]\nEnabled = true`}</pre>
    </div>
  )
}
