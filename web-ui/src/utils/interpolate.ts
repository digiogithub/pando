/**
 * Replaces `{{name}}` placeholders. i18next already does this when it is
 * initialised; this covers the not-ready case (the default value is returned
 * verbatim), so component text with variables reads the same in unit tests.
 */
export function interpolate(text: string, vars: Record<string, string | number>): string {
  return text.replace(/\{\{\s*(\w+)\s*\}\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m))
}
