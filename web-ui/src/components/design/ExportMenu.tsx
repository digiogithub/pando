import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import { faFileExport, faSpinner } from '@fortawesome/free-solid-svg-icons'
import { useDesignStore, type ExportFormat } from '@pando/client/stores/designStore'
import { getBaseURL } from '@pando/client/services/api'
import api from '@pando/client/services/api'
import { saveUrlToDisk } from '../../services/desktopRuntime'

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
    <div style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen((v) => !v)}
        disabled={exporting}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.35rem',
          padding: '0.25rem 0.5rem',
          fontSize: 11,
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--fg-muted)',
          cursor: exporting ? 'wait' : 'pointer',
        }}
      >
        <FontAwesomeIcon icon={exporting ? faSpinner : faFileExport} spin={exporting} style={{ fontSize: 10 }} />
        {t('design.export.label')}
      </button>

      {open && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            right: 0,
            marginTop: 4,
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-sm)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
            zIndex: 20,
            minWidth: 140,
          }}
        >
          {FORMATS.map((format) => (
            <button
              key={format}
              onClick={() => void run(format)}
              style={{
                display: 'block',
                width: '100%',
                textAlign: 'left',
                padding: '0.4rem 0.6rem',
                fontSize: 11,
                background: 'none',
                border: 'none',
                color: 'var(--fg)',
                cursor: 'pointer',
              }}
            >
              {t(`design.export.${format}`)}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
