/**
 * Copies text to the clipboard.
 *
 * Prefers the async Clipboard API, but falls back to a hidden textarea +
 * `document.execCommand('copy')` when that API is unavailable or rejects.
 * This matters for two real Pando surfaces:
 *  - the Wails desktop webview, where `navigator.clipboard` is sometimes
 *    absent or throws depending on platform/webview version;
 *  - a plain `http://` LAN address (the WebUI "External Access" toggle),
 *    which is a non-secure context, and browsers only expose
 *    `navigator.clipboard` on secure contexts (https or localhost).
 *
 * Returns whether the copy succeeded, so callers can decide whether to show
 * a "copied" confirmation.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Fall through to the legacy fallback below.
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  // Keep it out of view and out of the layout flow, but still selectable —
  // some browsers refuse execCommand('copy') on a display:none element.
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '1px'
  textarea.style.height = '1px'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.outline = 'none'
  textarea.style.boxShadow = 'none'
  textarea.style.background = 'transparent'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)

  const selection = document.getSelection()
  const previousRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null

  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)

  let ok: boolean
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }

  document.body.removeChild(textarea)
  if (selection) {
    selection.removeAllRanges()
    if (previousRange) selection.addRange(previousRange)
  }

  return ok
}
