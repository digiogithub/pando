import { useEffect } from 'react'
import { useEvaluatorStore } from '@/stores/evaluatorStore'
import LoadingSpinner from '@/components/shared/LoadingSpinner'
import MetricsCards from './MetricsCards'
import UCBRankingTable from './UCBRankingTable'
import SkillsList from './SkillsList'

export default function SelfImprovementView() {
  const { metrics, templates, skills, loading, fetchAll } = useEvaluatorStore()

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--bg)',
        overflow: 'hidden',
      }}
    >
      {/* Page title */}
      <div
        style={{
          padding: '1rem 1.5rem 0',
          flexShrink: 0,
        }}
      >
        <h2 style={{ fontSize: 16, fontWeight: 700, color: 'var(--fg)' }}>Self-Improvement</h2>
      </div>

      {loading && !metrics && templates.length === 0 ? (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flex: 1,
          }}
        >
          <LoadingSpinner size={28} />
        </div>
      ) : (
        <>
          {/* Metrics row */}
          <MetricsCards metrics={metrics} />

          {/* Divider */}
          <div style={{ height: 1, background: 'var(--border)', flexShrink: 0, marginInline: '1.5rem' }} />

          {/* UCB table + skills list */}
          <div
            style={{
              display: 'flex',
              gap: '1rem',
              padding: '1rem 1.5rem 1.5rem',
              flex: 1,
              minHeight: 0,
              overflow: 'hidden',
            }}
          >
            <UCBRankingTable templates={templates} />
            <SkillsList skills={skills} />
          </div>
        </>
      )}
    </div>
  )
}
