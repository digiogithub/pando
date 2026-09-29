import { useState } from 'react'
import type { EvaluatorDoctor } from '@pando/client/types'
import { Button } from '@/components/ui'

interface DoctorBannerProps {
  doctor: EvaluatorDoctor | null
}

/**
 * Warning shown when the evaluator is enabled but nothing was evaluated among
 * the recent sessions, or a correction pattern is broken. The full report is
 * the same text `pando evaluator doctor` prints.
 */
export default function DoctorBanner({ doctor }: DoctorBannerProps) {
  const [open, setOpen] = useState(false)
  if (!doctor) return null

  const disabled = !doctor.report.enabled
  if (!doctor.problem && !disabled) return null

  return (
    <div className="mx-6 mt-3 flex flex-col gap-2">
      <div className="settings-banner settings-banner--danger flex items-center justify-between gap-3" role="alert">
        <span>
          {disabled
            ? (doctor.report.disabled_reasons?.[0] ?? 'The self-improvement evaluator is disabled.')
            : doctor.problem}
        </span>
        <Button size="sm" variant="secondary" onClick={() => setOpen((v) => !v)}>
          {open ? 'Hide report' : 'Show report'}
        </Button>
      </div>
      {open && (
        <pre className="max-h-72 overflow-auto rounded-md border border-border bg-shell p-3 font-mono text-xs text-fg">
          {doctor.text}
        </pre>
      )}
    </div>
  )
}
