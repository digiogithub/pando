#!/usr/bin/env node
/**
 * WCAG contrast validator for the theme tokens in src/styles/tokens.css.
 *
 * tokens.css is the single runtime source of truth, so this script parses it,
 * resolves the cascade for every family x mode x accent combination with a
 * tiny selector matcher (":root", [attr="v"], :not([attr="v"]), comma lists)
 * and checks:
 *   fg, fg-muted          >= 4.5:1  on bg, bg-shell, bg-raised
 *   fg-faint              >= 3:1    on bg, bg-shell, bg-raised
 *   accent-fg on accent   >= 4.5:1
 *   accent on bg          >= 3:1    (non-text UI: focus, selection, icons)
 *
 * Exit code 1 on any failure. Usage: bun run check:contrast
 */
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(resolve(here, '../src/styles/tokens.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')

const FAMILIES = ['pando', 'paper', 'slate', 'forest']
const MODES = ['light', 'dark']
const ACCENTS = [null, 'gold', 'terracotta', 'violet', 'blue', 'green', 'rose', 'graphite']

// ---- Parse rule blocks -----------------------------------------------------
const rules = []
const blockRe = /([^{}]+)\{([^{}]*)\}/g
let m
let order = 0
while ((m = blockRe.exec(css))) {
  const vars = {}
  for (const decl of m[2].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const name = decl.slice(0, i).trim()
    if (name.startsWith('--')) vars[name] = decl.slice(i + 1).trim()
  }
  for (const sel of m[1].split(',')) {
    rules.push({ selector: sel.trim(), vars, order: order++ })
  }
}

// ---- Minimal selector matching + specificity -------------------------------
function parseSelector(sel) {
  const parts = []
  const re = /:root|:not\(\[([\w-]+)="([^"]*)"\]\)|\[([\w-]+)="([^"]*)"\]/g
  let rest = sel
  let t
  while ((t = re.exec(sel))) {
    rest = rest.replace(t[0], '')
    if (t[0] === ':root') parts.push({ kind: 'root' })
    else if (t[1]) parts.push({ kind: 'not', attr: t[1], value: t[2] })
    else parts.push({ kind: 'attr', attr: t[3], value: t[4] })
  }
  if (rest.trim() !== '') return null // unsupported selector: ignore
  return parts
}

function matches(parts, attrs) {
  return parts.every((p) => {
    if (p.kind === 'root') return true
    if (p.kind === 'attr') return attrs[p.attr] === p.value
    return attrs[p.attr] !== p.value
  })
}

function resolveVars(attrs) {
  const applicable = []
  for (const r of rules) {
    const parts = parseSelector(r.selector)
    if (!parts || !matches(parts, attrs)) continue
    applicable.push({ spec: parts.length, order: r.order, vars: r.vars })
  }
  applicable.sort((a, b) => a.spec - b.spec || a.order - b.order)
  return Object.assign({}, ...applicable.map((a) => a.vars))
}

// ---- Colour math -----------------------------------------------------------
function hexToRgb(hex) {
  const h = hex.replace('#', '')
  const full = h.length === 3 ? h.split('').map((c) => c + c).join('') : h.slice(0, 6)
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return null
  return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16) / 255)
}
function luminance(rgb) {
  const [r, g, b] = rgb.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(a, b) {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

// ---- Checks ----------------------------------------------------------------
const CHECKS = [
  ...['--bg', '--bg-shell', '--bg-raised'].flatMap((bg) => [
    { fg: '--fg', bg, min: 4.5 },
    { fg: '--fg-muted', bg, min: 4.5 },
    { fg: '--fg-faint', bg, min: 3 },
  ]),
  { fg: '--accent-fg', bg: '--accent', min: 4.5 },
  { fg: '--accent', bg: '--bg', min: 3 },
]

let failures = 0
let total = 0
for (const family of FAMILIES) {
  for (const mode of MODES) {
    for (const accent of ACCENTS) {
      const attrs = { 'data-theme-name': family, 'data-theme': mode }
      if (accent) attrs['data-accent'] = accent
      const vars = resolveVars(attrs)
      const label = `${family}-${mode}${accent ? ` +${accent}` : ''}`
      for (const c of CHECKS) {
        total++
        const fg = vars[c.fg] && hexToRgb(vars[c.fg])
        const bg = vars[c.bg] && hexToRgb(vars[c.bg])
        if (!fg || !bg) {
          failures++
          console.error(`FAIL ${label}: ${c.fg} (${vars[c.fg]}) / ${c.bg} (${vars[c.bg]}) not a hex colour`)
          continue
        }
        const ratio = contrast(fg, bg)
        if (ratio < c.min) {
          failures++
          console.error(
            `FAIL ${label}: ${c.fg} ${vars[c.fg]} on ${c.bg} ${vars[c.bg]} = ${ratio.toFixed(2)} (< ${c.min})`,
          )
        }
      }
    }
  }
}

// ---- themes.ts / index.html must mirror tokens.css -------------------------
// Parsed as text so the script runs under plain Node as well as Bun.
const themesTs = readFileSync(resolve(here, '../src/styles/themes.ts'), 'utf8')
const indexHtml = readFileSync(resolve(here, '../index.html'), 'utf8')
for (const family of FAMILIES) {
  const block = themesTs.match(new RegExp(`\\b${family}: \\{[\\s\\S]*?dark: \\{[^}]*\\}`))
  for (const mode of MODES) {
    const vars = resolveVars({ 'data-theme-name': family, 'data-theme': mode })
    const line = block && block[0].match(new RegExp(`${mode}: \\{([^}]*)\\}`))
    for (const [key, token] of [['bg', '--bg'], ['shell', '--bg-shell'], ['raised', '--bg-raised'], ['fg', '--fg'], ['accent', '--accent']]) {
      total++
      const v = line && line[1].match(new RegExp(`\\b${key}: '(#[0-9a-fA-F]{6})'`))
      if (!v || v[1].toLowerCase() !== String(vars[token]).toLowerCase()) {
        failures++
        console.error(`FAIL themes.ts ${family}.${mode}.${key} = ${v ? v[1] : '?'} but tokens.css ${token} = ${vars[token]}`)
      }
    }
  }
  total++
  const html = indexHtml.match(new RegExp(`${family}: \\['(#[0-9a-fA-F]{6})', '(#[0-9a-fA-F]{6})'\\]`))
  const light = resolveVars({ 'data-theme-name': family, 'data-theme': 'light' })['--bg']
  const dark = resolveVars({ 'data-theme-name': family, 'data-theme': 'dark' })['--bg']
  if (!html || html[1].toLowerCase() !== light.toLowerCase() || html[2].toLowerCase() !== dark.toLowerCase()) {
    failures++
    console.error(`FAIL index.html bgs.${family} does not match tokens.css --bg (${light}, ${dark})`)
  }
}
for (const accent of ACCENTS.filter(Boolean)) {
  for (const mode of MODES) {
    total++
    const vars = resolveVars({ 'data-theme-name': 'pando', 'data-theme': mode, 'data-accent': accent })
    const m2 = themesTs.match(new RegExp(`\\b${accent}: \\{[^}]*${mode}: '(#[0-9a-fA-F]{6})'`))
    if (!m2 || m2[1].toLowerCase() !== vars['--accent'].toLowerCase()) {
      failures++
      console.error(`FAIL themes.ts accent ${accent}.${mode} = ${m2 ? m2[1] : '?'} but tokens.css = ${vars['--accent']}`)
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures}/${total} contrast checks failed`)
  process.exit(1)
}
console.log(`OK: ${total} contrast + palette-sync checks passed (${FAMILIES.length} families x ${MODES.length} modes x ${ACCENTS.length} accent variants)`)
