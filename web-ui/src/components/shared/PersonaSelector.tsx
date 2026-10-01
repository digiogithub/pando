import { useCallback, useEffect, useRef, useState } from 'react'
import { UserRound, ChevronDown, TriangleAlert } from '@/components/ui/icons'
import { Menu, MenuItem } from '@/components/ui'
import api from '@pando/client/services/api'
import { useToastStore } from '@pando/client/stores/toastStore'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { usePersonaRoutingStore } from '@pando/client/stores/personaRoutingStore'
import type { ActivePersonaResponse } from '@pando/client/types'

function formatPersonaName(name: string): string {
  if (!name) return 'Auto'
  return name
    .split('-')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

/** Label of the Auto entry: "Auto (software-engineer)" once a persona was applied. */
export function autoLabel(applied: string | null | undefined): string {
  return applied ? `Auto (${applied})` : 'Auto'
}

export default function PersonaSelector() {
  const [personas, setPersonas] = useState<string[]>([])
  const [active, setActive] = useState<string>('')
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [auto, setAuto] = useState(false)
  const [decisionModel, setDecisionModel] = useState(false)
  const [fetchedApplied, setFetchedApplied] = useState('')
  const [routerProblem, setRouterProblem] = useState('')
  const buttonRef = useRef<HTMLButtonElement>(null)

  const activeSessionId = useSessionStore((s) => s.activeSessionId)
  const isStreaming = useSessionStore((s) => s.isStreaming)
  const noticeApplied = usePersonaRoutingStore((s) => s.applied)
  const noticeCount = usePersonaRoutingStore((s) => s.noticeCount)

  useEffect(() => {
    api.get<{ personas: string[] }>('/api/v1/personas').then((d) => setPersonas(d.personas)).catch(() => {})
  }, [])

  // Re-read the selection (and the persona auto-selection applied to the
  // session) on mount, on session change, when a turn finishes and when a
  // "Persona:" notice arrives.
  const refresh = useCallback(
    (isCancelled: () => boolean = () => false) => {
      const q = activeSessionId ? `?sessionId=${encodeURIComponent(activeSessionId)}` : ''
      api
        .get<ActivePersonaResponse>(`/api/v1/personas/active${q}`)
        .then((d) => {
          if (isCancelled()) return
          setActive(d.active ?? '')
          setAuto(!!d.auto)
          setDecisionModel(!!d.decisionModel)
          setFetchedApplied(d.applied ?? '')
        })
        .catch(() => {})
    },
    [activeSessionId],
  )

  useEffect(() => {
    if (isStreaming) return
    let cancelled = false
    refresh(() => cancelled)
    return () => {
      cancelled = true
    }
  }, [refresh, isStreaming, noticeCount])

  // The decision router health, only while the option is in use.
  useEffect(() => {
    if (!decisionModel) {
      setRouterProblem('')
      return
    }
    let cancelled = false
    api
      .get<{ ok: boolean; report?: { ok: boolean; problems?: string[] }; error?: string }>('/api/v1/model-auto-mode/router/health')
      .then((r) => {
        if (cancelled) return
        const ok = r.report ? r.report.ok : r.ok
        setRouterProblem(ok ? '' : (r.report?.problems?.[0] ?? r.error ?? 'The decision router is unhealthy'))
      })
      .catch((e) => {
        if (!cancelled) setRouterProblem(e instanceof Error ? e.message : 'The decision router is unhealthy')
      })
    return () => {
      cancelled = true
    }
  }, [decisionModel, open])

  const applied = auto ? (noticeApplied ?? (fetchedApplied || null)) : null
  const autoText = autoLabel(applied)
  const labelOf = (name: string) => (name ? formatPersonaName(name) : autoText)

  async function selectPersona(name: string) {
    setOpen(false)
    if (loading) return
    setLoading(true)
    try {
      await api.put('/api/v1/personas/active', { name })
      setActive(name)
      refresh()
    } catch (e) {
      useToastStore.getState().addToast(e instanceof Error ? e.message : 'Failed to change persona', 'error')
    } finally {
      setLoading(false)
    }
  }

  const options = ['', ...personas]

  return (
    <div className="flex items-center">
      <button
        ref={buttonRef}
        onClick={() => setOpen((o) => !o)}
        title={`Persona: ${labelOf(active)}${routerProblem ? ' (decision router unhealthy, using the fallback model)' : ''}`}
        data-active={!!active || undefined}
        data-open={open || undefined}
        className={`persona-trigger${loading ? ' opacity-60' : ''}`}
      >
        <UserRound size={14} />
        <span className="persona-label">{labelOf(active)}</span>
        {routerProblem && <TriangleAlert size={12} className="text-warning" aria-label="Decision router unhealthy" data-testid="persona-router-warning" />}
        <ChevronDown size={14} className="opacity-60" />
      </button>

      <Menu open={open} onClose={() => setOpen(false)} anchorRef={buttonRef} placement="bottom-end" aria-label="Select persona">
        {options.map((name) => (
          <MenuItem
            key={name || '__auto__'}
            icon={<UserRound size={14} />}
            checked={name === active}
            onSelect={() => void selectPersona(name)}
          >
            {labelOf(name)}
          </MenuItem>
        ))}
        {routerProblem && (
          <div className="px-3 py-2 text-xs text-muted" role="status" data-testid="persona-router-problem">
            Decision router unhealthy: {routerProblem}. The fallback model selects the persona.
          </div>
        )}
      </Menu>
    </div>
  )
}
