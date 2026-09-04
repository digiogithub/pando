import api from './api'

/**
 * The settings policy declared by the compiled-in extensions.
 *
 * A standalone Pando returns empty lists and no banner, which is the shape the
 * shell already renders. When an extension declares a policy, whole sections
 * stop being this host's business and others are shown without being editable.
 *
 * Sections are dotted configuration paths ("providerAccounts",
 * "internalTools.braveApiKey"), matched the way the backend matches them: a
 * path covers itself and everything under it, segment by segment and
 * case-insensitively.
 *
 * Hiding is presentation only. The backend refuses writes to the same paths, so
 * a client that ignores this policy still cannot change them.
 */
export interface UIPolicyBanner {
  text: string
  link: string
}

export interface UIPolicy {
  hiddenSections: string[]
  readOnlySections: string[]
  readOnlyLabel: string
  banner: UIPolicyBanner
}

/** The policy an unextended host has: no restriction at all. */
export const EMPTY_UI_POLICY: UIPolicy = {
  hiddenSections: [],
  readOnlySections: [],
  readOnlyLabel: '',
  banner: { text: '', link: '' },
}

/**
 * Fetches the policy. Errors are the caller's to handle: a failure here must
 * never stop the settings from rendering, and the safe fallback is the empty
 * policy, since the backend enforces the restriction regardless.
 */
export async function fetchUIPolicy(): Promise<UIPolicy> {
  const res = await api.get<Partial<UIPolicy>>('/api/v1/config/ui-policy')
  return {
    hiddenSections: res.hiddenSections ?? [],
    readOnlySections: res.readOnlySections ?? [],
    readOnlyLabel: res.readOnlyLabel ?? '',
    banner: { text: res.banner?.text ?? '', link: res.banner?.link ?? '' },
  }
}

/**
 * Reports whether one dotted path covers another, comparing whole segments
 * case-insensitively in both directions. Kept in step with PathsOverlap in
 * internal/config: "mcpServers" covers "mcpServers.github.command", and asking
 * about "mcpServers" when "mcpServers.github" is restricted also matches,
 * because the section contains a restricted path.
 */
export function pathsOverlap(a: string, b: string): boolean {
  const left = a.trim().replace(/^\.+|\.+$/g, '').split('.').filter(Boolean)
  const right = b.trim().replace(/^\.+|\.+$/g, '').split('.').filter(Boolean)
  if (left.length === 0 || right.length === 0) return false
  const shorter = left.length < right.length ? left : right
  const longer = left.length < right.length ? right : left
  return shorter.every((seg, i) => seg.toLowerCase() === longer[i].toLowerCase())
}
