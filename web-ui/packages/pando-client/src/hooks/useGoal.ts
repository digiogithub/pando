import { useCallback, useEffect, useState } from 'react'
import api from '../services/api'
import type { GoalStatus, SSEEvent } from '../types'

interface GoalResponse {
  goal: GoalStatus | null
}

export function useGoal(sessionId: string | null) {
  const [goal, setGoal] = useState<GoalStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)

  const refreshGoal = useCallback(async () => {
    if (!sessionId) {
      setGoal(null)
      return
    }

    setLoading(true)
    setError(null)
    try {
      const data = await api.get<GoalResponse>(`/api/v1/sessions/${sessionId}/goal`)
      setGoal(data.goal ?? null)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load goal status'
      setError(message)
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    void refreshGoal()
  }, [refreshGoal])

  const applyGoalEvent = useCallback((event: SSEEvent) => {
    if (event.type === 'goal_status') {
      setGoal(event.goal ?? null)
    }
  }, [])

  const cancelGoal = useCallback(async () => {
    if (!sessionId) return null

    setCancelling(true)
    setError(null)
    try {
      const data = await api.post<GoalResponse>(`/api/v1/sessions/${sessionId}/goal/cancel`, {})
      setGoal(data.goal ?? null)
      return data.goal ?? null
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to cancel goal'
      setError(message)
      throw err
    } finally {
      setCancelling(false)
    }
  }, [sessionId])

  return {
    goal,
    loading,
    error,
    cancelling,
    refreshGoal,
    applyGoalEvent,
    cancelGoal,
  }
}
