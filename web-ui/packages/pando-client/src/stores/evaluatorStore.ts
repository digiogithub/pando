import { create } from 'zustand'
import type { EvaluatorMetrics, TemplateSection, Skill } from '../types'
import api from '../services/api'

interface EvaluatorStore {
  metrics: EvaluatorMetrics | null
  sections: TemplateSection[]
  skills: Skill[]
  loading: boolean
  fetchAll: () => Promise<void>
}

export const useEvaluatorStore = create<EvaluatorStore>((set) => ({
  metrics: null,
  sections: [],
  skills: [],
  loading: false,

  fetchAll: async () => {
    set({ loading: true })
    try {
      const [metrics, templatesData, skillsData] = await Promise.all([
        api.get<EvaluatorMetrics>('/api/v1/evaluator/metrics').catch(() => null),
        api.get<{ sections: TemplateSection[] }>('/api/v1/evaluator/templates').catch(() => ({ sections: [] })),
        api.get<{ skills: Skill[] }>('/api/v1/evaluator/skills').catch(() => ({ skills: [] })),
      ])
      set({ metrics, sections: templatesData.sections ?? [], skills: skillsData.skills ?? [] })
    } finally {
      set({ loading: false })
    }
  },
}))
