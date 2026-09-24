import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Dialog } from '@/components/ui'

export interface UnsavedChangesDialogProps {
  open: boolean
  /** Names of the sections holding unsaved edits. */
  sections: string[]
  /** Saves the edits; resolves true on success. The dialog stays open on failure. */
  onSave: () => Promise<boolean>
  onDiscard: () => void
  /** Stay where the user is (also Esc and click outside). */
  onCancel: () => void
}

/**
 * Asks what to do with unsaved settings before leaving a section:
 * save and continue, discard and continue, or stay.
 */
export default function UnsavedChangesDialog({ open, sections, onSave, onDiscard, onCancel }: UnsavedChangesDialogProps) {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  const [failed, setFailed] = useState(false)

  const close = (fn: () => void) => {
    setFailed(false)
    fn()
  }

  const save = async () => {
    setSaving(true)
    setFailed(false)
    try {
      const ok = await onSave()
      if (!ok) setFailed(true)
    } finally {
      setSaving(false)
    }
  }

  const names = sections.join(', ')

  return (
    <Dialog
      open={open}
      onClose={() => { if (!saving) close(onCancel) }}
      size="sm"
      title={t('settings.unsaved.title')}
      // Cancel is an explicit footer action; Esc and a click outside also cancel.
      hideClose
      footer={
        <>
          <Button variant="ghost" onClick={() => close(onCancel)} disabled={saving}>
            {t('settings.unsaved.cancel')}
          </Button>
          <Button variant="danger" onClick={() => close(onDiscard)} disabled={saving}>
            {t('settings.unsaved.discard')}
          </Button>
          <Button variant="primary" onClick={() => void save()} loading={saving} data-autofocus>
            {t('settings.unsaved.saveAndContinue')}
          </Button>
        </>
      }
    >
      <p className="settings-unsaved-text">
        {names ? t('settings.unsaved.descriptionSections', { sections: names }) : t('settings.unsaved.description')}
      </p>
      {failed && (
        <p className="settings-unsaved-error" role="alert">
          {t('settings.unsaved.saveFailed')}
        </p>
      )}
    </Dialog>
  )
}
