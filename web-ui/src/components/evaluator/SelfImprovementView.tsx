import { useEffect, useState } from 'react'
import { useEvaluatorStore } from '@pando/client/stores/evaluatorStore'
import { Spinner, Tabs } from '@/components/ui'
import MetricsCards from './MetricsCards'
import UCBRankingTable from './UCBRankingTable'
import SkillsList from './SkillsList'
import SessionsList from './SessionsList'
import DoctorBanner from './DoctorBanner'
import DailyChart from './DailyChart'

export default function SelfImprovementView() {
  const { metrics, sections, skills, sessions, doctor, loading, fetchAll, reviewSkill } = useEvaluatorStore()
  const [tab, setTab] = useState<'variants' | 'sessions'>('variants')

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  return (
    <div className="view">
      {/* Page title */}
      <div className="flex-shrink-0 px-6 pt-5">
        <h2 className="view-title">Self-Improvement</h2>
      </div>

      {loading && !metrics && sections.length === 0 ? (
        <div className="flex flex-1 items-center justify-center">
          <Spinner size={26} />
        </div>
      ) : (
        <>
          <DoctorBanner doctor={doctor} />

          {/* Metrics row */}
          <MetricsCards metrics={metrics} />
          {metrics?.daily && metrics.daily.length > 0 && <DailyChart daily={metrics.daily} />}

          <div className="mx-6 mt-3 flex-shrink-0">
            <Tabs
              aria-label="Self-improvement sections"
              value={tab}
              onChange={setTab}
              items={[
                { value: 'variants', label: 'Variants and skills' },
                { value: 'sessions', label: `Sessions (${sessions.length})` },
              ]}
            />
          </div>

          {/* Divider */}
          <div className="mx-6 h-px flex-shrink-0 bg-border" />

          {/* UCB table + skills list */}
          {tab === 'variants' ? (
            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-6 pt-4 md:flex-row md:overflow-hidden md:px-6">
              <UCBRankingTable sections={sections} />
              <SkillsList skills={skills} onReview={reviewSkill} />
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 pb-6 pt-4 md:px-6">
              <SessionsList sessions={sessions} />
            </div>
          )}
        </>
      )}
    </div>
  )
}
