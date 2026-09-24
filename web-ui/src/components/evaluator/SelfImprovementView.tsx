import { useEffect } from 'react'
import { useEvaluatorStore } from '@pando/client/stores/evaluatorStore'
import { Spinner } from '@/components/ui'
import MetricsCards from './MetricsCards'
import UCBRankingTable from './UCBRankingTable'
import SkillsList from './SkillsList'

export default function SelfImprovementView() {
  const { metrics, templates, skills, loading, fetchAll } = useEvaluatorStore()

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  return (
    <div className="view">
      {/* Page title */}
      <div className="flex-shrink-0 px-6 pt-5">
        <h2 className="view-title">Self-Improvement</h2>
      </div>

      {loading && !metrics && templates.length === 0 ? (
        <div className="flex flex-1 items-center justify-center">
          <Spinner size={26} />
        </div>
      ) : (
        <>
          {/* Metrics row */}
          <MetricsCards metrics={metrics} />

          {/* Divider */}
          <div className="mx-6 h-px flex-shrink-0 bg-border" />

          {/* UCB table + skills list */}
          <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-6 pt-4 md:flex-row md:overflow-hidden md:px-6">
            <UCBRankingTable templates={templates} />
            <SkillsList skills={skills} />
          </div>
        </>
      )}
    </div>
  )
}
