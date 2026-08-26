import { useEffect, useRef, useCallback } from 'react'
import { useDesignStore, type DesignSelection } from '@pando/client/stores/designStore'

/** Message envelope shared with internal/design/preview/bridge.js. */
const BRIDGE_SOURCE = 'pando-design'

interface BridgeMessage {
  source?: string
  type?: string
  nodeId?: string
  selection?: string
  tag?: string
  text?: string
  slide?: number
  slides?: number
}

interface PreviewFrameProps {
  /** Bridged preview address; the frame renders an explanation when empty. */
  url?: string
  /** Changing this reloads the frame — a new version or a fresh render. */
  nonce: number
  /** Node the rest of the UI wants highlighted inside the preview. */
  selectedNodeId?: string
  slide?: number
  emptyMessage: string
}

/**
 * PreviewFrame is the canvas: an iframe over the preview server with the
 * selection bridge talking to it over postMessage.
 *
 * It is read-only by design (decision 2 of the plan): the user selects and asks
 * the agent, never drags or edits in place. Everything it can do is therefore a
 * message in one of two directions — a selection coming out, a select/slide
 * command going in.
 */
export default function PreviewFrame({ url, nonce, selectedNodeId, slide, emptyMessage }: PreviewFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null)
  const setSelection = useDesignStore((s) => s.setSelection)
  const setSlide = useDesignStore((s) => s.setSlide)
  const readyRef = useRef(false)

  const post = useCallback((message: Record<string, unknown>) => {
    // The preview is served from the Pando origin, but a loopback fallback
    // server is a *different* origin, so the target has to stay permissive.
    // Nothing secret travels this way: it is node ids and slide numbers.
    frameRef.current?.contentWindow?.postMessage({ source: BRIDGE_SOURCE, ...message }, '*')
  }, [])

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const data = event.data as BridgeMessage | null
      if (!data || data.source !== BRIDGE_SOURCE) return

      if (data.type === 'ready') {
        readyRef.current = true
        return
      }
      if (data.type === 'selected') {
        if (!data.nodeId) {
          setSelection(null)
          return
        }
        const selection: DesignSelection = {
          nodeId: data.nodeId,
          selection: data.selection ?? `design://${data.nodeId}`,
          tag: data.tag,
          text: data.text,
          slide: data.slide,
        }
        setSelection(selection)
        return
      }
      if (data.type === 'slide' && typeof data.slide === 'number') {
        setSlide(data.slide)
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [setSelection, setSlide])

  // A reload drops the bridge, so the ready flag has to be re-armed with it.
  useEffect(() => {
    readyRef.current = false
  }, [url, nonce])

  // Push a selection made outside the canvas (inspector row, issue link) into
  // the preview. The bridge answers with its own "selected" message, which is
  // what keeps the two views agreeing on one node.
  useEffect(() => {
    if (!selectedNodeId) return
    const timer = window.setTimeout(() => post({ type: 'select', nodeId: selectedNodeId }), 120)
    return () => window.clearTimeout(timer)
  }, [selectedNodeId, post, nonce])

  useEffect(() => {
    if (!slide) return
    const timer = window.setTimeout(() => post({ type: 'goToSlide', slide }), 120)
    return () => window.clearTimeout(timer)
  }, [slide, post, nonce])

  if (!url) {
    return (
      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '2rem',
          textAlign: 'center',
          color: 'var(--fg-muted)',
          fontSize: 13,
          lineHeight: 1.6,
        }}
      >
        {emptyMessage}
      </div>
    )
  }

  return (
    <iframe
      ref={frameRef}
      // The nonce is part of the key so a new version remounts the frame
      // instead of relying on the browser to re-request a document it thinks
      // it already has.
      key={`${url}#${nonce}`}
      src={url}
      title="design preview"
      style={{
        flex: 1,
        width: '100%',
        border: 'none',
        background: 'white',
      }}
    />
  )
}
