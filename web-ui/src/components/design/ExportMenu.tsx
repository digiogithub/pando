import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useDesignStore, type ExportFormat } from '@pando/client/stores/designStore'
import { getBaseURL } from '@pando/client/services/api'
import api from '@pando/client/services/api'
import { saveUrlToDisk } from '../../services/desktopRuntime'
import { Button, Menu, MenuItem } from '@/components/ui'
import { Upload } from '@/components/ui/icons'

interface ExportMenuProps {
  artifactId: string
  /** Deck slide to export for PNG; 0 exports the whole document. */
  slide: number
}

const FORMATS: ExportFormat[] = ['html', 'png', 'pdf']

/**
 * ExportMenu writes an export on the server and then hands the user the file.
 *
 * The download is a second request on purpose: an export can be a multi-megabyte
 * PDF, and routing those bytes through the JSON response that reports where the
 * file landed would make the Studio feel like it hung.
 */
export default function ExportMenu({ artifactId, slide }: ExportMenuProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const exporting = useDesignStore((s) => s.exporting)
  const exportArtifact = useDesignStore((s) => s.exportArtifact)

  const run = async (format: ExportFormat) => {
    setOpen(false)
    const downloadPath = await exportArtifact(artifactId, format, { slide: format === 'png' ? slide : 0 })
    if (!downloadPath) return
    // The download endpoint sits behind the API token like every other route,
    // and a plain <a href> cannot set a header, so the token rides the query
    // string the same way the SSE streams do.
    const token = api.getToken()
    const url = `${getBaseURL()}${downloadPath}${token ? `&token=${encodeURIComponent(token)}` : ''}`
    // saveUrlToDisk is window.open in a browser and a native save dialog in the
    // desktop shell, where a webview download would otherwise go nowhere.
    await saveUrlToDisk(url, `${artifactId}.${format}`)
  }

  return (
    <>
      <Button
        ref={triggerRef}
        size="sm"
        variant="secondary"
        icon={<Upload size={12} />}
        loading={exporting}
        onClick={() => setOpen((v) => !v)}
      >
        {t('design.export.label')}
      </Button>
      <Menu open={open} onClose={() => setOpen(false)} anchorRef={triggerRef} placement="bottom-end" aria-label={t('design.export.label')}>
        {FORMATS.map((format) => (
          <MenuItem key={format} onSelect={() => void run(format)}>
            {t(`design.export.${format}`)}
          </MenuItem>
        ))}
      </Menu>
    </>
  )
}
