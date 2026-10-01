import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLayoutStore } from '@pando/client/stores/layoutStore'
import type { ContextFilterInfo } from '@pando/client/types'
import { interpolate } from '@/utils/interpolate'
import { ChevronDown, ChevronRight, Funnel, TriangleAlert } from '@/components/ui/icons'

const SOURCE_ORDER = ['code', 'kb', 'events', 'memory']
const SOURCE_DEFAULTS: Record<string, string> = { code: 'Code', kb: 'KB', events: 'Events', memory: 'Memories' }

/**
 * Visibility policy: the full chat always shows the notice. Simple chat mode
 * keeps the conversation uncluttered, so it only surfaces a filter pass that
 * actually removed something.
 */
function contextFilterVisible(info: ContextFilterInfo, simpleMode: boolean): boolean {
  return !simpleMode || info.dropped > 0
}

/**
 * Compact "Context filter: kept 4/9 · 38 ms" row for the decision-model
 * relevance filter, expandable to the per-source counts and the router used.
 */
export default function ContextFilterNotice({ info }: { info: ContextFilterInfo }) {
  const { t } = useTranslation()
  const simple = useLayoutStore((s) => s.chatMode === 'simple')
  const [open, setOpen] = useState(false)
  if (!contextFilterVisible(info, simple)) return null

  const tt = (key: string, def: string, vars: Record<string, string | number> = {}) =>
    interpolate(t(key, { defaultValue: def, ...vars }), vars)

  const total = info.kept + info.dropped
  const partial = !!info.reason
  const sources = SOURCE_ORDER.filter((s) => info.bySource?.[s]).concat(
    Object.keys(info.bySource ?? {}).filter((s) => !SOURCE_ORDER.includes(s)),
  )
  const detailed = sources.length > 0 || !!info.routerModel || partial
  const summary =
    tt('chat.contextFilter.summary', 'Context filter: kept {{kept}}/{{total}}', { kept: info.kept, total }) +
    (typeof info.latencyMs === 'number' ? ` · ${info.latencyMs} ms` : '')
  const Chevron = open ? ChevronDown : ChevronRight

  return (
    <div className="flex flex-col gap-1" data-testid="context-filter-row">
      <div
        className={`chat-routing${partial ? ' chat-routing--warn' : ''}`}
        title={info.notice?.trim()}
        data-partial={partial || undefined}
      >
        {partial ? (
          <TriangleAlert size={12} className="chat-routing-icon" />
        ) : (
          <Funnel size={12} className="chat-routing-icon" />
        )}
        <span className="chat-routing-text">{summary}</span>
        {detailed && (
          <button
            type="button"
            className="chat-routing-toggle"
            aria-expanded={open}
            aria-label={open ? t('chat.contextFilter.hideDetails', 'Hide details') : t('chat.contextFilter.showDetails', 'Show details')}
            onClick={() => setOpen((v) => !v)}
          >
            <Chevron size={12} />
          </button>
        )}
      </div>
      {open && detailed && (
        <ul className="chat-routing chat-routing-details" data-testid="context-filter-details">
          {sources.map((src) => {
            const c = info.bySource![src]
            return (
              <li key={src} data-testid={`context-filter-source-${src}`}>
                {t(`chat.contextFilter.sources.${src}`, SOURCE_DEFAULTS[src] ?? src)}:{' '}
                {tt('chat.contextFilter.keptOf', 'kept {{kept}}/{{total}}', { kept: c.kept, total: c.kept + c.dropped })}
              </li>
            )
          })}
          {typeof info.threshold === 'number' && (
            <li>{tt('chat.contextFilter.threshold', 'Threshold: {{value}}', { value: info.threshold })}</li>
          )}
          {info.routerModel && (
            <li data-testid="context-filter-router">
              {t('chat.contextFilter.router', 'Decision model')}: {info.routerProvider ? `${info.routerProvider}/` : ''}
              {info.routerModel}
            </li>
          )}
          {partial && (
            <li data-testid="context-filter-partial">
              {tt('chat.contextFilter.partial', 'Partial result: {{reason}}', { reason: info.reason!.replace(/^partial:/, '') })}
            </li>
          )}
        </ul>
      )}
    </div>
  )
}
