import { Search, ArrowDown, RefreshCw } from '@/components/ui/icons'
import { IconButton, Input, SegmentedControl, type TabItem } from '@/components/ui'
import { useLogsStore } from '@pando/client/stores/logsStore'
import type { LogLevel } from '@pando/client/stores/logsStore'

const LEVELS: TabItem<LogLevel>[] = [
  { value: 'all', label: 'All' },
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
]

export default function LogFilters() {
  const { levelFilter, searchQuery, autoScroll, setLevelFilter, setSearchQuery, setAutoScroll, fetchLogs } =
    useLogsStore()

  return (
    <div className="flex flex-shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-2.5">
      {/* Level filter */}
      <SegmentedControl items={LEVELS} value={levelFilter} onChange={setLevelFilter} size="sm" aria-label="Filter by level" />

      {/* Search input */}
      <div className="filter-bar-search min-w-40 flex-1">
        <Search size={13} />
        <Input
          type="text"
          placeholder="Search logs…"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
      </div>

      {/* Auto-scroll toggle */}
      <IconButton
        aria-label="Auto-scroll"
        tooltip={autoScroll ? 'Auto-scroll on' : 'Auto-scroll off'}
        icon={<ArrowDown size={14} />}
        active={autoScroll}
        onClick={() => setAutoScroll(!autoScroll)}
      />

      {/* Refresh button */}
      <IconButton aria-label="Refresh logs" tooltip icon={<RefreshCw size={14} />} onClick={fetchLogs} />
    </div>
  )
}
