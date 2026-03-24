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
      const [metrics, templatesData, skillsData] = await Promise.all([
        api.get<EvaluatorMetrics>('/api/v1/evaluator/metrics').catch(() => null),
        api.get<{ templates: PromptTemplate[] }>('/api/v1/evaluator/templates').catch(() => ({ templates: [] })),
        api.get<{ skills: Skill[] }>('/api/v1/evaluator/skills').catch(() => ({ skills: [] })),
      ])
      set({ metrics, templates: templatesData.templates ?? [], skills: skillsData.skills ?? [] })
    } finally {
      set({ loading: false })
    }
  },
}))
