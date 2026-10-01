import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

const PROJECTS_DIR = path.resolve(process.cwd(), 'src/components/projects')
const ALLOWED_LITERALS = new Set<string>([
  'Promise',
  '.pando.toml',
  '.pando/data/',
  '.pando/mesnada/',
  'agents/skills/',
])
const JSX_TEXT_PATTERN = />([^<>{}\n]*[A-Za-z][^<>{}\n]*)</g
const ATTR_PATTERN = /\b(?:title|aria-label|placeholder)=["']([^"']*[A-Za-z][^"']*)["']/g

function lineNumberFor(source: string, index: number): number {
  return source.slice(0, index).split('\n').length
}

describe('projects i18n guard', () => {
  it('does not contain hardcoded user-facing English in project components', () => {
    const files = fs.readdirSync(PROJECTS_DIR).filter((file) => file.endsWith('.tsx'))
    const offenders: string[] = []

    for (const file of files) {
      const fullPath = path.join(PROJECTS_DIR, file)
      const source = fs.readFileSync(fullPath, 'utf8')
      const sourceWithoutComments = source
        .replace(/\/\*[\s\S]*?\*\//g, '')
        .replace(/\/\/.*$/gm, '')

      for (const match of sourceWithoutComments.matchAll(JSX_TEXT_PATTERN)) {
        const literal = (match[1] ?? '').trim().replace(/\s+/g, ' ')
        if (!literal || ALLOWED_LITERALS.has(literal)) continue
        offenders.push(`${file}:${lineNumberFor(source, match.index ?? 0)} JSX "${literal}"`)
      }

      for (const match of sourceWithoutComments.matchAll(ATTR_PATTERN)) {
        const literal = (match[1] ?? '').trim().replace(/\s+/g, ' ')
        if (!literal || ALLOWED_LITERALS.has(literal)) continue
        offenders.push(`${file}:${lineNumberFor(source, match.index ?? 0)} attr "${literal}"`)
      }
    }

    expect(offenders).toEqual([])
  })
})
