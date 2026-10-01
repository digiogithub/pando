import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

const ROOT = path.resolve(__dirname, '..')
const SCAN_DIRS = ['src', 'packages/pando-client/src'].map((dir) => path.join(ROOT, dir))

function walkFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const fullPath = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      return walkFiles(fullPath)
    }
    if (!/\.(ts|tsx)$/.test(entry.name)) {
      return []
    }
    if (entry.name.endsWith('.d.ts') || /\.test\.(ts|tsx)$/.test(entry.name) || /\.spec\.(ts|tsx)$/.test(entry.name)) {
      return []
    }
    return [fullPath]
  })
}

function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^\\:])\/\/.*$/gm, '$1')
}

function normalized(relativePath: string): string {
  return relativePath.split(path.sep).join('/')
}

function collectMatches(pattern: RegExp, allowList: Set<string>): string[] {
  const matches: string[] = []

  for (const dir of SCAN_DIRS) {
    for (const file of walkFiles(dir)) {
      const relativePath = normalized(path.relative(ROOT, file))
      if (allowList.has(relativePath)) continue
      const source = stripComments(fs.readFileSync(file, 'utf8'))
      if (pattern.test(source)) {
        matches.push(relativePath)
      }
      pattern.lastIndex = 0
    }
  }

  return matches
}

describe('runtime source guards', () => {
  it('keeps __PANDO_API_BASE__ reads inside the API/storage services', () => {
    const matches = collectMatches(
      /\b__PANDO_API_BASE__\b/,
      new Set([
        'packages/pando-client/src/services/api.ts',
        'packages/pando-client/src/services/storage.ts',
      ]),
    )

    expect(matches).toEqual([])
  })

  it('keeps browser storage access inside the storage service', () => {
    const matches = collectMatches(
      /\b(?:localStorage|sessionStorage)\b/,
      new Set(['packages/pando-client/src/services/storage.ts']),
    )

    expect(matches).toEqual([])
  })
})
