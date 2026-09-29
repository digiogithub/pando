import type { TemplateSection } from '@pando/client/types'
import EmptyState from '@/components/shared/EmptyState'

interface UCBRankingTableProps {
  sections: TemplateSection[]
}

export default function UCBRankingTable({ sections }: UCBRankingTableProps) {
  return (
    <div className="flex min-w-0 flex-[2] flex-col overflow-hidden rounded-md border border-border">
      <div className="border-b border-border bg-shell px-4 py-2.5 text-sm font-semibold text-fg">
        Prompt Variants — UCB Ranking
      </div>

      {sections.length === 0 ? (
        <EmptyState
          title="No prompt variants yet"
          description="Add files under .pando/prompts/variants/<section>/<variant>.md.tpl to A/B test a prompt section."
        />
      ) : (
        <div className="view-table-wrap flex-1">
          <table className="view-table">
            <thead>
              <tr>
                <th>Section</th>
                <th>Variant</th>
                <th>UCB Score</th>
                <th>Avg Reward</th>
                <th>Uses</th>
                <th>File</th>
              </tr>
            </thead>
            <tbody>
              {sections.flatMap((sec) =>
                [...sec.variants]
                  .sort((a, b) => b.ucb_score - a.ucb_score)
                  .map((v) => (
                    <tr key={v.id}>
                      <td className="is-mono">{sec.section}</td>
                      <td className="is-mono">
                        {v.name}
                        {v.missing ? ' (file removed)' : ''}
                      </td>
                      <td className="font-semibold text-fg">{v.times_used > 0 ? v.ucb_score.toFixed(2) : '-'}</td>
                      <td>{v.times_used > 0 ? `${(v.avg_reward * 100).toFixed(0)}%` : '-'}</td>
                      <td className="is-muted">{v.times_used}</td>
                      <td className="is-mono is-muted">{v.is_default ? 'embedded' : v.path}</td>
                    </tr>
                  )),
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
