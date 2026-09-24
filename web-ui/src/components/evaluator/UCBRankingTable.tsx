import type { PromptTemplate } from '@pando/client/types'
import EmptyState from '@/components/shared/EmptyState'

interface UCBRankingTableProps {
  templates: PromptTemplate[]
}

export default function UCBRankingTable({ templates }: UCBRankingTableProps) {
  const sorted = [...templates].sort((a, b) => b.ucb_score - a.ucb_score)

  return (
    <div className="flex min-w-0 flex-[2] flex-col overflow-hidden rounded-md border border-border">
      <div className="border-b border-border bg-shell px-4 py-2.5 text-sm font-semibold text-fg">
        Prompt Templates — UCB Ranking
      </div>

      {sorted.length === 0 ? (
        <EmptyState title="No prompt templates yet" description="Templates will appear here once self-improvement has evaluated sessions." />
      ) : (
        <div className="view-table-wrap flex-1">
          <table className="view-table">
            <thead>
              <tr>
                <th className="text-center" style={{ width: 40 }}>#</th>
                <th>Template Name</th>
                <th>UCB Score</th>
                <th>Win Rate</th>
                <th>Uses</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((tpl, idx) => (
                <tr key={tpl.id}>
                  <td className="text-center is-muted" style={{ width: 40 }}>{idx + 1}</td>
                  <td className="is-mono">{tpl.name}</td>
                  <td className="font-semibold text-fg">{tpl.ucb_score.toFixed(2)}</td>
                  <td>{(tpl.win_rate * 100).toFixed(0)}%</td>
                  <td className="is-muted">{tpl.uses}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
