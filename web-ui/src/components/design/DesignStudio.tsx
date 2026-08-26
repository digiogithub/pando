import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import {
  faArrowLeft, faRotate, faArrowUpRightFromSquare, faComments,
  faCrosshairs, faClockRotateLeft, faSpinner,
} from '@fortawesome/free-solid-svg-icons'
import { useDesignStore, type DesignArtifact } from '@pando/client/stores/designStore'
import ChatView from '@/components/chat/ChatView'
import PreviewFrame from './PreviewFrame'
import InspectorPanel from './InspectorPanel'
import VersionTimeline from './VersionTimeline'
import SlideStrip from './SlideStrip'
import { openExternal } from '../../services/desktopRuntime'
import ExportMenu from './ExportMenu'

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
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Toolbar */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.4rem 0.6rem',
          borderBottom: '1px solid var(--border)',
          flexShrink: 0,
          flexWrap: 'wrap',
        }}
      >
        <button
          onClick={onBack}
          title={t('design.backToGallery')}
          style={toolbarButton}
        >
          <FontAwesomeIcon icon={faArrowLeft} style={{ fontSize: 10 }} />
        </button>
        <span style={{ fontSize: 13, fontWeight: 600 }}>{artifact.title}</span>
        <span style={{ fontSize: 10.5, color: 'var(--fg-muted)' }}>
          {t(`design.kind.${artifact.kind}`)} · v{artifact.current_version}
        </span>

        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
          <button
            onClick={() => void render(artifact.id)}
            disabled={rendering}
            title={t('design.renderHint')}
            style={{ ...toolbarButton, cursor: rendering ? 'wait' : 'pointer' }}
          >
            <FontAwesomeIcon icon={rendering ? faSpinner : faRotate} spin={rendering} style={{ fontSize: 10 }} />
            {t('design.render')}
          </button>
          {artifact.url && (
            <button
              onClick={() => void openExternal(artifact.url!)}
              title={t('design.openExternal')}
              style={toolbarButton}
            >
              <FontAwesomeIcon icon={faArrowUpRightFromSquare} style={{ fontSize: 10 }} />
            </button>
          )}
          <ExportMenu artifactId={artifact.id} slide={slide} />
        </div>

        {isMobile && (
          <div style={{ display: 'flex', gap: '0.3rem', width: '100%', marginTop: '0.35rem' }}>
            <PaneTab active={pane === 'chat'} onClick={() => setPane('chat')} icon={faComments} label={t('design.pane.chat')} />
            <PaneTab active={pane === 'canvas'} onClick={() => setPane('canvas')} icon={faRotate} label={t('design.pane.canvas')} />
            <PaneTab active={pane === 'side'} onClick={() => setPane('side')} icon={faCrosshairs} label={t('design.pane.inspector')} />
          </div>
        )}
      </div>

      <div style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
        {showChat && (
          <div
            style={{
              width: isMobile ? '100%' : 380,
              flexShrink: 0,
              borderRight: isMobile ? 'none' : '1px solid var(--border)',
              display: 'flex',
              flexDirection: 'column',
              minWidth: 0,
              overflow: 'hidden',
            }}
          >
            {selection && (
              <div
                style={{
                  padding: '0.35rem 0.6rem',
                  borderBottom: '1px solid var(--border)',
                  fontSize: 10.5,
                  color: 'var(--fg-muted)',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.35rem',
                }}
              >
                <FontAwesomeIcon icon={faCrosshairs} style={{ fontSize: 9, color: 'var(--primary)' }} />
                <code style={{ color: 'var(--primary)' }}>{selection.selection}</code>
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {selection.text}
                </span>
              </div>
            )}
            <div style={{ flex: 1, minHeight: 0 }}>
              <ChatView />
            </div>
          </div>
        )}

        {showCanvas && (
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0, background: 'var(--bg)' }}>
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
          <div
            style={{
              width: isMobile ? '100%' : 300,
              flexShrink: 0,
              borderLeft: isMobile ? 'none' : '1px solid var(--border)',
              display: 'flex',
              flexDirection: 'column',
              minWidth: 0,
            }}
          >
            <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
              <SideTab active={sidePanel === 'inspector'} onClick={() => setSidePanel('inspector')} icon={faCrosshairs} label={t('design.inspector.title')} />
              <SideTab active={sidePanel === 'versions'} onClick={() => setSidePanel('versions')} icon={faClockRotateLeft} label={t('design.versions.title')} />
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

const toolbarButton: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.35rem',
  padding: '0.25rem 0.5rem',
  fontSize: 11,
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-sm)',
  color: 'var(--fg-muted)',
  cursor: 'pointer',
  textDecoration: 'none',
}

function PaneTab({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: typeof faComments; label: string }) {
  return (
    <button
      onClick={onClick}
      style={{
        flex: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '0.3rem',
        padding: '0.3rem',
        fontSize: 11,
        background: active ? 'var(--primary)' : 'var(--surface)',
        color: active ? 'white' : 'var(--fg-muted)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-sm)',
        cursor: 'pointer',
      }}
    >
      <FontAwesomeIcon icon={icon} style={{ fontSize: 10 }} />
      {label}
    </button>
  )
}

function SideTab({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: typeof faComments; label: string }) {
  return (
    <button
      onClick={onClick}
      style={{
        flex: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '0.3rem',
        padding: '0.4rem',
        fontSize: 11,
        background: active ? 'var(--surface)' : 'transparent',
        color: active ? 'var(--fg)' : 'var(--fg-muted)',
        border: 'none',
        borderBottom: active ? '2px solid var(--primary)' : '2px solid transparent',
        cursor: 'pointer',
      }}
    >
      <FontAwesomeIcon icon={icon} style={{ fontSize: 10 }} />
      {label}
    </button>
  )
}
