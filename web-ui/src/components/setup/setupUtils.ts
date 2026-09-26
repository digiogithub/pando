/** Turns a thrown API error (raw `{"error": "..."}` text) into a readable message. */
export function apiErrorMessage(e: unknown, fallback: string): string {
  if (!(e instanceof Error)) return fallback
  try {
    const parsed = JSON.parse(e.message) as { error?: string; message?: string }
    return parsed.error ?? parsed.message ?? e.message
  } catch {
    return e.message || fallback
  }
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

export function shortenPath(path: string): string {
  return path.replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~')
}
