import { beforeEach, describe, expect, it } from 'vitest'
import { storageKey } from './storage'

describe('storageKey', () => {
  beforeEach(() => {
    delete window.__PANDO_API_BASE__
  })

  it('keeps the main-instance key unchanged when no API base is configured', () => {
    expect(storageKey('pando_token')).toBe('pando_token')
  })

  it('keeps the main-instance key unchanged for an origin-only absolute base', () => {
    window.__PANDO_API_BASE__ = 'https://example.test/'
    expect(storageKey('pando_token')).toBe('pando_token')
  })

  it('suffixes the key when the API base has a path', () => {
    window.__PANDO_API_BASE__ = '/api/v1/projects/project-1/web'
    expect(storageKey('pando_token')).toMatch(/^pando_token@[0-9a-f]{8}$/)
  })

  it('normalizes equivalent API base paths to the same key', () => {
    window.__PANDO_API_BASE__ = '/api/v1/projects/project-1/web/'
    const relativeKey = storageKey('pando_token')

    window.__PANDO_API_BASE__ = 'https://example.test/api/v1/projects/project-1/web'
    expect(storageKey('pando_token')).toBe(relativeKey)
  })

  it('uses different suffixes for different base paths', () => {
    window.__PANDO_API_BASE__ = '/api/v1/projects/project-1/web'
    const firstKey = storageKey('pando_token')

    window.__PANDO_API_BASE__ = '/api/v1/projects/project-2/web'
    expect(storageKey('pando_token')).not.toBe(firstKey)
  })
})
