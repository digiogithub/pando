import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faCrosshairs, faXmark, faArrowRightToBracket } from '@fortawesome/free-solid-svg-icons'
import { useDesignStore, type DesignNode } from '@pando/client/stores/designStore'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'
import IssuePanel from './IssuePanel'

interface InspectorPanelProps {
  artifactId: string
}

/**
 * InspectorPanel is the third column of the Studio: the structure index the
 * renderer produced, filtered, with the selected node's details.
 *
 * Its one job beyond looking is turning a selection into prompt context — the
 * "select and ask" half of decision 2. The chip it writes into the composer is
 * the design://<node_id> reference the agent's design_patch understands.
 */
export default function InspectorPanel({ artifactId }: InspectorPanelProps) {
  const { t } = useTranslation()
  const nodes = useDesignStore((s) => s.nodes)
  const nodesTotal = useDesignStore((s) => s.nodesTotal)
  const selection = useDesignStore((s) => s.selection)
  const setSelection = useDesignStore((s) => s.setSelection)
  const fetchNodes = useDesignStore((s) => s.fetchNodes)
  const insertIntoDraft = useChatDraftStore((s) => s.insertIntoDraft)
  const critique = useDesignStore((s) => s.critique)
  const [filter, setFilter] = useState('')
  // The structure and the findings answer different questions about the same
  // render, so they share the column rather than competing for it.
  const [tab, setTab] = useState<'structure' | 'issues'>('structure')

  const filtered = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    if (!needle) return nodes
    return nodes.filter(
      (node) =>
        node.text?.toLowerCase().includes(needle) ||
        node.selector?.toLowerCase().includes(needle) ||
        node.role?.toLowerCase().includes(needle),
    )
  }, [nodes, filter])

  const selectedNode = useMemo(
    () => nodes.find((n) => n.node_id === selection?.nodeId),
    [nodes, selection?.nodeId],
  )

  const askAboutSelection = () => {
    if (!selection) return
    const label = selectedNode?.selector || selection.tag || selection.nodeId
    insertIntoDraft(`${selection.selection} (${label})`)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border)' }}>
        {(['structure', 'issues'] as const).map((name) => (
          <button
            key={name}
            onClick={() => setTab(name)}
            style={{
              flex: 1,
              padding: '0.4rem 0.5rem',
              fontSize: 11,
              fontWeight: tab === name ? 600 : 400,
              background: 'transparent',
              border: 'none',
              borderBottom: tab === name ? '2px solid var(--primary)' : '2px solid transparent',
              color: tab === name ? 'var(--fg)' : 'var(--fg-muted)',
              cursor: 'pointer',
            }}
          >
            {t(`design.critique.tabs.${name}`)}
            {name === 'issues' && critique && critique.issues.length > 0 ? ` (${critique.issues.length})` : ''}
          </button>
        ))}
      </div>

      {tab === 'issues' ? (
        <IssuePanel artifactId={artifactId} />
      ) : (
        <>
        <div style={{ padding: '0.5rem 0.75rem', borderBottom: '1px solid var(--border)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
            <FontAwesomeIcon icon={faCrosshairs} style={{ fontSize: 11, color: 'var(--fg-muted)' }} />
            <span style={{ fontSize: 12, fontWeight: 600 }}>{t('design.inspector.title')}</span>
            <span style={{ fontSize: 11, color: 'var(--fg-muted)', marginLeft: 'auto' }}>
              {t('design.inspector.nodeCount', { count: nodesTotal })}
            </span>
          </div>
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t('design.inspector.filterPlaceholder')}
            style={{
              width: '100%',
              padding: '0.3rem 0.5rem',
              fontSize: 12,
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--fg)',
              outline: 'none',
            }}
          />
        </div>

        {selection && (
          <div style={{ padding: '0.6rem 0.75rem', borderBottom: '1px solid var(--border)', background: 'var(--surface)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
              <code style={{ fontSize: 11, color: 'var(--primary)', wordBreak: 'break-all' }}>{selection.selection}</code>
              <button
                onClick={() => setSelection(null)}
                title={t('design.inspector.clearSelection')}
                style={{ marginLeft: 'auto', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--fg-muted)' }}
              >
                <FontAwesomeIcon icon={faXmark} style={{ fontSize: 11 }} />
              </button>
            </div>
            {selectedNode?.selector && (
              <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginTop: 4, wordBreak: 'break-all' }}>
                {selectedNode.selector}
              </div>
            )}
            {(selectedNode?.text || selection.text) && (
              <div style={{ fontSize: 11, marginTop: 4, opacity: 0.85 }}>
                “{(selectedNode?.text || selection.text || '').slice(0, 120)}”
              </div>
            )}
            <button
              onClick={askAboutSelection}
              style={{
                marginTop: '0.5rem',
                display: 'flex',
                alignItems: 'center',
                gap: '0.4rem',
                padding: '0.3rem 0.55rem',
                fontSize: 11,
                background: 'var(--primary)',
                color: 'white',
                border: 'none',
                borderRadius: 'var(--radius-sm)',
                cursor: 'pointer',
              }}
            >
              <FontAwesomeIcon icon={faArrowRightToBracket} style={{ fontSize: 10 }} />
              {t('design.inspector.askAbout')}
            </button>
            {selectedNode?.styles && Object.keys(selectedNode.styles).length > 0 && (
              <StyleList styles={selectedNode.styles} />
            )}
          </div>
        )}

        <div style={{ flex: 1, overflow: 'auto' }}>
          {filtered.length === 0 ? (
            <div style={{ padding: '1rem 0.75rem', fontSize: 12, color: 'var(--fg-muted)', lineHeight: 1.6 }}>
              {nodesTotal === 0 ? t('design.inspector.emptyIndex') : t('design.inspector.noMatches')}
            </div>
          ) : (
            filtered.map((node) => (
              <NodeRow
                key={node.node_id}
                node={node}
                active={node.node_id === selection?.nodeId}
                onSelect={() =>
                  setSelection({
                    nodeId: node.node_id,
                    selection: `design://${node.node_id}`,
                    tag: node.role,
                    text: node.text,
                    slide: node.slide,
                  })
                }
                onLoadStyles={() => void fetchNodes(artifactId, { nodeId: node.node_id, styles: true })}
              />
            ))
          )}
        </div>
        </>
      )}
    </div>
  )
}

