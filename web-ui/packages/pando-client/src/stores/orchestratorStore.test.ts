import { beforeEach, describe, expect, it, vi } from 'vitest'
import api from '../services/api'
import { buildTaskPrompt, useOrchestratorStore } from './orchestratorStore'

describe('orchestratorStore.createTask', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    useOrchestratorStore.setState({ tasks: [] })
  })

  it('builds the prompt from the name and the details', () => {
    expect(buildTaskPrompt({ name: ' Refactor auth ' })).toBe('Refactor auth')
    expect(buildTaskPrompt({ name: 'Refactor auth', description: ' Split the module ' })).toBe('Refactor auth\n\nSplit the module')
    expect(buildTaskPrompt({ name: '', description: 'Only details' })).toBe('Only details')
  })

  it('posts the required prompt (not name/description) and refreshes the list', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 't1' } as never)
    const get = vi.spyOn(api, 'get').mockResolvedValue({ tasks: [{ id: 't1' }], total: 1 } as never)
    await useOrchestratorStore.getState().createTask({ name: 'Refactor auth', description: 'Split it', model: 'gpt-5' })
    expect(post).toHaveBeenCalledWith('/api/v1/orchestrator/tasks', {
      prompt: 'Refactor auth\n\nSplit it',
      background: true,
      model: 'gpt-5',
    })
    expect(get).toHaveBeenCalledWith('/api/v1/orchestrator/tasks')
    expect(useOrchestratorStore.getState().tasks).toHaveLength(1)
  })

  it('omits an empty model and rejects an empty task', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 't2' } as never)
    vi.spyOn(api, 'get').mockResolvedValue({ tasks: [], total: 0 } as never)
    await useOrchestratorStore.getState().createTask({ name: 'x', model: '' })
    expect(post).toHaveBeenCalledWith('/api/v1/orchestrator/tasks', { prompt: 'x', background: true })
    await expect(useOrchestratorStore.getState().createTask({ name: ' ' })).rejects.toThrow()
  })
})
