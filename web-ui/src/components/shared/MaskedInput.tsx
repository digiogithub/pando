import { useState } from 'react'
import { Button, IconButton, Input } from '@/components/ui'
import { Eye, EyeOff } from '@/components/ui/icons'

interface MaskedInputProps {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  actionLabel?: string
  onAction?: () => void
}

export default function MaskedInput({
  label,
  value,
  onChange,
  placeholder,
  actionLabel,
  onAction,
}: MaskedInputProps) {
  const [visible, setVisible] = useState(false)

  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</label>
      <div className="flex gap-2">
        <div className="masked-input-wrap">
          <Input
            type={visible ? 'text' : 'password'}
            value={value}
            placeholder={placeholder}
            onChange={(e) => onChange(e.target.value)}
          />
          <IconButton
            type="button"
            aria-label={visible ? 'Hide' : 'Show'}
            tooltip
            icon={visible ? <EyeOff size={14} /> : <Eye size={14} />}
            size="sm"
            className="masked-input-toggle"
            onClick={() => setVisible((v) => !v)}
          />
        </div>
        {actionLabel && onAction && (
          <Button type="button" variant="secondary" onClick={onAction} className="shrink-0 whitespace-nowrap">
            {actionLabel}
          </Button>
        )}
      </div>
    </div>
  )
}
