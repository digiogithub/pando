import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import clsx from 'clsx'
import { IconButton } from '@/components/ui'
import { Check, Copy } from '@/components/ui/icons'
import { copyText } from './setupUtils'

/** A shell command in a monospace block with a copy button. */
export function CommandLine({ command, className }: { command: string; className?: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  return (
    <div className={clsx('flex items-center gap-2 rounded-sm border border-border bg-shell py-1.5 pl-3 pr-1.5', className)}>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap font-mono text-xs text-fg">{command}</code>
      <IconButton
        size="sm"
        aria-label={t('setup.common.copy')}
        tooltip
        icon={copied ? <Check size={14} /> : <Copy size={14} />}
        onClick={() => {
          void copyText(command).then((ok) => {
            if (!ok) return
            setCopied(true)
            window.setTimeout(() => setCopied(false), 1500)
          })
        }}
      />
    </div>
  )
}

/** Selectable option card used by the scope and provider pickers. */
export function ChoiceCard({
  selected,
  disabled,
  onClick,
  icon,
  title,
  badge,
  children,
}: {
  selected: boolean
  disabled?: boolean
  onClick: () => void
  icon?: ReactNode
  title: ReactNode
  badge?: ReactNode
  children?: ReactNode
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-pressed={selected}
      onClick={onClick}
      className={clsx(
        'flex w-full items-start gap-3 rounded-md border p-3 text-left transition-colors',
        selected ? 'border-accent bg-accent-soft' : 'border-border bg-card hover:bg-hover',
        disabled && 'cursor-not-allowed opacity-50 hover:bg-card',
      )}
    >
      {icon && <span className={clsx('mt-0.5 shrink-0', selected ? 'text-accent' : 'text-muted')}>{icon}</span>}
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-center gap-2 text-sm font-medium text-fg">
          {title}
          {badge}
        </span>
        {children && <span className="text-xs text-muted">{children}</span>}
      </span>
    </button>
  )
}

/** Inline status line (info / success / warning / danger). */
export function Notice({ tone = 'info', children }: { tone?: 'info' | 'success' | 'warning' | 'danger'; children: ReactNode }) {
  const tones = {
    info: 'bg-info-soft text-fg',
    success: 'bg-success-soft text-fg',
    warning: 'bg-warning-soft text-fg',
    danger: 'bg-danger-soft text-danger',
  }
  return <div className={clsx('rounded-sm px-3 py-2 text-sm', tones[tone])}>{children}</div>
}

