import { useEffect, useState } from 'react'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { QuestionAnswer } from '@pando/client/types'

/**
 * QuestionDialog surfaces AskUserQuestion prompts emitted by the agent. It mirrors
 * the TUI question dialog: the user steps through one or more questions, selects
 * options (single or multi), can provide a free-text "Other" answer, reviews a
 * summary and confirms. The agent blocks server-side until the user responds.
 *
 * Only the first pending request is shown at a time.
 */
export default function QuestionDialog() {
  const pending = useSessionStore((s) => s.pendingQuestions)
  const respond = useSessionStore((s) => s.respondQuestion)
  const cancel = useSessionStore((s) => s.cancelQuestion)

  const req = pending[0]

  // Per-question selected labels and free-text "Other" answers.
  const [selected, setSelected] = useState<Record<number, string[]>>({})
  const [otherText, setOtherText] = useState<Record<number, string>>({})
  const [qIndex, setQIndex] = useState(0)
  const [summary, setSummary] = useState(false)

  // Reset local state whenever a new request takes the foreground.
  useEffect(() => {
    setSelected({})
    setOtherText({})
    setQIndex(0)
    setSummary(false)
  }, [req?.id])

  if (!req) return null

  const questions = req.questions
  const q = questions[qIndex]

  const toggleOption = (label: string) => {
    setSelected((prev) => {
      const cur = prev[qIndex] ?? []
      if (q.multi_select) {
        return {
          ...prev,
          [qIndex]: cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label],
        }
      }
      // Single select: replace.
      return { ...prev, [qIndex]: cur.includes(label) ? [] : [label] }
    })
  }

  const next = () => {
    if (qIndex < questions.length - 1) {
      setQIndex(qIndex + 1)
    } else {
      setSummary(true)
    }
  }

  const prev = () => {
    if (summary) {
      setSummary(false)
      setQIndex(questions.length - 1)
    } else if (qIndex > 0) {
      setQIndex(qIndex - 1)
    }
  }

  const confirm = () => {
    const answers: QuestionAnswer[] = questions.map((item, i) => ({
      questionId: item.id,
      selected: selected[i] ?? [],
      otherText: otherText[i] ?? '',
    }))
    void respond(req.id, req.session_id, answers)
  }

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1100,
      }}
    >
      <div
        style={{
          background: 'var(--card-bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-lg)',
          padding: '1.5rem',
          width: 520,
          maxWidth: '92vw',
          maxHeight: '85vh',
          overflowY: 'auto',
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
      >
        {summary ? (
          <>
            <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '0.75rem' }}>
              Revisa tus respuestas
            </h3>
            <div style={{ marginBottom: '1rem' }}>
              {questions.map((item, i) => {
                const labels = [...(selected[i] ?? [])]
                if (otherText[i]) labels.push(`Otro: ${otherText[i]}`)
                return (
                  <div key={item.id} style={{ marginBottom: '0.75rem' }}>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--fg)' }}>{item.question}</div>
                    <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>
                      → {labels.length > 0 ? labels.join(', ') : '(sin selección)'}
                    </div>
                  </div>
                )
              })}
            </div>
          </>
        ) : (
          <>
            <div style={{ fontSize: 11, color: 'var(--fg-muted)', marginBottom: '0.25rem' }}>
              {questions.length > 1 ? `Pregunta ${qIndex + 1}/${questions.length}` : 'Pregunta'}
              {q.header ? ` · ${q.header}` : ''}
            </div>
            <h3 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)', marginBottom: '0.75rem' }}>
              {q.question}
            </h3>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem', marginBottom: '0.75rem' }}>
              {q.options.map((opt) => {
                const isOn = (selected[qIndex] ?? []).includes(opt.label)
                return (
                  <button
                    key={opt.label}
                    onClick={() => toggleOption(opt.label)}
                    style={{
                      textAlign: 'left',
                      padding: '0.6rem 0.75rem',
                      borderRadius: 'var(--radius-sm)',
                      border: `1px solid ${isOn ? 'var(--primary)' : 'var(--border)'}`,
                      background: isOn ? 'color-mix(in srgb, var(--primary) 15%, transparent)' : 'transparent',
                      color: 'var(--fg)',
                      fontSize: 13,
                      cursor: 'pointer',
                      fontFamily: 'inherit',
                    }}
                  >
                    <span style={{ marginRight: 8 }}>
                      {q.multi_select ? (isOn ? '☑' : '☐') : isOn ? '◉' : '○'}
                    </span>
                    <strong>{opt.label}</strong>
                    {opt.description ? (
                      <span style={{ color: 'var(--fg-muted)' }}> — {opt.description}</span>
                    ) : null}
                  </button>
                )
              })}

              <input
                type="text"
                value={otherText[qIndex] ?? ''}
                placeholder="Otra respuesta (texto libre)..."
                onChange={(e) => setOtherText((prev) => ({ ...prev, [qIndex]: e.target.value }))}
                style={{
                  padding: '0.6rem 0.75rem',
                  borderRadius: 'var(--radius-sm)',
                  border: '1px solid var(--border)',
                  background: 'transparent',
                  color: 'var(--fg)',
                  fontSize: 13,
                  fontFamily: 'inherit',
                }}
              />
            </div>
          </>
        )}

        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'space-between', flexWrap: 'wrap' }}>
          <button
            onClick={() => void cancel(req.id, req.session_id)}
            style={{
              padding: '0.5rem 1rem',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--fg)',
              fontSize: 13,
              cursor: 'pointer',
              fontFamily: 'inherit',
            }}
          >
            Cancelar
          </button>

          <div style={{ display: 'flex', gap: '0.5rem' }}>
            {(qIndex > 0 || summary) && (
              <button
                onClick={prev}
                style={{
                  padding: '0.5rem 1rem',
                  borderRadius: 'var(--radius-sm)',
                  border: '1px solid var(--border)',
                  background: 'transparent',
                  color: 'var(--fg)',
                  fontSize: 13,
                  cursor: 'pointer',
                  fontFamily: 'inherit',
                }}
              >
                Atrás
              </button>
            )}
            <button
              onClick={summary ? confirm : next}
              style={{
                padding: '0.5rem 1rem',
                borderRadius: 'var(--radius-sm)',
                border: 'none',
                background: 'var(--primary)',
                color: 'white',
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
                fontFamily: 'inherit',
              }}
            >
              {summary ? 'Confirmar' : qIndex < questions.length - 1 ? 'Siguiente' : 'Revisar'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
