import { useCallback, useRef, useState, type ReactNode } from 'react'
import ConfirmDialog from './ConfirmDialog'
import PromptDialog from './PromptDialog'

interface ConfirmOptions {
  title: string
  message: string
  confirmLabel?: string
  dangerous?: boolean
}

interface PromptOptions {
  title: string
  label?: string
  defaultValue?: string
  confirmLabel?: string
}

type Pending =
  | { kind: 'confirm'; options: ConfirmOptions; resolve: (ok: boolean) => void }
  | { kind: 'prompt'; options: PromptOptions; resolve: (value: string | null) => void }

/**
 * Promise-based confirm/prompt backed by in-app dialogs. Native window.confirm()
 * and window.prompt() silently return false/null inside the Wails desktop
 * webview on macOS, so UI code must not rely on them. Render `dialogs` once in
 * the component tree.
 */
export function useDialogs(): {
  confirm: (options: ConfirmOptions) => Promise<boolean>
  prompt: (options: PromptOptions) => Promise<string | null>
  dialogs: ReactNode
} {
  const [pending, setPending] = useState<Pending | null>(null)
  const pendingRef = useRef<Pending | null>(null)

  const open = useCallback((next: Pending) => {
    // Settle a dialog that is being replaced so its awaiting caller never hangs.
    const prev = pendingRef.current
    if (prev?.kind === 'confirm') prev.resolve(false)
    else if (prev?.kind === 'prompt') prev.resolve(null)
    pendingRef.current = next
    setPending(next)
  }, [])

  const settle = useCallback(() => {
    pendingRef.current = null
    setPending(null)
  }, [])

  const confirm = useCallback(
    (options: ConfirmOptions) => new Promise<boolean>((resolve) => open({ kind: 'confirm', options, resolve })),
    [open],
  )

  const prompt = useCallback(
    (options: PromptOptions) => new Promise<string | null>((resolve) => open({ kind: 'prompt', options, resolve })),
    [open],
  )

  let dialogs: ReactNode = null
  if (pending?.kind === 'confirm') {
    dialogs = (
      <ConfirmDialog
        {...pending.options}
        onConfirm={() => {
          settle()
          pending.resolve(true)
        }}
        onCancel={() => {
          settle()
          pending.resolve(false)
        }}
      />
    )
  } else if (pending?.kind === 'prompt') {
    dialogs = (
      <PromptDialog
        {...pending.options}
        onSubmit={(value) => {
          settle()
          pending.resolve(value)
        }}
        onCancel={() => {
          settle()
          pending.resolve(null)
        }}
      />
    )
  }

  return { confirm, prompt, dialogs }
}
