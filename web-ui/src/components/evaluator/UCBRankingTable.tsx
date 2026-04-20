import type { PromptTemplate } from '@/types'
import EmptyState from '@/components/shared/EmptyState'

interface UCBRankingTableProps {
  templates: PromptTemplate[]
}

const cellStyle: React.CSSProperties = {
  padding: '0.625rem 0.75rem',
  fontSize: 13,
  color: 'var(--fg)',
  borderBottom: '1px solid var(--border)',
  verticalAlign: 'middle',
}

export default function UCBRankingTable({ templates }: UCBRankingTableProps) {
  const sorted = [...templates].sort((a, b) => b.ucb_score - a.ucb_score)

  return (
    <div
      style={{
        flex: 2,
        display: 'flex',
        flexDirection: 'column',
        minWidth: 0,
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md)',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '0.75rem 1rem',
          fontWeight: 600,
          fontSize: 13,
          color: 'var(--fg)',
          borderBottom: '1px solid var(--border)',
          background: 'var(--surface)',
        }}
      >
        Prompt Templates — UCB Ranking
      </div>

      {sorted.length === 0 ? (
        <EmptyState title="No prompt templates yet" description="Templates will appear here once self-improvement has evaluated sessions." />
      ) : (
        <div style={{ overflowX: 'auto', flex: 1 }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['#', 'Template Name', 'UCB Score', 'Win Rate', 'Uses'].map((h, i) => (
                  <th
                    key={h}
                    style={{
                      padding: '0.5rem 0.75rem',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--fg-muted)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                      textAlign: i === 0 ? 'center' : 'left',
                      borderBottom: '1px solid var(--border)',
                      background: 'var(--surface)',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sorted.map((tpl, idx) => (
                <tr key={tpl.id}>
                  <td
                    style={{
                      ...cellStyle,
                      textAlign: 'center',
                      color: 'var(--fg-muted)',
                      width: 40,
                    }}
                  >
                    {idx + 1}
                  </td>
                  <td style={{ ...cellStyle, fontFamily: 'monospace', fontSize: 12 }}>
                    {tpl.name}
                  </td>
                  <td style={{ ...cellStyle, fontWeight: 700, color: 'var(--primary)' }}>
                    {tpl.ucb_score.toFixed(2)}
                  </td>
                  <td style={cellStyle}>
                    {(tpl.win_rate * 100).toFixed(0)}%
                  </td>
                  <td style={{ ...cellStyle, color: 'var(--fg-muted)' }}>{tpl.uses}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
