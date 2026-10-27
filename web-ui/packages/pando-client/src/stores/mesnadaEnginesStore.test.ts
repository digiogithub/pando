import { beforeEach, describe, expect, it, vi } from 'vitest'
import api from '../services/api'
import type { MesnadaEngineStatus } from '../types'
import {
  isNativeEngine,
  normalizeEngineName,
  setEngineDisabled,
  useMesnadaEnginesStore,
  withDisabled,
} from './mesnadaEnginesStore'

function engine(name: string, overrides: Partial<MesnadaEngineStatus> = {}): MesnadaEngineStatus {
  return {
    name,
    label: name,
    kind: 'cli',
    binary: name,
    installed: true,
    path: `/usr/bin/${name}`,
    enabled: true,
    locked: false,
    ...overrides,
  }
}

describe('mesnadaEnginesStore helpers', () => {
  it('normalizes aliases and recognizes native engines', () => {
    expect(normalizeEngineName(' Claude-Code ')).toBe('claude')
    expect(normalizeEngineName('gemini-cli')).toBe('gemini')
    expect(isNativeEngine('pando')).toBe(true)
    expect(isNativeEngine('warm-acp')).toBe(true)
    expect(isNativeEngine('acp-server')).toBe(true)
    expect(isNativeEngine('')).toBe(true)
    expect(isNativeEngine('claude')).toBe(false)
  })

  it('adds and removes engines from the disabled list, sorted and deduplicated', () => {
    let list = setEngineDisabled(undefined, 'copilot', true)
    list = setEngineDisabled(list, 'claude-code', true)
    list = setEngineDisabled(list, 'claude', true)
    expect(list).toEqual(['claude', 'copilot'])
    expect(setEngineDisabled(list, 'claude', false)).toEqual(['copilot'])
  })

  it('never disables a native engine and strips natives already in the list', () => {
    expect(setEngineDisabled(['pando', 'gemini'], 'pando', true)).toEqual(['gemini'])
    expect(setEngineDisabled([], 'warm-acp', true)).toEqual([])
  })

  it('overlays pending disabled edits while keeping locked engines on', () => {
    const engines = [engine('pando', { kind: 'native', locked: true }), engine('claude'), engine('copilot', { enabled: false })]
    const view = withDisabled(engines, ['claude', 'pando'])
    expect(view.map((e) => [e.name, e.enabled])).toEqual([
      ['pando', true],
      ['claude', false],
      ['copilot', true],
    ])
  })
})

describe('useMesnadaEnginesStore', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useMesnadaEnginesStore.setState({ engines: [], loading: false, error: null })
  })

  it('loads detected engines from the API', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ engines: [engine('claude')] })
    await useMesnadaEnginesStore.getState().fetchEngines()
    expect(get).toHaveBeenCalledWith('/api/v1/mesnada/engines')
    expect(useMesnadaEnginesStore.getState().engines.map((e) => e.name)).toEqual(['claude'])
    expect(useMesnadaEnginesStore.getState().loading).toBe(false)
  })

  it('records an error and clears the list when the request fails', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('boom'))
    await useMesnadaEnginesStore.getState().fetchEngines()
    expect(useMesnadaEnginesStore.getState().engines).toEqual([])
    expect(useMesnadaEnginesStore.getState().error).toBe('boom')
  })
})
