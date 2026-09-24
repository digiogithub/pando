import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useDesignStore, type DesignArtifact } from '@pando/client/stores/designStore'
import ChatView from '@/components/chat/ChatView'
import { Button, IconButton, SegmentedControl, Tabs } from '@/components/ui'
import { ArrowLeft, Crosshair, ExternalLink, History, LayoutGrid, MessageSquare, RotateCw } from '@/components/ui/icons'
import PreviewFrame from './PreviewFrame'
import InspectorPanel from './InspectorPanel'
import VersionTimeline from './VersionTimeline'
import SlideStrip from './SlideStrip'
import { openExternal } from '../../services/desktopRuntime'
import ExportMenu from './ExportMenu'
import '@/styles/design.css'

const MOBILE_QUERY = '(max-width: 1024px)'

/** Which of the three panes owns the screen when it cannot show them all. */
type Pane = 'chat' | 'canvas' | 'side'
/** The right column shows one of two things at a time. */
type SidePanel = 'inspector' | 'versions'

interface DesignStudioProps {
  artifact: DesignArtifact
  onBack: () => void
}

/**
 * DesignStudio is the three-column workspace: chat, canvas, inspector.
 *
 * The canvas is the preview server's own document in an iframe, not a
 * re-rendering of the artifact — what the user looks at is exactly what a
 * browser opened at that URL would show, which is the whole point of serving it
 * over HTTP rather than rebuilding it in React.
 */
export default function DesignStudio({ artifact, onBack }: DesignStudioProps) {
  const { t } = useTranslation()
  const reloadNonce = useDesignStore((s) => s.reloadNonce)
  const selection = useDesignStore((s) => s.selection)
  const slide = useDesignStore((s) => s.slide)
  const rendering = useDesignStore((s) => s.rendering)
  const render = useDesignStore((s) => s.render)
  const status = useDesignStore((s) => s.status)
  const canvasURL = useDesignStore((s) => s.canvasURL)

  // The canvas is a window of its own, not a pane: it holds every artifact at
  // once, so framing it inside the Studio would nest one canvas in another.
  const openCanvas = async () => {
    const url = await canvasURL()
    if (url) void openExternal(url)
  }

  const [isMobile, setIsMobile] = useState(() => window.matchMedia(MOBILE_QUERY).matches)
  useEffect(() => {
    const mql = window.matchMedia(MOBILE_QUERY)
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  // Narrow viewports get one pane at a time; the canvas is the landing pane
  // because it is the thing the section exists to show.
  const [pane, setPane] = useState<Pane>('canvas')
  const [sidePanel, setSidePanel] = useState<SidePanel>('inspector')

  const showChat = !isMobile || pane === 'chat'
  const showCanvas = !isMobile || pane === 'canvas'
  const showSide = !isMobile || pane === 'side'

  const previewURL = artifact.bridge_url
  const emptyMessage = previewEmptyMessage(t, status, artifact)

  return (
    <div className="design-view-shell">
      {/* Toolbar */}
      <div className="design-studio-toolbar">
        <IconButton aria-label={t('design.backToGallery')} tooltip icon={<ArrowLeft size={14} />} onClick={onBack} />
        <span className="design-studio-title">{artifact.title}</span>
        <span className="design-studio-meta">
          {t(`design.kind.${artifact.kind}`)} · v{artifact.current_version}
        </span>

        <div className="design-studio-actions">
          <Button size="sm" variant="secondary" icon={<RotateCw size={12} />} loading={rendering} onClick={() => void render(artifact.id)} title={t('design.renderHint')}>
            {t('design.render')}
          </Button>
          <Button size="sm" variant="secondary" icon={<LayoutGrid size={12} />} onClick={() => void openCanvas()} title={t('design.openCanvas')}>
            {t('design.canvasWindow')}
          </Button>
          {artifact.url && (
            <IconButton aria-label={t('design.openExternal')} tooltip icon={<ExternalLink size={13} />} onClick={() => void openExternal(artifact.url!)} />
          )}
          <ExportMenu artifactId={artifact.id} slide={slide} />
        </div>

        {isMobile && (
          <div className="design-pane-tabs">
            <SegmentedControl
              size="sm"
              aria-label={t('design.title')}
              value={pane}
              onChange={setPane}
              items={[
                { value: 'chat', label: t('design.pane.chat'), icon: <MessageSquare size={11} /> },
                { value: 'canvas', label: t('design.pane.canvas'), icon: <RotateCw size={11} /> },
                { value: 'side', label: t('design.pane.inspector'), icon: <Crosshair size={11} /> },
              ]}
            />
          </div>
        )}
      </div>

      <div style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
        {showChat && (
          <div className="design-chat-col" style={{ width: isMobile ? '100%' : 380, borderRight: isMobile ? 'none' : undefined }}>
            {selection && (
              <div className="design-selection-banner">
                <Crosshair size={10} />
                <code>{selection.selection}</code>
                <span className="design-selection-banner-text">{selection.text}</span>
              </div>
            )}
            <div style={{ flex: 1, minHeight: 0 }}>
              <ChatView />
            </div>
          </div>
        )}

        {showCanvas && (
          <div className="design-canvas-col">
            <PreviewFrame
              url={previewURL}
              nonce={reloadNonce}
              selectedNodeId={selection?.nodeId}
              slide={slide}
              emptyMessage={emptyMessage}
            />
            {artifact.kind === 'deck' && <SlideStrip slides={artifact.slides ?? 0} />}
          </div>
        )}

        {showSide && (
          <div className="design-side-col" style={{ width: isMobile ? '100%' : 300, borderLeft: isMobile ? 'none' : undefined }}>
            <div className="design-side-tabs">
              <Tabs
                aria-label={t('design.inspector.title')}
                value={sidePanel}
                onChange={setSidePanel}
                items={[
                  { value: 'inspector', label: t('design.inspector.title'), icon: <Crosshair size={13} /> },
                  { value: 'versions', label: t('design.versions.title'), icon: <History size={13} /> },
                ]}
              />
            </div>
            <div style={{ flex: 1, minHeight: 0 }}>
              {sidePanel === 'inspector' ? (
                <InspectorPanel artifactId={artifact.id} />
              ) : (
                <VersionTimeline artifactId={artifact.id} currentVersion={artifact.current_version} />
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

/** Explains an empty canvas instead of leaving a blank frame. */
function previewEmptyMessage(
  t: (key: string, opts?: Record<string, unknown>) => string,
  status: { preview: boolean; preview_reason?: string } | null,
  artifact: DesignArtifact,
): string {
  if (status && !status.preview && status.preview_reason) return status.preview_reason
  if (artifact.file_url && !artifact.bridge_url) return t('design.canvas.noPreviewServer')
  return t('design.canvas.notRenderedYet')
}
