import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useDesignStore } from '@pando/client/stores/designStore'
import { openExternal } from '../../services/desktopRuntime'
import { Button, SegmentedControl } from '@/components/ui'
import { LayoutGrid, Palette, TriangleAlert } from '@/components/ui/icons'
import ArtifactGallery from './ArtifactGallery'
import TemplateGallery from './TemplateGallery'
import DesignStudio from './DesignStudio'
import '@/styles/design.css'

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
    return <div style={{ padding: '2rem', color: 'var(--fg-muted)', fontSize: 13 }}>{t('design.notFound')}</div>
  }

  return (
    <div className="design-view-shell">
      <div className="design-toolbar">
        <Palette size={14} />
        <span className="design-toolbar-title">{t('design.title')}</span>
        {status && !status.renderer && (
          <span className="design-toolbar-warning">
            <TriangleAlert size={11} />
            {t('design.noRenderer')}
          </span>
        )}
        <div className="design-toolbar-actions">
          <Button size="sm" variant="secondary" icon={<LayoutGrid size={12} />} onClick={() => void openCanvas()} title={t('design.openCanvas')}>
            {t('design.canvasWindow')}
          </Button>
          <SegmentedControl
            size="sm"
            aria-label={t('design.title')}
            value={tab}
            onChange={setTab}
            items={(['artifacts', 'templates'] as const).map((name) => ({ value: name, label: t(`design.tab.${name}`) }))}
          />
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
    <div className="design-disabled">
      <Palette size={22} />
      <div className="design-disabled-title">{t('design.disabledTitle')}</div>
      <p>{t('design.disabledBody')}</p>
      <pre>{`[design]\nEnabled = true`}</pre>
    </div>
  )
}
