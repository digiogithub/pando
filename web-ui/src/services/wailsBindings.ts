/**
 * Wails binding wrappers with graceful fallbacks for web mode.
 * In Wails desktop mode these call the Go backend directly.
 * In web mode they use browser equivalents or no-ops.
 */
import { isDesktop } from './desktop'

async function loadBindings() {
  if (!isDesktop) return null
  try {
    return await import('../../wailsjs/go/desktop/App')
  } catch {
    return null
  }
}

export async function getVersion(): Promise<string> {
  const b = await loadBindings()
  if (b) return b.GetVersion()
  return ''
}

export async function selectDirectory(): Promise<string> {
  const b = await loadBindings()
  if (b) return b.SelectDirectory()
  return ''
}

export async function openFileDialog(title: string): Promise<string> {
  const b = await loadBindings()
  if (b) return b.OpenFileDialog(title)
  return ''
}

export async function saveFileDialog(title: string, defaultFilename: string): Promise<string> {
  const b = await loadBindings()
  if (b) return b.SaveFileDialog(title, defaultFilename)
  return ''
}

export async function openInBrowser(url: string): Promise<void> {
  const b = await loadBindings()
  if (b) { b.OpenInBrowser(url); return }
  window.open(url, '_blank', 'noopener,noreferrer')
}

export async function windowMinimise(): Promise<void> {
  const b = await loadBindings()
  if (b) b.WindowMinimise()
}

export async function windowMaximise(): Promise<void> {
  const b = await loadBindings()
  if (b) b.WindowMaximise()
}

export async function windowToggleMaximise(): Promise<void> {
  const b = await loadBindings()
  if (b) b.WindowToggleMaximise()
}

export async function setWindowTitle(title: string): Promise<void> {
  const b = await loadBindings()
  if (b) b.WindowSetTitle(title)
  else document.title = title
}
