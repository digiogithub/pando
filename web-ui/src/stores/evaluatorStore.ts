import { create } from 'zustand'
import type { EvaluatorMetrics, PromptTemplate, Skill } from '@/types'
import api from '@/services/api'

interface EvaluatorStore {
  metrics: EvaluatorMetrics | null
  templates: PromptTemplate[]
  skills: Skill[]
  loading: boolean
  fetchAll: () => Promise<void>
}

export const useEvaluatorStore = create<EvaluatorStore>((set) => ({
  metrics: null,
  templates: [],
  skills: [],
  loading: false,

  fetchAll: async () => {
    set({ loading: true })
    try {
      const [metrics, templates, skills] = await Promise.all([
        api.get<EvaluatorMetrics>('/api/v1/evaluator/metrics').catch(() => null),
        api.get<PromptTemplate[]>('/api/v1/evaluator/templates').catch(() => []),
        api.get<Skill[]>('/api/v1/evaluator/skills').catch(() => []),
      ])
      set({ metrics, templates: templates ?? [], skills: skills ?? [] })
    } finally {
      set({ loading: false })
    }
  },
}))
