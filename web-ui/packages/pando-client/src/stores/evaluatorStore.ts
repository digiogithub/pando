import { create } from 'zustand'
import type { EvaluatorDoctor, EvaluatorMetrics, EvaluatorSessionScore, TemplateSection, Skill } from '../types'
import api from '../services/api'

interface EvaluatorStore {
  metrics: EvaluatorMetrics | null
  sections: TemplateSection[]
  skills: Skill[]
  /** most recent evaluated sessions with their score explanation */
  sessions: EvaluatorSessionScore[]
  doctor: EvaluatorDoctor | null
  loading: boolean
  fetchAll: () => Promise<void>
  /** Approves or rejects a learned skill; approval reaches the next new session. */
  reviewSkill: (id: string, decision: 'approve' | 'reject') => Promise<void>
}

export const useEvaluatorStore = create<EvaluatorStore>((set, get) => ({
  metrics: null,
  sections: [],
  skills: [],
  sessions: [],
  doctor: null,
  loading: false,

  fetchAll: async () => {
    set({ loading: true })
    try {
      const [metrics, templatesData, skillsData, sessionsData, doctor] = await Promise.all([
        api.get<EvaluatorMetrics>('/api/v1/evaluator/metrics').catch(() => null),
        api.get<{ sections: TemplateSection[] }>('/api/v1/evaluator/templates').catch(() => ({ sections: [] })),
        api.get<{ skills: Skill[] }>('/api/v1/evaluator/skills').catch(() => ({ skills: [] })),
        api.get<{ sessions: EvaluatorSessionScore[] }>('/api/v1/evaluator/sessions').catch(() => ({ sessions: [] })),
        api.get<EvaluatorDoctor>('/api/v1/evaluator/doctor').catch(() => null),
      ])
      set({
        metrics,
        sections: templatesData.sections ?? [],
        skills: skillsData.skills ?? [],
        sessions: sessionsData.sessions ?? [],
        doctor,
      })
    } finally {
      set({ loading: false })
    }
  },

  reviewSkill: async (id, decision) => {
    await api.post(`/api/v1/evaluator/skills/${encodeURIComponent(id)}/${decision}`, {})
    await get().fetchAll()
  },
}))
