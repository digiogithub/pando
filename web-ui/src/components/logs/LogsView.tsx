import { useEffect } from 'react'
import { useLogsStore } from '@pando/client/stores/logsStore'
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
    <div className="view">
      <LogFilters />

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <LogTable />
      </div>

      {selectedEntry && <LogDetail />}
    </div>
  )
}
