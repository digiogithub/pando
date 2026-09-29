import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { EvaluatorSessionScore } from '@pando/client/types'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import { Badge, Button } from '@/components/ui'
import type { BadgeTone } from '@/components/ui'
import EmptyState from '@/components/shared/EmptyState'

interface SessionsListProps {
  sessions: EvaluatorSessionScore[]
}

function rewardTone(r: number): BadgeTone {
  if (r >= 0.7) return 'success'
  if (r >= 0.4) return 'warning'
  return 'danger'
}

function SessionRow({ s }: { s: EvaluatorSessionScore }) {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const setActiveSession = useSessionStore((st) => st.setActiveSession)

  const components = Object.entries(s.components?.components ?? {})
  const weights = s.components?.weights ?? {}
  const judge = s.judge_analysis

  const openInChat = async () => {
    await setActiveSession(s.session_id)
    navigate('/chat')
  }

  return (
    <div className="border-b border-border px-4 py-3">
      <div className="flex items-center justify-between gap-3">
        <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          <div className="truncate text-sm font-semibold text-fg" title={s.session_id}>
            {s.title || s.session_id}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted">
            <span>{new Date(s.evaluated_at * 1000).toLocaleString()}</span>
            <span>{s.message_count} messages</span>
            <span>{s.corrections} correction{s.corrections === 1 ? '' : 's'}</span>
            {s.feedback && <Badge tone={s.feedback === 'good' ? 'success' : 'danger'}>feedback: {s.feedback}</Badge>}
            {s.judge_model && <Badge>judged</Badge>}
          </div>
        </button>
        <div className="flex flex-shrink-0 items-center gap-2">
          <Badge tone={rewardTone(s.reward)}>{s.reward.toFixed(2)}</Badge>
          <Button size="sm" variant="ghost" onClick={openInChat}>
            Open
          </Button>
        </div>
      </div>

      {open && (
        <div className="mt-3 flex flex-col gap-3 text-xs text-fg">
          <div>
            <div className="mb-1 font-semibold text-muted">Score components (score x weight)</div>
            {components.length === 0 ? (
              <div className="text-muted">No decomposition stored for this session (scored by an older version).</div>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {components.map(([name, score]) => (
                  <Badge key={name}>
                    {name} {score.toFixed(2)} x {(weights[name] ?? 0).toFixed(2)}
                  </Badge>
                ))}
              </div>
            )}
          </div>

          {s.pattern_hits.length > 0 && (
            <div>
              <div className="mb-1 font-semibold text-muted">Correction pattern hits</div>
              <ul className="flex flex-col gap-1">
                {s.pattern_hits.map((h) => (
                  <li key={`${h.index}-${h.pattern}`} className="font-mono">
                    <span className="text-muted">{h.pattern}</span> on &quot;{h.snippet}&quot; (weight {h.weight.toFixed(2)})
                  </li>
                ))}
              </ul>
            </div>
          )}

          {s.feedback_note && (
            <div>
              <span className="font-semibold text-muted">Feedback note: </span>
              {s.feedback_note}
            </div>
          )}

          <div className="flex flex-wrap gap-x-6 gap-y-2">
            <div>
              <div className="mb-1 font-semibold text-muted">Prompt variants used</div>
              {s.variants.length === 0 ? (
                <span className="text-muted">none (embedded defaults)</span>
              ) : (
                s.variants.map((v) => (
                  <div key={v.section} className="font-mono">
                    {v.section}: {v.variant}
                  </div>
                ))
              )}
            </div>
            <div>
              <div className="mb-1 font-semibold text-muted">Skills injected</div>
              {s.skills.length === 0 ? (
                <span className="text-muted">none</span>
              ) : (
                s.skills.map((k) => <div key={k.id}>{k.title || k.id}</div>)
              )}
            </div>
          </div>

          {judge && (
            <div>
              <div className="mb-1 font-semibold text-muted">
                Judge ({s.judge_model}, {s.judge_prompt_tokens + s.judge_completion_tokens} tokens)
                {judge.task_type ? `, task ${judge.task_type}` : ''}
              </div>
              <p className="whitespace-pre-wrap leading-snug">{judge.reasoning}</p>
              {judge.key_points && judge.key_points.length > 0 && (
                <ul className="mt-1 list-disc pl-4">
                  {judge.key_points.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default function SessionsList({ sessions }: SessionsListProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-border">
      <div className="border-b border-border bg-shell px-4 py-2.5 text-sm font-semibold text-fg">
        Recent evaluated sessions
      </div>
      {sessions.length === 0 ? (
        <EmptyState
          title="No sessions evaluated yet"
          description="Sessions are scored when you switch away from them, after they go idle, and by the startup backfill. See the warning above or run `pando evaluator doctor`."
        />
      ) : (
        <div className="flex-1 overflow-y-auto">
          {sessions.map((s) => (
            <SessionRow key={s.id} s={s} />
          ))}
        </div>
      )}
    </div>
  )
}
