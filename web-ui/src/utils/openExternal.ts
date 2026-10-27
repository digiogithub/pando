/** Opens a URL in a new browser tab. */
export function openExternal(url: string): void {
  window.open(url, '_blank', 'noopener,noreferrer')
}
