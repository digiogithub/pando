import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '@pando/client/stores/sessionStore'
import type { QuestionAnswer } from '@pando/client/types'
import { Button, Dialog, Input } from '@/components/ui'
import { Check } from '@/components/ui/icons'

/**
 * QuestionDialog surfaces AskUserQuestion prompts emitted by the agent. It mirrors
 * the TUI question dialog: the user steps through one or more questions, selects
 * options (single or multi), can provide a free-text "Other" answer, reviews a
 * summary and confirms. The agent blocks server-side until the user responds.
 *
 * Only the first pending request is shown at a time.
 */
export default function QuestionDialog() {
  const { t } = useTranslation()
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

  const title = summary ? t('chat.question.reviewTitle') : q.question
  const eyebrow = summary
    ? undefined
    : `${questions.length > 1 ? t('chat.question.counter', { current: qIndex + 1, total: questions.length }) : t('chat.question.single')}${q.header ? ` · ${q.header}` : ''}`

  return (
    <Dialog
      open
      // The agent is blocked on the answer: cancelling is an explicit action.
      onClose={() => {}}
      dismissible={false}
      hideClose
      size="md"
      title={
        <>
          {eyebrow && <span className="chat-q-eyebrow">{eyebrow}</span>}
          {title}
        </>
      }
      footer={
        <div className="chat-dialog-footer-split">
          <Button variant="ghost" onClick={() => void cancel(req.id, req.session_id)}>
            {t('chat.question.cancel')}
          </Button>
          <div>
            {(qIndex > 0 || summary) && <Button onClick={prev}>{t('chat.question.back')}</Button>}
            <Button variant="primary" onClick={summary ? confirm : next}>
              {summary ? t('chat.question.confirm') : qIndex < questions.length - 1 ? t('chat.question.next') : t('chat.question.review')}
            </Button>
          </div>
        </div>
      }
    >
      {summary ? (
        <div className="chat-q-summary">
          {questions.map((item, i) => {
            const labels = [...(selected[i] ?? [])]
            if (otherText[i]) labels.push(t('chat.question.otherAnswer', { text: otherText[i] }))
            return (
              <div key={item.id}>
                <div className="chat-q-summary-q">{item.question}</div>
                <div className="chat-q-summary-a">→ {labels.length > 0 ? labels.join(', ') : t('chat.question.noSelection')}</div>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="chat-q-options" role={q.multi_select ? 'group' : 'radiogroup'}>
          {q.options.map((opt) => {
            const isOn = (selected[qIndex] ?? []).includes(opt.label)
            return (
              <button
                key={opt.label}
                type="button"
                role={q.multi_select ? 'checkbox' : 'radio'}
                aria-checked={isOn}
                className="chat-q-option"
                onClick={() => toggleOption(opt.label)}
              >
                <span className={q.multi_select ? 'chat-q-mark chat-q-mark--multi' : 'chat-q-mark'}>
                  {isOn && <Check size={11} strokeWidth={3} />}
                </span>
                <span>
                  <span className="chat-q-option-label">{opt.label}</span>
                  {opt.description ? <span className="chat-q-option-desc"> — {opt.description}</span> : null}
                </span>
              </button>
            )
          })}

          <Input
            type="text"
            value={otherText[qIndex] ?? ''}
            placeholder={t('chat.question.otherPlaceholder')}
            aria-label={t('chat.question.otherPlaceholder')}
            onChange={(e) => setOtherText((prev) => ({ ...prev, [qIndex]: e.target.value }))}
          />
        </div>
      )}
    </Dialog>
  )
}