function StyleList({ styles }: { styles: Record<string, string> }) {
  const entries = Object.entries(styles).slice(0, 12)
  return (
    <div style={{ marginTop: '0.5rem', fontFamily: "'JetBrains Mono', monospace", fontSize: 10.5, lineHeight: 1.7 }}>
      {entries.map(([prop, value]) => (
        <div key={prop} style={{ display: 'flex', gap: '0.4rem' }}>
          <span style={{ color: 'var(--fg-muted)' }}>{prop}:</span>
          <span style={{ wordBreak: 'break-all' }}>{value}</span>
        </div>
      ))}
    </div>
  )
}

interface NodeRowProps {
  node: DesignNode
  active: boolean
  onSelect: () => void
  onLoadStyles: () => void
}

function NodeRow({ node, active, onSelect, onLoadStyles }: NodeRowProps) {
  return (
    <button
      onClick={() => {
        onSelect()
        if (!node.styles) onLoadStyles()
      }}
      style={{
        display: 'block',
        width: '100%',
        textAlign: 'left',
        padding: '0.35rem 0.75rem',
        background: active ? 'var(--surface-hover, var(--surface))' : 'transparent',
        borderLeft: active ? '2px solid var(--primary)' : '2px solid transparent',
        border: 'none',
        borderBottom: '1px solid var(--border)',
        cursor: 'pointer',
        color: 'var(--fg)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.4rem' }}>
        <span style={{ fontSize: 11, fontWeight: 600, color: 'var(--primary)' }}>{node.role || 'node'}</span>
        <span style={{ fontSize: 10, color: 'var(--fg-muted)' }}>{node.node_id}</span>
        {node.slide ? <span style={{ fontSize: 10, color: 'var(--fg-muted)' }}>· s{node.slide}</span> : null}
      </div>
      {node.text && (
        <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {node.text}
        </div>
      )}
    </button>
  )
}
