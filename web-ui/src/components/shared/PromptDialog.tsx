import { useState } from 'react'
import { Button, Dialog, Input } from '@/components/ui'

interface PromptDialogProps {
  title: string
  label?: string
  defaultValue?: string
  confirmLabel?: string
  onSubmit: (value: string) => void
  onCancel: () => void
}

/** In-app replacement for window.prompt(), which the desktop webview (WKWebView) does not implement. */
export default function PromptDialog({
  title,
  label,
  defaultValue = '',
  confirmLabel = 'OK',
  onSubmit,
  onCancel,
}: PromptDialogProps) {
  const [value, setValue] = useState(defaultValue)
  const trimmed = value.trim()

  const submit = () => {
    if (trimmed) onSubmit(trimmed)
  }

  return (
    <Dialog
      open
      onClose={onCancel}
      title={title}
      size="sm"
      closeLabel="Cancel"
      hideClose
      footer={
        <>
          <Button variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit} disabled={!trimmed}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          aria-label={label ?? title}
          placeholder={label}
          onFocus={(e) => e.currentTarget.select()}
          data-autofocus
        />
      </form>
    </Dialog>
  )
}
