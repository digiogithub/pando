import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { useTranslation } from 'react-i18next'
import { useDesignStore, type DesignNode } from '@pando/client/stores/designStore'
import { useChatDraftStore } from '@pando/client/stores/chatDraftStore'
import { Button, IconButton, Input, Tabs } from '@/components/ui'
import { Crosshair, SendHorizontal, X } from '@/components/ui/icons'
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

  const issuesCount = critique && critique.issues.length > 0 ? ` (${critique.issues.length})` : ''

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <Tabs
        aria-label={t('design.critique.tabs.structure')}
        value={tab}
        onChange={setTab}
        items={[
          { value: 'structure', label: t('design.critique.tabs.structure') },
          { value: 'issues', label: `${t('design.critique.tabs.issues')}${issuesCount}` },
        ]}
      />

      {tab === 'issues' ? (
        <IssuePanel artifactId={artifactId} />
      ) : (
        <>
          <div className="design-inspector-search">
            <div className="design-inspector-search-row">
              <Crosshair size={11} />
              <span className="design-inspector-search-title">{t('design.inspector.title')}</span>
              <span className="design-inspector-count">{t('design.inspector.nodeCount', { count: nodesTotal })}</span>
            </div>
            <Input size="sm" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('design.inspector.filterPlaceholder')} />
          </div>

          {selection && (
            <div className="design-selection-detail">
              <div className="design-selection-detail-top">
                <code className="design-selection-code">{selection.selection}</code>
                <IconButton
                  aria-label={t('design.inspector.clearSelection')}
                  tooltip
                  icon={<X size={12} />}
                  size="sm"
                  onClick={() => setSelection(null)}
                  style={{ marginLeft: 'auto' }}
                />
              </div>
              {selectedNode?.selector && <div className="design-selection-selector">{selectedNode.selector}</div>}
              {(selectedNode?.text || selection.text) && (
                <div className="design-selection-quote">“{(selectedNode?.text || selection.text || '').slice(0, 120)}”</div>
              )}
              <Button size="sm" variant="primary" icon={<SendHorizontal size={12} />} onClick={askAboutSelection} className="design-selection-ask">
                {t('design.inspector.askAbout')}
              </Button>
              {selectedNode?.styles && Object.keys(selectedNode.styles).length > 0 && <StyleList styles={selectedNode.styles} />}
            </div>
          )}

          <div className="design-node-list">
            {filtered.length === 0 ? (
              <div className="design-node-empty">{nodesTotal === 0 ? t('design.inspector.emptyIndex') : t('design.inspector.noMatches')}</div>
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
    <div className="design-style-list">
      {entries.map(([prop, value]) => (
        <div key={prop} className="design-style-row">
          <span className="design-style-prop">{prop}:</span>
          <span className="design-style-value">{value}</span>
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
      type="button"
      onClick={() => {
        onSelect()
        if (!node.styles) onLoadStyles()
      }}
      className={clsx('design-node-row', active && 'design-node-row--active')}
    >
      <div className="design-node-row-top">
        <span className="design-node-role">{node.role || 'node'}</span>
        <span className="design-node-id">{node.node_id}</span>
        {node.slide ? <span className="design-node-slide">· s{node.slide}</span> : null}
      </div>
      {node.text && <div className="design-node-text">{node.text}</div>}
    </button>
  )
}
