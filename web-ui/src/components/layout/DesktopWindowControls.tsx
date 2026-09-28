import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { BrandMark } from '@/components/brand'
import { Copy, Minus, Square, X } from '@/components/ui/icons'
import {
  closeWindow,
  minimiseWindow,
  onTitleBarDoubleClick,
  toggleMaximiseWindow,
  useDesktopShell,
  useHasWindowTitleBar,
  useTrayAvailable,
  useWindowMaximised,
} from '@/services/desktopWindow'
import '@/styles/desktop-window.css'

/**
 * Minimise (to tray) / maximise / close for the frameless desktop window.
 * Renders nothing in a browser tab.
 */
export default function DesktopWindowControls() {
  const { t } = useTranslation()
  const shell = useDesktopShell()
  const maximised = useWindowMaximised(shell)
  const tray = useTrayAvailable(shell)
  if (!shell) return null

  const minimiseLabel = tray
    ? t('shell.minimiseToTray', 'Minimise to tray')
    : t('shell.minimise', 'Minimise')
  const maximiseLabel = maximised
    ? t('shell.restoreWindow', 'Restore')
    : t('shell.maximiseWindow', 'Maximise')
  const closeLabel = t('shell.closeWindow', 'Close')

  return (
    <div className="shell-window-controls" role="group" aria-label={t('shell.windowControls', 'Window controls')}>
      <button type="button" className="shell-window-btn" aria-label={minimiseLabel} title={minimiseLabel} onClick={minimiseWindow}>
        <Minus size={14} />
      </button>
      <button type="button" className="shell-window-btn" aria-label={maximiseLabel} title={maximiseLabel} onClick={toggleMaximiseWindow}>
        {maximised ? <Copy size={12} /> : <Square size={12} />}
      </button>
      <button type="button" className="shell-window-btn shell-window-btn--close" aria-label={closeLabel} title={closeLabel} onClick={closeWindow}>
        <X size={14} />
      </button>
    </div>
  )
}

/**
 * Slim title bar for the desktop window when no layout header is on screen:
 * the simple chat, the editor, the splash and the login dialog. Without it the
 * frameless window could not be moved or closed there.
 */
export function DesktopFrameBar() {
  const shell = useDesktopShell()
  const covered = useHasWindowTitleBar()
  const visible = shell && !covered

  // Standalone views size themselves to the viewport; the root attribute lets
  // CSS take the bar's height off them.
  useEffect(() => {
    const root = document.documentElement
    if (visible) root.setAttribute('data-desktop-frame', '')
    else root.removeAttribute('data-desktop-frame')
    return () => root.removeAttribute('data-desktop-frame')
  }, [visible])
  if (!visible) return null

  return (
    <div className="desktop-frame-bar" onDoubleClick={onTitleBarDoubleClick}>
      <span className="desktop-frame-brand" aria-hidden="true">
        <BrandMark size={14} />
        <span>Pando</span>
      </span>
      <DesktopWindowControls />
    </div>
  )
}
