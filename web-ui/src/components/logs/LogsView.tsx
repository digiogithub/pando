import { useEffect } from 'react'
import { useLogsStore } from '@/stores/logsStore'
import LogFilters from './LogFilters'
import LogTable from './LogTable'
import LogDetail from './LogDetail'

const POLL_INTERVAL = 5000

export default function LogsView() {
  const { fetchLogs, selectedEntry } = useLogsStore()

  useEffect(() => {
    fetchLogs()

    // Poll every 5 seconds as fallback (no SSE for logs yet)
    const timer = setInterval(fetchLogs, POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [fetchLogs])

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
        background: 'var(--bg)',
      }}
    >
      <LogFilters />

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', minHeight: 0 }}>
        <LogTable />
      </div>

      {selectedEntry && <LogDetail />}
    </div>
  )
}
